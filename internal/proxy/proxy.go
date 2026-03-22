package proxy

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
	"time"

	"tsuskills-gateway/config"
	"tsuskills-gateway/internal/logger"

	"github.com/gorilla/mux"
	"go.uber.org/zap"
)

// SetupRoutes регистрирует reverse-proxy маршруты для каждого сервиса
func SetupRoutes(r *mux.Router, services map[string]config.ServiceTarget, log logger.Logger) {
	for name, svc := range services {
		target, err := url.Parse(svc.URL)
		if err != nil {
			log.Fatal(context.Background(), fmt.Sprintf("Invalid URL for service %s: %v", name, err))
		}

		proxy := newProxy(target, svc, log)

		// регистрируем все методы на prefix и prefix/*
		r.PathPrefix(svc.Prefix).Handler(proxy)

		log.Info(context.Background(), "Registered proxy route",
			zap.String("service", name),
			zap.String("prefix", svc.Prefix),
			zap.String("target", svc.URL),
		)
	}

	// Агрегированный health check
	r.HandleFunc("/health", healthHandler(services, log)).Methods(http.MethodGet)
}

func newProxy(target *url.URL, svc config.ServiceTarget, log logger.Logger) *httputil.ReverseProxy {
	proxy := httputil.NewSingleHostReverseProxy(target)

	originalDirector := proxy.Director
	proxy.Director = func(r *http.Request) {
		originalDirector(r)

		// если strip_prefix — убираем prefix из пути
		if svc.StripPrefix && svc.Prefix != "" {
			r.URL.Path = strings.TrimPrefix(r.URL.Path, svc.Prefix)
			if r.URL.Path == "" {
				r.URL.Path = "/"
			}
		}

		r.Host = target.Host
	}

	proxy.ErrorHandler = func(w http.ResponseWriter, r *http.Request, err error) {
		log.Error(r.Context(), "Proxy error",
			zap.String("target", target.String()),
			zap.String("path", r.URL.Path),
			zap.Error(err),
		)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadGateway)
		fmt.Fprintf(w, `{"error":"Bad Gateway","message":"Service unavailable: %s"}`, target.Host)
	}

	// таймаут на подключение к upstream
	proxy.Transport = &http.Transport{
		ResponseHeaderTimeout: 10 * time.Second,
		IdleConnTimeout:       30 * time.Second,
		MaxIdleConns:          50,
		MaxIdleConnsPerHost:   10,
	}

	return proxy
}

// healthHandler проверяет доступность всех upstream-сервисов
func healthHandler(services map[string]config.ServiceTarget, log logger.Logger) http.HandlerFunc {
	client := &http.Client{Timeout: 3 * time.Second}

	return func(w http.ResponseWriter, r *http.Request) {
		status := make(map[string]string, len(services))
		allOK := true

		// дедуплицируем URL-ы (skills/organizations/resumes/applications → один skills-service)
		checked := make(map[string]bool)

		for name, svc := range services {
			if checked[svc.URL] {
				status[name] = status[findNameByURL(services, svc.URL, name)]
				continue
			}
			checked[svc.URL] = true

			healthURL := svc.URL + "/health"
			resp, err := client.Get(healthURL)
			if err != nil {
				status[name] = "unavailable"
				allOK = false
				continue
			}
			resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				status[name] = "ok"
			} else {
				status[name] = fmt.Sprintf("unhealthy (status %d)", resp.StatusCode)
				allOK = false
			}
		}

		result := map[string]interface{}{
			"gateway":  "ok",
			"services": status,
		}

		w.Header().Set("Content-Type", "application/json")
		if allOK {
			w.WriteHeader(http.StatusOK)
		} else {
			w.WriteHeader(http.StatusServiceUnavailable)
		}
		json.NewEncoder(w).Encode(result)
	}
}

func findNameByURL(services map[string]config.ServiceTarget, targetURL, exclude string) string {
	for name, svc := range services {
		if svc.URL == targetURL && name != exclude {
			return name
		}
	}
	return exclude
}
