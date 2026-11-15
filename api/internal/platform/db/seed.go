package db

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/joao-accioly/artisan-store/api/internal/config"
	"github.com/joao-accioly/artisan-store/api/internal/domain"
	"github.com/joao-accioly/artisan-store/api/internal/repository/sqlc"
	"github.com/joao-accioly/artisan-store/api/internal/security"
)

// SeedAdmin cria o primeiro usuário administrador caso a tabela users esteja vazia
// e as credenciais de seed tenham sido fornecidas na configuração.
func SeedAdmin(ctx context.Context, pool *pgxpool.Pool, cfg *config.Config, hasher security.PasswordHasher, log *slog.Logger) error {
	email := strings.ToLower(strings.TrimSpace(cfg.SeedAdminEmail))
	if email == "" || cfg.SeedAdminPassword == "" {
		return nil
	}

	queries := sqlc.New(pool)
	count, err := queries.CountUsers(ctx)
	if err != nil {
		return fmt.Errorf("count users for seed: %w", err)
	}
	if count > 0 {
		return nil
	}

	if len(cfg.SeedAdminPassword) < 8 {
		return fmt.Errorf("invalid SEED_ADMIN_PASSWORD: must be at least 8 characters")
	}

	name := strings.TrimSpace(cfg.SeedAdminName)
	if name == "" {
		name = "Administrador"
	}

	hash, err := hasher.Hash(cfg.SeedAdminPassword)
	if err != nil {
		return fmt.Errorf("hash seed admin password: %w", err)
	}

	id, err := uuid.NewV7()
	if err != nil {
		return fmt.Errorf("generate seed admin uuid: %w", err)
	}

	if _, err := queries.CreateUser(ctx, sqlc.CreateUserParams{
		ID:           id,
		Name:         name,
		Email:        email,
		PasswordHash: hash,
		Role:         string(domain.RoleAdmin),
		Active:       true,
	}); err != nil {
		return fmt.Errorf("create seed admin user: %w", err)
	}

	if log != nil {
		log.Info("seed admin user created", slog.String("email", email))
	}
	return nil
}
