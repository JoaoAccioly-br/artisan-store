package domain

import (
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

// Role representa o papel (nível de permissão) de um funcionário no sistema.
type Role string

// Papéis válidos de funcionários.
const (
	RoleAdmin      Role = "admin"
	RoleAtendente  Role = "atendente"
	RoleCozinha    Role = "cozinha"
	RoleEntregador Role = "entregador"
)

// AllRoles lista todos os papéis válidos para validação e RBAC.
var AllRoles = []Role{
	RoleAdmin,
	RoleAtendente,
	RoleCozinha,
	RoleEntregador,
}

// IsValid informa se o papel pertence ao conjunto permitido no domínio.
func (r Role) IsValid() bool {
	switch r {
	case RoleAdmin, RoleAtendente, RoleCozinha, RoleEntregador:
		return true
	default:
		return false
	}
}

// ParseRole converte e valida uma string para domain.Role.
func ParseRole(raw string) (Role, error) {
	role := Role(strings.ToLower(strings.TrimSpace(raw)))
	if !role.IsValid() {
		return "", NewValidationError(fmt.Sprintf("Papel inválido: %q", raw), map[string]any{
			"role": "deve ser um de: admin, atendente, cozinha, entregador",
		})
	}
	return role, nil
}

// User representa um funcionário cadastrado no sistema.
type User struct {
	ID           uuid.UUID
	Name         string
	Email        string
	PasswordHash string
	Role         Role
	Active       bool
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

// PaginationMeta representa os metadados de paginação limit/offset.
type PaginationMeta struct {
	Page       int
	PageSize   int
	TotalItems int64
	TotalPages int
}

// NewPaginationMeta calcula os metadados de paginação a partir do total de itens.
func NewPaginationMeta(page, pageSize int, totalItems int64) PaginationMeta {
	totalPages := 0
	if totalItems > 0 && pageSize > 0 {
		totalPages = int((totalItems + int64(pageSize) - 1) / int64(pageSize))
	}
	return PaginationMeta{
		Page:       page,
		PageSize:   pageSize,
		TotalItems: totalItems,
		TotalPages: totalPages,
	}
}
