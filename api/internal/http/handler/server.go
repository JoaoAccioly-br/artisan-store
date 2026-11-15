package handler

import (
	"context"

	"github.com/google/uuid"

	"github.com/joao-accioly/artisan-store/api/internal/domain"
	"github.com/joao-accioly/artisan-store/api/internal/service"
)

// DBPinger abstrai a verificação de conectividade com o banco de dados.
type DBPinger interface {
	Ping(ctx context.Context) error
}

// AuthUseCase define as operações de autenticação consumidas pelos handlers HTTP.
type AuthUseCase interface {
	Login(ctx context.Context, email, password string) (*service.AuthTokens, *domain.User, error)
	Refresh(ctx context.Context, rawRefreshToken string) (*service.AuthTokens, error)
	Logout(ctx context.Context, userID uuid.UUID, rawRefreshToken string) error
	Me(ctx context.Context, userID uuid.UUID) (*domain.User, error)
}

// UserUseCase define as operações administrativas de usuários consumidas pelos handlers HTTP.
type UserUseCase interface {
	List(ctx context.Context, role *domain.Role, page, pageSize int) ([]domain.User, domain.PaginationMeta, error)
	Create(ctx context.Context, input service.CreateUserInput) (*domain.User, error)
	GetByID(ctx context.Context, id uuid.UUID) (*domain.User, error)
	Update(ctx context.Context, actorID, targetID uuid.UUID, input service.UpdateUserInput) (*domain.User, error)
}

// ServerDeps agrupa as dependências dos handlers da API.
type ServerDeps struct {
	DB    DBPinger
	Auth  AuthUseCase
	Users UserUseCase
}

// Server implementa o contrato oapi.StrictServerInterface da API.
type Server struct {
	db    DBPinger
	auth  AuthUseCase
	users UserUseCase
}

// NewServer constrói o servidor HTTP com as dependências necessárias.
func NewServer(deps ServerDeps) *Server {
	return &Server{
		db:    deps.DB,
		auth:  deps.Auth,
		users: deps.Users,
	}
}
