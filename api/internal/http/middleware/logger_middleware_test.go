package middleware_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/joao-accioly/artisan-store/api/internal/http/middleware"
	"github.com/joao-accioly/artisan-store/api/internal/platform/logger"
)

func TestLogger_LogsStructuredFieldsAndLevels(t *testing.T) {
	tests := []struct {
		name           string
		handler        http.HandlerFunc
		expectedLevel  string
		expectedStatus float64
		expectedBytes  float64
	}{
		{
			name: "2xx logs INFO",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusCreated)
				_, _ = w.Write([]byte("created"))
			},
			expectedLevel:  "INFO",
			expectedStatus: float64(http.StatusCreated),
			expectedBytes:  7,
		},
		{
			name: "implicit 200 without WriteHeader or Write logs INFO",
			handler: func(http.ResponseWriter, *http.Request) {
				// Não chama WriteHeader nem Write: ww.Status() retorna 0, convertido para 200.
			},
			expectedLevel:  "INFO",
			expectedStatus: float64(http.StatusOK),
			expectedBytes:  0,
		},
		{
			name: "4xx logs WARN",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusBadRequest)
				_, _ = w.Write([]byte("bad"))
			},
			expectedLevel:  "WARN",
			expectedStatus: float64(http.StatusBadRequest),
			expectedBytes:  3,
		},
		{
			name: "5xx logs ERROR",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusServiceUnavailable)
				_, _ = w.Write([]byte("down"))
			},
			expectedLevel:  "ERROR",
			expectedStatus: float64(http.StatusServiceUnavailable),
			expectedBytes:  4,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var buf bytes.Buffer
			log := logger.NewWithWriter(&buf, "production", "debug")

			h := middleware.RequestID(middleware.Logger(log)(tc.handler))

			req := httptest.NewRequest(http.MethodPost, "/api/v1/orders", nil)
			req.RemoteAddr = "198.51.100.7:54321"
			req.Header.Set(middleware.RequestIDHeader, "req-log-test-1")
			rec := httptest.NewRecorder()

			h.ServeHTTP(rec, req)

			var entry map[string]any
			require.NoError(t, json.Unmarshal(buf.Bytes(), &entry))

			assert.Equal(t, tc.expectedLevel, entry["level"])
			assert.Equal(t, "http request", entry["msg"])
			assert.Equal(t, "req-log-test-1", entry["request_id"])
			assert.Equal(t, http.MethodPost, entry["method"])
			assert.Equal(t, "/api/v1/orders", entry["path"])
			assert.Equal(t, tc.expectedStatus, entry["status"])
			assert.Equal(t, tc.expectedBytes, entry["bytes"])
			assert.Equal(t, "198.51.100.7:54321", entry["remote_ip"])
			assert.Contains(t, entry, "duration_ms")
		})
	}
}
