package middleware

import (
	"context"
	"net/http"
	"slices"
	"strings"

	"github.com/google/uuid"

	"github.com/joao-accioly/artisan-store/api/internal/domain"
	"github.com/joao-accioly/artisan-store/api/internal/http/oapi"
	"github.com/joao-accioly/artisan-store/api/internal/security"
)

type contextKey string

const authUserContextKey contextKey = "authenticated_user"

// AuthenticatedUser representa a identidade autenticada extraída do JWT no contexto da requisição.
type AuthenticatedUser struct {
	ID    uuid.UUID
	Email string
	Role  domain.Role
}

// OperationPolicy define se uma operação OpenAPI é pública ou quais papéis têm acesso.
type OperationPolicy struct {
	Public       bool
	AllowedRoles []domain.Role
}

// DefaultOperationPolicies mapeia explicitamente a política de acesso por operationID do OpenAPI.
// Operações não cadastradas falham fechadas (negam acesso por padrão).
func DefaultOperationPolicies() map[string]OperationPolicy {
	adminOnly := []domain.Role{domain.RoleAdmin}

	return map[string]OperationPolicy{
		// System & Public
		"getHealthz":       {Public: true},
		"publicGetStore":   {Public: true},
		"publicGetMenu":    {Public: true},
		"publicGetProduct": {Public: true},

		// Auth
		"adminLogin":        {Public: true},
		"adminRefreshToken": {Public: true},
		"adminLogout":       {AllowedRoles: domain.AllRoles},
		"adminGetMe":        {AllowedRoles: domain.AllRoles},

		// Users
		"adminListUsers":  {AllowedRoles: adminOnly},
		"adminCreateUser": {AllowedRoles: adminOnly},
		"adminGetUser":    {AllowedRoles: adminOnly},
		"adminUpdateUser": {AllowedRoles: adminOnly},

		// Settings
		"adminGetSettings":    {AllowedRoles: domain.AllRoles},
		"adminUpdateSettings": {AllowedRoles: adminOnly},

		// Catalog
		"adminListCategories":       {AllowedRoles: domain.AllRoles},
		"adminGetCategory":          {AllowedRoles: domain.AllRoles},
		"adminCreateCategory":       {AllowedRoles: adminOnly},
		"adminUpdateCategory":       {AllowedRoles: adminOnly},
		"adminDeleteCategory":       {AllowedRoles: adminOnly},
		"adminListProducts":         {AllowedRoles: domain.AllRoles},
		"adminGetProduct":           {AllowedRoles: domain.AllRoles},
		"adminCreateProduct":        {AllowedRoles: adminOnly},
		"adminUpdateProduct":        {AllowedRoles: adminOnly},
		"adminDeleteProduct":        {AllowedRoles: adminOnly},
		"adminUploadProductImage":   {AllowedRoles: adminOnly},
		"adminCreateProductVariant": {AllowedRoles: adminOnly},
		"adminUpdateProductVariant": {AllowedRoles: adminOnly},
		"adminDeleteProductVariant": {AllowedRoles: adminOnly},
	}
}

// ContextWithAuthenticatedUser injeta um AuthenticatedUser no contexto (útil também em testes).
func ContextWithAuthenticatedUser(ctx context.Context, user AuthenticatedUser) context.Context {
	return context.WithValue(ctx, authUserContextKey, user)
}

// GetAuthenticatedUser retorna o usuário autenticado do contexto, se presente.
func GetAuthenticatedUser(ctx context.Context) (AuthenticatedUser, bool) {
	u, ok := ctx.Value(authUserContextKey).(AuthenticatedUser)
	return u, ok
}

// NewStrictAuthMiddleware aplica autenticação JWT e autorização RBAC por operationID (fail-closed).
func NewStrictAuthMiddleware(jwtManager *security.JWTManager, policies map[string]OperationPolicy) oapi.StrictMiddlewareFunc {
	return func(next oapi.StrictHandlerFunc, operationID string) oapi.StrictHandlerFunc {
		return func(ctx context.Context, w http.ResponseWriter, r *http.Request, request any) (any, error) {
			policy, exists := findPolicy(policies, operationID)
			if !exists {
				return nil, domain.NewForbiddenError("Operação sem política de autorização configurada")
			}

			if policy.Public {
				return next(ctx, w, r, request)
			}

			if jwtManager == nil {
				return nil, domain.NewInternalError("Gerenciador JWT não configurado", nil)
			}

			token, err := extractBearerToken(r.Header.Get("Authorization"))
			if err != nil {
				return nil, err
			}

			claims, err := jwtManager.ParseAccessToken(token)
			if err != nil {
				return nil, err
			}

			if len(policy.AllowedRoles) > 0 && !slices.Contains(policy.AllowedRoles, claims.Role) {
				return nil, domain.NewForbiddenError("Permissão insuficiente para executar esta operação")
			}

			authCtx := ContextWithAuthenticatedUser(ctx, AuthenticatedUser{
				ID:    claims.UserID,
				Email: claims.Email,
				Role:  claims.Role,
			})
			return next(authCtx, w, r, request)
		}
	}
}

func findPolicy(policies map[string]OperationPolicy, operationID string) (OperationPolicy, bool) {
	if policy, ok := policies[operationID]; ok {
		return policy, true
	}
	// oapi-codegen passa operationIDs em PascalCase nos wrappers (ex.: "AdminListUsers").
	for key, policy := range policies {
		if strings.EqualFold(key, operationID) {
			return policy, true
		}
	}
	return OperationPolicy{}, false
}

func extractBearerToken(header string) (string, error) {
	if strings.TrimSpace(header) == "" {
		return "", domain.NewUnauthorizedError("Cabeçalho Authorization não informado")
	}

	parts := strings.SplitN(header, " ", 2)
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") || strings.TrimSpace(parts[1]) == "" {
		return "", domain.NewUnauthorizedError("Formato de token Authorization inválido; use Bearer <token>")
	}

	return strings.TrimSpace(parts[1]), nil
}
