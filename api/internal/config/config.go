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

// Config agrupa todas as configurações da aplicação carregadas via variáveis de ambiente.
type Config struct {
	AppEnv         string        `env:"APP_ENV" envDefault:"development"`
	Host           string        `env:"API_HOST" envDefault:"0.0.0.0"`
	Port           int           `env:"API_PORT" envDefault:"8080"`
	LogLevel       string        `env:"LOG_LEVEL" envDefault:"info"`
	DatabaseURL    string        `env:"DATABASE_URL,required"`
	AutoMigrate    bool          `env:"AUTO_MIGRATE" envDefault:"true"`
	AllowedOrigins []string      `env:"CORS_ALLOWED_ORIGINS" envSeparator:"," envDefault:"http://localhost:3000,http://localhost:5173"`
	RateLimitRPM   int           `env:"RATE_LIMIT_RPM" envDefault:"120"`
	ReadTimeout    time.Duration `env:"HTTP_READ_TIMEOUT" envDefault:"10s"`
	WriteTimeout   time.Duration `env:"HTTP_WRITE_TIMEOUT" envDefault:"30s"`
	IdleTimeout    time.Duration `env:"HTTP_IDLE_TIMEOUT" envDefault:"60s"`
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

	return &cfg, nil
}
