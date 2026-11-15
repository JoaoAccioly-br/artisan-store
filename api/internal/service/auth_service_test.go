package service_test

import (
	"bytes"
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"

	"github.com/joao-accioly/artisan-store/api/internal/domain"
	"github.com/joao-accioly/artisan-store/api/internal/platform/db"
	"github.com/joao-accioly/artisan-store/api/internal/platform/logger"
	"github.com/joao-accioly/artisan-store/api/internal/repository/sqlc"
	"github.com/joao-accioly/artisan-store/api/internal/security"
	"github.com/joao-accioly/artisan-store/api/internal/service"
)

type failingHasher struct{}

func (f failingHasher) Hash(string) (string, error) {
	return "", errors.New("hash failure")
}

func (f failingHasher) Verify(string, string) (bool, error) {
	return false, errors.New("verify failure")
}

func setupServicePostgres(t *testing.T, dbName string) *pgxpool.Pool {
	t.Helper()
	ctx := context.Background()

	pgContainer, err := postgres.Run(ctx,
		"postgres:16-alpine",
		postgres.WithDatabase(dbName),
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
	t.Cleanup(pool.Close)

	require.NoError(t, db.MigrateUp(ctx, pool))
	return pool
}

func testFastHasher() *security.Argon2idHasher {
	return security.NewArgon2idHasher(security.Argon2idParams{
		Memory:      8 * 1024,
		Iterations:  1,
		Parallelism: 1,
		SaltLength:  16,
		KeyLength:   32,
	})
}

func TestNewAuthService_HasherError(t *testing.T) {
	jwtMgr, err := security.NewJWTManager("test-secret-for-auth-service-unit-32chars", 15*time.Minute)
	require.NoError(t, err)

	svc, err := service.NewAuthService(nil, failingHasher{}, jwtMgr, 24*time.Hour, nil)
	assert.Nil(t, svc)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "generate dummy hash")
}

func TestAuthService_Integration(t *testing.T) {
	if testing.Short() {
		t.Skip("pulando teste de integração com Docker no modo -short")
	}

	ctx := context.Background()
	pool := setupServicePostgres(t, "doceria_auth_service_test")
	hasher := testFastHasher()
	jwtMgr, err := security.NewJWTManager("auth-service-integration-secret-32chars", 15*time.Minute)
	require.NoError(t, err)

	var logBuf bytes.Buffer
	log := logger.NewWithWriter(&logBuf, "development", "debug")

	authSvc, err := service.NewAuthService(pool, hasher, jwtMgr, 7*24*time.Hour, log)
	require.NoError(t, err)
	userSvc := service.NewUserService(pool, hasher)
	queries := sqlc.New(pool)

	cleanTables := func(t *testing.T) {
		t.Helper()
		_, execErr := pool.Exec(ctx, "TRUNCATE TABLE refresh_tokens, users CASCADE")
		require.NoError(t, execErr)
		logBuf.Reset()
	}

	createUser := func(t *testing.T, name, email, password string, role domain.Role, active bool) *domain.User {
		t.Helper()
		u, createErr := userSvc.Create(ctx, service.CreateUserInput{
			Name:     name,
			Email:    email,
			Password: password,
			Role:     role,
		})
		require.NoError(t, createErr)
		if !active {
			f := false
			u, createErr = userSvc.Update(ctx, uuid.Nil, u.ID, service.UpdateUserInput{Active: &f})
			require.NoError(t, createErr)
		}
		return u
	}

	t.Run("Login scenarios", func(t *testing.T) {
		cleanTables(t)
		activeUser := createUser(t, "Maria", "maria@doceria.com", "SenhaForte#123", domain.RoleAdmin, true)
		createUser(t, "Inativo", "inativo@doceria.com", "SenhaForte#123", domain.RoleAtendente, false)

		t.Run("success with case-insensitive and padded email", func(t *testing.T) {
			tokens, user, loginErr := authSvc.Login(ctx, "  MARIA@Doceria.COM  ", "SenhaForte#123")
			require.NoError(t, loginErr)
			require.NotNil(t, tokens)
			require.NotNil(t, user)

			assert.Equal(t, activeUser.ID, user.ID)
			assert.Equal(t, "maria@doceria.com", user.Email)
			assert.Equal(t, "Bearer", tokens.TokenType)
			assert.Equal(t, 900, tokens.ExpiresIn)
			assert.NotEmpty(t, tokens.AccessToken)
			assert.NotEmpty(t, tokens.RefreshToken)

			claims, parseErr := jwtMgr.ParseAccessToken(tokens.AccessToken)
			require.NoError(t, parseErr)
			assert.Equal(t, activeUser.ID, claims.UserID)
			assert.Equal(t, domain.RoleAdmin, claims.Role)

			rt, rtErr := queries.GetRefreshTokenByHash(ctx, security.HashToken(tokens.RefreshToken))
			require.NoError(t, rtErr)
			assert.Equal(t, activeUser.ID, rt.UserID)
			assert.False(t, rt.RevokedAt.Valid)
		})

		t.Run("wrong password returns 401", func(t *testing.T) {
			tokens, user, loginErr := authSvc.Login(ctx, "maria@doceria.com", "SenhaErrada#123")
			assert.Nil(t, tokens)
			assert.Nil(t, user)
			domErr, ok := domain.AsDomainError(loginErr)
			require.True(t, ok)
			assert.Equal(t, domain.CodeUnauthorized, domErr.Code)
		})

		t.Run("non-existent email returns 401", func(t *testing.T) {
			tokens, user, loginErr := authSvc.Login(ctx, "naoexiste@doceria.com", "SenhaForte#123")
			assert.Nil(t, tokens)
			assert.Nil(t, user)
			domErr, ok := domain.AsDomainError(loginErr)
			require.True(t, ok)
			assert.Equal(t, domain.CodeUnauthorized, domErr.Code)
		})

		t.Run("inactive user returns 401", func(t *testing.T) {
			tokens, user, loginErr := authSvc.Login(ctx, "inativo@doceria.com", "SenhaForte#123")
			assert.Nil(t, tokens)
			assert.Nil(t, user)
			domErr, ok := domain.AsDomainError(loginErr)
			require.True(t, ok)
			assert.Equal(t, domain.CodeUnauthorized, domErr.Code)
			assert.Contains(t, domErr.Message, "inativo")
		})

		t.Run("empty email or password returns 400 validation error", func(t *testing.T) {
			for _, creds := range []struct{ email, pass string }{
				{"", "SenhaForte#123"},
				{"   ", "SenhaForte#123"},
				{"maria@doceria.com", ""},
				{"", ""},
			} {
				_, _, loginErr := authSvc.Login(ctx, creds.email, creds.pass)
				domErr, ok := domain.AsDomainError(loginErr)
				require.True(t, ok)
				assert.Equal(t, domain.CodeValidation, domErr.Code)
			}
		})
	})

	t.Run("Refresh rotation and reuse detection", func(t *testing.T) {
		cleanTables(t)
		user := createUser(t, "Maria", "maria@doceria.com", "SenhaForte#123", domain.RoleAdmin, true)

		t.Run("empty refresh token returns 400", func(t *testing.T) {
			_, refErr := authSvc.Refresh(ctx, "   ")
			domErr, ok := domain.AsDomainError(refErr)
			require.True(t, ok)
			assert.Equal(t, domain.CodeValidation, domErr.Code)
		})

		t.Run("unknown refresh token returns 401", func(t *testing.T) {
			_, refErr := authSvc.Refresh(ctx, "rt_unknown_token")
			domErr, ok := domain.AsDomainError(refErr)
			require.True(t, ok)
			assert.Equal(t, domain.CodeUnauthorized, domErr.Code)
		})

		t.Run("valid rotation and reuse detection revokes entire family without affecting other session", func(t *testing.T) {
			sess1, _, loginErr := authSvc.Login(ctx, user.Email, "SenhaForte#123")
			require.NoError(t, loginErr)

			// Segunda sessão separada do mesmo usuário (outra family_id).
			sess2, _, loginErr := authSvc.Login(ctx, user.Email, "SenhaForte#123")
			require.NoError(t, loginErr)

			// Rotaciona token T1 -> T2.
			rotated1, refErr := authSvc.Refresh(ctx, sess1.RefreshToken)
			require.NoError(t, refErr)
			assert.NotEqual(t, sess1.RefreshToken, rotated1.RefreshToken)

			oldRT, dbErr := queries.GetRefreshTokenByHash(ctx, security.HashToken(sess1.RefreshToken))
			require.NoError(t, dbErr)
			newRT, dbErr := queries.GetRefreshTokenByHash(ctx, security.HashToken(rotated1.RefreshToken))
			require.NoError(t, dbErr)

			assert.True(t, oldRT.RevokedAt.Valid)
			assert.True(t, oldRT.ReplacedByID.Valid)
			assert.Equal(t, newRT.ID, uuid.UUID(oldRT.ReplacedByID.Bytes))
			assert.Equal(t, oldRT.FamilyID, newRT.FamilyID)
			assert.False(t, newRT.RevokedAt.Valid)

			// Rotação legítima T2 -> T3 funciona na mesma família.
			rotated2, refErr := authSvc.Refresh(ctx, rotated1.RefreshToken)
			require.NoError(t, refErr)
			t3RT, dbErr := queries.GetRefreshTokenByHash(ctx, security.HashToken(rotated2.RefreshToken))
			require.NoError(t, dbErr)
			assert.Equal(t, oldRT.FamilyID, t3RT.FamilyID)

			// Reuse detection: reapresentar T1 já revogado revoga T3 (toda a família)!
			_, reuseErr := authSvc.Refresh(ctx, sess1.RefreshToken)
			domErr, ok := domain.AsDomainError(reuseErr)
			require.True(t, ok)
			assert.Equal(t, domain.CodeUnauthorized, domErr.Code)
			assert.Contains(t, logBuf.String(), "refresh token reuse detected")

			// T3 agora deve estar revogado e falhar.
			t3RTCheck, dbErr := queries.GetRefreshTokenByHash(ctx, security.HashToken(rotated2.RefreshToken))
			require.NoError(t, dbErr)
			assert.True(t, t3RTCheck.RevokedAt.Valid)

			_, t3Err := authSvc.Refresh(ctx, rotated2.RefreshToken)
			domErr, ok = domain.AsDomainError(t3Err)
			require.True(t, ok)
			assert.Equal(t, domain.CodeUnauthorized, domErr.Code)

			// Sessão 2 (família diferente) continua válida!
			sess2Rotated, sess2Err := authSvc.Refresh(ctx, sess2.RefreshToken)
			require.NoError(t, sess2Err)
			assert.NotEmpty(t, sess2Rotated.RefreshToken)
		})

		t.Run("expired refresh token fails with 401 and gets revoked", func(t *testing.T) {
			sess, _, loginErr := authSvc.Login(ctx, user.Email, "SenhaForte#123")
			require.NoError(t, loginErr)

			hash := security.HashToken(sess.RefreshToken)
			_, execErr := pool.Exec(ctx, "UPDATE refresh_tokens SET expires_at = NOW() - INTERVAL '1 minute' WHERE token_hash = $1", hash)
			require.NoError(t, execErr)

			_, refErr := authSvc.Refresh(ctx, sess.RefreshToken)
			domErr, ok := domain.AsDomainError(refErr)
			require.True(t, ok)
			assert.Equal(t, domain.CodeUnauthorized, domErr.Code)
			assert.Contains(t, domErr.Message, "expirado")

			rt, dbErr := queries.GetRefreshTokenByHash(ctx, hash)
			require.NoError(t, dbErr)
			assert.True(t, rt.RevokedAt.Valid)
		})

		t.Run("refresh for deactivated user fails with 401 and revokes family", func(t *testing.T) {
			target := createUser(t, "Carlos", "carlos@doceria.com", "SenhaForte#123", domain.RoleAtendente, true)
			sess, _, loginErr := authSvc.Login(ctx, target.Email, "SenhaForte#123")
			require.NoError(t, loginErr)

			_, execErr := pool.Exec(ctx, "UPDATE users SET active = FALSE WHERE id = $1", target.ID)
			require.NoError(t, execErr)

			_, refErr := authSvc.Refresh(ctx, sess.RefreshToken)
			domErr, ok := domain.AsDomainError(refErr)
			require.True(t, ok)
			assert.Equal(t, domain.CodeUnauthorized, domErr.Code)
			assert.Contains(t, domErr.Message, "inativo")

			rt, dbErr := queries.GetRefreshTokenByHash(ctx, security.HashToken(sess.RefreshToken))
			require.NoError(t, dbErr)
			assert.True(t, rt.RevokedAt.Valid)
		})
	})

	t.Run("Concurrent Refresh race (FOR UPDATE)", func(t *testing.T) {
		cleanTables(t)
		createUser(t, "Concorrente", "concorrente@doceria.com", "SenhaForte#123", domain.RoleAdmin, true)

		sess, _, loginErr := authSvc.Login(ctx, "concorrente@doceria.com", "SenhaForte#123")
		require.NoError(t, loginErr)

		const workers = 2
		start := make(chan struct{})
		type result struct {
			tokens *service.AuthTokens
			err    error
		}
		results := make([]result, workers)

		var wg sync.WaitGroup
		wg.Add(workers)
		for i := 0; i < workers; i++ {
			go func(idx int) {
				defer wg.Done()
				<-start
				toks, refErr := authSvc.Refresh(ctx, sess.RefreshToken)
				results[idx] = result{tokens: toks, err: refErr}
			}(i)
		}
		close(start)
		wg.Wait()

		successes := 0
		unauthorizeds := 0
		for _, r := range results {
			if r.err == nil {
				successes++
				assert.NotNil(t, r.tokens)
				assert.NotEmpty(t, r.tokens.RefreshToken)
				continue
			}
			domErr, ok := domain.AsDomainError(r.err)
			require.True(t, ok, "expected domain error, got %v", r.err)
			assert.Equal(t, domain.CodeUnauthorized, domErr.Code)
			unauthorizeds++
		}

		assert.Equal(t, 1, successes, "exactly one concurrent refresh must succeed")
		assert.Equal(t, 1, unauthorizeds, "the losing concurrent refresh must fail with 401")

		// O token original deve estar revogado com replaced_by_id preenchido.
		origRT, dbErr := queries.GetRefreshTokenByHash(ctx, security.HashToken(sess.RefreshToken))
		require.NoError(t, dbErr)
		assert.True(t, origRT.RevokedAt.Valid)
		assert.True(t, origRT.ReplacedByID.Valid)
	})

	t.Run("Logout scenarios", func(t *testing.T) {
		cleanTables(t)
		u1 := createUser(t, "User 1", "u1@doceria.com", "SenhaForte#123", domain.RoleAdmin, true)
		u2 := createUser(t, "User 2", "u2@doceria.com", "SenhaForte#123", domain.RoleAtendente, true)

		sess1, _, loginErr := authSvc.Login(ctx, u1.Email, "SenhaForte#123")
		require.NoError(t, loginErr)
		sess2, _, loginErr := authSvc.Login(ctx, u2.Email, "SenhaForte#123")
		require.NoError(t, loginErr)

		t.Run("empty refresh token returns 400", func(t *testing.T) {
			err := authSvc.Logout(ctx, u1.ID, " ")
			domErr, ok := domain.AsDomainError(err)
			require.True(t, ok)
			assert.Equal(t, domain.CodeValidation, domErr.Code)
		})

		t.Run("logout another user's token returns 403 and keeps token active", func(t *testing.T) {
			err := authSvc.Logout(ctx, u1.ID, sess2.RefreshToken)
			domErr, ok := domain.AsDomainError(err)
			require.True(t, ok)
			assert.Equal(t, domain.CodeForbidden, domErr.Code)

			rt2, dbErr := queries.GetRefreshTokenByHash(ctx, security.HashToken(sess2.RefreshToken))
			require.NoError(t, dbErr)
			assert.False(t, rt2.RevokedAt.Valid)
		})

		t.Run("valid logout revokes token and is idempotent", func(t *testing.T) {
			require.NoError(t, authSvc.Logout(ctx, u1.ID, sess1.RefreshToken))

			rt1, dbErr := queries.GetRefreshTokenByHash(ctx, security.HashToken(sess1.RefreshToken))
			require.NoError(t, dbErr)
			assert.True(t, rt1.RevokedAt.Valid)

			// Reapresentar token revogado via Refresh falha.
			_, refErr := authSvc.Refresh(ctx, sess1.RefreshToken)
			domErr, ok := domain.AsDomainError(refErr)
			require.True(t, ok)
			assert.Equal(t, domain.CodeUnauthorized, domErr.Code)

			// Logout repetido é idempotente.
			require.NoError(t, authSvc.Logout(ctx, u1.ID, sess1.RefreshToken))
		})

		t.Run("logout with unknown token is idempotent", func(t *testing.T) {
			require.NoError(t, authSvc.Logout(ctx, u1.ID, "rt_does_not_exist"))
		})
	})

	t.Run("Me scenarios", func(t *testing.T) {
		cleanTables(t)
		activeUser := createUser(t, "Ativo", "ativo@doceria.com", "SenhaForte#123", domain.RoleCozinha, true)
		inactiveUser := createUser(t, "Inativo", "inativo@doceria.com", "SenhaForte#123", domain.RoleEntregador, false)

		got, meErr := authSvc.Me(ctx, activeUser.ID)
		require.NoError(t, meErr)
		assert.Equal(t, activeUser.ID, got.ID)
		assert.Equal(t, domain.RoleCozinha, got.Role)

		_, meErr = authSvc.Me(ctx, inactiveUser.ID)
		domErr, ok := domain.AsDomainError(meErr)
		require.True(t, ok)
		assert.Equal(t, domain.CodeUnauthorized, domErr.Code)

		missingID, idErr := uuid.NewV7()
		require.NoError(t, idErr)
		_, meErr = authSvc.Me(ctx, missingID)
		domErr, ok = domain.AsDomainError(meErr)
		require.True(t, ok)
		assert.Equal(t, domain.CodeUnauthorized, domErr.Code)
	})
}
