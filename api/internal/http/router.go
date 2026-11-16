package http

import (
	"log/slog"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/joao-accioly/artisan-store/api/internal/config"
	"github.com/joao-accioly/artisan-store/api/internal/domain"
	"github.com/joao-accioly/artisan-store/api/internal/http/handler"
	"github.com/joao-accioly/artisan-store/api/internal/http/middleware"
	"github.com/joao-accioly/artisan-store/api/internal/http/oapi"
	"github.com/joao-accioly/artisan-store/api/internal/http/response"
	"github.com/joao-accioly/artisan-store/api/internal/security"
)

// RouterDeps agrupa as dependências necessárias para montar o roteador HTTP.
type RouterDeps struct {
	DB             handler.DBPinger
	JWTManager     *security.JWTManager
	Auth           handler.AuthUseCase
	Users          handler.UserUseCase
	Catalog        handler.CatalogUseCase
	MaxUploadBytes int64
	UploadHandler  http.Handler
}

// NewRouter monta o roteador principal chi com middlewares globais, tratamento de erros
// padronizado, autenticação/RBAC e handlers gerados a partir do contrato OpenAPI.
func NewRouter(cfg *config.Config, log *slog.Logger, deps RouterDeps) http.Handler {
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

	if deps.UploadHandler != nil {
		r.Handle("/uploads/*", deps.UploadHandler)
	}

	srv := handler.NewServer(handler.ServerDeps{
		DB:             deps.DB,
		Auth:           deps.Auth,
		Users:          deps.Users,
		Catalog:        deps.Catalog,
		MaxUploadBytes: deps.MaxUploadBytes,
	})

	strictMiddlewares := []oapi.StrictMiddlewareFunc{
		middleware.NewStrictAuthMiddleware(deps.JWTManager, middleware.DefaultOperationPolicies()),
	}

	strictHandler := oapi.NewStrictHandlerWithOptions(srv, strictMiddlewares, oapi.StrictHTTPServerOptions{
		RequestErrorHandlerFunc: func(w http.ResponseWriter, _ *http.Request, err error) {
			if strings.Contains(err.Error(), "decode JSON body") {
				response.Error(w, log, domain.NewInvalidJSONError(err))
				return
			}
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
