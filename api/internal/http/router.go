package http

import (
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/joao-accioly/artisan-store/api/internal/config"
	"github.com/joao-accioly/artisan-store/api/internal/domain"
	"github.com/joao-accioly/artisan-store/api/internal/http/handler"
	"github.com/joao-accioly/artisan-store/api/internal/http/middleware"
	"github.com/joao-accioly/artisan-store/api/internal/http/oapi"
	"github.com/joao-accioly/artisan-store/api/internal/http/response"
)

// NewRouter monta o roteador principal chi com middlewares globais, tratamento de erros
// padronizado e handlers gerados a partir do contrato OpenAPI.
func NewRouter(cfg *config.Config, log *slog.Logger, db handler.DBPinger) http.Handler {
	r := chi.NewRouter()

	r.Use(middleware.RequestID)
	r.Use(middleware.Recoverer(log))
	r.Use(middleware.Logger(log))
	r.Use(middleware.CORS(cfg.AllowedOrigins))
	r.Use(middleware.RateLimit(cfg.RateLimitRPM, log))

	r.NotFound(func(w http.ResponseWriter, _ *http.Request) {
		response.Error(w, log, domain.NewNotFoundError("Rota não encontrada"))
	})

	r.MethodNotAllowed(func(w http.ResponseWriter, _ *http.Request) {
		response.Error(w, log, &domain.Error{
			Code:    domain.CodeMethodNotAllowed,
			Message: "Método HTTP não permitido para esta rota",
		})
	})

	srv := handler.NewServer(db)
	strictHandler := oapi.NewStrictHandlerWithOptions(srv, nil, oapi.StrictHTTPServerOptions{
		RequestErrorHandlerFunc: func(w http.ResponseWriter, _ *http.Request, err error) {
			response.Error(w, log, domain.NewValidationError(err.Error(), nil))
		},
		ResponseErrorHandlerFunc: func(w http.ResponseWriter, _ *http.Request, err error) {
			response.Error(w, log, err)
		},
	})

	return oapi.HandlerWithOptions(strictHandler, oapi.ChiServerOptions{
		BaseRouter: r,
		ErrorHandlerFunc: func(w http.ResponseWriter, _ *http.Request, err error) {
			response.Error(w, log, domain.NewValidationError(err.Error(), nil))
		},
	})
}
