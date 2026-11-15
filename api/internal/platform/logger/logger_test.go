package logger_test

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/joao-accioly/artisan-store/api/internal/platform/logger"
)

func TestNew_ReturnsConfiguredLogger(t *testing.T) {
	log := logger.New("development", "info")
	require.NotNil(t, log)
}

func TestNewWithWriter_FormatByEnvironment(t *testing.T) {
	t.Run("production uses JSON format case-insensitively", func(t *testing.T) {
		for _, env := range []string{"production", "PRODUCTION", "Production"} {
			t.Run(env, func(t *testing.T) {
				var buf bytes.Buffer
				log := logger.NewWithWriter(&buf, env, "info")

				log.Info("pedido criado", slog.String("order_id", "ord-123"))

				var entry map[string]any
				require.NoError(t, json.Unmarshal(buf.Bytes(), &entry))
				assert.Equal(t, "INFO", entry["level"])
				assert.Equal(t, "pedido criado", entry["msg"])
				assert.Equal(t, "ord-123", entry["order_id"])
			})
		}
	})

	t.Run("non-production uses text format", func(t *testing.T) {
		for _, env := range []string{"development", "staging", ""} {
			t.Run("env="+env, func(t *testing.T) {
				var buf bytes.Buffer
				log := logger.NewWithWriter(&buf, env, "info")

				log.Info("servidor iniciado", slog.Int("port", 8080))

				output := buf.String()
				assert.Contains(t, output, "level=INFO")
				assert.Contains(t, output, `msg="servidor iniciado"`)
				assert.Contains(t, output, "port=8080")
				assert.False(t, json.Valid(buf.Bytes()))
			})
		}
	})
}

func TestNewWithWriter_LogLevels(t *testing.T) {
	tests := []struct {
		name        string
		level       string
		expectDebug bool
		expectInfo  bool
		expectWarn  bool
		expectError bool
	}{
		{
			name:        "debug",
			level:       "debug",
			expectDebug: true,
			expectInfo:  true,
			expectWarn:  true,
			expectError: true,
		},
		{
			name:        "debug with spaces and uppercase",
			level:       "  DEBUG  ",
			expectDebug: true,
			expectInfo:  true,
			expectWarn:  true,
			expectError: true,
		},
		{
			name:        "info",
			level:       "info",
			expectDebug: false,
			expectInfo:  true,
			expectWarn:  true,
			expectError: true,
		},
		{
			name:        "warn",
			level:       "warn",
			expectDebug: false,
			expectInfo:  false,
			expectWarn:  true,
			expectError: true,
		},
		{
			name:        "warning alias",
			level:       "warning",
			expectDebug: false,
			expectInfo:  false,
			expectWarn:  true,
			expectError: true,
		},
		{
			name:        "error",
			level:       "error",
			expectDebug: false,
			expectInfo:  false,
			expectWarn:  false,
			expectError: true,
		},
		{
			name:        "unknown defaults to info",
			level:       "trace",
			expectDebug: false,
			expectInfo:  true,
			expectWarn:  true,
			expectError: true,
		},
		{
			name:        "empty defaults to info",
			level:       "",
			expectDebug: false,
			expectInfo:  true,
			expectWarn:  true,
			expectError: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var buf bytes.Buffer
			log := logger.NewWithWriter(&buf, "production", tc.level)

			log.Debug("msg-debug")
			log.Info("msg-info")
			log.Warn("msg-warn")
			log.Error("msg-error")

			out := buf.String()
			assert.Equal(t, tc.expectDebug, bytes.Contains(buf.Bytes(), []byte("msg-debug")), "debug in %q", out)
			assert.Equal(t, tc.expectInfo, bytes.Contains(buf.Bytes(), []byte("msg-info")), "info in %q", out)
			assert.Equal(t, tc.expectWarn, bytes.Contains(buf.Bytes(), []byte("msg-warn")), "warn in %q", out)
			assert.Equal(t, tc.expectError, bytes.Contains(buf.Bytes(), []byte("msg-error")), "error in %q", out)
		})
	}
}
