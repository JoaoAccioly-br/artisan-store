package domain_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/joao-accioly/artisan-store/api/internal/domain"
)

func TestRole_IsValid(t *testing.T) {
	for _, r := range domain.AllRoles {
		assert.True(t, r.IsValid(), "expected role %q to be valid", r)
	}

	assert.Len(t, domain.AllRoles, 4)

	invalidRoles := []domain.Role{
		"",
		"ADMIN",
		" admin ",
		"gerente",
		"cliente",
		"superadmin",
	}
	for _, r := range invalidRoles {
		assert.False(t, r.IsValid(), "expected role %q to be invalid", r)
	}
}

func TestParseRole(t *testing.T) {
	tests := []struct {
		name     string
		raw      string
		expected domain.Role
		wantErr  bool
	}{
		{name: "admin exact", raw: "admin", expected: domain.RoleAdmin},
		{name: "atendente exact", raw: "atendente", expected: domain.RoleAtendente},
		{name: "cozinha exact", raw: "cozinha", expected: domain.RoleCozinha},
		{name: "entregador exact", raw: "entregador", expected: domain.RoleEntregador},
		{name: "trim and uppercase", raw: "  ADMIN  ", expected: domain.RoleAdmin},
		{name: "mixed case with tabs", raw: "\tAtEnDeNtE\n", expected: domain.RoleAtendente},
		{name: "cozinha with spaces", raw: " CoZiNhA ", expected: domain.RoleCozinha},
		{name: "empty", raw: "", wantErr: true},
		{name: "whitespace only", raw: "   \t\n ", wantErr: true},
		{name: "unknown role", raw: "gerente", wantErr: true},
		{name: "partial match", raw: "admin1", wantErr: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			role, err := domain.ParseRole(tc.raw)
			if tc.wantErr {
				require.Error(t, err)
				assert.Empty(t, role)
				domErr, ok := domain.AsDomainError(err)
				require.True(t, ok)
				assert.Equal(t, domain.CodeValidation, domErr.Code)
				assert.Contains(t, domErr.Details, "role")
				return
			}

			require.NoError(t, err)
			assert.Equal(t, tc.expected, role)
		})
	}
}

func TestNewPaginationMeta(t *testing.T) {
	tests := []struct {
		name       string
		page       int
		pageSize   int
		totalItems int64
		wantPages  int
	}{
		{name: "zero items", page: 1, pageSize: 20, totalItems: 0, wantPages: 0},
		{name: "negative items", page: 1, pageSize: 20, totalItems: -5, wantPages: 0},
		{name: "exact division", page: 1, pageSize: 20, totalItems: 40, wantPages: 2},
		{name: "single page exact", page: 1, pageSize: 10, totalItems: 10, wantPages: 1},
		{name: "with remainder", page: 2, pageSize: 20, totalItems: 41, wantPages: 3},
		{name: "single item", page: 1, pageSize: 20, totalItems: 1, wantPages: 1},
		{name: "pageSize zero avoids division by zero", page: 1, pageSize: 0, totalItems: 25, wantPages: 0},
		{name: "negative pageSize", page: 1, pageSize: -10, totalItems: 25, wantPages: 0},
		{name: "both zero", page: 1, pageSize: 0, totalItems: 0, wantPages: 0},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			meta := domain.NewPaginationMeta(tc.page, tc.pageSize, tc.totalItems)
			assert.Equal(t, tc.page, meta.Page)
			assert.Equal(t, tc.pageSize, meta.PageSize)
			assert.Equal(t, tc.totalItems, meta.TotalItems)
			assert.Equal(t, tc.wantPages, meta.TotalPages)
		})
	}
}
