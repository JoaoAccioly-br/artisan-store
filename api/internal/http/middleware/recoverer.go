package middleware

import (
	"log/slog"
	"net/http"
	"runtime/debug"

	"github.com/joao-accioly/artisan-store/api/internal/domain"
	"github.com/joao-accioly/artisan-store/api/internal/http/response"
)

// Recoverer captura panics nos handlers, registra o stacktrace em log e retorna erro 500 JSON padronizado.
func Recoverer(log *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				if rec := recover(); rec != nil {
					log.Error("panic recovered",
						slog.String("request_id", GetRequestID(r.Context())),
						slog.Any("panic", rec),
						slog.String("stack", string(debug.Stack())),
					)
					response.Error(w, log, domain.NewInternalError("Erro interno inesperado", nil))
				}
			}()

			next.ServeHTTP(w, r)
		})
	}
}
