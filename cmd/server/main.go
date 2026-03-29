package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"tsuskills-gateway/config"
	"tsuskills-gateway/internal/logger"
	"tsuskills-gateway/internal/middleware"
	"tsuskills-gateway/internal/proxy"

	"github.com/gorilla/mux"
)

func main() {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	cfg, err := config.LoadConfig()
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	appLogger, err := logger.New(&cfg.Logger.Logger)
	if err != nil {
		log.Fatalf("Failed to create logger: %v", err)
	}

	appLogger.Info(ctx, "Starting API Gateway...")

	r := mux.NewRouter()

	// Middleware chain (gorilla/mux: первый Use = самый внешний wrapper):
	// 1. Recovery — ловит паники, внешний
	// 2. CORS — ОБЯЗАТЕЛЬНО до всего остального, чтобы preflight (OPTIONS)
	//    получал CORS-заголовки и 204, не проходя через JWT/Logging
	// 3. RequestID — генерирует ID запроса
	// 4. Logging — логирует запрос
	// 5. JWTAuth — проверяет токен (пропускает OPTIONS и публичные пути)
	r.Use(middleware.Recovery(appLogger))
	r.Use(middleware.CORS(cfg.CORS))
	r.Use(middleware.RequestID)
	r.Use(middleware.Logging(appLogger))
	r.Use(middleware.JWTAuth(cfg.JWT.SecretKey, cfg.PublicPaths, appLogger))

	// Настраиваем reverse proxy для всех сервисов
	proxy.SetupRoutes(r, cfg.Services, appLogger)

	httpServer := &http.Server{
		Addr:         fmt.Sprintf("%s:%d", cfg.Server.Host, cfg.Server.Port),
		Handler:      r,
		ReadTimeout:  cfg.Server.ReadTimeout,
		WriteTimeout: cfg.Server.WriteTimeout,
		IdleTimeout:  cfg.Server.IdleTimeout,
	}

	// Graceful shutdown
	go func() {
		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
		<-sigCh
		appLogger.Info(ctx, "Shutting down gateway...")
		shutCtx, shutCancel := context.WithTimeout(context.Background(), cfg.Server.ShutDownTimeOut)
		defer shutCancel()
		httpServer.Shutdown(shutCtx)
		cancel()
	}()

	appLogger.Info(ctx, fmt.Sprintf("Gateway listening on %s:%d", cfg.Server.Host, cfg.Server.Port))
	if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		appLogger.Fatal(ctx, fmt.Sprintf("Gateway failed: %v", err))
	}
	appLogger.Info(ctx, "Gateway stopped")
}
