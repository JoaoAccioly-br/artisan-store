package logger

import (
	"io"
	"log/slog"
	"os"
	"strings"
)

// New cria uma instância configurada de *slog.Logger com formato JSON em produção
// e texto legível em desenvolvimento.
func New(appEnv, level string) *slog.Logger {
	return NewWithWriter(os.Stdout, appEnv, level)
}

// NewWithWriter cria um *slog.Logger escrevendo no io.Writer informado (útil para testes).
func NewWithWriter(w io.Writer, appEnv, level string) *slog.Logger {
	opts := &slog.HandlerOptions{
		Level: parseLevel(level),
	}

	var handler slog.Handler
	if strings.EqualFold(appEnv, "production") {
		handler = slog.NewJSONHandler(w, opts)
	} else {
		handler = slog.NewTextHandler(w, opts)
	}

	return slog.New(handler)
}

func parseLevel(level string) slog.Level {
	switch strings.ToLower(strings.TrimSpace(level)) {
	case "debug":
		return slog.LevelDebug
	case "warn", "warning":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}
