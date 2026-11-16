package db_test

import (
	"context"
	"fmt"
	"net"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"

	"github.com/joao-accioly/artisan-store/api/internal/platform/db"
)

func tableExists(t *testing.T, ctx context.Context, pool *pgxpool.Pool, tableName string) bool {
	t.Helper()
	var exists bool
	err := pool.QueryRow(ctx, "SELECT to_regclass($1) IS NOT NULL", "public."+tableName).Scan(&exists)
	require.NoError(t, err)
	return exists
}

func assertCatalogSeedSingleton(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	var (
		count         int
		id            string
		storeName     string
		manualStatus  string
		ttlMinutes    int
		autoHideStock bool
	)
	err := pool.QueryRow(ctx, "SELECT COUNT(*) FROM store_settings").Scan(&count)
	require.NoError(t, err)
	assert.Equal(t, 1, count)

	err = pool.QueryRow(
		ctx,
		"SELECT id::text, store_name, manual_status, pix_reservation_ttl_minutes, auto_hide_out_of_stock FROM store_settings WHERE singleton_key = TRUE",
	).Scan(&id, &storeName, &manualStatus, &ttlMinutes, &autoHideStock)
	require.NoError(t, err)
	assert.Equal(t, "01930000-0000-7000-8000-000000000001", id)
	assert.Equal(t, "Artisan Store Doceria", storeName)
	assert.Equal(t, "auto", manualStatus)
	assert.Equal(t, 15, ttlMinutes)
	assert.True(t, autoHideStock)
}

func TestNewPool_InvalidURL(t *testing.T) {
	tests := []struct {
		name string
		url  string
	}{
		{name: "malformed scheme", url: "://invalid-url"},
		{name: "invalid sslmode", url: "postgres://user:pass@localhost:5432/doceria?sslmode=invalid"},
		{name: "invalid percent escape", url: "postgres://user:pass@localhost:5432/doceria%zz"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			pool, err := db.NewPool(context.Background(), tc.url)
			assert.Nil(t, pool)
			require.Error(t, err)
			assert.Contains(t, err.Error(), "parse database url")
		})
	}
}

func TestNewPool_UnreachableDatabase(t *testing.T) {
	t.Run("connection refused on unused port", func(t *testing.T) {
		ln, err := net.Listen("tcp", "127.0.0.1:0")
		require.NoError(t, err)
		addr := ln.Addr().String()
		require.NoError(t, ln.Close())

		ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
		defer cancel()

		dsn := fmt.Sprintf("postgres://doceria:secret@%s/doceria?sslmode=disable", addr)
		pool, err := db.NewPool(ctx, dsn)
		assert.Nil(t, pool)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "ping database")
	})

	t.Run("canceled context", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		pool, err := db.NewPool(ctx, "postgres://doceria:secret@127.0.0.1:5432/doceria?sslmode=disable")
		assert.Nil(t, pool)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "ping database")
	})
}

func TestPostgres_Integration(t *testing.T) {
	if testing.Short() {
		t.Skip("pulando teste de integração com Docker no modo -short")
	}

	ctx := context.Background()
	pgContainer, err := postgres.Run(ctx,
		"postgres:16-alpine",
		postgres.WithDatabase("doceria_db_test"),
		postgres.WithUsername("doceria"),
		postgres.WithPassword("doceria_test_password"),
		testcontainers.WithWaitStrategy(
			wait.ForLog("database system is ready to accept connections").
				WithOccurrence(2).
				WithStartupTimeout(30*time.Second),
		),
	)
	require.NoError(t, err)

	t.Cleanup(func() {
		require.NoError(t, pgContainer.Terminate(context.Background()))
	})

	connStr, err := pgContainer.ConnectionString(ctx, "sslmode=disable")
	require.NoError(t, err)

	pool, err := db.NewPool(ctx, connStr)
	require.NoError(t, err)
	defer pool.Close()

	require.NoError(t, pool.Ping(ctx))

	// Aplica todas as migrations (00001_init, 00002_users, 00003_catalog) e valida estrutura e seeds.
	require.NoError(t, db.MigrateUp(ctx, pool))

	var value string
	err = pool.QueryRow(ctx, "SELECT value FROM system_metadata WHERE key = 'schema_initialized'").Scan(&value)
	require.NoError(t, err)
	assert.Equal(t, "true", value)
	assert.True(t, tableExists(t, ctx, pool, "system_metadata"))
	assert.True(t, tableExists(t, ctx, pool, "users"))
	assert.True(t, tableExists(t, ctx, pool, "refresh_tokens"))
	assert.True(t, tableExists(t, ctx, pool, "store_settings"))
	assert.True(t, tableExists(t, ctx, pool, "categories"))
	assert.True(t, tableExists(t, ctx, pool, "products"))
	assert.True(t, tableExists(t, ctx, pool, "product_variants"))
	assertCatalogSeedSingleton(t, ctx, pool)

	// Reaplicação idempotente.
	require.NoError(t, db.MigrateUp(ctx, pool))
	assertCatalogSeedSingleton(t, ctx, pool)

	// Passo 1 MigrateDown: reverte apenas 00003_catalog.sql.
	require.NoError(t, db.MigrateDown(ctx, pool))
	assert.False(t, tableExists(t, ctx, pool, "product_variants"))
	assert.False(t, tableExists(t, ctx, pool, "products"))
	assert.False(t, tableExists(t, ctx, pool, "categories"))
	assert.False(t, tableExists(t, ctx, pool, "store_settings"))
	assert.True(t, tableExists(t, ctx, pool, "refresh_tokens"))
	assert.True(t, tableExists(t, ctx, pool, "users"))
	assert.True(t, tableExists(t, ctx, pool, "system_metadata"))

	// Passo 2 MigrateDown: reverte apenas 00002_users.sql.
	require.NoError(t, db.MigrateDown(ctx, pool))
	assert.False(t, tableExists(t, ctx, pool, "refresh_tokens"))
	assert.False(t, tableExists(t, ctx, pool, "users"))
	assert.True(t, tableExists(t, ctx, pool, "system_metadata"))

	// Passo 3 MigrateDown: reverte 00001_init.sql.
	require.NoError(t, db.MigrateDown(ctx, pool))
	assert.False(t, tableExists(t, ctx, pool, "system_metadata"))

	// Sobe novamente para garantir reversibilidade completa de todas as migrations e restauração do seed.
	require.NoError(t, db.MigrateUp(ctx, pool))
	assert.True(t, tableExists(t, ctx, pool, "system_metadata"))
	assert.True(t, tableExists(t, ctx, pool, "users"))
	assert.True(t, tableExists(t, ctx, pool, "refresh_tokens"))
	assert.True(t, tableExists(t, ctx, pool, "store_settings"))
	assert.True(t, tableExists(t, ctx, pool, "categories"))
	assert.True(t, tableExists(t, ctx, pool, "products"))
	assert.True(t, tableExists(t, ctx, pool, "product_variants"))
	assertCatalogSeedSingleton(t, ctx, pool)

	// Pool fechado deve falhar nas operações de migration.
	pool.Close()
	err = db.MigrateUp(ctx, pool)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "execute migrations")

	err = db.MigrateDown(ctx, pool)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "execute migrations")
}
