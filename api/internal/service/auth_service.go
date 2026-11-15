package service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/joao-accioly/artisan-store/api/internal/domain"
	"github.com/joao-accioly/artisan-store/api/internal/repository"
	"github.com/joao-accioly/artisan-store/api/internal/repository/sqlc"
	"github.com/joao-accioly/artisan-store/api/internal/security"
)

// AuthTokens representa o par de tokens emitido após login ou renovação.
type AuthTokens struct {
	AccessToken  string
	RefreshToken string
	TokenType    string
	ExpiresIn    int
}

// AuthService concentra as regras de negócio de autenticação e sessão.
type AuthService struct {
	pool       *pgxpool.Pool
	queries    *sqlc.Queries
	hasher     security.PasswordHasher
	jwtManager *security.JWTManager
	refreshTTL time.Duration
	log        *slog.Logger
	dummyHash  string
}

// NewAuthService constrói o serviço de autenticação.
func NewAuthService(
	pool *pgxpool.Pool,
	hasher security.PasswordHasher,
	jwtManager *security.JWTManager,
	refreshTTL time.Duration,
	log *slog.Logger,
) (*AuthService, error) {
	dummyHash, err := hasher.Hash("dummy-constant-time-password")
	if err != nil {
		return nil, fmt.Errorf("generate dummy hash: %w", err)
	}

	return &AuthService{
		pool:       pool,
		queries:    sqlc.New(pool),
		hasher:     hasher,
		jwtManager: jwtManager,
		refreshTTL: refreshTTL,
		log:        log,
		dummyHash:  dummyHash,
	}, nil
}

// Login autentica um funcionário por e-mail/senha e emite access token JWT + refresh token opaco.
func (s *AuthService) Login(ctx context.Context, email, password string) (*AuthTokens, *domain.User, error) {
	cleanEmail := strings.ToLower(strings.TrimSpace(email))
	if cleanEmail == "" || password == "" {
		return nil, nil, domain.NewValidationError("E-mail e senha são obrigatórios", map[string]any{
			"credentials": "informe e-mail e senha",
		})
	}

	dbUser, err := s.queries.GetUserByEmail(ctx, cleanEmail)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			// Executa verificação dummy para mitigar enumeração de usuários por tempo de resposta.
			if _, verifyErr := s.hasher.Verify(password, s.dummyHash); verifyErr != nil && s.log != nil {
				s.log.Warn("dummy password verify failed", slog.String("error", verifyErr.Error()))
			}
			return nil, nil, domain.NewUnauthorizedError("E-mail ou senha inválidos")
		}
		return nil, nil, domain.NewInternalError("Erro ao buscar usuário", err)
	}

	ok, err := s.hasher.Verify(password, dbUser.PasswordHash)
	if err != nil {
		return nil, nil, domain.NewInternalError("Erro ao verificar credenciais", err)
	}
	if !ok {
		return nil, nil, domain.NewUnauthorizedError("E-mail ou senha inválidos")
	}

	if !dbUser.Active {
		return nil, nil, domain.NewUnauthorizedError("Usuário inativo")
	}

	user := mapSQLCUser(dbUser)

	accessToken, expiresIn, err := s.jwtManager.GenerateAccessToken(user)
	if err != nil {
		return nil, nil, domain.NewInternalError("Erro ao gerar access token", err)
	}

	rawRefresh, refreshHash, err := security.GenerateOpaqueRefreshToken()
	if err != nil {
		return nil, nil, domain.NewInternalError("Erro ao gerar refresh token", err)
	}

	tokenID, err := uuid.NewV7()
	if err != nil {
		return nil, nil, domain.NewInternalError("Erro ao gerar identificador do token", err)
	}
	familyID, err := uuid.NewV7()
	if err != nil {
		return nil, nil, domain.NewInternalError("Erro ao gerar família do token", err)
	}

	if _, err := s.queries.CreateRefreshToken(ctx, sqlc.CreateRefreshTokenParams{
		ID:        tokenID,
		UserID:    user.ID,
		FamilyID:  familyID,
		TokenHash: refreshHash,
		ExpiresAt: time.Now().UTC().Add(s.refreshTTL),
	}); err != nil {
		return nil, nil, domain.NewInternalError("Erro ao persistir refresh token", err)
	}

	return &AuthTokens{
		AccessToken:  accessToken,
		RefreshToken: rawRefresh,
		TokenType:    "Bearer",
		ExpiresIn:    expiresIn,
	}, &user, nil
}

// Refresh valida e rotaciona um refresh token, detectando reuso de tokens já revogados.
func (s *AuthService) Refresh(ctx context.Context, rawRefreshToken string) (*AuthTokens, error) {
	if strings.TrimSpace(rawRefreshToken) == "" {
		return nil, domain.NewValidationError("Refresh token é obrigatório", map[string]any{
			"refresh_token": "campo obrigatório",
		})
	}

	tokenHash := security.HashToken(rawRefreshToken)
	now := time.Now().UTC()

	var (
		tokens       *AuthTokens
		failureError error
	)

	err := repository.WithTx(ctx, s.pool, func(q *sqlc.Queries) error {
		rt, err := q.GetRefreshTokenByHashForUpdate(ctx, tokenHash)
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				failureError = domain.NewUnauthorizedError("Refresh token inválido")
				return nil
			}
			return domain.NewInternalError("Erro ao consultar refresh token", err)
		}

		// Reuse detection: se um token já revogado for reapresentado, revoga toda a família.
		if rt.RevokedAt.Valid {
			if revErr := q.RevokeRefreshTokenFamily(ctx, rt.FamilyID); revErr != nil {
				return domain.NewInternalError("Erro ao revogar família de tokens", revErr)
			}
			if s.log != nil {
				s.log.Warn("refresh token reuse detected; session family revoked",
					slog.String("user_id", rt.UserID.String()),
					slog.String("family_id", rt.FamilyID.String()),
				)
			}
			failureError = domain.NewUnauthorizedError("Refresh token já utilizado; sessão encerrada por segurança")
			return nil
		}

		if !rt.ExpiresAt.After(now) {
			if revErr := q.RevokeRefreshToken(ctx, sqlc.RevokeRefreshTokenParams{
				ID:           rt.ID,
				ReplacedByID: pgtype.UUID{},
			}); revErr != nil {
				return domain.NewInternalError("Erro ao revogar refresh token expirado", revErr)
			}
			failureError = domain.NewUnauthorizedError("Refresh token expirado")
			return nil
		}

		dbUser, err := q.GetUserByID(ctx, rt.UserID)
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				failureError = domain.NewUnauthorizedError("Usuário não encontrado")
				return nil
			}
			return domain.NewInternalError("Erro ao buscar usuário do token", err)
		}

		if !dbUser.Active {
			if revErr := q.RevokeRefreshTokenFamily(ctx, rt.FamilyID); revErr != nil {
				return domain.NewInternalError("Erro ao revogar tokens de usuário inativo", revErr)
			}
			failureError = domain.NewUnauthorizedError("Usuário inativo")
			return nil
		}

		user := mapSQLCUser(dbUser)
		accessToken, expiresIn, err := s.jwtManager.GenerateAccessToken(user)
		if err != nil {
			return domain.NewInternalError("Erro ao gerar access token", err)
		}

		newRawRefresh, newRefreshHash, err := security.GenerateOpaqueRefreshToken()
		if err != nil {
			return domain.NewInternalError("Erro ao gerar novo refresh token", err)
		}

		newTokenID, err := uuid.NewV7()
		if err != nil {
			return domain.NewInternalError("Erro ao gerar identificador do novo token", err)
		}

		if _, err := q.CreateRefreshToken(ctx, sqlc.CreateRefreshTokenParams{
			ID:        newTokenID,
			UserID:    user.ID,
			FamilyID:  rt.FamilyID,
			TokenHash: newRefreshHash,
			ExpiresAt: now.Add(s.refreshTTL),
		}); err != nil {
			return domain.NewInternalError("Erro ao salvar novo refresh token", err)
		}

		if err := q.RevokeRefreshToken(ctx, sqlc.RevokeRefreshTokenParams{
			ID:           rt.ID,
			ReplacedByID: pgtype.UUID{Bytes: newTokenID, Valid: true},
		}); err != nil {
			return domain.NewInternalError("Erro ao rotacionar refresh token", err)
		}

		tokens = &AuthTokens{
			AccessToken:  accessToken,
			RefreshToken: newRawRefresh,
			TokenType:    "Bearer",
			ExpiresIn:    expiresIn,
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	if failureError != nil {
		return nil, failureError
	}

	return tokens, nil
}

// Logout revoga o refresh token fornecido caso pertença ao usuário autenticado.
func (s *AuthService) Logout(ctx context.Context, userID uuid.UUID, rawRefreshToken string) error {
	if strings.TrimSpace(rawRefreshToken) == "" {
		return domain.NewValidationError("Refresh token é obrigatório", map[string]any{
			"refresh_token": "campo obrigatório",
		})
	}

	tokenHash := security.HashToken(rawRefreshToken)
	rt, err := s.queries.GetRefreshTokenByHash(ctx, tokenHash)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		return domain.NewInternalError("Erro ao consultar refresh token", err)
	}

	if rt.UserID != userID {
		return domain.NewForbiddenError("Refresh token não pertence ao usuário autenticado")
	}

	if err := s.queries.RevokeRefreshToken(ctx, sqlc.RevokeRefreshTokenParams{
		ID:           rt.ID,
		ReplacedByID: pgtype.UUID{},
	}); err != nil {
		return domain.NewInternalError("Erro ao revogar refresh token", err)
	}

	return nil
}

// Me retorna os dados atualizados do usuário autenticado.
func (s *AuthService) Me(ctx context.Context, userID uuid.UUID) (*domain.User, error) {
	dbUser, err := s.queries.GetUserByID(ctx, userID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domain.NewUnauthorizedError("Usuário autenticado não encontrado")
		}
		return nil, domain.NewInternalError("Erro ao carregar perfil do usuário", err)
	}

	if !dbUser.Active {
		return nil, domain.NewUnauthorizedError("Usuário inativo")
	}

	user := mapSQLCUser(dbUser)
	return &user, nil
}

func mapSQLCUser(u sqlc.User) domain.User {
	return domain.User{
		ID:           u.ID,
		Name:         u.Name,
		Email:        u.Email,
		PasswordHash: u.PasswordHash,
		Role:         domain.Role(u.Role),
		Active:       u.Active,
		CreatedAt:    u.CreatedAt,
		UpdatedAt:    u.UpdatedAt,
	}
}
