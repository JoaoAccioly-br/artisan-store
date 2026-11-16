package config_test

import (
	"os"
	"path/filepath"
	"strings"
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
		"JWT_ACCESS_SECRET", "JWT_ACCESS_TTL", "JWT_REFRESH_TTL",
		"SEED_ADMIN_NAME", "SEED_ADMIN_EMAIL", "SEED_ADMIN_PASSWORD",
		"STORAGE_DRIVER", "STORAGE_LOCAL_DIR", "STORAGE_PUBLIC_URL",
		"STORAGE_MAX_UPLOAD_BYTES", "STORAGE_S3_ENDPOINT", "STORAGE_S3_REGION",
		"STORAGE_S3_BUCKET", "STORAGE_S3_ACCESS_KEY", "STORAGE_S3_SECRET_KEY",
		"STORAGE_S3_USE_PATH_STYLE",
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
	assert.Equal(t, "change-me-access-secret-min-32-chars-dev-only", cfg.JWTAccessSecret)
	assert.Equal(t, 15*time.Minute, cfg.JWTAccessTTL)
	assert.Equal(t, 168*time.Hour, cfg.JWTRefreshTTL)
	assert.Equal(t, "Administrador", cfg.SeedAdminName)
	assert.Empty(t, cfg.SeedAdminEmail)
	assert.Empty(t, cfg.SeedAdminPassword)
	assert.Equal(t, config.StorageDriverLocal, cfg.StorageDriver)
	assert.Equal(t, "./uploads", cfg.StorageLocalDir)
	assert.Equal(t, "http://localhost:8080/uploads", cfg.StoragePublicURL)
	assert.Equal(t, int64(5242880), cfg.StorageMaxUploadBytes)
	assert.Empty(t, cfg.StorageS3Endpoint)
	assert.Equal(t, "us-east-1", cfg.StorageS3Region)
	assert.Empty(t, cfg.StorageS3Bucket)
	assert.Empty(t, cfg.StorageS3AccessKey)
	assert.Empty(t, cfg.StorageS3SecretKey)
	assert.False(t, cfg.StorageS3UsePathStyle)
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
	t.Setenv("JWT_ACCESS_SECRET", "prod-super-secret-key-with-more-than-32-chars")
	t.Setenv("JWT_ACCESS_TTL", "10m")
	t.Setenv("JWT_REFRESH_TTL", "72h")
	t.Setenv("SEED_ADMIN_NAME", "Maria Confeiteira")
	t.Setenv("SEED_ADMIN_EMAIL", "admin@doceria.com")
	t.Setenv("SEED_ADMIN_PASSWORD", "SenhaForte#123")
	t.Setenv("STORAGE_DRIVER", "local")
	t.Setenv("STORAGE_LOCAL_DIR", "/var/lib/doceria/uploads")
	t.Setenv("STORAGE_PUBLIC_URL", "https://cdn.doceria.com/uploads")
	t.Setenv("STORAGE_MAX_UPLOAD_BYTES", "10485760")

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
	assert.Equal(t, "prod-super-secret-key-with-more-than-32-chars", cfg.JWTAccessSecret)
	assert.Equal(t, 10*time.Minute, cfg.JWTAccessTTL)
	assert.Equal(t, 72*time.Hour, cfg.JWTRefreshTTL)
	assert.Equal(t, "Maria Confeiteira", cfg.SeedAdminName)
	assert.Equal(t, "admin@doceria.com", cfg.SeedAdminEmail)
	assert.Equal(t, "SenhaForte#123", cfg.SeedAdminPassword)
	assert.Equal(t, config.StorageDriverLocal, cfg.StorageDriver)
	assert.Equal(t, "/var/lib/doceria/uploads", cfg.StorageLocalDir)
	assert.Equal(t, "https://cdn.doceria.com/uploads", cfg.StoragePublicURL)
	assert.Equal(t, int64(10485760), cfg.StorageMaxUploadBytes)
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

func TestLoad_JWTSecretValidation(t *testing.T) {
	t.Chdir(t.TempDir())

	t.Run("short or whitespace-only secret (< 32 chars) rejected", func(t *testing.T) {
		for _, secret := range []string{
			"   ",
			strings.Repeat(" ", 32),
			strings.Repeat("\t ", 20),
			"   " + strings.Repeat("a", 28) + "   ",
			"short-secret",
			strings.Repeat("a", 31),
		} {
			t.Setenv("DATABASE_URL", "postgres://user:pass@localhost:5432/doceria?sslmode=disable")
			t.Setenv("APP_ENV", "development")
			t.Setenv("JWT_ACCESS_SECRET", secret)

			_, err := config.Load()
			require.Error(t, err)
			assert.Contains(t, err.Error(), "invalid JWT_ACCESS_SECRET: must be at least 32 characters")
		}
	})

	t.Run("exact 32-char secret accepted in production", func(t *testing.T) {
		secret32 := strings.Repeat("s", 32)
		t.Setenv("DATABASE_URL", "postgres://user:pass@localhost:5432/doceria?sslmode=disable")
		t.Setenv("APP_ENV", "production")
		t.Setenv("JWT_ACCESS_SECRET", secret32)

		cfg, err := config.Load()
		require.NoError(t, err)
		assert.Equal(t, secret32, cfg.JWTAccessSecret)
	})

	t.Run("default dev secret forbidden in production", func(t *testing.T) {
		for _, envName := range []string{"production", "PRODUCTION"} {
			t.Setenv("DATABASE_URL", "postgres://user:pass@localhost:5432/doceria?sslmode=disable")
			t.Setenv("APP_ENV", envName)
			unsetEnvForTest(t, "JWT_ACCESS_SECRET")

			_, err := config.Load()
			require.Error(t, err)
			assert.Contains(t, err.Error(), "default development secret cannot be used in production")
		}
	})

	t.Run("default dev secret accepted in development", func(t *testing.T) {
		t.Setenv("DATABASE_URL", "postgres://user:pass@localhost:5432/doceria?sslmode=disable")
		t.Setenv("APP_ENV", "development")
		unsetEnvForTest(t, "JWT_ACCESS_SECRET")

		cfg, err := config.Load()
		require.NoError(t, err)
		assert.Equal(t, "change-me-access-secret-min-32-chars-dev-only", cfg.JWTAccessSecret)
	})
}

func TestLoad_JWTTTLValidation(t *testing.T) {
	t.Chdir(t.TempDir())

	t.Run("invalid JWT_ACCESS_TTL <= 0", func(t *testing.T) {
		for _, ttl := range []string{"0s", "-1m"} {
			t.Setenv("DATABASE_URL", "postgres://user:pass@localhost:5432/doceria?sslmode=disable")
			t.Setenv("JWT_ACCESS_TTL", ttl)

			_, err := config.Load()
			require.Error(t, err)
			assert.Contains(t, err.Error(), "invalid JWT_ACCESS_TTL")
		}
	})

	t.Run("invalid JWT_REFRESH_TTL <= 0", func(t *testing.T) {
		for _, ttl := range []string{"0s", "-1h"} {
			t.Setenv("DATABASE_URL", "postgres://user:pass@localhost:5432/doceria?sslmode=disable")
			t.Setenv("JWT_REFRESH_TTL", ttl)

			_, err := config.Load()
			require.Error(t, err)
			assert.Contains(t, err.Error(), "invalid JWT_REFRESH_TTL")
		}
	})

	t.Run("invalid duration syntax for JWT TTLs", func(t *testing.T) {
		t.Setenv("DATABASE_URL", "postgres://user:pass@localhost:5432/doceria?sslmode=disable")
		t.Setenv("JWT_ACCESS_TTL", "fifteen-minutes")
		_, err := config.Load()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "parse environment config")

		unsetEnvForTest(t, "JWT_ACCESS_TTL")
		t.Setenv("JWT_REFRESH_TTL", "seven-days")
		_, err = config.Load()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "parse environment config")
	})
}

func TestLoad_StorageValidation(t *testing.T) {
	t.Chdir(t.TempDir())

	t.Run("invalid STORAGE_MAX_UPLOAD_BYTES <= 0", func(t *testing.T) {
		for _, val := range []string{"0", "-1", "-5242880"} {
			t.Setenv("DATABASE_URL", "postgres://user:pass@localhost:5432/doceria?sslmode=disable")
			t.Setenv("STORAGE_MAX_UPLOAD_BYTES", val)

			_, err := config.Load()
			require.Error(t, err)
			assert.Contains(t, err.Error(), "invalid STORAGE_MAX_UPLOAD_BYTES")
		}

		t.Setenv("DATABASE_URL", "postgres://user:pass@localhost:5432/doceria?sslmode=disable")
		t.Setenv("STORAGE_MAX_UPLOAD_BYTES", "not-a-number")
		_, err := config.Load()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "parse environment config")
	})

	t.Run("invalid STORAGE_DRIVER rejected", func(t *testing.T) {
		for _, driver := range []string{"   ", "gcs", "azure"} {
			t.Setenv("DATABASE_URL", "postgres://user:pass@localhost:5432/doceria?sslmode=disable")
			t.Setenv("STORAGE_DRIVER", driver)

			_, err := config.Load()
			require.Error(t, err)
			assert.Contains(t, err.Error(), "invalid STORAGE_DRIVER")
		}
	})

	t.Run("local driver rejects whitespace STORAGE_LOCAL_DIR and normalizes driver case", func(t *testing.T) {
		for _, dir := range []string{"   ", "\t"} {
			t.Setenv("DATABASE_URL", "postgres://user:pass@localhost:5432/doceria?sslmode=disable")
			t.Setenv("STORAGE_DRIVER", "local")
			t.Setenv("STORAGE_LOCAL_DIR", dir)

			_, err := config.Load()
			require.Error(t, err)
			assert.Contains(t, err.Error(), "invalid STORAGE_LOCAL_DIR")
		}

		t.Setenv("DATABASE_URL", "postgres://user:pass@localhost:5432/doceria?sslmode=disable")
		t.Setenv("STORAGE_DRIVER", "  LOCAL  ")
		t.Setenv("STORAGE_LOCAL_DIR", "./my-uploads")

		cfg, err := config.Load()
		require.NoError(t, err)
		assert.Equal(t, config.StorageDriverLocal, cfg.StorageDriver)
		assert.Equal(t, "./my-uploads", cfg.StorageLocalDir)
	})

	t.Run("s3 driver validates required fields", func(t *testing.T) {
		setBaseS3 := func(t *testing.T) {
			t.Helper()
			t.Setenv("DATABASE_URL", "postgres://user:pass@localhost:5432/doceria?sslmode=disable")
			t.Setenv("STORAGE_DRIVER", "s3")
			t.Setenv("STORAGE_S3_BUCKET", "doceria-media")
			t.Setenv("STORAGE_S3_REGION", "sa-east-1")
			t.Setenv("STORAGE_S3_ACCESS_KEY", "minioadmin")
			t.Setenv("STORAGE_S3_SECRET_KEY", "minioadmin-secret")
		}

		t.Run("missing bucket", func(t *testing.T) {
			setBaseS3(t)
			t.Setenv("STORAGE_S3_BUCKET", "   ")
			_, err := config.Load()
			require.Error(t, err)
			assert.Contains(t, err.Error(), "invalid STORAGE_S3_BUCKET")
		})

		t.Run("missing region", func(t *testing.T) {
			setBaseS3(t)
			t.Setenv("STORAGE_S3_REGION", " ")
			_, err := config.Load()
			require.Error(t, err)
			assert.Contains(t, err.Error(), "invalid STORAGE_S3_REGION")
		})

		t.Run("missing access key", func(t *testing.T) {
			setBaseS3(t)
			t.Setenv("STORAGE_S3_ACCESS_KEY", "")
			_, err := config.Load()
			require.Error(t, err)
			assert.Contains(t, err.Error(), "invalid STORAGE_S3 credentials")
		})

		t.Run("missing secret key", func(t *testing.T) {
			setBaseS3(t)
			t.Setenv("STORAGE_S3_SECRET_KEY", "   ")
			_, err := config.Load()
			require.Error(t, err)
			assert.Contains(t, err.Error(), "invalid STORAGE_S3 credentials")
		})

		t.Run("valid s3 configuration with custom endpoint, public url and path style", func(t *testing.T) {
			setBaseS3(t)
			t.Setenv("STORAGE_DRIVER", "  S3 ")
			t.Setenv("STORAGE_PUBLIC_URL", "https://cdn.doceria.com")
			t.Setenv("STORAGE_S3_ENDPOINT", "http://localhost:9000")
			t.Setenv("STORAGE_S3_USE_PATH_STYLE", "true")

			cfg, err := config.Load()
			require.NoError(t, err)
			assert.Equal(t, config.StorageDriverS3, cfg.StorageDriver)
			assert.Equal(t, "doceria-media", cfg.StorageS3Bucket)
			assert.Equal(t, "sa-east-1", cfg.StorageS3Region)
			assert.Equal(t, "minioadmin", cfg.StorageS3AccessKey)
			assert.Equal(t, "minioadmin-secret", cfg.StorageS3SecretKey)
			assert.Equal(t, "https://cdn.doceria.com", cfg.StoragePublicURL)
			assert.Equal(t, "http://localhost:9000", cfg.StorageS3Endpoint)
			assert.True(t, cfg.StorageS3UsePathStyle)
		})
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
