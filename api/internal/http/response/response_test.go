package response_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/joao-accioly/artisan-store/api/internal/domain"
	"github.com/joao-accioly/artisan-store/api/internal/http/oapi"
	"github.com/joao-accioly/artisan-store/api/internal/http/response"
	"github.com/joao-accioly/artisan-store/api/internal/platform/logger"
)

func TestError_Mapping(t *testing.T) {
	tests := []struct {
		name           string
		err            error
		expectedStatus int
		expectedCode   string
		expectedMsg    string
		expectDetails  bool
		expectLogText  string
	}{
		{
			name:           "validation error with details",
			err:            domain.NewValidationError("campo obrigatório", map[string]any{"name": "obrigatório"}),
			expectedStatus: http.StatusBadRequest,
			expectedCode:   string(domain.CodeValidation),
			expectedMsg:    "campo obrigatório",
			expectDetails:  true,
		},
		{
			name:           "invalid json",
			err:            domain.NewInvalidJSONError(errors.New("unexpected EOF")),
			expectedStatus: http.StatusBadRequest,
			expectedCode:   string(domain.CodeInvalidJSON),
			expectedMsg:    "Corpo da requisição em JSON inválido",
		},
		{
			name:           "unauthorized",
			err:            domain.NewUnauthorizedError("token ausente ou expirado"),
			expectedStatus: http.StatusUnauthorized,
			expectedCode:   string(domain.CodeUnauthorized),
			expectedMsg:    "token ausente ou expirado",
		},
		{
			name:           "forbidden",
			err:            domain.NewForbiddenError("acesso restrito a administradores"),
			expectedStatus: http.StatusForbidden,
			expectedCode:   string(domain.CodeForbidden),
			expectedMsg:    "acesso restrito a administradores",
		},
		{
			name:           "not found",
			err:            domain.NewNotFoundError("pedido não encontrado"),
			expectedStatus: http.StatusNotFound,
			expectedCode:   string(domain.CodeNotFound),
			expectedMsg:    "pedido não encontrado",
		},
		{
			name: "method not allowed",
			err: &domain.Error{
				Code:    domain.CodeMethodNotAllowed,
				Message: "método não permitido",
			},
			expectedStatus: http.StatusMethodNotAllowed,
			expectedCode:   string(domain.CodeMethodNotAllowed),
			expectedMsg:    "método não permitido",
		},
		{
			name:           "conflict",
			err:            domain.NewConflictError("e-mail já cadastrado"),
			expectedStatus: http.StatusConflict,
			expectedCode:   string(domain.CodeConflict),
			expectedMsg:    "e-mail já cadastrado",
		},
		{
			name:           "rate limited",
			err:            domain.NewRateLimitedError("muitas requisições"),
			expectedStatus: http.StatusTooManyRequests,
			expectedCode:   string(domain.CodeRateLimited),
			expectedMsg:    "muitas requisições",
		},
		{
			name:           "wrapped domain error preserves code",
			err:            fmt.Errorf("auth middleware: %w", domain.NewForbiddenError("sem permissão")),
			expectedStatus: http.StatusForbidden,
			expectedCode:   string(domain.CodeForbidden),
			expectedMsg:    "sem permissão",
		},
		{
			name:           "service unavailable logs internal cause",
			err:            domain.NewServiceUnavailableError("Banco indisponível", errors.New("dial tcp 127.0.0.1:5432: connect: connection refused")),
			expectedStatus: http.StatusServiceUnavailable,
			expectedCode:   string(domain.CodeServiceUnavailable),
			expectedMsg:    "Banco indisponível",
			expectLogText:  "connection refused",
		},
		{
			name:           "domain internal error logs cause without leaking it",
			err:            domain.NewInternalError("Falha ao processar pedido", errors.New("db secret constraint violation")),
			expectedStatus: http.StatusInternalServerError,
			expectedCode:   string(domain.CodeInternal),
			expectedMsg:    "Falha ao processar pedido",
			expectLogText:  "db secret constraint violation",
		},
		{
			name: "domain 500 with empty message falls back to default",
			err: &domain.Error{
				Code: domain.CodeInternal,
				Err:  errors.New("nil pointer in service"),
			},
			expectedStatus: http.StatusInternalServerError,
			expectedCode:   string(domain.CodeInternal),
			expectedMsg:    "Erro interno inesperado",
			expectLogText:  "nil pointer in service",
		},
		{
			name: "unknown domain error code defaults to 500",
			err: &domain.Error{
				Code:    domain.ErrorCode("UNKNOWN_CODE"),
				Message: "Falha desconhecida",
				Err:     errors.New("root cause"),
			},
			expectedStatus: http.StatusInternalServerError,
			expectedCode:   "UNKNOWN_CODE",
			expectedMsg:    "Falha desconhecida",
			expectLogText:  "root cause",
		},
		{
			name:           "unexpected plain error hides internals",
			err:            errors.New("sql: password leak"),
			expectedStatus: http.StatusInternalServerError,
			expectedCode:   string(domain.CodeInternal),
			expectedMsg:    "Erro interno inesperado",
			expectLogText:  "password leak",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var logBuf bytes.Buffer
			log := logger.NewWithWriter(&logBuf, "production", "info")

			rec := httptest.NewRecorder()
			response.Error(rec, log, tc.err)

			assert.Equal(t, tc.expectedStatus, rec.Code)
			assert.Equal(t, "application/json; charset=utf-8", rec.Header().Get("Content-Type"))

			var body oapi.ErrorResponse
			require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
			assert.Equal(t, tc.expectedCode, body.Error.Code)
			assert.Equal(t, tc.expectedMsg, body.Error.Message)

			if tc.expectDetails {
				require.NotNil(t, body.Error.Details)
				assert.Equal(t, "obrigatório", (*body.Error.Details)["name"])
			} else {
				assert.Nil(t, body.Error.Details)
				assert.NotContains(t, rec.Body.String(), `"details"`)
			}

			if tc.expectLogText != "" {
				assert.Contains(t, logBuf.String(), tc.expectLogText)
				assert.NotContains(t, body.Error.Message, tc.expectLogText)
			} else {
				assert.Empty(t, logBuf.String())
			}
		})
	}
}

func TestToErrorResponse_NilLoggerDoesNotPanic(t *testing.T) {
	t.Run("plain error with nil logger", func(t *testing.T) {
		status, body := response.ToErrorResponse(errors.New("boom"), nil)
		assert.Equal(t, http.StatusInternalServerError, status)
		assert.Equal(t, string(domain.CodeInternal), body.Error.Code)
		assert.Equal(t, "Erro interno inesperado", body.Error.Message)
	})

	t.Run("domain 500 error with nil logger", func(t *testing.T) {
		status, body := response.ToErrorResponse(domain.NewInternalError("falha", errors.New("cause")), nil)
		assert.Equal(t, http.StatusInternalServerError, status)
		assert.Equal(t, string(domain.CodeInternal), body.Error.Code)
		assert.Equal(t, "falha", body.Error.Message)
	})

	t.Run("empty details map is omitted", func(t *testing.T) {
		status, body := response.ToErrorResponse(domain.NewValidationError("inválido", map[string]any{}), nil)
		assert.Equal(t, http.StatusBadRequest, status)
		assert.Nil(t, body.Error.Details)
	})
}

func TestJSON(t *testing.T) {
	t.Run("nil payload writes status and header with empty body", func(t *testing.T) {
		rec := httptest.NewRecorder()
		response.JSON(rec, http.StatusNoContent, nil)

		assert.Equal(t, http.StatusNoContent, rec.Code)
		assert.Equal(t, "application/json; charset=utf-8", rec.Header().Get("Content-Type"))
		assert.Empty(t, rec.Body.String())
	})

	t.Run("struct payload is encoded as JSON", func(t *testing.T) {
		rec := httptest.NewRecorder()
		response.JSON(rec, http.StatusCreated, map[string]string{"id": "123"})

		assert.Equal(t, http.StatusCreated, rec.Code)
		assert.Equal(t, "application/json; charset=utf-8", rec.Header().Get("Content-Type"))

		var out map[string]string
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &out))
		assert.Equal(t, "123", out["id"])
	})
}
