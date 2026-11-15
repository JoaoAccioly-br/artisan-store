package handler

import (
	"context"

	openapi_types "github.com/oapi-codegen/runtime/types"

	"github.com/joao-accioly/artisan-store/api/internal/domain"
	"github.com/joao-accioly/artisan-store/api/internal/http/middleware"
	"github.com/joao-accioly/artisan-store/api/internal/http/oapi"
)

// AdminLogin autentica um funcionário por e-mail/senha e retorna tokens JWT e refresh.
func (s *Server) AdminLogin(ctx context.Context, request oapi.AdminLoginRequestObject) (oapi.AdminLoginResponseObject, error) {
	if s.auth == nil {
		return nil, domain.NewInternalError("Serviço de autenticação não configurado", nil)
	}
	if request.Body == nil {
		return nil, domain.NewValidationError("Corpo da requisição é obrigatório", nil)
	}

	tokens, user, err := s.auth.Login(ctx, string(request.Body.Email), request.Body.Password)
	if err != nil {
		return nil, err
	}

	return oapi.AdminLogin200JSONResponse{
		AccessToken:  tokens.AccessToken,
		RefreshToken: tokens.RefreshToken,
		TokenType:    tokens.TokenType,
		ExpiresIn:    tokens.ExpiresIn,
		User:         toOAPIUser(*user),
	}, nil
}

// AdminRefreshToken renova o par de tokens rotacionando o refresh token atual.
func (s *Server) AdminRefreshToken(ctx context.Context, request oapi.AdminRefreshTokenRequestObject) (oapi.AdminRefreshTokenResponseObject, error) {
	if s.auth == nil {
		return nil, domain.NewInternalError("Serviço de autenticação não configurado", nil)
	}
	if request.Body == nil {
		return nil, domain.NewValidationError("Corpo da requisição é obrigatório", nil)
	}

	tokens, err := s.auth.Refresh(ctx, request.Body.RefreshToken)
	if err != nil {
		return nil, err
	}

	return oapi.AdminRefreshToken200JSONResponse{
		AccessToken:  tokens.AccessToken,
		RefreshToken: tokens.RefreshToken,
		TokenType:    tokens.TokenType,
		ExpiresIn:    tokens.ExpiresIn,
	}, nil
}

// AdminLogout encerra a sessão revogando o refresh token informado.
func (s *Server) AdminLogout(ctx context.Context, request oapi.AdminLogoutRequestObject) (oapi.AdminLogoutResponseObject, error) {
	if s.auth == nil {
		return nil, domain.NewInternalError("Serviço de autenticação não configurado", nil)
	}
	if request.Body == nil {
		return nil, domain.NewValidationError("Corpo da requisição é obrigatório", nil)
	}

	authUser, ok := middleware.GetAuthenticatedUser(ctx)
	if !ok {
		return nil, domain.NewUnauthorizedError("Usuário não autenticado")
	}

	if err := s.auth.Logout(ctx, authUser.ID, request.Body.RefreshToken); err != nil {
		return nil, err
	}

	return oapi.AdminLogout204Response{}, nil
}

// AdminGetMe retorna o perfil atualizado do funcionário autenticado.
func (s *Server) AdminGetMe(ctx context.Context, _ oapi.AdminGetMeRequestObject) (oapi.AdminGetMeResponseObject, error) {
	if s.auth == nil {
		return nil, domain.NewInternalError("Serviço de autenticação não configurado", nil)
	}

	authUser, ok := middleware.GetAuthenticatedUser(ctx)
	if !ok {
		return nil, domain.NewUnauthorizedError("Usuário não autenticado")
	}

	user, err := s.auth.Me(ctx, authUser.ID)
	if err != nil {
		return nil, err
	}

	return oapi.AdminGetMe200JSONResponse(toOAPIUser(*user)), nil
}

func toOAPIUser(u domain.User) oapi.UserResponse {
	return oapi.UserResponse{
		Id:        openapi_types.UUID(u.ID),
		Name:      u.Name,
		Email:     openapi_types.Email(u.Email),
		Role:      oapi.UserRole(u.Role),
		Active:    u.Active,
		CreatedAt: u.CreatedAt.UTC(),
		UpdatedAt: u.UpdatedAt.UTC(),
	}
}
