package handler

import (
	"context"

	"github.com/google/uuid"

	"github.com/joao-accioly/artisan-store/api/internal/domain"
	"github.com/joao-accioly/artisan-store/api/internal/service"
)

const defaultMaxUploadBytes int64 = 5 * 1024 * 1024

// DBPinger abstrai a verificação de conectividade com o banco de dados.
type DBPinger interface {
	Ping(ctx context.Context) error
}

// AuthUseCase define as operações de autenticação consumidas pelos handlers HTTP.
type AuthUseCase interface {
	Login(ctx context.Context, email, password string) (*service.AuthTokens, *domain.User, error)
	Refresh(ctx context.Context, rawRefreshToken string) (*service.AuthTokens, error)
	Logout(ctx context.Context, userID uuid.UUID, rawRefreshToken string) error
	Me(ctx context.Context, userID uuid.UUID) (*domain.User, error)
}

// UserUseCase define as operações administrativas de usuários consumidas pelos handlers HTTP.
type UserUseCase interface {
	List(ctx context.Context, role *domain.Role, page, pageSize int) ([]domain.User, domain.PaginationMeta, error)
	Create(ctx context.Context, input service.CreateUserInput) (*domain.User, error)
	GetByID(ctx context.Context, id uuid.UUID) (*domain.User, error)
	Update(ctx context.Context, actorID, targetID uuid.UUID, input service.UpdateUserInput) (*domain.User, error)
}

// CatalogUseCase define as operações públicas e administrativas de catálogo e loja.
type CatalogUseCase interface {
	GetSettings(ctx context.Context) (*domain.StoreSettings, error)
	GetPublicStore(ctx context.Context) (*domain.StoreSettings, bool, error)
	UpdateSettings(ctx context.Context, input service.UpdateStoreSettingsInput) (*domain.StoreSettings, error)
	ListCategories(ctx context.Context, active *bool, page, pageSize int) ([]domain.Category, domain.PaginationMeta, error)
	GetCategory(ctx context.Context, id uuid.UUID) (*domain.Category, error)
	CreateCategory(ctx context.Context, input service.CreateCategoryInput) (*domain.Category, error)
	UpdateCategory(ctx context.Context, id uuid.UUID, input service.UpdateCategoryInput) (*domain.Category, error)
	DeleteCategory(ctx context.Context, id uuid.UUID) error
	ListProducts(ctx context.Context, filter service.ProductListFilter, page, pageSize int) ([]domain.Product, domain.PaginationMeta, error)
	GetProduct(ctx context.Context, id uuid.UUID) (*domain.Product, error)
	CreateProduct(ctx context.Context, input service.CreateProductInput) (*domain.Product, error)
	UpdateProduct(ctx context.Context, id uuid.UUID, input service.UpdateProductInput) (*domain.Product, error)
	DeleteProduct(ctx context.Context, id uuid.UUID) error
	UploadProductImage(ctx context.Context, productID uuid.UUID, data []byte) (*domain.Product, error)
	CreateVariant(ctx context.Context, productID uuid.UUID, input service.CreateVariantInput) (*domain.ProductVariant, error)
	UpdateVariant(ctx context.Context, productID, variantID uuid.UUID, input service.UpdateVariantInput) (*domain.ProductVariant, error)
	DeleteVariant(ctx context.Context, productID, variantID uuid.UUID) error
	GetPublicMenu(ctx context.Context, filter service.PublicMenuFilter) ([]domain.PublicMenuCategory, error)
	GetPublicProduct(ctx context.Context, slugOrID string) (*domain.PublicProduct, error)
}

// ServerDeps agrupa as dependências dos handlers da API.
type ServerDeps struct {
	DB             DBPinger
	Auth           AuthUseCase
	Users          UserUseCase
	Catalog        CatalogUseCase
	MaxUploadBytes int64
}

// Server implementa o contrato oapi.StrictServerInterface da API.
type Server struct {
	db             DBPinger
	auth           AuthUseCase
	users          UserUseCase
	catalog        CatalogUseCase
	maxUploadBytes int64
}

// NewServer constrói o servidor HTTP com as dependências necessárias.
func NewServer(deps ServerDeps) *Server {
	maxBytes := deps.MaxUploadBytes
	if maxBytes <= 0 {
		maxBytes = defaultMaxUploadBytes
	}

	return &Server{
		db:             deps.DB,
		auth:           deps.Auth,
		users:          deps.Users,
		catalog:        deps.Catalog,
		maxUploadBytes: maxBytes,
	}
}
