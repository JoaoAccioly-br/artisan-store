package middleware

import (
	"context"
	"net/http"

	chimiddleware "github.com/go-chi/chi/v5/middleware"
)

// RequestIDHeader é o cabeçalho HTTP usado para propagar o identificador da requisição.
const RequestIDHeader = "X-Request-Id"

// RequestID garante um identificador único por requisição, propagando-o no contexto e no cabeçalho de resposta.
func RequestID(next http.Handler) http.Handler {
	return chimiddleware.RequestID(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if reqID := chimiddleware.GetReqID(r.Context()); reqID != "" {
			w.Header().Set(RequestIDHeader, reqID)
		}
		next.ServeHTTP(w, r)
	}))
}

// GetRequestID retorna o request ID associado ao contexto da requisição.
func GetRequestID(ctx context.Context) string {
	return chimiddleware.GetReqID(ctx)
}
