package handler

import (
	"context"

	"github.com/google/uuid"

	"github.com/joao-accioly/artisan-store/api/internal/domain"
	"github.com/joao-accioly/artisan-store/api/internal/http/middleware"
	"github.com/joao-accioly/artisan-store/api/internal/http/oapi"
	"github.com/joao-accioly/artisan-store/api/internal/service"
)

// AdminListUsers lista funcionários paginados com filtro opcional por papel.
func (s *Server) AdminListUsers(ctx context.Context, request oapi.AdminListUsersRequestObject) (oapi.AdminListUsersResponseObject, error) {
	if s.users == nil {
		return nil, domain.NewInternalError("Serviço de usuários não configurado", nil)
	}

	page := 1
	if request.Params.Page != nil {
		page = *request.Params.Page
	}

	pageSize := 20
	if request.Params.PageSize != nil {
		pageSize = *request.Params.PageSize
	}

	var roleFilter *domain.Role
	if request.Params.Role != nil {
		parsed, err := domain.ParseRole(string(*request.Params.Role))
		if err != nil {
			return nil, err
		}
		roleFilter = &parsed
	}

	users, meta, err := s.users.List(ctx, roleFilter, page, pageSize)
	if err != nil {
		return nil, err
	}

	items := make([]oapi.UserResponse, 0, len(users))
	for _, u := range users {
		items = append(items, toOAPIUser(u))
	}

	return oapi.AdminListUsers200JSONResponse{
		Data: items,
		Meta: oapi.PaginationMeta{
			Page:       meta.Page,
			PageSize:   meta.PageSize,
			TotalItems: int(meta.TotalItems),
			TotalPages: meta.TotalPages,
		},
	}, nil
}

// AdminCreateUser cadastra um novo funcionário.
func (s *Server) AdminCreateUser(ctx context.Context, request oapi.AdminCreateUserRequestObject) (oapi.AdminCreateUserResponseObject, error) {
	if s.users == nil {
		return nil, domain.NewInternalError("Serviço de usuários não configurado", nil)
	}
	if request.Body == nil {
		return nil, domain.NewValidationError("Corpo da requisição é obrigatório", nil)
	}

	role, err := domain.ParseRole(string(request.Body.Role))
	if err != nil {
		return nil, err
	}

	user, err := s.users.Create(ctx, service.CreateUserInput{
		Name:     request.Body.Name,
		Email:    string(request.Body.Email),
		Password: request.Body.Password,
		Role:     role,
	})
	if err != nil {
		return nil, err
	}

	return oapi.AdminCreateUser201JSONResponse(toOAPIUser(*user)), nil
}

// AdminGetUser busca um funcionário pelo ID.
func (s *Server) AdminGetUser(ctx context.Context, request oapi.AdminGetUserRequestObject) (oapi.AdminGetUserResponseObject, error) {
	if s.users == nil {
		return nil, domain.NewInternalError("Serviço de usuários não configurado", nil)
	}

	user, err := s.users.GetByID(ctx, uuid.UUID(request.Id))
	if err != nil {
		return nil, err
	}

	return oapi.AdminGetUser200JSONResponse(toOAPIUser(*user)), nil
}

// AdminUpdateUser atualiza dados cadastrais, papel, status ou senha de um funcionário.
func (s *Server) AdminUpdateUser(ctx context.Context, request oapi.AdminUpdateUserRequestObject) (oapi.AdminUpdateUserResponseObject, error) {
	if s.users == nil {
		return nil, domain.NewInternalError("Serviço de usuários não configurado", nil)
	}
	if request.Body == nil {
		return nil, domain.NewValidationError("Corpo da requisição é obrigatório", nil)
	}

	actor, ok := middleware.GetAuthenticatedUser(ctx)
	if !ok {
		return nil, domain.NewUnauthorizedError("Usuário não autenticado")
	}

	var parsedRole *domain.Role
	if request.Body.Role != nil {
		role, err := domain.ParseRole(string(*request.Body.Role))
		if err != nil {
			return nil, err
		}
		parsedRole = &role
	}

	user, err := s.users.Update(ctx, actor.ID, uuid.UUID(request.Id), service.UpdateUserInput{
		Name:     request.Body.Name,
		Role:     parsedRole,
		Active:   request.Body.Active,
		Password: request.Body.Password,
	})
	if err != nil {
		return nil, err
	}

	return oapi.AdminUpdateUser200JSONResponse(toOAPIUser(*user)), nil
}
