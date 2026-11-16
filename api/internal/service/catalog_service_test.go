package service_test

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/joao-accioly/artisan-store/api/internal/domain"
	"github.com/joao-accioly/artisan-store/api/internal/service"
)

type stubStorage struct {
	saveFn   func(ctx context.Context, key, contentType string, data []byte) (string, error)
	deleteFn func(ctx context.Context, key string) error
}

func (s *stubStorage) Save(ctx context.Context, key, contentType string, data []byte) (string, error) {
	if s.saveFn != nil {
		return s.saveFn(ctx, key, contentType, data)
	}
	return "https://cdn.example.com/" + key, nil
}

func (s *stubStorage) Delete(ctx context.Context, key string) error {
	if s.deleteFn != nil {
		return s.deleteFn(ctx, key)
	}
	return nil
}

func TestNewCatalogService_ValidationAndDefaults(t *testing.T) {
	svc, err := service.NewCatalogService(nil, nil, 0, nil)
	assert.Nil(t, svc)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "database pool is required")
}

func TestCatalogService_UploadProductImage_StorageErrors(t *testing.T) {
	if testing.Short() {
		t.Skip("pulando teste de integração com Docker no modo -short")
	}

	ctx := context.Background()
	pool := setupServicePostgres(t, "doceria_catalog_service_test")

	validPNG := []byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A, 0x00, 0x00, 0x00, 0x0D, 'I', 'H', 'D', 'R'}

	t.Run("nil storage returns internal error", func(t *testing.T) {
		svc, err := service.NewCatalogService(pool, nil, 0, nil)
		require.NoError(t, err)

		id, err := uuid.NewV7()
		require.NoError(t, err)

		_, err = svc.UploadProductImage(ctx, id, validPNG)
		domErr, ok := domain.AsDomainError(err)
		require.True(t, ok)
		assert.Equal(t, domain.CodeInternal, domErr.Code)
	})

	t.Run("storage save failure returns internal error", func(t *testing.T) {
		st := &stubStorage{
			saveFn: func(context.Context, string, string, []byte) (string, error) {
				return "", errors.New("disk full")
			},
		}
		svc, err := service.NewCatalogService(pool, st, 1024, nil)
		require.NoError(t, err)

		cat, err := svc.CreateCategory(ctx, service.CreateCategoryInput{
			Name:   "Doces",
			Active: true,
		})
		require.NoError(t, err)

		prod, err := svc.CreateProduct(ctx, service.CreateProductInput{
			CategoryID:      cat.ID,
			Name:            "Trufa",
			ServiceType:     domain.ServiceTypeReadyStock,
			Active:          true,
			AvailableOnline: true,
			Variants: []service.CreateVariantInput{
				{Name: "Unidade", PriceCents: 600, CurrentStock: 10, Active: true},
			},
		})
		require.NoError(t, err)

		_, err = svc.UploadProductImage(ctx, prod.ID, validPNG)
		domErr, ok := domain.AsDomainError(err)
		require.True(t, ok)
		assert.Equal(t, domain.CodeInternal, domErr.Code)
	})

	t.Run("pagination page_size > 100 returns validation error", func(t *testing.T) {
		svc, err := service.NewCatalogService(pool, &stubStorage{}, 1024, nil)
		require.NoError(t, err)

		_, _, err = svc.ListCategories(ctx, nil, 1, 101)
		domErr, ok := domain.AsDomainError(err)
		require.True(t, ok)
		assert.Equal(t, domain.CodeValidation, domErr.Code)
		assert.Contains(t, domErr.Details, "page_size")

		_, _, err = svc.ListProducts(ctx, service.ProductListFilter{}, 1, 101)
		domErr, ok = domain.AsDomainError(err)
		require.True(t, ok)
		assert.Equal(t, domain.CodeValidation, domErr.Code)
		assert.Contains(t, domErr.Details, "page_size")
	})
}
