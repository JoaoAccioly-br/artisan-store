package middleware_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/joao-accioly/artisan-store/api/internal/domain"
	"github.com/joao-accioly/artisan-store/api/internal/http/middleware"
	"github.com/joao-accioly/artisan-store/api/internal/http/oapi"
	"github.com/joao-accioly/artisan-store/api/internal/platform/logger"
)

func TestRateLimit_EnforcesPerIPAndLogs429(t *testing.T) {
	var logBuf bytes.Buffer
	log := logger.NewWithWriter(&logBuf, "production", "warn")

	h := middleware.RequestID(middleware.RateLimit(2, log)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})))

	for i := 0; i < 2; i++ {
		req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
		req.RemoteAddr = "192.0.2.10:12345"
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		assert.Equal(t, http.StatusOK, rec.Code)
	}

	// 3a requisição do mesmo IP (porta diferente) deve ser bloqueada.
	reqBlocked := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	reqBlocked.RemoteAddr = "192.0.2.10:54321"
	reqBlocked.Header.Set(middleware.RequestIDHeader, "rl-req-1")
	recBlocked := httptest.NewRecorder()
	h.ServeHTTP(recBlocked, reqBlocked)

	assert.Equal(t, http.StatusTooManyRequests, recBlocked.Code)
	assert.Equal(t, "application/json; charset=utf-8", recBlocked.Header().Get("Content-Type"))

	var body oapi.ErrorResponse
	require.NoError(t, json.Unmarshal(recBlocked.Body.Bytes(), &body))
	assert.Equal(t, string(domain.CodeRateLimited), body.Error.Code)
	assert.Equal(t, "Muitas requisições. Tente novamente em instantes.", body.Error.Message)

	logOut := logBuf.String()
	assert.Contains(t, logOut, "rate limit exceeded")
	assert.Contains(t, logOut, "rl-req-1")
	assert.Contains(t, logOut, "192.0.2.10:54321")
	assert.Contains(t, logOut, "/healthz")

	// IP distinto não deve ser bloqueado.
	reqOtherIP := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	reqOtherIP.RemoteAddr = "192.0.2.20:11111"
	recOtherIP := httptest.NewRecorder()
	h.ServeHTTP(recOtherIP, reqOtherIP)
	assert.Equal(t, http.StatusOK, recOtherIP.Code)
}

func TestRateLimit_IPv6PrefixAndBareIPFallback(t *testing.T) {
	var logBuf bytes.Buffer
	log := logger.NewWithWriter(&logBuf, "production", "warn")

	h := middleware.RateLimit(1, log)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	// Endereços IPv6 no mesmo bloco /64 compartilham o mesmo bucket.
	reqV6First := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	reqV6First.RemoteAddr = "[2001:db8:1:2::1]:1111"
	recV6First := httptest.NewRecorder()
	h.ServeHTTP(recV6First, reqV6First)
	assert.Equal(t, http.StatusOK, recV6First.Code)

	reqV6Second := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	reqV6Second.RemoteAddr = "[2001:db8:1:2::2]:2222"
	recV6Second := httptest.NewRecorder()
	h.ServeHTTP(recV6Second, reqV6Second)
	assert.Equal(t, http.StatusTooManyRequests, recV6Second.Code)

	// RemoteAddr sem porta (fallback de SplitHostPort) continua funcionando.
	reqBare1 := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	reqBare1.RemoteAddr = "203.0.113.55"
	recBare1 := httptest.NewRecorder()
	h.ServeHTTP(recBare1, reqBare1)
	assert.Equal(t, http.StatusOK, recBare1.Code)

	reqBare2 := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	reqBare2.RemoteAddr = "203.0.113.55"
	recBare2 := httptest.NewRecorder()
	h.ServeHTTP(recBare2, reqBare2)
	assert.Equal(t, http.StatusTooManyRequests, recBare2.Code)
}

func TestRateLimit_IgnoresSpoofedForwardingHeaders(t *testing.T) {
	var logBuf bytes.Buffer
	log := logger.NewWithWriter(&logBuf, "production", "warn")

	h := middleware.RateLimit(1, log)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req1 := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	req1.RemoteAddr = "198.51.100.5:4000"
	req1.Header.Set("X-Forwarded-For", "1.1.1.1")
	rec1 := httptest.NewRecorder()
	h.ServeHTTP(rec1, req1)
	assert.Equal(t, http.StatusOK, rec1.Code)

	// Trocar X-Forwarded-For e X-Real-IP mantendo RemoteAddr não pode burlar o limite.
	req2 := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	req2.RemoteAddr = "198.51.100.5:4001"
	req2.Header.Set("X-Forwarded-For", "8.8.8.8")
	req2.Header.Set("X-Real-IP", "9.9.9.9")
	rec2 := httptest.NewRecorder()
	h.ServeHTTP(rec2, req2)
	assert.Equal(t, http.StatusTooManyRequests, rec2.Code)
}

func TestRateLimit_ConcurrentRequests(t *testing.T) {
	var logBuf bytes.Buffer
	log := logger.NewWithWriter(&logBuf, "production", "error")

	const limit = 5
	const total = 20

	h := middleware.RateLimit(limit, log)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	var okCount atomic.Int32
	var limitedCount atomic.Int32
	var wg sync.WaitGroup
	wg.Add(total)

	for i := 0; i < total; i++ {
		go func() {
			defer wg.Done()
			req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
			req.RemoteAddr = "203.0.113.9:8080"
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)

			switch rec.Code {
			case http.StatusOK:
				okCount.Add(1)
			case http.StatusTooManyRequests:
				limitedCount.Add(1)
			default:
				t.Errorf("unexpected status code: %d", rec.Code)
			}
		}()
	}

	wg.Wait()

	assert.Equal(t, int32(limit), okCount.Load())
	assert.Equal(t, int32(total-limit), limitedCount.Load())
}
