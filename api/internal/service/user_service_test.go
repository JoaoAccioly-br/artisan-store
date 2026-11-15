package service_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/joao-accioly/artisan-store/api/internal/domain"
	"github.com/joao-accioly/artisan-store/api/internal/repository/sqlc"
	"github.com/joao-accioly/artisan-store/api/internal/security"
	"github.com/joao-accioly/artisan-store/api/internal/service"
)

func TestUserService_Integration(t *testing.T) {
	if testing.Short() {
		t.Skip("pulando teste de integração com Docker no modo -short")
	}

	ctx := context.Background()
	pool := setupServicePostgres(t, "doceria_user_service_test")
	hasher := testFastHasher()
	jwtMgr, err := security.NewJWTManager("user-service-integration-secret-32chars", 15*time.Minute)
	require.NoError(t, err)

	userSvc := service.NewUserService(pool, hasher)
	authSvc, err := service.NewAuthService(pool, hasher, jwtMgr, 24*time.Hour, nil)
	require.NoError(t, err)
	queries := sqlc.New(pool)

	cleanTables := func(t *testing.T) {
		t.Helper()
		_, execErr := pool.Exec(ctx, "TRUNCATE TABLE refresh_tokens, users CASCADE")
		require.NoError(t, execErr)
	}

	t.Run("Create user validations, success and 409 case-insensitive conflict", func(t *testing.T) {
		cleanTables(t)

		t.Run("rejects invalid fields with details", func(t *testing.T) {
			invalidInputs := []struct {
				name       string
				input      service.CreateUserInput
				detailKeys []string
			}{
				{
					name: "all fields invalid",
					input: service.CreateUserInput{
						Name:     " A ",
						Email:    "invalid-email",
						Password: "123",
						Role:     domain.Role("gerente"),
					},
					detailKeys: []string{"name", "email", "password", "role"},
				},
				{
					name: "email without dot",
					input: service.CreateUserInput{
						Name:     "Carlos",
						Email:    "carlos@localhost",
						Password: "Password#123",
						Role:     domain.RoleAtendente,
					},
					detailKeys: []string{"email"},
				},
				{
					name: "email exceeding 254 chars",
					input: service.CreateUserInput{
						Name:     "Carlos",
						Email:    strings.Repeat("a", 245) + "@doceria.com",
						Password: "Password#123",
						Role:     domain.RoleAtendente,
					},
					detailKeys: []string{"email"},
				},
				{
					name: "password exceeding 128 bytes",
					input: service.CreateUserInput{
						Name:     "Carlos",
						Email:    "carlos@doceria.com",
						Password: strings.Repeat("x", 129),
						Role:     domain.RoleAtendente,
					},
					detailKeys: []string{"password"},
				},
			}

			for _, tc := range invalidInputs {
				t.Run(tc.name, func(t *testing.T) {
					u, createErr := userSvc.Create(ctx, tc.input)
					assert.Nil(t, u)
					domErr, ok := domain.AsDomainError(createErr)
					require.True(t, ok)
					assert.Equal(t, domain.CodeValidation, domErr.Code)
					for _, key := range tc.detailKeys {
						assert.Contains(t, domErr.Details, key)
					}
				})
			}
		})

		t.Run("creates user normalizing name/email and rejects duplicate case-insensitive email with 409", func(t *testing.T) {
			created, createErr := userSvc.Create(ctx, service.CreateUserInput{
				Name:     "  Carlos Atendente  ",
				Email:    "  Carlos@Doceria.COM ",
				Password: "Password#123",
				Role:     domain.RoleAtendente,
			})
			require.NoError(t, createErr)
			assert.NotEqual(t, uuid.Nil, created.ID)
			assert.Equal(t, "Carlos Atendente", created.Name)
			assert.Equal(t, "carlos@doceria.com", created.Email)
			assert.Equal(t, domain.RoleAtendente, created.Role)
			assert.True(t, created.Active)

			ok, verErr := hasher.Verify("Password#123", created.PasswordHash)
			require.NoError(t, verErr)
			assert.True(t, ok)

			// Conflito case-insensitive / com espaços.
			dup, dupErr := userSvc.Create(ctx, service.CreateUserInput{
				Name:     "Outro Carlos",
				Email:    "CARLOS@DOCERIA.COM",
				Password: "Password#456",
				Role:     domain.RoleCozinha,
			})
			assert.Nil(t, dup)
			domErr, ok := domain.AsDomainError(dupErr)
			require.True(t, ok)
			assert.Equal(t, domain.CodeConflict, domErr.Code)
		})
	})

	t.Run("List pagination, role filtering and validations", func(t *testing.T) {
		cleanTables(t)

		seedUsers := []service.CreateUserInput{
			{Name: "User 1", Email: "u1@doceria.com", Password: "Password#123", Role: domain.RoleAtendente},
			{Name: "User 2", Email: "u2@doceria.com", Password: "Password#123", Role: domain.RoleAtendente},
			{Name: "User 3", Email: "u3@doceria.com", Password: "Password#123", Role: domain.RoleAtendente},
			{Name: "User 4", Email: "u4@doceria.com", Password: "Password#123", Role: domain.RoleCozinha},
			{Name: "User 5", Email: "u5@doceria.com", Password: "Password#123", Role: domain.RoleAdmin},
		}
		for _, in := range seedUsers {
			_, cErr := userSvc.Create(ctx, in)
			require.NoError(t, cErr)
		}

		// page <= 0 e pageSize <= 0 assumem defaults (1 e 20).
		all, meta, listErr := userSvc.List(ctx, nil, 0, 0)
		require.NoError(t, listErr)
		assert.Len(t, all, 5)
		assert.Equal(t, 1, meta.Page)
		assert.Equal(t, 20, meta.PageSize)
		assert.Equal(t, int64(5), meta.TotalItems)
		assert.Equal(t, 1, meta.TotalPages)

		// Paginação page=2, pageSize=2.
		p2, meta2, listErr := userSvc.List(ctx, nil, 2, 2)
		require.NoError(t, listErr)
		assert.Len(t, p2, 2)
		assert.Equal(t, 2, meta2.Page)
		assert.Equal(t, 2, meta2.PageSize)
		assert.Equal(t, int64(5), meta2.TotalItems)
		assert.Equal(t, 3, meta2.TotalPages)

		// Última página com sobra.
		p3, meta3, listErr := userSvc.List(ctx, nil, 3, 2)
		require.NoError(t, listErr)
		assert.Len(t, p3, 1)
		assert.Equal(t, 3, meta3.TotalPages)

		// Filtro por role.
		atendente := domain.RoleAtendente
		filtered, fMeta, listErr := userSvc.List(ctx, &atendente, 1, 2)
		require.NoError(t, listErr)
		assert.Len(t, filtered, 2)
		assert.Equal(t, int64(3), fMeta.TotalItems)
		assert.Equal(t, 2, fMeta.TotalPages)
		for _, u := range filtered {
			assert.Equal(t, domain.RoleAtendente, u.Role)
		}

		// page_size > 100 rejeitado.
		_, _, listErr = userSvc.List(ctx, nil, 1, 101)
		domErr, ok := domain.AsDomainError(listErr)
		require.True(t, ok)
		assert.Equal(t, domain.CodeValidation, domErr.Code)

		// role inválido rejeitado.
		badRole := domain.Role("invalido")
		_, _, listErr = userSvc.List(ctx, &badRole, 1, 20)
		domErr, ok = domain.AsDomainError(listErr)
		require.True(t, ok)
		assert.Equal(t, domain.CodeValidation, domErr.Code)
	})

	t.Run("GetByID returns 200 or 404", func(t *testing.T) {
		cleanTables(t)

		created, cErr := userSvc.Create(ctx, service.CreateUserInput{
			Name:     "Helena",
			Email:    "helena@doceria.com",
			Password: "Password#123",
			Role:     domain.RoleCozinha,
		})
		require.NoError(t, cErr)

		got, gErr := userSvc.GetByID(ctx, created.ID)
		require.NoError(t, gErr)
		assert.Equal(t, created.ID, got.ID)
		assert.Equal(t, "helena@doceria.com", got.Email)

		missingID, idErr := uuid.NewV7()
		require.NoError(t, idErr)
		missing, gErr := userSvc.GetByID(ctx, missingID)
		assert.Nil(t, missing)
		domErr, ok := domain.AsDomainError(gErr)
		require.True(t, ok)
		assert.Equal(t, domain.CodeNotFound, domErr.Code)
	})

	t.Run("Update validations, self-protection, 404 and token revocation", func(t *testing.T) {
		cleanTables(t)

		admin, cErr := userSvc.Create(ctx, service.CreateUserInput{
			Name:     "Admin",
			Email:    "admin@doceria.com",
			Password: "Password#123",
			Role:     domain.RoleAdmin,
		})
		require.NoError(t, cErr)

		target, cErr := userSvc.Create(ctx, service.CreateUserInput{
			Name:     "João",
			Email:    "joao@doceria.com",
			Password: "Password#123",
			Role:     domain.RoleAtendente,
		})
		require.NoError(t, cErr)

		t.Run("no fields returns 400", func(t *testing.T) {
			_, uErr := userSvc.Update(ctx, admin.ID, target.ID, service.UpdateUserInput{})
			domErr, ok := domain.AsDomainError(uErr)
			require.True(t, ok)
			assert.Equal(t, domain.CodeValidation, domErr.Code)
		})

		t.Run("invalid fields return 400 with details", func(t *testing.T) {
			badName := "A"
			badRole := domain.Role("ceo")
			badPass := "short"
			_, uErr := userSvc.Update(ctx, admin.ID, target.ID, service.UpdateUserInput{
				Name:     &badName,
				Role:     &badRole,
				Password: &badPass,
			})
			domErr, ok := domain.AsDomainError(uErr)
			require.True(t, ok)
			assert.Equal(t, domain.CodeValidation, domErr.Code)
			assert.Contains(t, domErr.Details, "name")
			assert.Contains(t, domErr.Details, "role")
			assert.Contains(t, domErr.Details, "password")
		})

		t.Run("self-deactivation and self-demotion return 400", func(t *testing.T) {
			inactive := false
			_, uErr := userSvc.Update(ctx, admin.ID, admin.ID, service.UpdateUserInput{Active: &inactive})
			domErr, ok := domain.AsDomainError(uErr)
			require.True(t, ok)
			assert.Equal(t, domain.CodeValidation, domErr.Code)

			demoted := domain.RoleAtendente
			_, uErr = userSvc.Update(ctx, admin.ID, admin.ID, service.UpdateUserInput{Role: &demoted})
			domErr, ok = domain.AsDomainError(uErr)
			require.True(t, ok)
			assert.Equal(t, domain.CodeValidation, domErr.Code)

			// Auto-atualização permitida (ex.: alterar próprio nome mantendo role admin).
			newName := "Admin Principal"
			sameRole := domain.RoleAdmin
			activeTrue := true
			updatedAdmin, uErr := userSvc.Update(ctx, admin.ID, admin.ID, service.UpdateUserInput{
				Name:   &newName,
				Role:   &sameRole,
				Active: &activeTrue,
			})
			require.NoError(t, uErr)
			assert.Equal(t, "Admin Principal", updatedAdmin.Name)
		})

		t.Run("target not found returns 404", func(t *testing.T) {
			missingID, idErr := uuid.NewV7()
			require.NoError(t, idErr)
			newName := "Fantasma"
			_, uErr := userSvc.Update(ctx, admin.ID, missingID, service.UpdateUserInput{Name: &newName})
			domErr, ok := domain.AsDomainError(uErr)
			require.True(t, ok)
			assert.Equal(t, domain.CodeNotFound, domErr.Code)
		})

		t.Run("name-only update keeps active refresh tokens", func(t *testing.T) {
			sess, _, lErr := authSvc.Login(ctx, target.Email, "Password#123")
			require.NoError(t, lErr)

			newName := "  João Silva  "
			updated, uErr := userSvc.Update(ctx, admin.ID, target.ID, service.UpdateUserInput{Name: &newName})
			require.NoError(t, uErr)
			assert.Equal(t, "João Silva", updated.Name)

			rt, dbErr := queries.GetRefreshTokenByHash(ctx, security.HashToken(sess.RefreshToken))
			require.NoError(t, dbErr)
			assert.False(t, rt.RevokedAt.Valid)
		})

		t.Run("changing role revokes active refresh tokens", func(t *testing.T) {
			sess, _, lErr := authSvc.Login(ctx, target.Email, "Password#123")
			require.NoError(t, lErr)

			newRole := domain.RoleCozinha
			updated, uErr := userSvc.Update(ctx, admin.ID, target.ID, service.UpdateUserInput{Role: &newRole})
			require.NoError(t, uErr)
			assert.Equal(t, domain.RoleCozinha, updated.Role)

			rt, dbErr := queries.GetRefreshTokenByHash(ctx, security.HashToken(sess.RefreshToken))
			require.NoError(t, dbErr)
			assert.True(t, rt.RevokedAt.Valid)
		})

		t.Run("changing password updates hash and revokes active refresh tokens", func(t *testing.T) {
			sess, _, lErr := authSvc.Login(ctx, target.Email, "Password#123")
			require.NoError(t, lErr)

			newPass := "NovaSenha#456"
			_, uErr := userSvc.Update(ctx, admin.ID, target.ID, service.UpdateUserInput{Password: &newPass})
			require.NoError(t, uErr)

			rt, dbErr := queries.GetRefreshTokenByHash(ctx, security.HashToken(sess.RefreshToken))
			require.NoError(t, dbErr)
			assert.True(t, rt.RevokedAt.Valid)

			// Senha antiga falha; nova senha funciona.
			_, _, oldLoginErr := authSvc.Login(ctx, target.Email, "Password#123")
			require.Error(t, oldLoginErr)
			_, _, newLoginErr := authSvc.Login(ctx, target.Email, "NovaSenha#456")
			require.NoError(t, newLoginErr)
		})

		t.Run("deactivating user revokes active refresh tokens and re-activating allows login", func(t *testing.T) {
			sess, _, lErr := authSvc.Login(ctx, target.Email, "NovaSenha#456")
			require.NoError(t, lErr)

			inactive := false
			updated, uErr := userSvc.Update(ctx, admin.ID, target.ID, service.UpdateUserInput{Active: &inactive})
			require.NoError(t, uErr)
			assert.False(t, updated.Active)

			rt, dbErr := queries.GetRefreshTokenByHash(ctx, security.HashToken(sess.RefreshToken))
			require.NoError(t, dbErr)
			assert.True(t, rt.RevokedAt.Valid)

			_, refErr := authSvc.Refresh(ctx, sess.RefreshToken)
			require.Error(t, refErr)

			// Reativação funciona normalmente.
			active := true
			updated, uErr = userSvc.Update(ctx, admin.ID, target.ID, service.UpdateUserInput{Active: &active})
			require.NoError(t, uErr)
			assert.True(t, updated.Active)
		})
	})
}
