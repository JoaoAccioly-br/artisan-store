package db_test

import (
	"context"
	"fmt"
	"net"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"

	"github.com/joao-accioly/artisan-store/api/internal/platform/db"
)

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

	// Aplica migrations e valida seed inicial.
	require.NoError(t, db.MigrateUp(ctx, pool))

	var value string
	err = pool.QueryRow(ctx, "SELECT value FROM system_metadata WHERE key = 'schema_initialized'").Scan(&value)
	require.NoError(t, err)
	assert.Equal(t, "true", value)

	// Reaplicação idempotente.
	require.NoError(t, db.MigrateUp(ctx, pool))

	// Reverte migration e valida remoção da tabela.
	require.NoError(t, db.MigrateDown(ctx, pool))

	var exists bool
	err = pool.QueryRow(ctx, "SELECT to_regclass('public.system_metadata') IS NOT NULL").Scan(&exists)
	require.NoError(t, err)
	assert.False(t, exists)

	// Sobe novamente para garantir reversibilidade completa.
	require.NoError(t, db.MigrateUp(ctx, pool))

	// Pool fechado deve falhar nas operações de migration.
	pool.Close()
	err = db.MigrateUp(ctx, pool)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "execute migrations")

	err = db.MigrateDown(ctx, pool)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "execute migrations")
}
