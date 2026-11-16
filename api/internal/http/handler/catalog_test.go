package handler_test

import (
	"bytes"
	"context"
	"mime/multipart"
	"testing"

	"github.com/google/uuid"
	openapi_types "github.com/oapi-codegen/runtime/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/joao-accioly/artisan-store/api/internal/domain"
	"github.com/joao-accioly/artisan-store/api/internal/http/handler"
	"github.com/joao-accioly/artisan-store/api/internal/http/oapi"
	"github.com/joao-accioly/artisan-store/api/internal/service"
)

type stubCatalogUseCase struct {
	uploadImageFn func(ctx context.Context, productID uuid.UUID, data []byte) (*domain.Product, error)
}

func (s *stubCatalogUseCase) GetSettings(context.Context) (*domain.StoreSettings, error) {
	return nil, nil
}
func (s *stubCatalogUseCase) GetPublicStore(context.Context) (*domain.StoreSettings, bool, error) {
	return nil, false, nil
}
func (s *stubCatalogUseCase) UpdateSettings(context.Context, service.UpdateStoreSettingsInput) (*domain.StoreSettings, error) {
	return nil, nil
}
func (s *stubCatalogUseCase) ListCategories(context.Context, *bool, int, int) ([]domain.Category, domain.PaginationMeta, error) {
	return nil, domain.PaginationMeta{}, nil
}
func (s *stubCatalogUseCase) GetCategory(context.Context, uuid.UUID) (*domain.Category, error) {
	return nil, nil
}
func (s *stubCatalogUseCase) CreateCategory(context.Context, service.CreateCategoryInput) (*domain.Category, error) {
	return nil, nil
}
func (s *stubCatalogUseCase) UpdateCategory(context.Context, uuid.UUID, service.UpdateCategoryInput) (*domain.Category, error) {
	return nil, nil
}
func (s *stubCatalogUseCase) DeleteCategory(context.Context, uuid.UUID) error {
	return nil
}
func (s *stubCatalogUseCase) ListProducts(context.Context, service.ProductListFilter, int, int) ([]domain.Product, domain.PaginationMeta, error) {
	return nil, domain.PaginationMeta{}, nil
}
func (s *stubCatalogUseCase) GetProduct(context.Context, uuid.UUID) (*domain.Product, error) {
	return nil, nil
}
func (s *stubCatalogUseCase) CreateProduct(context.Context, service.CreateProductInput) (*domain.Product, error) {
	return nil, nil
}
func (s *stubCatalogUseCase) UpdateProduct(context.Context, uuid.UUID, service.UpdateProductInput) (*domain.Product, error) {
	return nil, nil
}
func (s *stubCatalogUseCase) DeleteProduct(context.Context, uuid.UUID) error {
	return nil
}
func (s *stubCatalogUseCase) UploadProductImage(ctx context.Context, productID uuid.UUID, data []byte) (*domain.Product, error) {
	if s.uploadImageFn != nil {
		return s.uploadImageFn(ctx, productID, data)
	}
	return nil, nil
}
func (s *stubCatalogUseCase) CreateVariant(context.Context, uuid.UUID, service.CreateVariantInput) (*domain.ProductVariant, error) {
	return nil, nil
}
func (s *stubCatalogUseCase) UpdateVariant(context.Context, uuid.UUID, uuid.UUID, service.UpdateVariantInput) (*domain.ProductVariant, error) {
	return nil, nil
}
func (s *stubCatalogUseCase) DeleteVariant(context.Context, uuid.UUID, uuid.UUID) error {
	return nil
}
func (s *stubCatalogUseCase) GetPublicMenu(context.Context, service.PublicMenuFilter) ([]domain.PublicMenuCategory, error) {
	return nil, nil
}
func (s *stubCatalogUseCase) GetPublicProduct(context.Context, string) (*domain.PublicProduct, error) {
	return nil, nil
}

func assertDomainCode(t *testing.T, err error, code domain.ErrorCode) {
	t.Helper()
	require.Error(t, err)
	domErr, ok := domain.AsDomainError(err)
	require.True(t, ok, "expected domain error, got %T: %v", err, err)
	assert.Equal(t, code, domErr.Code)
}

func TestServer_CatalogHandlers_GuardClauses(t *testing.T) {
	ctx := context.Background()

	t.Run("nil catalog service returns 500 internal error across all endpoints", func(t *testing.T) {
		srv := handler.NewServer(handler.ServerDeps{})

		_, err := srv.PublicGetStore(ctx, oapi.PublicGetStoreRequestObject{})
		assertDomainCode(t, err, domain.CodeInternal)
		_, err = srv.PublicGetMenu(ctx, oapi.PublicGetMenuRequestObject{})
		assertDomainCode(t, err, domain.CodeInternal)
		_, err = srv.PublicGetProduct(ctx, oapi.PublicGetProductRequestObject{})
		assertDomainCode(t, err, domain.CodeInternal)
		_, err = srv.AdminGetSettings(ctx, oapi.AdminGetSettingsRequestObject{})
		assertDomainCode(t, err, domain.CodeInternal)
		_, err = srv.AdminUpdateSettings(ctx, oapi.AdminUpdateSettingsRequestObject{})
		assertDomainCode(t, err, domain.CodeInternal)
		_, err = srv.AdminListCategories(ctx, oapi.AdminListCategoriesRequestObject{})
		assertDomainCode(t, err, domain.CodeInternal)
		_, err = srv.AdminCreateCategory(ctx, oapi.AdminCreateCategoryRequestObject{})
		assertDomainCode(t, err, domain.CodeInternal)
		_, err = srv.AdminGetCategory(ctx, oapi.AdminGetCategoryRequestObject{})
		assertDomainCode(t, err, domain.CodeInternal)
		_, err = srv.AdminUpdateCategory(ctx, oapi.AdminUpdateCategoryRequestObject{})
		assertDomainCode(t, err, domain.CodeInternal)
		_, err = srv.AdminDeleteCategory(ctx, oapi.AdminDeleteCategoryRequestObject{})
		assertDomainCode(t, err, domain.CodeInternal)
		_, err = srv.AdminListProducts(ctx, oapi.AdminListProductsRequestObject{})
		assertDomainCode(t, err, domain.CodeInternal)
		_, err = srv.AdminCreateProduct(ctx, oapi.AdminCreateProductRequestObject{})
		assertDomainCode(t, err, domain.CodeInternal)
		_, err = srv.AdminGetProduct(ctx, oapi.AdminGetProductRequestObject{})
		assertDomainCode(t, err, domain.CodeInternal)
		_, err = srv.AdminUpdateProduct(ctx, oapi.AdminUpdateProductRequestObject{})
		assertDomainCode(t, err, domain.CodeInternal)
		_, err = srv.AdminDeleteProduct(ctx, oapi.AdminDeleteProductRequestObject{})
		assertDomainCode(t, err, domain.CodeInternal)
		_, err = srv.AdminUploadProductImage(ctx, oapi.AdminUploadProductImageRequestObject{})
		assertDomainCode(t, err, domain.CodeInternal)
		_, err = srv.AdminCreateProductVariant(ctx, oapi.AdminCreateProductVariantRequestObject{})
		assertDomainCode(t, err, domain.CodeInternal)
		_, err = srv.AdminUpdateProductVariant(ctx, oapi.AdminUpdateProductVariantRequestObject{})
		assertDomainCode(t, err, domain.CodeInternal)
		_, err = srv.AdminDeleteProductVariant(ctx, oapi.AdminDeleteProductVariantRequestObject{})
		assertDomainCode(t, err, domain.CodeInternal)
	})

	t.Run("nil request bodies and invalid service_type return 400 validation error", func(t *testing.T) {
		srv := handler.NewServer(handler.ServerDeps{Catalog: &stubCatalogUseCase{}})

		_, err := srv.AdminUpdateSettings(ctx, oapi.AdminUpdateSettingsRequestObject{Body: nil})
		assertDomainCode(t, err, domain.CodeValidation)
		_, err = srv.AdminCreateCategory(ctx, oapi.AdminCreateCategoryRequestObject{Body: nil})
		assertDomainCode(t, err, domain.CodeValidation)
		_, err = srv.AdminUpdateCategory(ctx, oapi.AdminUpdateCategoryRequestObject{Body: nil})
		assertDomainCode(t, err, domain.CodeValidation)
		_, err = srv.AdminCreateProduct(ctx, oapi.AdminCreateProductRequestObject{Body: nil})
		assertDomainCode(t, err, domain.CodeValidation)
		_, err = srv.AdminUpdateProduct(ctx, oapi.AdminUpdateProductRequestObject{Body: nil})
		assertDomainCode(t, err, domain.CodeValidation)
		_, err = srv.AdminUploadProductImage(ctx, oapi.AdminUploadProductImageRequestObject{Body: nil})
		assertDomainCode(t, err, domain.CodeValidation)
		_, err = srv.AdminCreateProductVariant(ctx, oapi.AdminCreateProductVariantRequestObject{Body: nil})
		assertDomainCode(t, err, domain.CodeValidation)
		_, err = srv.AdminUpdateProductVariant(ctx, oapi.AdminUpdateProductVariantRequestObject{Body: nil})
		assertDomainCode(t, err, domain.CodeValidation)

		badST := oapi.ServiceType("delivery")
		_, err = srv.PublicGetMenu(ctx, oapi.PublicGetMenuRequestObject{
			Params: oapi.PublicGetMenuParams{ServiceType: &badST},
		})
		assertDomainCode(t, err, domain.CodeValidation)

		_, err = srv.AdminListProducts(ctx, oapi.AdminListProductsRequestObject{
			Params: oapi.AdminListProductsParams{ServiceType: &badST},
		})
		assertDomainCode(t, err, domain.CodeValidation)

		_, err = srv.AdminCreateProduct(ctx, oapi.AdminCreateProductRequestObject{
			Body: &oapi.AdminCreateProductJSONRequestBody{
				Name:        "Brigadeiro",
				ServiceType: badST,
			},
		})
		assertDomainCode(t, err, domain.CodeValidation)

		_, err = srv.AdminUpdateProduct(ctx, oapi.AdminUpdateProductRequestObject{
			Body: &oapi.AdminUpdateProductJSONRequestBody{
				ServiceType: &badST,
			},
		})
		assertDomainCode(t, err, domain.CodeValidation)
	})

	t.Run("multipart skips non-file fields before reading file", func(t *testing.T) {
		pid, err := uuid.NewV7()
		require.NoError(t, err)

		var capturedBytes []byte
		srv := handler.NewServer(handler.ServerDeps{
			Catalog: &stubCatalogUseCase{
				uploadImageFn: func(_ context.Context, productID uuid.UUID, data []byte) (*domain.Product, error) {
					assert.Equal(t, pid, productID)
					capturedBytes = data
					return &domain.Product{ID: pid, Variants: []domain.ProductVariant{}}, nil
				},
			},
			MaxUploadBytes: 1024,
		})

		var buf bytes.Buffer
		mw := multipart.NewWriter(&buf)
		require.NoError(t, mw.WriteField("caption", "foto principal"))
		fw, err := mw.CreateFormFile("file", "foto.png")
		require.NoError(t, err)
		payload := []byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A}
		_, err = fw.Write(payload)
		require.NoError(t, err)
		require.NoError(t, mw.Close())

		mr := multipart.NewReader(&buf, mw.Boundary())
		resp, err := srv.AdminUploadProductImage(ctx, oapi.AdminUploadProductImageRequestObject{
			Id:   openapi_types.UUID(pid),
			Body: mr,
		})
		require.NoError(t, err)
		assert.NotNil(t, resp)
		assert.Equal(t, payload, capturedBytes)
	})
}
