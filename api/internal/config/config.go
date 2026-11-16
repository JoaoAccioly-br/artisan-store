package config

import (
	"errors"
	"fmt"
	"io/fs"
	"strings"
	"time"

	"github.com/caarlos0/env/v11"
	"github.com/joho/godotenv"
)

const defaultDevJWTSecret = "change-me-access-secret-min-32-chars-dev-only"

// StorageDriver identifica o backend de armazenamento de arquivos configurado.
const (
	StorageDriverLocal = "local"
	StorageDriverS3    = "s3"
)

// Config agrupa todas as configurações da aplicação carregadas via variáveis de ambiente.
type Config struct {
	AppEnv                string        `env:"APP_ENV" envDefault:"development"`
	Host                  string        `env:"API_HOST" envDefault:"0.0.0.0"`
	Port                  int           `env:"API_PORT" envDefault:"8080"`
	LogLevel              string        `env:"LOG_LEVEL" envDefault:"info"`
	DatabaseURL           string        `env:"DATABASE_URL,required"`
	AutoMigrate           bool          `env:"AUTO_MIGRATE" envDefault:"true"`
	AllowedOrigins        []string      `env:"CORS_ALLOWED_ORIGINS" envSeparator:"," envDefault:"http://localhost:3000,http://localhost:5173"`
	RateLimitRPM          int           `env:"RATE_LIMIT_RPM" envDefault:"120"`
	ReadTimeout           time.Duration `env:"HTTP_READ_TIMEOUT" envDefault:"10s"`
	WriteTimeout          time.Duration `env:"HTTP_WRITE_TIMEOUT" envDefault:"30s"`
	IdleTimeout           time.Duration `env:"HTTP_IDLE_TIMEOUT" envDefault:"60s"`
	JWTAccessSecret       string        `env:"JWT_ACCESS_SECRET" envDefault:"change-me-access-secret-min-32-chars-dev-only"`
	JWTAccessTTL          time.Duration `env:"JWT_ACCESS_TTL" envDefault:"15m"`
	JWTRefreshTTL         time.Duration `env:"JWT_REFRESH_TTL" envDefault:"168h"`
	SeedAdminName         string        `env:"SEED_ADMIN_NAME" envDefault:"Administrador"`
	SeedAdminEmail        string        `env:"SEED_ADMIN_EMAIL" envDefault:""`
	SeedAdminPassword     string        `env:"SEED_ADMIN_PASSWORD" envDefault:""`
	StorageDriver         string        `env:"STORAGE_DRIVER" envDefault:"local"`
	StorageLocalDir       string        `env:"STORAGE_LOCAL_DIR" envDefault:"./uploads"`
	StoragePublicURL      string        `env:"STORAGE_PUBLIC_URL" envDefault:"http://localhost:8080/uploads"`
	StorageMaxUploadBytes int64         `env:"STORAGE_MAX_UPLOAD_BYTES" envDefault:"5242880"`
	StorageS3Endpoint     string        `env:"STORAGE_S3_ENDPOINT" envDefault:""`
	StorageS3Region       string        `env:"STORAGE_S3_REGION" envDefault:"us-east-1"`
	StorageS3Bucket       string        `env:"STORAGE_S3_BUCKET" envDefault:""`
	StorageS3AccessKey    string        `env:"STORAGE_S3_ACCESS_KEY" envDefault:""`
	StorageS3SecretKey    string        `env:"STORAGE_S3_SECRET_KEY" envDefault:""`
	StorageS3UsePathStyle bool          `env:"STORAGE_S3_USE_PATH_STYLE" envDefault:"false"`
}

// Address retorna o endereço host:port no formato aceito por http.Server.
func (c Config) Address() string {
	return fmt.Sprintf("%s:%d", c.Host, c.Port)
}

// IsProduction indica se a aplicação está rodando em ambiente produtivo.
func (c Config) IsProduction() bool {
	return strings.EqualFold(c.AppEnv, "production")
}

// Load lê arquivos .env locais (se existirem) e faz o parse das variáveis de ambiente.
func Load() (*Config, error) {
	// Em desenvolvimento o .env pode estar na raiz do monorepo ou dentro de api/.
	for _, path := range []string{".env", "../.env"} {
		if err := godotenv.Load(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return nil, fmt.Errorf("load %s: %w", path, err)
		}
	}

	var cfg Config
	if err := env.Parse(&cfg); err != nil {
		return nil, fmt.Errorf("parse environment config: %w", err)
	}

	if cfg.Port <= 0 || cfg.Port > 65535 {
		return nil, fmt.Errorf("invalid API_PORT: %d", cfg.Port)
	}

	if cfg.RateLimitRPM <= 0 {
		return nil, fmt.Errorf("invalid RATE_LIMIT_RPM: %d", cfg.RateLimitRPM)
	}

	if len(strings.TrimSpace(cfg.JWTAccessSecret)) < 32 {
		return nil, fmt.Errorf("invalid JWT_ACCESS_SECRET: must be at least 32 characters")
	}

	if cfg.IsProduction() && cfg.JWTAccessSecret == defaultDevJWTSecret {
		return nil, fmt.Errorf("invalid JWT_ACCESS_SECRET: default development secret cannot be used in production")
	}

	if cfg.JWTAccessTTL <= 0 {
		return nil, fmt.Errorf("invalid JWT_ACCESS_TTL: %s", cfg.JWTAccessTTL)
	}

	if cfg.JWTRefreshTTL <= 0 {
		return nil, fmt.Errorf("invalid JWT_REFRESH_TTL: %s", cfg.JWTRefreshTTL)
	}

	if cfg.StorageMaxUploadBytes <= 0 {
		return nil, fmt.Errorf("invalid STORAGE_MAX_UPLOAD_BYTES: %d", cfg.StorageMaxUploadBytes)
	}

	cfg.StorageDriver = strings.ToLower(strings.TrimSpace(cfg.StorageDriver))
	switch cfg.StorageDriver {
	case StorageDriverLocal:
		if strings.TrimSpace(cfg.StorageLocalDir) == "" {
			return nil, fmt.Errorf("invalid STORAGE_LOCAL_DIR: must not be empty")
		}
	case StorageDriverS3:
		if strings.TrimSpace(cfg.StorageS3Bucket) == "" {
			return nil, fmt.Errorf("invalid STORAGE_S3_BUCKET: required when STORAGE_DRIVER=s3")
		}
		if strings.TrimSpace(cfg.StorageS3Region) == "" {
			return nil, fmt.Errorf("invalid STORAGE_S3_REGION: required when STORAGE_DRIVER=s3")
		}
		if strings.TrimSpace(cfg.StorageS3AccessKey) == "" || strings.TrimSpace(cfg.StorageS3SecretKey) == "" {
			return nil, fmt.Errorf("invalid STORAGE_S3 credentials: access key and secret key are required when STORAGE_DRIVER=s3")
		}
	default:
		return nil, fmt.Errorf("invalid STORAGE_DRIVER %q: must be local or s3", cfg.StorageDriver)
	}

	return &cfg, nil
}
