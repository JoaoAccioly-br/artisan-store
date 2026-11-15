package middleware_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/joao-accioly/artisan-store/api/internal/http/middleware"
)

func TestCORS(t *testing.T) {
	allowedOrigins := []string{"http://localhost:3000", "https://admin.doceria.com"}

	t.Run("allows configured origin on simple GET", func(t *testing.T) {
		h := middleware.CORS(allowedOrigins)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusOK)
		}))

		req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
		req.Header.Set("Origin", "http://localhost:3000")
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)

		assert.Equal(t, http.StatusOK, rec.Code)
		assert.Equal(t, "http://localhost:3000", rec.Header().Get("Access-Control-Allow-Origin"))
		assert.Equal(t, "true", rec.Header().Get("Access-Control-Allow-Credentials"))
		assert.Equal(t, middleware.RequestIDHeader, rec.Header().Get("Access-Control-Expose-Headers"))
	})

	t.Run("rejects disallowed origin on simple GET", func(t *testing.T) {
		h := middleware.CORS(allowedOrigins)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusOK)
		}))

		req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
		req.Header.Set("Origin", "https://evil.example.com")
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)

		assert.Empty(t, rec.Header().Get("Access-Control-Allow-Origin"))
	})

	t.Run("handles preflight OPTIONS for allowed origin and headers", func(t *testing.T) {
		nextCalled := false
		h := middleware.CORS(allowedOrigins)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			nextCalled = true
			w.WriteHeader(http.StatusOK)
		}))

		req := httptest.NewRequest(http.MethodOptions, "/api/v1/orders", nil)
		req.Header.Set("Origin", "https://admin.doceria.com")
		req.Header.Set("Access-Control-Request-Method", http.MethodPost)
		req.Header.Set("Access-Control-Request-Headers", "Authorization, Content-Type, Idempotency-Key, X-Request-Id")
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)

		assert.False(t, nextCalled)
		assert.Contains(t, []int{http.StatusOK, http.StatusNoContent}, rec.Code)
		assert.Equal(t, "https://admin.doceria.com", rec.Header().Get("Access-Control-Allow-Origin"))
		assert.Equal(t, "true", rec.Header().Get("Access-Control-Allow-Credentials"))
		assert.Equal(t, "300", rec.Header().Get("Access-Control-Max-Age"))
		assert.Contains(t, rec.Header().Get("Access-Control-Allow-Methods"), http.MethodPost)
		allowHeaders := rec.Header().Get("Access-Control-Allow-Headers")
		assert.Contains(t, allowHeaders, "Authorization")
		assert.Contains(t, allowHeaders, "Idempotency-Key")
	})

	t.Run("preflight OPTIONS from disallowed origin does not expose allow origin", func(t *testing.T) {
		h := middleware.CORS(allowedOrigins)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusOK)
		}))

		req := httptest.NewRequest(http.MethodOptions, "/api/v1/orders", nil)
		req.Header.Set("Origin", "https://evil.example.com")
		req.Header.Set("Access-Control-Request-Method", http.MethodPost)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)

		assert.Empty(t, rec.Header().Get("Access-Control-Allow-Origin"))
	})
}
