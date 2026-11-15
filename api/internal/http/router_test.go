package http_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/joao-accioly/artisan-store/api/internal/config"
	"github.com/joao-accioly/artisan-store/api/internal/domain"
	apphttp "github.com/joao-accioly/artisan-store/api/internal/http"
	"github.com/joao-accioly/artisan-store/api/internal/http/middleware"
	"github.com/joao-accioly/artisan-store/api/internal/http/oapi"
	"github.com/joao-accioly/artisan-store/api/internal/platform/logger"
)

type stubDBPinger struct {
	pingFn func(ctx context.Context) error
}

func (s *stubDBPinger) Ping(ctx context.Context) error {
	return s.pingFn(ctx)
}

func newTestConfig() *config.Config {
	return &config.Config{
		AppEnv:         "production",
		AllowedOrigins: []string{"http://localhost:3000"},
		RateLimitRPM:   60,
	}
}

func TestNewRouter_Unit(t *testing.T) {
	t.Run("GET /healthz returns 200 and request log", func(t *testing.T) {
		var logBuf bytes.Buffer
		log := logger.NewWithWriter(&logBuf, "production", "info")
		router := apphttp.NewRouter(newTestConfig(), log, apphttp.RouterDeps{
			DB: &stubDBPinger{
				pingFn: func(context.Context) error { return nil },
			},
		})

		req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
		req.Header.Set("Origin", "http://localhost:3000")
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)

		assert.Equal(t, http.StatusOK, rec.Code)
		assert.NotEmpty(t, rec.Header().Get(middleware.RequestIDHeader))
		assert.Equal(t, "http://localhost:3000", rec.Header().Get("Access-Control-Allow-Origin"))
		assert.Contains(t, logBuf.String(), `"status":200`)
	})

	t.Run("GET /healthz with nil db returns 503", func(t *testing.T) {
		var logBuf bytes.Buffer
		log := logger.NewWithWriter(&logBuf, "production", "info")
		router := apphttp.NewRouter(newTestConfig(), log, apphttp.RouterDeps{})

		req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)

		assert.Equal(t, http.StatusServiceUnavailable, rec.Code)
		var body oapi.ErrorResponse
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
		assert.Equal(t, string(domain.CodeServiceUnavailable), body.Error.Code)
	})

	t.Run("POST /healthz returns 405 Method Not Allowed", func(t *testing.T) {
		var logBuf bytes.Buffer
		log := logger.NewWithWriter(&logBuf, "production", "info")
		router := apphttp.NewRouter(newTestConfig(), log, apphttp.RouterDeps{})

		req := httptest.NewRequest(http.MethodPost, "/healthz", nil)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)

		assert.Equal(t, http.StatusMethodNotAllowed, rec.Code)
		assert.NotEmpty(t, rec.Header().Get(middleware.RequestIDHeader))
		var body oapi.ErrorResponse
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
		assert.Equal(t, string(domain.CodeMethodNotAllowed), body.Error.Code)
		assert.Contains(t, logBuf.String(), `"level":"WARN"`)
	})

	t.Run("unknown route returns 404 Not Found", func(t *testing.T) {
		var logBuf bytes.Buffer
		log := logger.NewWithWriter(&logBuf, "production", "info")
		router := apphttp.NewRouter(newTestConfig(), log, apphttp.RouterDeps{})

		req := httptest.NewRequest(http.MethodGet, "/unknown", nil)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)

		assert.Equal(t, http.StatusNotFound, rec.Code)
		var body oapi.ErrorResponse
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
		assert.Equal(t, string(domain.CodeNotFound), body.Error.Code)
	})

	t.Run("panic inside handler is recovered with 500 JSON", func(t *testing.T) {
		var logBuf bytes.Buffer
		log := logger.NewWithWriter(&logBuf, "production", "info")
		router := apphttp.NewRouter(newTestConfig(), log, apphttp.RouterDeps{
			DB: &stubDBPinger{
				pingFn: func(context.Context) error {
					panic("unexpected db driver panic")
				},
			},
		})

		req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)

		assert.Equal(t, http.StatusInternalServerError, rec.Code)
		assert.NotEmpty(t, rec.Header().Get(middleware.RequestIDHeader))
		assert.Contains(t, logBuf.String(), "panic recovered")

		var body oapi.ErrorResponse
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
		assert.Equal(t, string(domain.CodeInternal), body.Error.Code)
	})

	t.Run("CORS preflight OPTIONS on /healthz succeeds", func(t *testing.T) {
		var logBuf bytes.Buffer
		log := logger.NewWithWriter(&logBuf, "production", "info")
		router := apphttp.NewRouter(newTestConfig(), log, apphttp.RouterDeps{})

		req := httptest.NewRequest(http.MethodOptions, "/healthz", nil)
		req.Header.Set("Origin", "http://localhost:3000")
		req.Header.Set("Access-Control-Request-Method", http.MethodGet)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)

		assert.Contains(t, []int{http.StatusOK, http.StatusNoContent}, rec.Code)
		assert.Equal(t, "http://localhost:3000", rec.Header().Get("Access-Control-Allow-Origin"))
	})
}
