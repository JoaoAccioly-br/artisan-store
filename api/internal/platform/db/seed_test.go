package db_test

import (
	"bytes"
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"

	"github.com/joao-accioly/artisan-store/api/internal/config"
	"github.com/joao-accioly/artisan-store/api/internal/domain"
	"github.com/joao-accioly/artisan-store/api/internal/platform/db"
	"github.com/joao-accioly/artisan-store/api/internal/platform/logger"
	"github.com/joao-accioly/artisan-store/api/internal/repository/sqlc"
	"github.com/joao-accioly/artisan-store/api/internal/security"
)

func TestSeedAdmin_Integration(t *testing.T) {
	if testing.Short() {
		t.Skip("pulando teste de integração com Docker no modo -short")
	}

	ctx := context.Background()
	pgContainer, err := postgres.Run(ctx,
		"postgres:16-alpine",
		postgres.WithDatabase("doceria_seed_test"),
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

	require.NoError(t, db.MigrateUp(ctx, pool))

	hasher := security.NewArgon2idHasher(security.Argon2idParams{
		Memory:      8 * 1024,
		Iterations:  1,
		Parallelism: 1,
		SaltLength:  16,
		KeyLength:   32,
	})
	queries := sqlc.New(pool)

	cleanUsers := func(t *testing.T) {
		t.Helper()
		_, execErr := pool.Exec(ctx, "TRUNCATE TABLE users CASCADE")
		require.NoError(t, execErr)
	}

	t.Run("ignores when email or password is empty", func(t *testing.T) {
		cleanUsers(t)

		err := db.SeedAdmin(ctx, pool, &config.Config{
			SeedAdminName:     "Admin",
			SeedAdminEmail:    "   ",
			SeedAdminPassword: "Password#123",
		}, hasher, nil)
		require.NoError(t, err)

		err = db.SeedAdmin(ctx, pool, &config.Config{
			SeedAdminName:     "Admin",
			SeedAdminEmail:    "admin@doceria.com",
			SeedAdminPassword: "",
		}, hasher, nil)
		require.NoError(t, err)

		count, countErr := queries.CountUsers(ctx)
		require.NoError(t, countErr)
		assert.Zero(t, count)
	})

	t.Run("validates password length >= 8 chars", func(t *testing.T) {
		cleanUsers(t)

		err := db.SeedAdmin(ctx, pool, &config.Config{
			SeedAdminName:     "Admin",
			SeedAdminEmail:    "admin@doceria.com",
			SeedAdminPassword: "1234567",
		}, hasher, nil)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "at least 8 characters")

		count, countErr := queries.CountUsers(ctx)
		require.NoError(t, countErr)
		assert.Zero(t, count)
	})

	t.Run("creates admin when empty, defaults blank name, and is idempotent", func(t *testing.T) {
		cleanUsers(t)

		var logBuf bytes.Buffer
		log := logger.NewWithWriter(&logBuf, "development", "info")

		cfg := &config.Config{
			SeedAdminName:     "   ",
			SeedAdminEmail:    "  Admin@Doceria.COM ",
			SeedAdminPassword: "AdminPassword#123",
		}
		require.NoError(t, db.SeedAdmin(ctx, pool, cfg, hasher, log))
		assert.Contains(t, logBuf.String(), "seed admin user created")

		count, countErr := queries.CountUsers(ctx)
		require.NoError(t, countErr)
		assert.Equal(t, int64(1), count)

		created, getErr := queries.GetUserByEmail(ctx, "admin@doceria.com")
		require.NoError(t, getErr)
		assert.Equal(t, "Administrador", created.Name)
		assert.Equal(t, "admin@doceria.com", created.Email)
		assert.Equal(t, string(domain.RoleAdmin), created.Role)
		assert.True(t, created.Active)

		ok, verifyErr := hasher.Verify("AdminPassword#123", created.PasswordHash)
		require.NoError(t, verifyErr)
		assert.True(t, ok)

		// Chamada repetida deve ser idempotente e não criar duplicata.
		require.NoError(t, db.SeedAdmin(ctx, pool, &config.Config{
			SeedAdminName:     "Outro Nome",
			SeedAdminEmail:    "other@doceria.com",
			SeedAdminPassword: "123",
		}, hasher, nil))

		count, countErr = queries.CountUsers(ctx)
		require.NoError(t, countErr)
		assert.Equal(t, int64(1), count)
	})

	t.Run("does not create admin when another non-admin user exists", func(t *testing.T) {
		cleanUsers(t)

		uid, idErr := uuid.NewV7()
		require.NoError(t, idErr)
		hash, hashErr := hasher.Hash("SenhaAtendente#1")
		require.NoError(t, hashErr)

		_, createErr := queries.CreateUser(ctx, sqlc.CreateUserParams{
			ID:           uid,
			Name:         "Atendente",
			Email:        "atendente@doceria.com",
			PasswordHash: hash,
			Role:         string(domain.RoleAtendente),
			Active:       true,
		})
		require.NoError(t, createErr)

		require.NoError(t, db.SeedAdmin(ctx, pool, &config.Config{
			SeedAdminName:     "Admin",
			SeedAdminEmail:    "admin@doceria.com",
			SeedAdminPassword: "AdminPassword#123",
		}, hasher, nil))

		count, countErr := queries.CountUsers(ctx)
		require.NoError(t, countErr)
		assert.Equal(t, int64(1), count)
	})

	t.Run("returns error when database pool is closed", func(t *testing.T) {
		pool.Close()
		err := db.SeedAdmin(ctx, pool, &config.Config{
			SeedAdminName:     "Admin",
			SeedAdminEmail:    "admin@doceria.com",
			SeedAdminPassword: "AdminPassword#123",
		}, hasher, nil)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "count users for seed")
	})
}
