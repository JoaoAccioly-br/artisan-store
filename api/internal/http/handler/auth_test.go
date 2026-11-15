package handler_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	openapi_types "github.com/oapi-codegen/runtime/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/joao-accioly/artisan-store/api/internal/domain"
	"github.com/joao-accioly/artisan-store/api/internal/http/handler"
	"github.com/joao-accioly/artisan-store/api/internal/http/middleware"
	"github.com/joao-accioly/artisan-store/api/internal/http/oapi"
	"github.com/joao-accioly/artisan-store/api/internal/service"
)

type stubAuthUseCase struct {
	loginFn   func(ctx context.Context, email, password string) (*service.AuthTokens, *domain.User, error)
	refreshFn func(ctx context.Context, rawRefreshToken string) (*service.AuthTokens, error)
	logoutFn  func(ctx context.Context, userID uuid.UUID, rawRefreshToken string) error
	meFn      func(ctx context.Context, userID uuid.UUID) (*domain.User, error)
}

func (s *stubAuthUseCase) Login(ctx context.Context, email, password string) (*service.AuthTokens, *domain.User, error) {
	return s.loginFn(ctx, email, password)
}

func (s *stubAuthUseCase) Refresh(ctx context.Context, rawRefreshToken string) (*service.AuthTokens, error) {
	return s.refreshFn(ctx, rawRefreshToken)
}

func (s *stubAuthUseCase) Logout(ctx context.Context, userID uuid.UUID, rawRefreshToken string) error {
	return s.logoutFn(ctx, userID, rawRefreshToken)
}

func (s *stubAuthUseCase) Me(ctx context.Context, userID uuid.UUID) (*domain.User, error) {
	return s.meFn(ctx, userID)
}

func TestServer_AuthHandlers_GuardClauses(t *testing.T) {
	uid, err := uuid.NewV7()
	require.NoError(t, err)
	authCtx := middleware.ContextWithAuthenticatedUser(context.Background(), middleware.AuthenticatedUser{
		ID:    uid,
		Email: "admin@doceria.com",
		Role:  domain.RoleAdmin,
	})

	t.Run("nil auth service returns 500 internal error", func(t *testing.T) {
		srv := handler.NewServer(handler.ServerDeps{})

		_, lErr := srv.AdminLogin(context.Background(), oapi.AdminLoginRequestObject{})
		domErr, ok := domain.AsDomainError(lErr)
		require.True(t, ok)
		assert.Equal(t, domain.CodeInternal, domErr.Code)

		_, rErr := srv.AdminRefreshToken(context.Background(), oapi.AdminRefreshTokenRequestObject{})
		domErr, ok = domain.AsDomainError(rErr)
		require.True(t, ok)
		assert.Equal(t, domain.CodeInternal, domErr.Code)

		_, loErr := srv.AdminLogout(authCtx, oapi.AdminLogoutRequestObject{})
		domErr, ok = domain.AsDomainError(loErr)
		require.True(t, ok)
		assert.Equal(t, domain.CodeInternal, domErr.Code)

		_, mErr := srv.AdminGetMe(authCtx, oapi.AdminGetMeRequestObject{})
		domErr, ok = domain.AsDomainError(mErr)
		require.True(t, ok)
		assert.Equal(t, domain.CodeInternal, domErr.Code)
	})

	t.Run("nil request bodies return 400 validation error", func(t *testing.T) {
		srv := handler.NewServer(handler.ServerDeps{Auth: &stubAuthUseCase{}})

		_, lErr := srv.AdminLogin(context.Background(), oapi.AdminLoginRequestObject{Body: nil})
		domErr, ok := domain.AsDomainError(lErr)
		require.True(t, ok)
		assert.Equal(t, domain.CodeValidation, domErr.Code)

		_, rErr := srv.AdminRefreshToken(context.Background(), oapi.AdminRefreshTokenRequestObject{Body: nil})
		domErr, ok = domain.AsDomainError(rErr)
		require.True(t, ok)
		assert.Equal(t, domain.CodeValidation, domErr.Code)

		_, loErr := srv.AdminLogout(authCtx, oapi.AdminLogoutRequestObject{Body: nil})
		domErr, ok = domain.AsDomainError(loErr)
		require.True(t, ok)
		assert.Equal(t, domain.CodeValidation, domErr.Code)
	})

	t.Run("missing authenticated user in context returns 401", func(t *testing.T) {
		srv := handler.NewServer(handler.ServerDeps{Auth: &stubAuthUseCase{}})

		_, loErr := srv.AdminLogout(context.Background(), oapi.AdminLogoutRequestObject{
			Body: &oapi.AdminLogoutJSONRequestBody{RefreshToken: "rt_123"},
		})
		domErr, ok := domain.AsDomainError(loErr)
		require.True(t, ok)
		assert.Equal(t, domain.CodeUnauthorized, domErr.Code)

		_, mErr := srv.AdminGetMe(context.Background(), oapi.AdminGetMeRequestObject{})
		domErr, ok = domain.AsDomainError(mErr)
		require.True(t, ok)
		assert.Equal(t, domain.CodeUnauthorized, domErr.Code)
	})
}

func TestServer_AuthHandlers_SuccessAndPropagation(t *testing.T) {
	uid, err := uuid.NewV7()
	require.NoError(t, err)
	now := time.Now().UTC().Truncate(time.Second)

	authCtx := middleware.ContextWithAuthenticatedUser(context.Background(), middleware.AuthenticatedUser{
		ID:    uid,
		Email: "maria@doceria.com",
		Role:  domain.RoleAdmin,
	})

	srv := handler.NewServer(handler.ServerDeps{
		Auth: &stubAuthUseCase{
			loginFn: func(_ context.Context, email, password string) (*service.AuthTokens, *domain.User, error) {
				assert.Equal(t, "maria@doceria.com", email)
				assert.Equal(t, "Senha#123", password)
				return &service.AuthTokens{
					AccessToken:  "jwt.access",
					RefreshToken: "rt_abc",
					TokenType:    "Bearer",
					ExpiresIn:    900,
				}, &domain.User{
					ID:        uid,
					Name:      "Maria",
					Email:     "maria@doceria.com",
					Role:      domain.RoleAdmin,
					Active:    true,
					CreatedAt: now,
					UpdatedAt: now,
				}, nil
			},
			refreshFn: func(_ context.Context, rawRefreshToken string) (*service.AuthTokens, error) {
				assert.Equal(t, "rt_abc", rawRefreshToken)
				return &service.AuthTokens{
					AccessToken:  "jwt.new",
					RefreshToken: "rt_def",
					TokenType:    "Bearer",
					ExpiresIn:    900,
				}, nil
			},
			logoutFn: func(_ context.Context, userID uuid.UUID, rawRefreshToken string) error {
				assert.Equal(t, uid, userID)
				assert.Equal(t, "rt_def", rawRefreshToken)
				return nil
			},
			meFn: func(_ context.Context, userID uuid.UUID) (*domain.User, error) {
				assert.Equal(t, uid, userID)
				return &domain.User{
					ID:        uid,
					Name:      "Maria",
					Email:     "maria@doceria.com",
					Role:      domain.RoleAdmin,
					Active:    true,
					CreatedAt: now,
					UpdatedAt: now,
				}, nil
			},
		},
	})

	loginResp, err := srv.AdminLogin(context.Background(), oapi.AdminLoginRequestObject{
		Body: &oapi.AdminLoginJSONRequestBody{
			Email:    openapi_types.Email("maria@doceria.com"),
			Password: "Senha#123",
		},
	})
	require.NoError(t, err)
	okLogin, ok := loginResp.(oapi.AdminLogin200JSONResponse)
	require.True(t, ok)
	assert.Equal(t, "jwt.access", okLogin.AccessToken)
	assert.Equal(t, openapi_types.UUID(uid), okLogin.User.Id)

	refreshResp, err := srv.AdminRefreshToken(context.Background(), oapi.AdminRefreshTokenRequestObject{
		Body: &oapi.AdminRefreshTokenJSONRequestBody{RefreshToken: "rt_abc"},
	})
	require.NoError(t, err)
	okRefresh, ok := refreshResp.(oapi.AdminRefreshToken200JSONResponse)
	require.True(t, ok)
	assert.Equal(t, "rt_def", okRefresh.RefreshToken)

	logoutResp, err := srv.AdminLogout(authCtx, oapi.AdminLogoutRequestObject{
		Body: &oapi.AdminLogoutJSONRequestBody{RefreshToken: "rt_def"},
	})
	require.NoError(t, err)
	_, ok = logoutResp.(oapi.AdminLogout204Response)
	assert.True(t, ok)

	meResp, err := srv.AdminGetMe(authCtx, oapi.AdminGetMeRequestObject{})
	require.NoError(t, err)
	okMe, ok := meResp.(oapi.AdminGetMe200JSONResponse)
	require.True(t, ok)
	assert.Equal(t, "Maria", okMe.Name)
}
