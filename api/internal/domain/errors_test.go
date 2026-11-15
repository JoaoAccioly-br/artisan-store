package domain_test

import (
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/joao-accioly/artisan-store/api/internal/domain"
)

func TestDomainError_UnwrapAndAs(t *testing.T) {
	cause := errors.New("connection refused")
	err := domain.NewServiceUnavailableError("Banco indisponível", cause)

	assert.ErrorIs(t, err, cause)
	assert.Equal(t, cause, err.Unwrap())
	assert.Equal(t, "SERVICE_UNAVAILABLE: Banco indisponível: connection refused", err.Error())

	domErr, ok := domain.AsDomainError(err)
	require.True(t, ok)
	assert.Equal(t, domain.CodeServiceUnavailable, domErr.Code)
	assert.Equal(t, "Banco indisponível", domErr.Message)

	wrapped := fmt.Errorf("health check failed: %w", err)
	domErrWrapped, ok := domain.AsDomainError(wrapped)
	require.True(t, ok)
	assert.Equal(t, domain.CodeServiceUnavailable, domErrWrapped.Code)
}

func TestDomainError_WithoutWrappedCause(t *testing.T) {
	err := domain.NewNotFoundError("produto não encontrado")

	assert.NoError(t, err.Unwrap())
	assert.Equal(t, "RESOURCE_NOT_FOUND: produto não encontrado", err.Error())
}

func TestDomainError_Constructors(t *testing.T) {
	cause := errors.New("underlying cause")

	tests := []struct {
		name         string
		err          *domain.Error
		expectedCode domain.ErrorCode
		expectedMsg  string
		expectedErr  error
	}{
		{
			name:         "validation",
			err:          domain.NewValidationError("dados inválidos", map[string]any{"field": "email"}),
			expectedCode: domain.CodeValidation,
			expectedMsg:  "dados inválidos",
		},
		{
			name:         "invalid json",
			err:          domain.NewInvalidJSONError(cause),
			expectedCode: domain.CodeInvalidJSON,
			expectedMsg:  "Corpo da requisição em JSON inválido",
			expectedErr:  cause,
		},
		{
			name:         "not found",
			err:          domain.NewNotFoundError("não encontrado"),
			expectedCode: domain.CodeNotFound,
			expectedMsg:  "não encontrado",
		},
		{
			name:         "conflict",
			err:          domain.NewConflictError("conflito"),
			expectedCode: domain.CodeConflict,
			expectedMsg:  "conflito",
		},
		{
			name:         "unauthorized",
			err:          domain.NewUnauthorizedError("não autenticado"),
			expectedCode: domain.CodeUnauthorized,
			expectedMsg:  "não autenticado",
		},
		{
			name:         "forbidden",
			err:          domain.NewForbiddenError("acesso negado"),
			expectedCode: domain.CodeForbidden,
			expectedMsg:  "acesso negado",
		},
		{
			name:         "rate limited",
			err:          domain.NewRateLimitedError("muitas requisições"),
			expectedCode: domain.CodeRateLimited,
			expectedMsg:  "muitas requisições",
		},
		{
			name:         "service unavailable",
			err:          domain.NewServiceUnavailableError("indisponível", cause),
			expectedCode: domain.CodeServiceUnavailable,
			expectedMsg:  "indisponível",
			expectedErr:  cause,
		},
		{
			name:         "internal",
			err:          domain.NewInternalError("erro interno", cause),
			expectedCode: domain.CodeInternal,
			expectedMsg:  "erro interno",
			expectedErr:  cause,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.expectedCode, tc.err.Code)
			assert.Equal(t, tc.expectedMsg, tc.err.Message)
			assert.Equal(t, tc.expectedErr, tc.err.Unwrap())

			extracted, ok := domain.AsDomainError(tc.err)
			require.True(t, ok)
			assert.Equal(t, tc.err, extracted)
		})
	}
}

func TestDomainError_ValidationDetails(t *testing.T) {
	err := domain.NewValidationError("Campos inválidos", map[string]any{
		"email": "formato inválido",
	})

	domErr, ok := domain.AsDomainError(err)
	require.True(t, ok)
	assert.Equal(t, domain.CodeValidation, domErr.Code)
	assert.Equal(t, "formato inválido", domErr.Details["email"])
}

func TestAsDomainError_PlainAndNilError(t *testing.T) {
	domErr, ok := domain.AsDomainError(errors.New("plain error"))
	assert.False(t, ok)
	assert.Nil(t, domErr)

	nilDomErr, ok := domain.AsDomainError(nil)
	assert.False(t, ok)
	assert.Nil(t, nilDomErr)
}
