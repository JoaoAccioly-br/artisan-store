package response

import (
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/joao-accioly/artisan-store/api/internal/domain"
	"github.com/joao-accioly/artisan-store/api/internal/http/oapi"
)

// JSON escreve uma resposta JSON com o status HTTP informado.
func JSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)

	if payload == nil {
		return
	}

	if err := json.NewEncoder(w).Encode(payload); err != nil {
		slog.Default().Error("failed to encode JSON response", slog.String("error", err.Error()))
	}
}

// ToErrorResponse converte um erro para status HTTP e payload padronizado do contrato OpenAPI.
func ToErrorResponse(err error, log *slog.Logger) (int, oapi.ErrorResponse) {
	domErr, ok := domain.AsDomainError(err)
	if !ok {
		if log != nil {
			log.Error("unexpected internal error", slog.String("error", err.Error()))
		}
		return http.StatusInternalServerError, oapi.ErrorResponse{
			Error: oapi.ErrorDetail{
				Code:    string(domain.CodeInternal),
				Message: "Erro interno inesperado",
			},
		}
	}

	status := statusFromCode(domErr.Code)
	if status >= http.StatusInternalServerError && log != nil {
		log.Error("domain server error",
			slog.String("code", string(domErr.Code)),
			slog.String("message", domErr.Message),
			slog.Any("error", domErr.Err),
		)
	}

	var details *map[string]any
	if len(domErr.Details) > 0 {
		d := domErr.Details
		details = &d
	}

	msg := domErr.Message
	if status == http.StatusInternalServerError && msg == "" {
		msg = "Erro interno inesperado"
	}

	return status, oapi.ErrorResponse{
		Error: oapi.ErrorDetail{
			Code:    string(domErr.Code),
			Message: msg,
			Details: details,
		},
	}
}

// Error escreve uma resposta de erro JSON padronizada mapeada a partir de um erro de domínio.
func Error(w http.ResponseWriter, log *slog.Logger, err error) {
	status, body := ToErrorResponse(err, log)
	JSON(w, status, body)
}

func statusFromCode(code domain.ErrorCode) int {
	switch code {
	case domain.CodeValidation, domain.CodeInvalidJSON:
		return http.StatusBadRequest
	case domain.CodeUnauthorized:
		return http.StatusUnauthorized
	case domain.CodeForbidden:
		return http.StatusForbidden
	case domain.CodeNotFound:
		return http.StatusNotFound
	case domain.CodeMethodNotAllowed:
		return http.StatusMethodNotAllowed
	case domain.CodeConflict:
		return http.StatusConflict
	case domain.CodeRateLimited:
		return http.StatusTooManyRequests
	case domain.CodeServiceUnavailable:
		return http.StatusServiceUnavailable
	default:
		return http.StatusInternalServerError
	}
}
