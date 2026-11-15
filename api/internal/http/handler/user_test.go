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

type stubUserUseCase struct {
	listFn    func(ctx context.Context, role *domain.Role, page, pageSize int) ([]domain.User, domain.PaginationMeta, error)
	createFn  func(ctx context.Context, input service.CreateUserInput) (*domain.User, error)
	getByIDFn func(ctx context.Context, id uuid.UUID) (*domain.User, error)
	updateFn  func(ctx context.Context, actorID, targetID uuid.UUID, input service.UpdateUserInput) (*domain.User, error)
}

func (s *stubUserUseCase) List(ctx context.Context, role *domain.Role, page, pageSize int) ([]domain.User, domain.PaginationMeta, error) {
	return s.listFn(ctx, role, page, pageSize)
}

func (s *stubUserUseCase) Create(ctx context.Context, input service.CreateUserInput) (*domain.User, error) {
	return s.createFn(ctx, input)
}

func (s *stubUserUseCase) GetByID(ctx context.Context, id uuid.UUID) (*domain.User, error) {
	return s.getByIDFn(ctx, id)
}

func (s *stubUserUseCase) Update(ctx context.Context, actorID, targetID uuid.UUID, input service.UpdateUserInput) (*domain.User, error) {
	return s.updateFn(ctx, actorID, targetID, input)
}

func TestServer_UserHandlers_GuardClauses(t *testing.T) {
	uid, err := uuid.NewV7()
	require.NoError(t, err)
	authCtx := middleware.ContextWithAuthenticatedUser(context.Background(), middleware.AuthenticatedUser{
		ID:    uid,
		Email: "admin@doceria.com",
		Role:  domain.RoleAdmin,
	})

	t.Run("nil user service returns 500 internal error", func(t *testing.T) {
		srv := handler.NewServer(handler.ServerDeps{})

		_, err := srv.AdminListUsers(authCtx, oapi.AdminListUsersRequestObject{})
		domErr, ok := domain.AsDomainError(err)
		require.True(t, ok)
		assert.Equal(t, domain.CodeInternal, domErr.Code)

		_, err = srv.AdminCreateUser(authCtx, oapi.AdminCreateUserRequestObject{})
		domErr, ok = domain.AsDomainError(err)
		require.True(t, ok)
		assert.Equal(t, domain.CodeInternal, domErr.Code)

		_, err = srv.AdminGetUser(authCtx, oapi.AdminGetUserRequestObject{})
		domErr, ok = domain.AsDomainError(err)
		require.True(t, ok)
		assert.Equal(t, domain.CodeInternal, domErr.Code)

		_, err = srv.AdminUpdateUser(authCtx, oapi.AdminUpdateUserRequestObject{})
		domErr, ok = domain.AsDomainError(err)
		require.True(t, ok)
		assert.Equal(t, domain.CodeInternal, domErr.Code)
	})

	t.Run("nil request bodies and missing auth context", func(t *testing.T) {
		srv := handler.NewServer(handler.ServerDeps{Users: &stubUserUseCase{}})

		_, err := srv.AdminCreateUser(authCtx, oapi.AdminCreateUserRequestObject{Body: nil})
		domErr, ok := domain.AsDomainError(err)
		require.True(t, ok)
		assert.Equal(t, domain.CodeValidation, domErr.Code)

		_, err = srv.AdminUpdateUser(authCtx, oapi.AdminUpdateUserRequestObject{Body: nil})
		domErr, ok = domain.AsDomainError(err)
		require.True(t, ok)
		assert.Equal(t, domain.CodeValidation, domErr.Code)

		newName := "Carlos"
		_, err = srv.AdminUpdateUser(context.Background(), oapi.AdminUpdateUserRequestObject{
			Id:   openapi_types.UUID(uid),
			Body: &oapi.AdminUpdateUserJSONRequestBody{Name: &newName},
		})
		domErr, ok = domain.AsDomainError(err)
		require.True(t, ok)
		assert.Equal(t, domain.CodeUnauthorized, domErr.Code)
	})

	t.Run("invalid role query and body parameters return 400", func(t *testing.T) {
		srv := handler.NewServer(handler.ServerDeps{Users: &stubUserUseCase{}})

		badRole := oapi.UserRole("gerente")
		_, err := srv.AdminListUsers(authCtx, oapi.AdminListUsersRequestObject{
			Params: oapi.AdminListUsersParams{Role: &badRole},
		})
		domErr, ok := domain.AsDomainError(err)
		require.True(t, ok)
		assert.Equal(t, domain.CodeValidation, domErr.Code)

		_, err = srv.AdminCreateUser(authCtx, oapi.AdminCreateUserRequestObject{
			Body: &oapi.AdminCreateUserJSONRequestBody{
				Name:     "Carlos",
				Email:    "carlos@doceria.com",
				Password: "Password#123",
				Role:     badRole,
			},
		})
		domErr, ok = domain.AsDomainError(err)
		require.True(t, ok)
		assert.Equal(t, domain.CodeValidation, domErr.Code)

		_, err = srv.AdminUpdateUser(authCtx, oapi.AdminUpdateUserRequestObject{
			Id:   openapi_types.UUID(uid),
			Body: &oapi.AdminUpdateUserJSONRequestBody{Role: &badRole},
		})
		domErr, ok = domain.AsDomainError(err)
		require.True(t, ok)
		assert.Equal(t, domain.CodeValidation, domErr.Code)
	})
}

func TestServer_UserHandlers_SuccessAndServiceErrors(t *testing.T) {
	actorID, err := uuid.NewV7()
	require.NoError(t, err)
	targetID, err := uuid.NewV7()
	require.NoError(t, err)
	now := time.Now().UTC().Truncate(time.Second)

	authCtx := middleware.ContextWithAuthenticatedUser(context.Background(), middleware.AuthenticatedUser{
		ID:    actorID,
		Email: "admin@doceria.com",
		Role:  domain.RoleAdmin,
	})

	srv := handler.NewServer(handler.ServerDeps{
		Users: &stubUserUseCase{
			listFn: func(_ context.Context, role *domain.Role, page, pageSize int) ([]domain.User, domain.PaginationMeta, error) {
				require.NotNil(t, role)
				assert.Equal(t, domain.RoleCozinha, *role)
				assert.Equal(t, 2, page)
				assert.Equal(t, 10, pageSize)
				return []domain.User{
					{
						ID:        targetID,
						Name:      "Ana",
						Email:     "ana@doceria.com",
						Role:      domain.RoleCozinha,
						Active:    true,
						CreatedAt: now,
						UpdatedAt: now,
					},
				}, domain.PaginationMeta{Page: 2, PageSize: 10, TotalItems: 11, TotalPages: 2}, nil
			},
			createFn: func(_ context.Context, input service.CreateUserInput) (*domain.User, error) {
				assert.Equal(t, domain.RoleAtendente, input.Role)
				return &domain.User{
					ID:        targetID,
					Name:      input.Name,
					Email:     input.Email,
					Role:      input.Role,
					Active:    true,
					CreatedAt: now,
					UpdatedAt: now,
				}, nil
			},
			getByIDFn: func(_ context.Context, id uuid.UUID) (*domain.User, error) {
				assert.Equal(t, targetID, id)
				return &domain.User{
					ID:        targetID,
					Name:      "Ana",
					Email:     "ana@doceria.com",
					Role:      domain.RoleCozinha,
					Active:    true,
					CreatedAt: now,
					UpdatedAt: now,
				}, nil
			},
			updateFn: func(_ context.Context, gotActor, gotTarget uuid.UUID, input service.UpdateUserInput) (*domain.User, error) {
				assert.Equal(t, actorID, gotActor)
				assert.Equal(t, targetID, gotTarget)
				require.NotNil(t, input.Role)
				assert.Equal(t, domain.RoleEntregador, *input.Role)
				return &domain.User{
					ID:        targetID,
					Name:      *input.Name,
					Email:     "ana@doceria.com",
					Role:      *input.Role,
					Active:    *input.Active,
					CreatedAt: now,
					UpdatedAt: now,
				}, nil
			},
		},
	})

	page := 2
	pageSize := 10
	roleParam := oapi.UserRole("COZINHA")
	listResp, err := srv.AdminListUsers(authCtx, oapi.AdminListUsersRequestObject{
		Params: oapi.AdminListUsersParams{
			Page:     &page,
			PageSize: &pageSize,
			Role:     &roleParam,
		},
	})
	require.NoError(t, err)
	okList, ok := listResp.(oapi.AdminListUsers200JSONResponse)
	require.True(t, ok)
	require.Len(t, okList.Data, 1)
	assert.Equal(t, 11, okList.Meta.TotalItems)

	createResp, err := srv.AdminCreateUser(authCtx, oapi.AdminCreateUserRequestObject{
		Body: &oapi.AdminCreateUserJSONRequestBody{
			Name:     "Carlos",
			Email:    "carlos@doceria.com",
			Password: "Password#123",
			Role:     oapi.Atendente,
		},
	})
	require.NoError(t, err)
	okCreate, ok := createResp.(oapi.AdminCreateUser201JSONResponse)
	require.True(t, ok)
	assert.Equal(t, openapi_types.UUID(targetID), okCreate.Id)

	getResp, err := srv.AdminGetUser(authCtx, oapi.AdminGetUserRequestObject{Id: openapi_types.UUID(targetID)})
	require.NoError(t, err)
	okGet, ok := getResp.(oapi.AdminGetUser200JSONResponse)
	require.True(t, ok)
	assert.Equal(t, "Ana", okGet.Name)

	newName := "Ana Paula"
	newRole := oapi.Entregador
	active := false
	updateResp, err := srv.AdminUpdateUser(authCtx, oapi.AdminUpdateUserRequestObject{
		Id: openapi_types.UUID(targetID),
		Body: &oapi.AdminUpdateUserJSONRequestBody{
			Name:   &newName,
			Role:   &newRole,
			Active: &active,
		},
	})
	require.NoError(t, err)
	okUpdate, ok := updateResp.(oapi.AdminUpdateUser200JSONResponse)
	require.True(t, ok)
	assert.Equal(t, "Ana Paula", okUpdate.Name)
	assert.Equal(t, oapi.Entregador, okUpdate.Role)
	assert.False(t, okUpdate.Active)
}
