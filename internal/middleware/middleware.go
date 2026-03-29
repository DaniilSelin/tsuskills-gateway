package middleware

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"tsuskills-gateway/config"
	"tsuskills-gateway/internal/logger"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"go.uber.org/zap"
)

// RequestID добавляет уникальный ID в контекст и заголовок ответа
func RequestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rid := uuid.New().String()
		ctx := context.WithValue(r.Context(), logger.RequestID, rid)
		w.Header().Set("X-Request-ID", rid)
		r.Header.Set("X-Request-ID", rid) // прокидываем в upstream
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// CORS обрабатывает preflight и устанавливает заголовки.
// Это ЕДИНСТВЕННОЕ место в системе, где выставляется CORS.
// Backend-сервисы тоже ставят CORS-заголовки, но proxy.ModifyResponse их удаляет.
func CORS(cfg config.CORSConfig) func(http.Handler) http.Handler {
	methods := strings.Join(cfg.AllowedMethods, ", ")
	headers := strings.Join(cfg.AllowedHeaders, ", ")
	maxAge := fmt.Sprintf("%d", cfg.MaxAge)

	// Проверяем, разрешён ли wildcard
	allowAll := len(cfg.AllowedOrigins) == 1 && cfg.AllowedOrigins[0] == "*"

	// Для быстрого поиска конкретных origin-ов
	originSet := make(map[string]bool, len(cfg.AllowedOrigins))
	for _, o := range cfg.AllowedOrigins {
		originSet[o] = true
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			origin := r.Header.Get("Origin")

			// Определяем, какой Origin вернуть
			if origin != "" {
				if allowAll {
					w.Header().Set("Access-Control-Allow-Origin", origin)
				} else if originSet[origin] {
					w.Header().Set("Access-Control-Allow-Origin", origin)
				}
				// Vary: Origin — обязательно когда не wildcard "*"
				w.Header().Add("Vary", "Origin")
			} else {
				// Без Origin (не-браузерный запрос) — просто пропускаем
				w.Header().Set("Access-Control-Allow-Origin", "*")
			}

			w.Header().Set("Access-Control-Allow-Methods", methods)
			w.Header().Set("Access-Control-Allow-Headers", headers)
			w.Header().Set("Access-Control-Max-Age", maxAge)
			w.Header().Set("Access-Control-Allow-Credentials", "true")

			// Preflight — сразу отвечаем 204, не проксируем
			if r.Method == http.MethodOptions {
				w.WriteHeader(http.StatusNoContent)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

// Logging логирует каждый запрос
func Logging(log logger.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			wr := &statusWriter{ResponseWriter: w, code: http.StatusOK}
			next.ServeHTTP(wr, r)
			log.Info(r.Context(), "gateway",
				zap.String("method", r.Method),
				zap.String("path", r.URL.Path),
				zap.String("remote", r.RemoteAddr),
				zap.Int("status", wr.code),
				zap.Duration("dur", time.Since(start)),
			)
		})
	}
}

// Recovery перехватывает паники
func Recovery(log logger.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				if err := recover(); err != nil {
					log.Error(r.Context(), "panic", zap.Any("err", err))
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(http.StatusInternalServerError)
					w.Write([]byte(`{"error":"Internal Server Error","message":"unexpected error"}`))
				}
			}()
			next.ServeHTTP(w, r)
		})
	}
}

// JWTAuth проверяет Bearer token на всех непубличных путях.
// При успехе прокидывает X-User-ID в заголовок запроса к upstream-сервису.
func JWTAuth(secretKey string, publicPaths []string, log logger.Logger) func(http.Handler) http.Handler {
	publicSet := make(map[string]bool, len(publicPaths))
	for _, p := range publicPaths {
		publicSet[p] = true
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// OPTIONS всегда пропускаем
			if r.Method == http.MethodOptions {
				next.ServeHTTP(w, r)
				return
			}

			// публичные пути пропускаем
			if isPublic(r.URL.Path, publicSet) {
				next.ServeHTTP(w, r)
				return
			}

			// достаём токен
			tokenStr := extractBearer(r)
			if tokenStr == "" {
				writeUnauthorized(w, "Missing or invalid Authorization header")
				return
			}

			// валидируем
			claims, err := validateToken(tokenStr, secretKey)
			if err != nil {
				log.Warn(r.Context(), "jwt validation failed", zap.Error(err))
				writeUnauthorized(w, "Invalid or expired token")
				return
			}

			// прокидываем user_id в upstream
			r.Header.Set("X-User-ID", claims.UserID)

			next.ServeHTTP(w, r)
		})
	}
}

// ──── helpers ────────────────────────────

type jwtClaims struct {
	UserID  string `json:"user_id"`
	TokenID string `json:"token_id"`
	jwt.RegisteredClaims
}

func validateToken(tokenStr, secret string) (*jwtClaims, error) {
	claims := &jwtClaims{}
	token, err := jwt.ParseWithClaims(tokenStr, claims, func(t *jwt.Token) (interface{}, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", t.Header["alg"])
		}
		return []byte(secret), nil
	})
	if err != nil {
		return nil, err
	}
	if !token.Valid {
		return nil, fmt.Errorf("invalid token")
	}
	return claims, nil
}

func extractBearer(r *http.Request) string {
	h := r.Header.Get("Authorization")
	if h == "" {
		return ""
	}
	parts := strings.SplitN(h, " ", 2)
	if len(parts) != 2 || !strings.EqualFold(parts[0], "bearer") {
		return ""
	}
	return strings.TrimSpace(parts[1])
}

func isPublic(path string, publicSet map[string]bool) bool {
	if publicSet[path] {
		return true
	}
	// проверяем префиксы — например /health попадёт сюда
	for p := range publicSet {
		if strings.HasPrefix(path, p) {
			return true
		}
	}
	return false
}

func writeUnauthorized(w http.ResponseWriter, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusUnauthorized)
	fmt.Fprintf(w, `{"error":"Unauthorized","code":"UNAUTHORIZED","message":"%s"}`, msg)
}

type statusWriter struct {
	http.ResponseWriter
	code int
}

func (w *statusWriter) WriteHeader(code int) {
	w.code = code
	w.ResponseWriter.WriteHeader(code)
}
