package middleware_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/joao-accioly/artisan-store/api/internal/domain"
	"github.com/joao-accioly/artisan-store/api/internal/http/middleware"
	"github.com/joao-accioly/artisan-store/api/internal/security"
)

const testAuthSecret = "middleware-auth-test-secret-32-bytes"

func TestContextAuthenticatedUser(t *testing.T) {
	u, ok := middleware.GetAuthenticatedUser(context.Background())
	assert.False(t, ok)
	assert.Equal(t, uuid.Nil, u.ID)

	uid, err := uuid.NewV7()
	require.NoError(t, err)
	expected := middleware.AuthenticatedUser{
		ID:    uid,
		Email: "maria@doceria.com",
		Role:  domain.RoleAdmin,
	}

	ctx := middleware.ContextWithAuthenticatedUser(context.Background(), expected)
	got, ok := middleware.GetAuthenticatedUser(ctx)
	assert.True(t, ok)
	assert.Equal(t, expected, got)
}

func TestDefaultOperationPolicies(t *testing.T) {
	policies := middleware.DefaultOperationPolicies()

	publicOps := []string{
		"getHealthz",
		"publicGetStore",
		"publicGetMenu",
		"publicGetProduct",
		"adminLogin",
		"adminRefreshToken",
	}
	for _, op := range publicOps {
		p, exists := policies[op]
		require.True(t, exists, "missing public policy for %s", op)
		assert.True(t, p.Public, "expected %s to be public", op)
	}

	allRolesOps := []string{
		"adminLogout",
		"adminGetMe",
		"adminGetSettings",
		"adminListCategories",
		"adminGetCategory",
		"adminListProducts",
		"adminGetProduct",
	}
	for _, op := range allRolesOps {
		p, exists := policies[op]
		require.True(t, exists, "missing all-roles policy for %s", op)
		assert.False(t, p.Public, "expected %s to require auth", op)
		assert.Equal(t, domain.AllRoles, p.AllowedRoles, "expected AllRoles for %s", op)
	}

	adminOnlyOps := []string{
		"adminListUsers",
		"adminCreateUser",
		"adminGetUser",
		"adminUpdateUser",
		"adminUpdateSettings",
		"adminCreateCategory",
		"adminUpdateCategory",
		"adminDeleteCategory",
		"adminCreateProduct",
		"adminUpdateProduct",
		"adminDeleteProduct",
		"adminUploadProductImage",
		"adminCreateProductVariant",
		"adminUpdateProductVariant",
		"adminDeleteProductVariant",
	}
	for _, adminOp := range adminOnlyOps {
		p, exists := policies[adminOp]
		require.True(t, exists, "missing policy for %s", adminOp)
		assert.False(t, p.Public, "expected %s to require auth", adminOp)
		assert.Equal(t, []domain.Role{domain.RoleAdmin}, p.AllowedRoles, "expected admin-only for %s", adminOp)
	}
}

func TestNewStrictAuthMiddleware(t *testing.T) {
	jwtMgr, err := security.NewJWTManager(testAuthSecret, 15*time.Minute)
	require.NoError(t, err)

	policies := middleware.DefaultOperationPolicies()
	mw := middleware.NewStrictAuthMiddleware(jwtMgr, policies)

	adminID, err := uuid.NewV7()
	require.NoError(t, err)
	adminToken, _, err := jwtMgr.GenerateAccessToken(domain.User{
		ID:    adminID,
		Email: "admin@doceria.com",
		Role:  domain.RoleAdmin,
	})
	require.NoError(t, err)

	atendenteID, err := uuid.NewV7()
	require.NoError(t, err)
	atendenteToken, _, err := jwtMgr.GenerateAccessToken(domain.User{
		ID:    atendenteID,
		Email: "atendente@doceria.com",
		Role:  domain.RoleAtendente,
	})
	require.NoError(t, err)

	cozinhaID, err := uuid.NewV7()
	require.NoError(t, err)
	cozinhaToken, _, err := jwtMgr.GenerateAccessToken(domain.User{
		ID:    cozinhaID,
		Email: "cozinha@doceria.com",
		Role:  domain.RoleCozinha,
	})
	require.NoError(t, err)

	t.Run("public operation passes without token", func(t *testing.T) {
		for _, opID := range []string{
			"getHealthz",
			"PublicGetStore",
			"PublicGetMenu",
			"PublicGetProduct",
			"AdminLogin",
			"AdminRefreshToken",
		} {
			called := false
			next := func(ctx context.Context, _ http.ResponseWriter, _ *http.Request, _ any) (any, error) {
				called = true
				_, hasUser := middleware.GetAuthenticatedUser(ctx)
				assert.False(t, hasUser)
				return "ok", nil
			}

			req := httptest.NewRequest(http.MethodGet, "/public", nil)
			rec := httptest.NewRecorder()
			resp, mwErr := mw(next, opID)(context.Background(), rec, req, nil)
			require.NoError(t, mwErr)
			assert.Equal(t, "ok", resp)
			assert.True(t, called)
		}
	})

	t.Run("unknown operation fails closed with 403 FORBIDDEN", func(t *testing.T) {
		called := false
		next := func(context.Context, http.ResponseWriter, *http.Request, any) (any, error) {
			called = true
			return "ok", nil
		}

		req := httptest.NewRequest(http.MethodGet, "/api/v1/unknown", nil)
		req.Header.Set("Authorization", "Bearer "+adminToken)
		rec := httptest.NewRecorder()
		resp, mwErr := mw(next, "UnknownOperation")(context.Background(), rec, req, nil)
		assert.Nil(t, resp)
		assert.False(t, called)
		require.Error(t, mwErr)

		domErr, ok := domain.AsDomainError(mwErr)
		require.True(t, ok)
		assert.Equal(t, domain.CodeForbidden, domErr.Code)
	})

	t.Run("nil jwtManager on protected operation returns 500 INTERNAL_ERROR", func(t *testing.T) {
		nilJWTMw := middleware.NewStrictAuthMiddleware(nil, policies)
		called := false
		next := func(context.Context, http.ResponseWriter, *http.Request, any) (any, error) {
			called = true
			return nil, nil
		}

		req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/auth/me", nil)
		req.Header.Set("Authorization", "Bearer "+adminToken)
		_, mwErr := nilJWTMw(next, "AdminGetMe")(context.Background(), httptest.NewRecorder(), req, nil)
		assert.False(t, called)
		require.Error(t, mwErr)

		domErr, ok := domain.AsDomainError(mwErr)
		require.True(t, ok)
		assert.Equal(t, domain.CodeInternal, domErr.Code)
	})

	t.Run("missing or malformed Authorization header returns 401 UNAUTHORIZED", func(t *testing.T) {
		headers := []struct {
			name   string
			header string
		}{
			{name: "missing header", header: ""},
			{name: "whitespace only", header: "   "},
			{name: "basic scheme", header: "Basic dXNlcjpwYXNz"},
			{name: "token scheme", header: "Token abc"},
			{name: "bearer without space or token", header: "Bearer"},
			{name: "bearer with empty token", header: "Bearer    "},
			{name: "raw token without scheme", header: adminToken},
		}

		for _, tc := range headers {
			t.Run(tc.name, func(t *testing.T) {
				called := false
				next := func(context.Context, http.ResponseWriter, *http.Request, any) (any, error) {
					called = true
					return nil, nil
				}

				req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/auth/me", nil)
				if tc.header != "" {
					req.Header.Set("Authorization", tc.header)
				}
				_, mwErr := mw(next, "AdminGetMe")(context.Background(), httptest.NewRecorder(), req, nil)
				assert.False(t, called)
				require.Error(t, mwErr)

				domErr, ok := domain.AsDomainError(mwErr)
				require.True(t, ok)
				assert.Equal(t, domain.CodeUnauthorized, domErr.Code)
			})
		}
	})

	t.Run("invalid or expired JWT returns 401 UNAUTHORIZED", func(t *testing.T) {
		expiredClaims := security.AccessClaims{
			UserID: adminID,
			Email:  "admin@doceria.com",
			Role:   domain.RoleAdmin,
			RegisteredClaims: jwt.RegisteredClaims{
				Issuer:    "artisan-store-api",
				Subject:   adminID.String(),
				IssuedAt:  jwt.NewNumericDate(time.Now().Add(-30 * time.Minute)),
				ExpiresAt: jwt.NewNumericDate(time.Now().Add(-15 * time.Minute)),
			},
		}
		expiredToken, signErr := jwt.NewWithClaims(jwt.SigningMethodHS256, expiredClaims).SignedString([]byte(testAuthSecret))
		require.NoError(t, signErr)

		for _, tok := range []string{"invalid.jwt.token", expiredToken} {
			called := false
			next := func(context.Context, http.ResponseWriter, *http.Request, any) (any, error) {
				called = true
				return nil, nil
			}

			req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/auth/me", nil)
			req.Header.Set("Authorization", "Bearer "+tok)
			_, mwErr := mw(next, "AdminGetMe")(context.Background(), httptest.NewRecorder(), req, nil)
			assert.False(t, called)
			require.Error(t, mwErr)

			domErr, ok := domain.AsDomainError(mwErr)
			require.True(t, ok)
			assert.Equal(t, domain.CodeUnauthorized, domErr.Code)
		}
	})

	t.Run("RBAC rejects atendente and cozinha on admin-only write/upload/delete operations with 403 FORBIDDEN", func(t *testing.T) {
		adminOnlyOps := []string{
			"AdminListUsers",
			"AdminCreateUser",
			"AdminGetUser",
			"AdminUpdateUser",
			"AdminUpdateSettings",
			"AdminCreateCategory",
			"AdminUpdateCategory",
			"AdminDeleteCategory",
			"AdminCreateProduct",
			"AdminUpdateProduct",
			"AdminDeleteProduct",
			"AdminUploadProductImage",
			"AdminCreateProductVariant",
			"AdminUpdateProductVariant",
			"AdminDeleteProductVariant",
		}

		for _, roleToken := range []struct {
			role  string
			token string
		}{
			{role: "atendente", token: atendenteToken},
			{role: "cozinha", token: cozinhaToken},
		} {
			for _, opID := range adminOnlyOps {
				called := false
				next := func(context.Context, http.ResponseWriter, *http.Request, any) (any, error) {
					called = true
					return nil, nil
				}

				req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/restricted", nil)
				req.Header.Set("Authorization", "Bearer "+roleToken.token)
				_, mwErr := mw(next, opID)(context.Background(), httptest.NewRecorder(), req, nil)
				assert.False(t, called, "role=%s op=%s should not pass", roleToken.role, opID)
				require.Error(t, mwErr, "role=%s op=%s should fail", roleToken.role, opID)

				domErr, ok := domain.AsDomainError(mwErr)
				require.True(t, ok)
				assert.Equal(t, domain.CodeForbidden, domErr.Code, "role=%s op=%s", roleToken.role, opID)
			}
		}
	})

	t.Run("admin succeeds on admin-only catalog operations", func(t *testing.T) {
		for _, opID := range []string{
			"AdminUpdateSettings",
			"AdminCreateCategory",
			"AdminUpdateCategory",
			"AdminDeleteCategory",
			"AdminCreateProduct",
			"AdminUpdateProduct",
			"AdminDeleteProduct",
			"AdminUploadProductImage",
			"AdminCreateProductVariant",
			"AdminUpdateProductVariant",
			"AdminDeleteProductVariant",
		} {
			called := false
			next := func(ctx context.Context, _ http.ResponseWriter, _ *http.Request, _ any) (any, error) {
				called = true
				u, ok := middleware.GetAuthenticatedUser(ctx)
				assert.True(t, ok)
				assert.Equal(t, domain.RoleAdmin, u.Role)
				return "ok", nil
			}

			req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/catalog", nil)
			req.Header.Set("Authorization", "Bearer "+adminToken)
			resp, mwErr := mw(next, opID)(context.Background(), httptest.NewRecorder(), req, nil)
			require.NoError(t, mwErr, "op=%s", opID)
			assert.Equal(t, "ok", resp)
			assert.True(t, called)
		}
	})

	t.Run("success injects AuthenticatedUser into context (including lowercase bearer scheme)", func(t *testing.T) {
		var capturedUser middleware.AuthenticatedUser
		var hasUser bool
		next := func(ctx context.Context, _ http.ResponseWriter, _ *http.Request, _ any) (any, error) {
			capturedUser, hasUser = middleware.GetAuthenticatedUser(ctx)
			return "authorized", nil
		}

		req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/users", nil)
		req.Header.Set("Authorization", "bearer "+adminToken)
		resp, mwErr := mw(next, "AdminListUsers")(context.Background(), httptest.NewRecorder(), req, nil)
		require.NoError(t, mwErr)
		assert.Equal(t, "authorized", resp)
		assert.True(t, hasUser)
		assert.Equal(t, adminID, capturedUser.ID)
		assert.Equal(t, "admin@doceria.com", capturedUser.Email)
		assert.Equal(t, domain.RoleAdmin, capturedUser.Role)
	})

	t.Run("atendente and cozinha succeed on catalog read operations", func(t *testing.T) {
		readOps := []string{
			"AdminGetMe",
			"AdminGetSettings",
			"AdminListCategories",
			"AdminGetCategory",
			"AdminListProducts",
			"AdminGetProduct",
		}

		for _, actor := range []struct {
			id    uuid.UUID
			role  domain.Role
			token string
		}{
			{id: atendenteID, role: domain.RoleAtendente, token: atendenteToken},
			{id: cozinhaID, role: domain.RoleCozinha, token: cozinhaToken},
		} {
			for _, opID := range readOps {
				var capturedUser middleware.AuthenticatedUser
				next := func(ctx context.Context, _ http.ResponseWriter, _ *http.Request, _ any) (any, error) {
					capturedUser, _ = middleware.GetAuthenticatedUser(ctx)
					return "ok", nil
				}

				req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/catalog", nil)
				req.Header.Set("Authorization", "Bearer "+actor.token)
				resp, mwErr := mw(next, opID)(context.Background(), httptest.NewRecorder(), req, nil)
				require.NoError(t, mwErr, "role=%s op=%s", actor.role, opID)
				assert.Equal(t, "ok", resp)
				assert.Equal(t, actor.id, capturedUser.ID)
				assert.Equal(t, actor.role, capturedUser.Role)
			}
		}
	})
}
