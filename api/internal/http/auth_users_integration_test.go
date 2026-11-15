package http_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/joao-accioly/artisan-store/api/internal/config"
	"github.com/joao-accioly/artisan-store/api/internal/domain"
	apphttp "github.com/joao-accioly/artisan-store/api/internal/http"
	"github.com/joao-accioly/artisan-store/api/internal/http/oapi"
	"github.com/joao-accioly/artisan-store/api/internal/platform/db"
	"github.com/joao-accioly/artisan-store/api/internal/platform/logger"
	"github.com/joao-accioly/artisan-store/api/internal/security"
	"github.com/joao-accioly/artisan-store/api/internal/service"
)

func doJSONRequest(t *testing.T, router http.Handler, method, path, token string, body any) *httptest.ResponseRecorder {
	t.Helper()

	var buf bytes.Buffer
	if body != nil {
		switch v := body.(type) {
		case string:
			buf.WriteString(v)
		default:
			require.NoError(t, json.NewEncoder(&buf).Encode(v))
		}
	}

	req := httptest.NewRequest(method, path, &buf)
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	return rec
}

func TestAuthAndUsersEndToEnd_Integration(t *testing.T) {
	if testing.Short() {
		t.Skip("pulando teste de integração com Docker no modo -short")
	}

	ctx := context.Background()
	connStr := setupPostgresContainer(t)

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

	var logBuf bytes.Buffer
	log := logger.NewWithWriter(&logBuf, "development", "debug")

	cfg := &config.Config{
		AppEnv:            "development",
		AllowedOrigins:    []string{"http://localhost:3000"},
		RateLimitRPM:      1000,
		JWTAccessSecret:   "e2e-integration-jwt-access-secret-32chars",
		JWTAccessTTL:      15 * time.Minute,
		JWTRefreshTTL:     24 * time.Hour,
		SeedAdminName:     "Maria Confeiteira",
		SeedAdminEmail:    "admin@doceria.com",
		SeedAdminPassword: "AdminPassword#123",
	}

	require.NoError(t, db.SeedAdmin(ctx, pool, cfg, hasher, log))

	jwtMgr, err := security.NewJWTManager(cfg.JWTAccessSecret, cfg.JWTAccessTTL)
	require.NoError(t, err)

	authSvc, err := service.NewAuthService(pool, hasher, jwtMgr, cfg.JWTRefreshTTL, log)
	require.NoError(t, err)
	userSvc := service.NewUserService(pool, hasher)

	router := apphttp.NewRouter(cfg, log, apphttp.RouterDeps{
		DB:         pool,
		JWTManager: jwtMgr,
		Auth:       authSvc,
		Users:      userSvc,
	})

	// 1. Login admin seedado.
	rec := doJSONRequest(t, router, http.MethodPost, "/api/v1/admin/auth/login", "", map[string]string{
		"email":    "  ADMIN@Doceria.COM ",
		"password": "AdminPassword#123",
	})
	require.Equal(t, http.StatusOK, rec.Code)

	var adminLogin oapi.LoginResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &adminLogin))
	assert.Equal(t, "Bearer", adminLogin.TokenType)
	assert.Equal(t, 900, adminLogin.ExpiresIn)
	assert.Equal(t, "admin@doceria.com", string(adminLogin.User.Email))
	assert.Equal(t, oapi.Admin, adminLogin.User.Role)
	adminID := uuid.UUID(adminLogin.User.Id)

	// Login com JSON malformado retorna 400 INVALID_JSON.
	rec = doJSONRequest(t, router, http.MethodPost, "/api/v1/admin/auth/login", "", "{invalid-json")
	assert.Equal(t, http.StatusBadRequest, rec.Code)
	var errBody oapi.ErrorResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &errBody))
	assert.Equal(t, string(domain.CodeInvalidJSON), errBody.Error.Code)

	// Login com senha errada retorna 401 UNAUTHORIZED.
	rec = doJSONRequest(t, router, http.MethodPost, "/api/v1/admin/auth/login", "", map[string]string{
		"email":    "admin@doceria.com",
		"password": "WrongPassword",
	})
	assert.Equal(t, http.StatusUnauthorized, rec.Code)

	// 2. /me sem token -> 401; com token -> 200.
	rec = doJSONRequest(t, router, http.MethodGet, "/api/v1/admin/auth/me", "", nil)
	assert.Equal(t, http.StatusUnauthorized, rec.Code)

	rec = doJSONRequest(t, router, http.MethodGet, "/api/v1/admin/auth/me", adminLogin.AccessToken, nil)
	require.Equal(t, http.StatusOK, rec.Code)
	var meResp oapi.UserResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &meResp))
	assert.Equal(t, "Maria Confeiteira", meResp.Name)

	// 3. Admin cria usuários via POST /api/v1/admin/users.
	rec = doJSONRequest(t, router, http.MethodPost, "/api/v1/admin/users", adminLogin.AccessToken, "{bad-json")
	assert.Equal(t, http.StatusBadRequest, rec.Code)
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &errBody))
	assert.Equal(t, string(domain.CodeInvalidJSON), errBody.Error.Code)

	rec = doJSONRequest(t, router, http.MethodPost, "/api/v1/admin/users", adminLogin.AccessToken, map[string]string{
		"name":     "Carlos Atendente",
		"email":    "carlos@doceria.com",
		"password": "UserPassword#123",
		"role":     "atendente",
	})
	require.Equal(t, http.StatusCreated, rec.Code)
	var carlosResp oapi.UserResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &carlosResp))
	carlosID := uuid.UUID(carlosResp.Id)

	// Conflito de e-mail duplicado -> 409 RESOURCE_CONFLICT.
	rec = doJSONRequest(t, router, http.MethodPost, "/api/v1/admin/users", adminLogin.AccessToken, map[string]string{
		"name":     "Carlos Duplicado",
		"email":    "CARLOS@DOCERIA.COM",
		"password": "UserPassword#123",
		"role":     "cozinha",
	})
	assert.Equal(t, http.StatusConflict, rec.Code)
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &errBody))
	assert.Equal(t, string(domain.CodeConflict), errBody.Error.Code)

	// Cria um usuário cozinha para testar listagem e filtros.
	rec = doJSONRequest(t, router, http.MethodPost, "/api/v1/admin/users", adminLogin.AccessToken, map[string]string{
		"name":     "Ana Cozinha",
		"email":    "ana@doceria.com",
		"password": "UserPassword#123",
		"role":     "cozinha",
	})
	require.Equal(t, http.StatusCreated, rec.Code)

	// 4. RBAC: atendente consegue /me, mas recebe 403 em /users.
	rec = doJSONRequest(t, router, http.MethodPost, "/api/v1/admin/auth/login", "", map[string]string{
		"email":    "carlos@doceria.com",
		"password": "UserPassword#123",
	})
	require.Equal(t, http.StatusOK, rec.Code)
	var carlosLogin oapi.LoginResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &carlosLogin))

	rec = doJSONRequest(t, router, http.MethodGet, "/api/v1/admin/auth/me", carlosLogin.AccessToken, nil)
	assert.Equal(t, http.StatusOK, rec.Code)

	for _, check := range []struct {
		method string
		path   string
		body   any
	}{
		{http.MethodGet, "/api/v1/admin/users", nil},
		{http.MethodPost, "/api/v1/admin/users", map[string]string{"name": "X", "email": "x@doceria.com", "password": "Password#1", "role": "cozinha"}},
		{http.MethodGet, "/api/v1/admin/users/" + carlosID.String(), nil},
		{http.MethodPatch, "/api/v1/admin/users/" + carlosID.String(), map[string]string{"name": "Novo"}},
	} {
		rec = doJSONRequest(t, router, check.method, check.path, carlosLogin.AccessToken, check.body)
		assert.Equal(t, http.StatusForbidden, rec.Code, "expected 403 for atendente on %s %s", check.method, check.path)
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &errBody))
		assert.Equal(t, string(domain.CodeForbidden), errBody.Error.Code)
	}

	// 5. CRUD de /users como admin: List, GetByID, Update, 404, 400.
	rec = doJSONRequest(t, router, http.MethodGet, "/api/v1/admin/users?page=1&page_size=2&role=atendente", adminLogin.AccessToken, nil)
	require.Equal(t, http.StatusOK, rec.Code)
	var listResp oapi.PaginatedUsersResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &listResp))
	assert.Len(t, listResp.Data, 1)
	assert.Equal(t, 1, listResp.Meta.TotalItems)
	assert.Equal(t, "carlos@doceria.com", string(listResp.Data[0].Email))

	rec = doJSONRequest(t, router, http.MethodGet, "/api/v1/admin/users?role=gerente", adminLogin.AccessToken, nil)
	assert.Equal(t, http.StatusBadRequest, rec.Code)

	rec = doJSONRequest(t, router, http.MethodGet, "/api/v1/admin/users/"+carlosID.String(), adminLogin.AccessToken, nil)
	require.Equal(t, http.StatusOK, rec.Code)

	missingID, err := uuid.NewV7()
	require.NoError(t, err)
	rec = doJSONRequest(t, router, http.MethodGet, "/api/v1/admin/users/"+missingID.String(), adminLogin.AccessToken, nil)
	assert.Equal(t, http.StatusNotFound, rec.Code)
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &errBody))
	assert.Equal(t, string(domain.CodeNotFound), errBody.Error.Code)

	rec = doJSONRequest(t, router, http.MethodGet, "/api/v1/admin/users/not-a-uuid", adminLogin.AccessToken, nil)
	assert.Equal(t, http.StatusBadRequest, rec.Code)

	// Update com payload vazio -> 400; JSON malformado -> 400 INVALID_JSON; 404; self-demotion -> 400.
	rec = doJSONRequest(t, router, http.MethodPatch, "/api/v1/admin/users/"+carlosID.String(), adminLogin.AccessToken, map[string]any{})
	assert.Equal(t, http.StatusBadRequest, rec.Code)

	rec = doJSONRequest(t, router, http.MethodPatch, "/api/v1/admin/users/"+carlosID.String(), adminLogin.AccessToken, "{")
	assert.Equal(t, http.StatusBadRequest, rec.Code)
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &errBody))
	assert.Equal(t, string(domain.CodeInvalidJSON), errBody.Error.Code)

	rec = doJSONRequest(t, router, http.MethodPatch, "/api/v1/admin/users/"+missingID.String(), adminLogin.AccessToken, map[string]any{"name": "Inexistente"})
	assert.Equal(t, http.StatusNotFound, rec.Code)

	rec = doJSONRequest(t, router, http.MethodPatch, "/api/v1/admin/users/"+adminID.String(), adminLogin.AccessToken, map[string]any{"active": false})
	assert.Equal(t, http.StatusBadRequest, rec.Code)

	// Atualiza Carlos para role cozinha e desativa -> revoga refresh token de Carlos!
	rec = doJSONRequest(t, router, http.MethodPatch, "/api/v1/admin/users/"+carlosID.String(), adminLogin.AccessToken, map[string]any{
		"name":   "Carlos Silva",
		"role":   "cozinha",
		"active": false,
	})
	require.Equal(t, http.StatusOK, rec.Code)
	var updatedCarlos oapi.UserResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &updatedCarlos))
	assert.Equal(t, "Carlos Silva", updatedCarlos.Name)
	assert.Equal(t, oapi.Cozinha, updatedCarlos.Role)
	assert.False(t, updatedCarlos.Active)

	// /me de usuário desativado retorna 401; refresh também retorna 401.
	rec = doJSONRequest(t, router, http.MethodGet, "/api/v1/admin/auth/me", carlosLogin.AccessToken, nil)
	assert.Equal(t, http.StatusUnauthorized, rec.Code)

	rec = doJSONRequest(t, router, http.MethodPost, "/api/v1/admin/auth/refresh", "", map[string]string{
		"refresh_token": carlosLogin.RefreshToken,
	})
	assert.Equal(t, http.StatusUnauthorized, rec.Code)

	// 6. Refresh rotativo, reuse detection e logout ponta-a-ponta com o admin.
	rec = doJSONRequest(t, router, http.MethodPost, "/api/v1/admin/auth/refresh", "", map[string]string{
		"refresh_token": adminLogin.RefreshToken,
	})
	require.Equal(t, http.StatusOK, rec.Code)
	var refreshedAdmin oapi.RefreshTokenResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &refreshedAdmin))
	assert.NotEqual(t, adminLogin.RefreshToken, refreshedAdmin.RefreshToken)

	// Reutilizar refresh token antigo revoga família inteira (inclusive o novo!).
	rec = doJSONRequest(t, router, http.MethodPost, "/api/v1/admin/auth/refresh", "", map[string]string{
		"refresh_token": adminLogin.RefreshToken,
	})
	assert.Equal(t, http.StatusUnauthorized, rec.Code)

	rec = doJSONRequest(t, router, http.MethodPost, "/api/v1/admin/auth/refresh", "", map[string]string{
		"refresh_token": refreshedAdmin.RefreshToken,
	})
	assert.Equal(t, http.StatusUnauthorized, rec.Code)

	// Novo login do admin + logout 204 + tentativa de refresh falha 401.
	rec = doJSONRequest(t, router, http.MethodPost, "/api/v1/admin/auth/login", "", map[string]string{
		"email":    "admin@doceria.com",
		"password": "AdminPassword#123",
	})
	require.Equal(t, http.StatusOK, rec.Code)
	var secondLogin oapi.LoginResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &secondLogin))

	// Logout tentando revogar token de outro usuário (reativa Carlos antes para gerar sessão dele).
	rec = doJSONRequest(t, router, http.MethodPatch, "/api/v1/admin/users/"+carlosID.String(), secondLogin.AccessToken, map[string]any{"active": true})
	require.Equal(t, http.StatusOK, rec.Code)
	rec = doJSONRequest(t, router, http.MethodPost, "/api/v1/admin/auth/login", "", map[string]string{
		"email":    "carlos@doceria.com",
		"password": "UserPassword#123",
	})
	require.Equal(t, http.StatusOK, rec.Code)
	var carlosNewLogin oapi.LoginResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &carlosNewLogin))

	rec = doJSONRequest(t, router, http.MethodPost, "/api/v1/admin/auth/logout", secondLogin.AccessToken, map[string]string{
		"refresh_token": carlosNewLogin.RefreshToken,
	})
	assert.Equal(t, http.StatusForbidden, rec.Code)

	// Logout legítimo do admin -> 204 e refresh token revogado.
	rec = doJSONRequest(t, router, http.MethodPost, "/api/v1/admin/auth/logout", secondLogin.AccessToken, map[string]string{
		"refresh_token": secondLogin.RefreshToken,
	})
	assert.Equal(t, http.StatusNoContent, rec.Code)

	rec = doJSONRequest(t, router, http.MethodPost, "/api/v1/admin/auth/refresh", "", map[string]string{
		"refresh_token": secondLogin.RefreshToken,
	})
	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}
