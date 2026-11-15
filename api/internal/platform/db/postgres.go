package db

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"

	"github.com/joao-accioly/artisan-store/api/db/migrations"
)

// NewPool abre e valida um pool de conexões PostgreSQL usando pgx/v5.
func NewPool(ctx context.Context, databaseURL string) (*pgxpool.Pool, error) {
	cfg, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		return nil, fmt.Errorf("parse database url: %w", err)
	}

	cfg.MaxConns = 20
	cfg.MinConns = 2
	cfg.MaxConnLifetime = 30 * time.Minute
	cfg.MaxConnIdleTime = 5 * time.Minute
	cfg.HealthCheckPeriod = 30 * time.Second

	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("create pgx pool: %w", err)
	}

	pingCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	if err := pool.Ping(pingCtx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ping database: %w", err)
	}

	return pool, nil
}

// MigrateUp aplica todas as migrations SQL pendentes via goose usando o FS embutido.
func MigrateUp(ctx context.Context, pool *pgxpool.Pool) error {
	return runGoose(ctx, pool, func(provider *goose.Provider) error {
		_, err := provider.Up(ctx)
		return err
	})
}

// MigrateDown reverte a última migration aplicada via goose.
func MigrateDown(ctx context.Context, pool *pgxpool.Pool) error {
	return runGoose(ctx, pool, func(provider *goose.Provider) error {
		_, err := provider.Down(ctx)
		return err
	})
}

func runGoose(_ context.Context, pool *pgxpool.Pool, fn func(*goose.Provider) error) (err error) {
	sqlDB := stdlib.OpenDBFromPool(pool)
	defer func() {
		if closeErr := sqlDB.Close(); closeErr != nil && err == nil {
			err = fmt.Errorf("close migration db: %w", closeErr)
		}
	}()

	provider, err := goose.NewProvider(goose.DialectPostgres, sqlDB, migrations.FS)
	if err != nil {
		return fmt.Errorf("create goose provider: %w", err)
	}

	if err := fn(provider); err != nil {
		return fmt.Errorf("execute migrations: %w", err)
	}

	return nil
}
