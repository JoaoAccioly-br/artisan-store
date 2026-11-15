package service

import (
	"context"
	"errors"
	"net/mail"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/joao-accioly/artisan-store/api/internal/domain"
	"github.com/joao-accioly/artisan-store/api/internal/repository"
	"github.com/joao-accioly/artisan-store/api/internal/repository/sqlc"
	"github.com/joao-accioly/artisan-store/api/internal/security"
)

const pgUniqueViolationCode = "23505"

// CreateUserInput agrupa os dados para cadastro de funcionário.
type CreateUserInput struct {
	Name     string
	Email    string
	Password string
	Role     domain.Role
}

// UpdateUserInput agrupa os campos editáveis de um funcionário.
type UpdateUserInput struct {
	Name     *string
	Role     *domain.Role
	Active   *bool
	Password *string
}

// UserService implementa a gestão administrativa de funcionários.
type UserService struct {
	pool    *pgxpool.Pool
	queries *sqlc.Queries
	hasher  security.PasswordHasher
}

// NewUserService constrói o serviço de usuários.
func NewUserService(pool *pgxpool.Pool, hasher security.PasswordHasher) *UserService {
	return &UserService{
		pool:    pool,
		queries: sqlc.New(pool),
		hasher:  hasher,
	}
}

// List retorna funcionários paginados via limit/offset com filtro opcional por papel.
func (s *UserService) List(ctx context.Context, role *domain.Role, page, pageSize int) ([]domain.User, domain.PaginationMeta, error) {
	if page <= 0 {
		page = 1
	}
	if pageSize <= 0 {
		pageSize = 20
	}
	if pageSize > 100 {
		return nil, domain.PaginationMeta{}, domain.NewValidationError("page_size não pode exceder 100", map[string]any{
			"page_size": "máximo permitido é 100",
		})
	}

	var roleFilter pgtype.Text
	if role != nil {
		if !role.IsValid() {
			return nil, domain.PaginationMeta{}, domain.NewValidationError("Papel inválido no filtro", map[string]any{
				"role": "deve ser admin, atendente, cozinha ou entregador",
			})
		}
		roleFilter = pgtype.Text{String: string(*role), Valid: true}
	}

	total, err := s.queries.CountUsersByFilter(ctx, roleFilter)
	if err != nil {
		return nil, domain.PaginationMeta{}, domain.NewInternalError("Erro ao contar usuários", err)
	}

	offset := int32((page - 1) * pageSize)
	rows, err := s.queries.ListUsers(ctx, sqlc.ListUsersParams{
		Role:   roleFilter,
		Limit:  int32(pageSize),
		Offset: offset,
	})
	if err != nil {
		return nil, domain.PaginationMeta{}, domain.NewInternalError("Erro ao listar usuários", err)
	}

	users := make([]domain.User, 0, len(rows))
	for _, row := range rows {
		users = append(users, mapSQLCUser(row))
	}

	return users, domain.NewPaginationMeta(page, pageSize, total), nil
}

// GetByID busca um funcionário pelo identificador UUIDv7.
func (s *UserService) GetByID(ctx context.Context, id uuid.UUID) (*domain.User, error) {
	dbUser, err := s.queries.GetUserByID(ctx, id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domain.NewNotFoundError("Usuário não encontrado")
		}
		return nil, domain.NewInternalError("Erro ao buscar usuário", err)
	}

	user := mapSQLCUser(dbUser)
	return &user, nil
}

// Create valida os dados, gera hash Argon2id e cadastra um novo funcionário.
func (s *UserService) Create(ctx context.Context, input CreateUserInput) (*domain.User, error) {
	name := strings.TrimSpace(input.Name)
	email := strings.ToLower(strings.TrimSpace(input.Email))

	details := make(map[string]any)
	if utf8.RuneCountInString(name) < 2 {
		details["name"] = "deve ter pelo menos 2 caracteres"
	}
	if !isValidEmail(email) {
		details["email"] = "formato de e-mail inválido"
	}
	if len(input.Password) < 8 || len(input.Password) > 128 {
		details["password"] = "deve ter entre 8 e 128 caracteres"
	}
	if !input.Role.IsValid() {
		details["role"] = "deve ser admin, atendente, cozinha ou entregador"
	}
	if len(details) > 0 {
		return nil, domain.NewValidationError("Dados inválidos para criação de usuário", details)
	}

	passwordHash, err := s.hasher.Hash(input.Password)
	if err != nil {
		return nil, domain.NewInternalError("Erro ao gerar hash da senha", err)
	}

	userID, err := uuid.NewV7()
	if err != nil {
		return nil, domain.NewInternalError("Erro ao gerar identificador do usuário", err)
	}

	dbUser, err := s.queries.CreateUser(ctx, sqlc.CreateUserParams{
		ID:           userID,
		Name:         name,
		Email:        email,
		PasswordHash: passwordHash,
		Role:         string(input.Role),
		Active:       true,
	})
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == pgUniqueViolationCode {
			return nil, domain.NewConflictError("E-mail já cadastrado")
		}
		return nil, domain.NewInternalError("Erro ao criar usuário", err)
	}

	user := mapSQLCUser(dbUser)
	return &user, nil
}

// Update altera dados cadastrais, papel, status ativo ou senha de um funcionário,
// revogando refresh tokens ativos quando a segurança da conta é afetada.
func (s *UserService) Update(ctx context.Context, actorID, targetID uuid.UUID, input UpdateUserInput) (*domain.User, error) {
	if input.Name == nil && input.Role == nil && input.Active == nil && input.Password == nil {
		return nil, domain.NewValidationError("Informe ao menos um campo para atualização", nil)
	}

	details := make(map[string]any)
	if input.Name != nil && utf8.RuneCountInString(strings.TrimSpace(*input.Name)) < 2 {
		details["name"] = "deve ter pelo menos 2 caracteres"
	}
	if input.Role != nil && !input.Role.IsValid() {
		details["role"] = "deve ser admin, atendente, cozinha ou entregador"
	}
	if input.Password != nil && (len(*input.Password) < 8 || len(*input.Password) > 128) {
		details["password"] = "deve ter entre 8 e 128 caracteres"
	}
	if len(details) > 0 {
		return nil, domain.NewValidationError("Dados inválidos para atualização de usuário", details)
	}

	if actorID == targetID {
		if input.Active != nil && !*input.Active {
			return nil, domain.NewValidationError("Não é permitido desativar o próprio usuário administrador", map[string]any{
				"active": "administrador logado não pode se auto-desativar",
			})
		}
		if input.Role != nil && *input.Role != domain.RoleAdmin {
			return nil, domain.NewValidationError("Não é permitido rebaixar o próprio papel de administrador", map[string]any{
				"role": "administrador logado não pode remover o próprio papel admin",
			})
		}
	}

	var updated *domain.User

	err := repository.WithTx(ctx, s.pool, func(q *sqlc.Queries) error {
		current, err := q.GetUserByID(ctx, targetID)
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return domain.NewNotFoundError("Usuário não encontrado")
			}
			return domain.NewInternalError("Erro ao buscar usuário para atualização", err)
		}

		nextName := current.Name
		if input.Name != nil {
			nextName = strings.TrimSpace(*input.Name)
		}

		nextRole := current.Role
		roleChanged := false
		if input.Role != nil && string(*input.Role) != current.Role {
			nextRole = string(*input.Role)
			roleChanged = true
		}

		nextActive := current.Active
		deactivated := false
		if input.Active != nil {
			nextActive = *input.Active
			if current.Active && !nextActive {
				deactivated = true
			}
		}

		nextPasswordHash := current.PasswordHash
		passwordChanged := false
		if input.Password != nil {
			hashed, hashErr := s.hasher.Hash(*input.Password)
			if hashErr != nil {
				return domain.NewInternalError("Erro ao gerar hash da nova senha", hashErr)
			}
			nextPasswordHash = hashed
			passwordChanged = true
		}

		dbUpdated, err := q.UpdateUser(ctx, sqlc.UpdateUserParams{
			ID:           targetID,
			Name:         nextName,
			Role:         nextRole,
			Active:       nextActive,
			PasswordHash: nextPasswordHash,
		})
		if err != nil {
			return domain.NewInternalError("Erro ao atualizar usuário", err)
		}

		if deactivated || passwordChanged || roleChanged {
			if revErr := q.RevokeAllUserRefreshTokens(ctx, targetID); revErr != nil {
				return domain.NewInternalError("Erro ao revogar sessões do usuário", revErr)
			}
		}

		u := mapSQLCUser(dbUpdated)
		updated = &u
		return nil
	})
	if err != nil {
		return nil, err
	}

	return updated, nil
}

func isValidEmail(email string) bool {
	if email == "" || len(email) > 254 || strings.ContainsAny(email, " \t\r\n") {
		return false
	}
	parsed, err := mail.ParseAddress(email)
	if err != nil {
		return false
	}
	return parsed.Address == email && strings.Contains(email, ".")
}
