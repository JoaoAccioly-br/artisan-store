package domain

import (
	"errors"
	"fmt"
)

// ErrorCode representa os códigos de erro de domínio expostos pela API.
type ErrorCode string

// Códigos de erro de domínio padronizados da API.
const (
	CodeValidation         ErrorCode = "VALIDATION_ERROR"
	CodeInvalidJSON        ErrorCode = "INVALID_JSON"
	CodeNotFound           ErrorCode = "RESOURCE_NOT_FOUND"
	CodeConflict           ErrorCode = "RESOURCE_CONFLICT"
	CodeUnauthorized       ErrorCode = "UNAUTHORIZED"
	CodeForbidden          ErrorCode = "FORBIDDEN"
	CodeRateLimited        ErrorCode = "RATE_LIMITED"
	CodeMethodNotAllowed   ErrorCode = "METHOD_NOT_ALLOWED"
	CodeServiceUnavailable ErrorCode = "SERVICE_UNAVAILABLE"
	CodeInternal           ErrorCode = "INTERNAL_ERROR"
)

// Error encapsula um erro de domínio tipado com código, mensagem e detalhes opcionais.
type Error struct {
	Code    ErrorCode
	Message string
	Details map[string]any
	Err     error
}

// Error implementa a interface padrão error.
func (e *Error) Error() string {
	if e.Err != nil {
		return fmt.Sprintf("%s: %s: %v", e.Code, e.Message, e.Err)
	}
	return fmt.Sprintf("%s: %s", e.Code, e.Message)
}

// Unwrap permite compatibilidade com errors.Is e errors.As.
func (e *Error) Unwrap() error {
	return e.Err
}

// NewValidationError cria um erro de validação com mapa opcional de campos.
func NewValidationError(message string, details map[string]any) *Error {
	return &Error{
		Code:    CodeValidation,
		Message: message,
		Details: details,
	}
}

// NewInvalidJSONError cria um erro para corpo JSON malformado.
func NewInvalidJSONError(err error) *Error {
	return &Error{
		Code:    CodeInvalidJSON,
		Message: "Corpo da requisição em JSON inválido",
		Err:     err,
	}
}

// NewNotFoundError cria um erro para recurso inexistente.
func NewNotFoundError(message string) *Error {
	return &Error{
		Code:    CodeNotFound,
		Message: message,
	}
}

// NewConflictError cria um erro para conflito de estado ou unicidade.
func NewConflictError(message string) *Error {
	return &Error{
		Code:    CodeConflict,
		Message: message,
	}
}

// NewUnauthorizedError cria um erro de autenticação ausente ou inválida.
func NewUnauthorizedError(message string) *Error {
	return &Error{
		Code:    CodeUnauthorized,
		Message: message,
	}
}

// NewForbiddenError cria um erro de permissão insuficiente.
func NewForbiddenError(message string) *Error {
	return &Error{
		Code:    CodeForbidden,
		Message: message,
	}
}

// NewRateLimitedError cria um erro de limite de requisições excedido.
func NewRateLimitedError(message string) *Error {
	return &Error{
		Code:    CodeRateLimited,
		Message: message,
	}
}

// NewServiceUnavailableError cria um erro de dependência externa indisponível.
func NewServiceUnavailableError(message string, err error) *Error {
	return &Error{
		Code:    CodeServiceUnavailable,
		Message: message,
		Err:     err,
	}
}

// NewInternalError cria um erro interno encapsulando a causa original para log.
func NewInternalError(message string, err error) *Error {
	return &Error{
		Code:    CodeInternal,
		Message: message,
		Err:     err,
	}
}

// AsDomainError extrai um *domain.Error da cadeia de erros, se existir.
func AsDomainError(err error) (*Error, bool) {
	var domErr *Error
	if errors.As(err, &domErr) {
		return domErr, true
	}
	return nil, false
}
