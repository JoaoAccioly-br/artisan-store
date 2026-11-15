package middleware_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/joao-accioly/artisan-store/api/internal/domain"
	"github.com/joao-accioly/artisan-store/api/internal/http/middleware"
	"github.com/joao-accioly/artisan-store/api/internal/http/oapi"
	"github.com/joao-accioly/artisan-store/api/internal/platform/logger"
)

func TestRecoverer_ReturnsStandard500OnPanic(t *testing.T) {
	panicValues := []struct {
		name string
		val  any
	}{
		{name: "string panic", val: "boom"},
		{name: "error panic", val: errors.New("unexpected nil dereference")},
		{name: "int panic", val: 42},
	}

	for _, tc := range panicValues {
		t.Run(tc.name, func(t *testing.T) {
			var logBuf bytes.Buffer
			log := logger.NewWithWriter(&logBuf, "production", "error")

			h := middleware.RequestID(middleware.Recoverer(log)(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
				panic(tc.val)
			})))

			req := httptest.NewRequest(http.MethodGet, "/panic", nil)
			req.Header.Set(middleware.RequestIDHeader, "panic-req-1")
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)

			assert.Equal(t, http.StatusInternalServerError, rec.Code)
			assert.Equal(t, "application/json; charset=utf-8", rec.Header().Get("Content-Type"))
			assert.Contains(t, logBuf.String(), "panic recovered")
			assert.Contains(t, logBuf.String(), "panic-req-1")
			assert.Contains(t, logBuf.String(), "stack")

			var body oapi.ErrorResponse
			require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
			assert.Equal(t, string(domain.CodeInternal), body.Error.Code)
			assert.Equal(t, "Erro interno inesperado", body.Error.Message)
		})
	}
}

func TestRecoverer_PassesThroughWithoutPanic(t *testing.T) {
	var logBuf bytes.Buffer
	log := logger.NewWithWriter(&logBuf, "production", "error")

	h := middleware.Recoverer(log)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	}))

	req := httptest.NewRequest(http.MethodGet, "/ok", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "ok", rec.Body.String())
	assert.Empty(t, logBuf.String())
}
