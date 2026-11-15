package middleware

import (
	"log/slog"
	"net"
	"net/http"
	"time"

	"github.com/go-chi/httprate"

	"github.com/joao-accioly/artisan-store/api/internal/domain"
	"github.com/joao-accioly/artisan-store/api/internal/http/response"
)

// RateLimit aplica limitação de requisições por IP usando janela deslizante de 1 minuto.
func RateLimit(requestsPerMinute int, log *slog.Logger) func(http.Handler) http.Handler {
	return httprate.LimitBy(
		requestsPerMinute,
		time.Minute,
		clientIPKey,
		httprate.WithLimitHandler(func(w http.ResponseWriter, r *http.Request) {
			log.Warn("rate limit exceeded",
				slog.String("request_id", GetRequestID(r.Context())),
				slog.String("remote_ip", r.RemoteAddr),
				slog.String("path", r.URL.Path),
			)
			response.Error(w, log, domain.NewRateLimitedError("Muitas requisições. Tente novamente em instantes."))
		}),
	)
}

// clientIPKey usa o IP extraído de r.RemoteAddr e canonicalizado para evitar spoofing de cabeçalhos
// (em produção atrás de proxy reverso confiável, um middleware RealIP específico deve atualizar RemoteAddr).
func clientIPKey(r *http.Request) (string, error) {
	ip, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		ip = r.RemoteAddr
	}
	return httprate.CanonicalizeIP(ip), nil
}
