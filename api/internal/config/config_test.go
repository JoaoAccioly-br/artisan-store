package config_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/joao-accioly/artisan-store/api/internal/config"
)

func unsetEnvForTest(t *testing.T, key string) {
	t.Helper()
	t.Setenv(key, "")
	require.NoError(t, os.Unsetenv(key))
}

func TestLoad_AllDefaults(t *testing.T) {
	t.Chdir(t.TempDir())
	for _, k := range []string{
		"APP_ENV", "API_HOST", "API_PORT", "LOG_LEVEL", "AUTO_MIGRATE",
		"CORS_ALLOWED_ORIGINS", "RATE_LIMIT_RPM", "HTTP_READ_TIMEOUT",
		"HTTP_WRITE_TIMEOUT", "HTTP_IDLE_TIMEOUT",
	} {
		unsetEnvForTest(t, k)
	}
	t.Setenv("DATABASE_URL", "postgres://user:pass@localhost:5432/doceria?sslmode=disable")

	cfg, err := config.Load()
	require.NoError(t, err)

	assert.Equal(t, "development", cfg.AppEnv)
	assert.False(t, cfg.IsProduction())
	assert.Equal(t, "0.0.0.0", cfg.Host)
	assert.Equal(t, 8080, cfg.Port)
	assert.Equal(t, "0.0.0.0:8080", cfg.Address())
	assert.Equal(t, "info", cfg.LogLevel)
	assert.Equal(t, "postgres://user:pass@localhost:5432/doceria?sslmode=disable", cfg.DatabaseURL)
	assert.True(t, cfg.AutoMigrate)
	assert.Equal(t, []string{"http://localhost:3000", "http://localhost:5173"}, cfg.AllowedOrigins)
	assert.Equal(t, 120, cfg.RateLimitRPM)
	assert.Equal(t, 10*time.Second, cfg.ReadTimeout)
	assert.Equal(t, 30*time.Second, cfg.WriteTimeout)
	assert.Equal(t, 60*time.Second, cfg.IdleTimeout)
}

func TestLoad_DefaultsAndOverrides(t *testing.T) {
	t.Chdir(t.TempDir())
	t.Setenv("DATABASE_URL", "postgres://user:pass@localhost:5432/doceria?sslmode=disable")
	t.Setenv("APP_ENV", "production")
	t.Setenv("API_HOST", "127.0.0.1")
	t.Setenv("API_PORT", "9090")
	t.Setenv("LOG_LEVEL", "debug")
	t.Setenv("AUTO_MIGRATE", "false")
	t.Setenv("CORS_ALLOWED_ORIGINS", "https://doceria.com,https://admin.doceria.com")
	t.Setenv("RATE_LIMIT_RPM", "60")
	t.Setenv("HTTP_READ_TIMEOUT", "15s")
	t.Setenv("HTTP_WRITE_TIMEOUT", "45s")
	t.Setenv("HTTP_IDLE_TIMEOUT", "90s")

	cfg, err := config.Load()
	require.NoError(t, err)

	assert.True(t, cfg.IsProduction())
	assert.Equal(t, "127.0.0.1:9090", cfg.Address())
	assert.Equal(t, "debug", cfg.LogLevel)
	assert.False(t, cfg.AutoMigrate)
	assert.Equal(t, []string{"https://doceria.com", "https://admin.doceria.com"}, cfg.AllowedOrigins)
	assert.Equal(t, 60, cfg.RateLimitRPM)
	assert.Equal(t, 15*time.Second, cfg.ReadTimeout)
	assert.Equal(t, 45*time.Second, cfg.WriteTimeout)
	assert.Equal(t, 90*time.Second, cfg.IdleTimeout)
}

func TestLoad_MissingDatabaseURL(t *testing.T) {
	t.Chdir(t.TempDir())
	unsetEnvForTest(t, "DATABASE_URL")

	_, err := config.Load()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "parse environment config")
	assert.Contains(t, err.Error(), "DATABASE_URL")
}

func TestLoad_PortValidation(t *testing.T) {
	t.Chdir(t.TempDir())

	invalidCases := []struct {
		name    string
		port    string
		errText string
	}{
		{name: "zero", port: "0", errText: "invalid API_PORT"},
		{name: "negative", port: "-1", errText: "invalid API_PORT"},
		{name: "above max", port: "65536", errText: "invalid API_PORT"},
		{name: "way above max", port: "70000", errText: "invalid API_PORT"},
		{name: "non numeric", port: "abc", errText: "parse environment config"},
	}

	for _, tc := range invalidCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("DATABASE_URL", "postgres://user:pass@localhost:5432/doceria?sslmode=disable")
			t.Setenv("API_PORT", tc.port)

			_, err := config.Load()
			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.errText)
		})
	}

	for _, validPort := range []string{"1", "65535"} {
		t.Run("valid boundary "+validPort, func(t *testing.T) {
			t.Setenv("DATABASE_URL", "postgres://user:pass@localhost:5432/doceria?sslmode=disable")
			t.Setenv("API_PORT", validPort)

			cfg, err := config.Load()
			require.NoError(t, err)
			assert.Equal(t, "0.0.0.0:"+validPort, cfg.Address())
		})
	}
}

func TestLoad_RateLimitRPMValidation(t *testing.T) {
	t.Chdir(t.TempDir())

	invalidCases := []struct {
		name    string
		rpm     string
		errText string
	}{
		{name: "zero", rpm: "0", errText: "invalid RATE_LIMIT_RPM"},
		{name: "negative", rpm: "-10", errText: "invalid RATE_LIMIT_RPM"},
		{name: "non numeric", rpm: "fast", errText: "parse environment config"},
	}

	for _, tc := range invalidCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("DATABASE_URL", "postgres://user:pass@localhost:5432/doceria?sslmode=disable")
			t.Setenv("RATE_LIMIT_RPM", tc.rpm)

			_, err := config.Load()
			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.errText)
		})
	}

	t.Run("valid boundary 1", func(t *testing.T) {
		t.Setenv("DATABASE_URL", "postgres://user:pass@localhost:5432/doceria?sslmode=disable")
		t.Setenv("RATE_LIMIT_RPM", "1")

		cfg, err := config.Load()
		require.NoError(t, err)
		assert.Equal(t, 1, cfg.RateLimitRPM)
	})
}

func TestLoad_InvalidTypedEnvVars(t *testing.T) {
	t.Chdir(t.TempDir())

	t.Run("invalid duration", func(t *testing.T) {
		t.Setenv("DATABASE_URL", "postgres://user:pass@localhost:5432/doceria?sslmode=disable")
		t.Setenv("HTTP_READ_TIMEOUT", "not-a-duration")

		_, err := config.Load()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "parse environment config")
	})

	t.Run("invalid bool", func(t *testing.T) {
		t.Setenv("DATABASE_URL", "postgres://user:pass@localhost:5432/doceria?sslmode=disable")
		t.Setenv("AUTO_MIGRATE", "maybe")

		_, err := config.Load()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "parse environment config")
	})
}

func TestLoad_DotEnvFile(t *testing.T) {
	t.Run("loads from local .env", func(t *testing.T) {
		dir := t.TempDir()
		t.Chdir(dir)
		unsetEnvForTest(t, "DATABASE_URL")
		unsetEnvForTest(t, "API_PORT")

		content := "DATABASE_URL=postgres://envuser:envpass@localhost:5432/doceria\nAPI_PORT=8181\n"
		require.NoError(t, os.WriteFile(filepath.Join(dir, ".env"), []byte(content), 0o600))

		cfg, err := config.Load()
		require.NoError(t, err)
		assert.Equal(t, "postgres://envuser:envpass@localhost:5432/doceria", cfg.DatabaseURL)
		assert.Equal(t, 8181, cfg.Port)
	})

	t.Run("returns error when .env is unreadable directory", func(t *testing.T) {
		dir := t.TempDir()
		t.Chdir(dir)
		require.NoError(t, os.Mkdir(filepath.Join(dir, ".env"), 0o750))

		_, err := config.Load()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "load .env")
	})
}

func TestConfig_IsProduction(t *testing.T) {
	assert.True(t, config.Config{AppEnv: "production"}.IsProduction())
	assert.True(t, config.Config{AppEnv: "PRODUCTION"}.IsProduction())
	assert.True(t, config.Config{AppEnv: "Production"}.IsProduction())
	assert.False(t, config.Config{AppEnv: "development"}.IsProduction())
	assert.False(t, config.Config{AppEnv: "staging"}.IsProduction())
	assert.False(t, config.Config{AppEnv: ""}.IsProduction())
}
