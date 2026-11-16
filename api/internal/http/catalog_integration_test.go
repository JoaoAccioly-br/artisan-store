package http_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/joao-accioly/artisan-store/api/internal/config"
	"github.com/joao-accioly/artisan-store/api/internal/domain"
	apphttp "github.com/joao-accioly/artisan-store/api/internal/http"
	"github.com/joao-accioly/artisan-store/api/internal/http/oapi"
	"github.com/joao-accioly/artisan-store/api/internal/platform/db"
	"github.com/joao-accioly/artisan-store/api/internal/platform/logger"
	"github.com/joao-accioly/artisan-store/api/internal/platform/storage"
	"github.com/joao-accioly/artisan-store/api/internal/security"
	"github.com/joao-accioly/artisan-store/api/internal/service"
)

func doMultipartUploadRequest(
	t *testing.T,
	router http.Handler,
	path string,
	token string,
	fieldName string,
	fileName string,
	payload []byte,
) *httptest.ResponseRecorder {
	t.Helper()

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	if fieldName != "" {
		part, err := writer.CreateFormFile(fieldName, fileName)
		require.NoError(t, err)
		if len(payload) > 0 {
			_, err = part.Write(payload)
			require.NoError(t, err)
		}
	}
	require.NoError(t, writer.Close())

	req := httptest.NewRequest(http.MethodPost, path, &body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	return rec
}

func testJPEGBytes() []byte {
	return []byte{0xFF, 0xD8, 0xFF, 0xE0, 0x00, 0x10, 'J', 'F', 'I', 'F', 0x00, 0x01, 0x01, 0x00, 0x00, 0x01, 0x00, 0x01, 0x00, 0x00}
}

func testPNGBytes() []byte {
	return []byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A, 0x00, 0x00, 0x00, 0x0D, 'I', 'H', 'D', 'R'}
}

func testWebPBytes() []byte {
	return []byte{'R', 'I', 'F', 'F', 0x10, 0x00, 0x00, 0x00, 'W', 'E', 'B', 'P', 'V', 'P', '8', ' '}
}

func TestCatalogEndToEnd_Integration(t *testing.T) {
	if testing.Short() {
		t.Skip("pulando teste de integração com Docker no modo -short")
	}

	ctx := context.Background()
	connStr := setupPostgresContainer(t)

	pool, err := db.NewPool(ctx, connStr)
	require.NoError(t, err)
	defer pool.Close()

	require.NoError(t, db.MigrateUp(ctx, pool))

	hasher := security.NewArgon2idHasher(security.Argon2idParams{
		Memory:      8 * 1024,
		Iterations:  1,
		Parallelism: 1,
		SaltLength:  16,
		KeyLength:   32,
	})

	var logBuf bytes.Buffer
	log := logger.NewWithWriter(&logBuf, "development", "debug")

	uploadDir := t.TempDir()
	const maxUploadBytes int64 = 1024
	localStorage, err := storage.NewLocalStorage(uploadDir, "http://localhost:8080/uploads")
	require.NoError(t, err)

	cfg := &config.Config{
		AppEnv:                "development",
		AllowedOrigins:        []string{"http://localhost:3000"},
		RateLimitRPM:          1000,
		JWTAccessSecret:       "catalog-e2e-integration-jwt-secret-32chars",
		JWTAccessTTL:          15 * time.Minute,
		JWTRefreshTTL:         24 * time.Hour,
		SeedAdminName:         "Maria Confeiteira",
		SeedAdminEmail:        "admin@doceria.com",
		SeedAdminPassword:     "AdminPassword#123",
		StorageDriver:         config.StorageDriverLocal,
		StorageLocalDir:       uploadDir,
		StoragePublicURL:      "http://localhost:8080/uploads",
		StorageMaxUploadBytes: maxUploadBytes,
	}

	require.NoError(t, db.SeedAdmin(ctx, pool, cfg, hasher, log))

	jwtMgr, err := security.NewJWTManager(cfg.JWTAccessSecret, cfg.JWTAccessTTL)
	require.NoError(t, err)

	authSvc, err := service.NewAuthService(pool, hasher, jwtMgr, cfg.JWTRefreshTTL, log)
	require.NoError(t, err)
	userSvc := service.NewUserService(pool, hasher)
	catalogSvc, err := service.NewCatalogService(pool, localStorage, cfg.StorageMaxUploadBytes, log)
	require.NoError(t, err)

	router := apphttp.NewRouter(cfg, log, apphttp.RouterDeps{
		DB:             pool,
		JWTManager:     jwtMgr,
		Auth:           authSvc,
		Users:          userSvc,
		Catalog:        catalogSvc,
		MaxUploadBytes: cfg.StorageMaxUploadBytes,
		UploadHandler:  localStorage.FileServerHandler("/uploads"),
	})

	loginUser := func(email, password string) string {
		rec := doJSONRequest(t, router, http.MethodPost, "/api/v1/admin/auth/login", "", map[string]string{
			"email":    email,
			"password": password,
		})
		require.Equal(t, http.StatusOK, rec.Code)
		var resp oapi.LoginResponse
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
		return resp.AccessToken
	}

	adminToken := loginUser("admin@doceria.com", "AdminPassword#123")

	// Cria usuários atendente e cozinha para testar RBAC ponta-a-ponta com JWT real.
	rec := doJSONRequest(t, router, http.MethodPost, "/api/v1/admin/users", adminToken, map[string]string{
		"name":     "Carlos Atendente",
		"email":    "atendente@doceria.com",
		"password": "UserPassword#123",
		"role":     "atendente",
	})
	require.Equal(t, http.StatusCreated, rec.Code)

	rec = doJSONRequest(t, router, http.MethodPost, "/api/v1/admin/users", adminToken, map[string]string{
		"name":     "Ana Cozinha",
		"email":    "cozinha@doceria.com",
		"password": "UserPassword#123",
		"role":     "cozinha",
	})
	require.Equal(t, http.StatusCreated, rec.Code)

	atendenteToken := loginUser("atendente@doceria.com", "UserPassword#123")
	cozinhaToken := loginUser("cozinha@doceria.com", "UserPassword#123")

	// 1. Settings: GET admin, PATCH validações e PATCH sucesso, GET public/store.
	t.Run("store settings and public store status", func(t *testing.T) {
		rec := doJSONRequest(t, router, http.MethodGet, "/api/v1/admin/settings", adminToken, nil)
		require.Equal(t, http.StatusOK, rec.Code)
		var initial oapi.StoreSettingsResponse
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &initial))
		assert.Equal(t, "01930000-0000-7000-8000-000000000001", uuid.UUID(initial.Id).String())
		assert.Equal(t, "Artisan Store Doceria", initial.StoreName)
		assert.Equal(t, oapi.StoreManualStatus("auto"), initial.ManualStatus)

		// Corpo vazio -> 400
		rec = doJSONRequest(t, router, http.MethodPatch, "/api/v1/admin/settings", adminToken, map[string]any{})
		assert.Equal(t, http.StatusBadRequest, rec.Code)

		// Validações inválidas de opening_hours, manual_status, accepted_payment_methods, email, ttl
		invalidHours := initial.OpeningHours
		invalidHours.Monday = oapi.DaySchedule{Open: "18:00", Close: "09:00", Closed: false}
		rec = doJSONRequest(t, router, http.MethodPatch, "/api/v1/admin/settings", adminToken, map[string]any{
			"store_name":                  "A",
			"email":                       "email-invalido",
			"opening_hours":               invalidHours,
			"accepted_payment_methods":    []string{},
			"manual_status":               "forced_open",
			"pix_reservation_ttl_minutes": 0,
		})
		assert.Equal(t, http.StatusBadRequest, rec.Code)
		var errResp oapi.ErrorResponse
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &errResp))
		assert.Equal(t, string(domain.CodeValidation), errResp.Error.Code)

		// Opening hours com formato inválido (sem zero à esquerda) -> 400
		badFmtHours := initial.OpeningHours
		badFmtHours.Tuesday = oapi.DaySchedule{Open: "9:00", Close: "18:00", Closed: false}
		rec = doJSONRequest(t, router, http.MethodPatch, "/api/v1/admin/settings", adminToken, map[string]any{
			"opening_hours": badFmtHours,
		})
		assert.Equal(t, http.StatusBadRequest, rec.Code)

		// Método de pagamento inválido -> 400
		rec = doJSONRequest(t, router, http.MethodPatch, "/api/v1/admin/settings", adminToken, map[string]any{
			"accepted_payment_methods": []string{"pix_online", "cripto"},
		})
		assert.Equal(t, http.StatusBadRequest, rec.Code)

		// Atualização válida com manual_status=open e deduplicação de pagamentos
		updatedHours := initial.OpeningHours
		updatedHours.Sunday = oapi.DaySchedule{Open: "10:00", Close: "14:00", Closed: false}
		rec = doJSONRequest(t, router, http.MethodPatch, "/api/v1/admin/settings", adminToken, map[string]any{
			"store_name":                  "Doceria Sabor Real",
			"phone":                       "(11) 3333-4444",
			"whatsapp":                    "5511999998888",
			"email":                       "contato@saborreal.com",
			"address_line":                "Rua dos Doces, 42",
			"opening_hours":               updatedHours,
			"accepted_payment_methods":    []string{"pix_online", "pix_online", "cartao_entrega"},
			"manual_status":               "open",
			"allow_scheduling":            false,
			"pix_reservation_ttl_minutes": 20,
			"auto_hide_out_of_stock":      true,
		})
		require.Equal(t, http.StatusOK, rec.Code)
		var saved oapi.StoreSettingsResponse
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &saved))
		assert.Equal(t, "Doceria Sabor Real", saved.StoreName)
		assert.Equal(t, []oapi.PaymentMethod{"pix_online", "cartao_entrega"}, saved.AcceptedPaymentMethods)
		assert.Equal(t, oapi.StoreManualStatus("open"), saved.ManualStatus)
		assert.Equal(t, 20, saved.PixReservationTtlMinutes)
		assert.False(t, saved.AllowScheduling)

		// GET /api/v1/public/store sem token: is_open_now=true e não expõe campos internos (id, created_at, updated_at)
		rec = doJSONRequest(t, router, http.MethodGet, "/api/v1/public/store", "", nil)
		require.Equal(t, http.StatusOK, rec.Code)
		var rawPublic map[string]any
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &rawPublic))
		assert.NotContains(t, rawPublic, "id")
		assert.NotContains(t, rawPublic, "created_at")
		assert.NotContains(t, rawPublic, "updated_at")

		var pubStore oapi.PublicStoreResponse
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &pubStore))
		assert.Equal(t, "Doceria Sabor Real", pubStore.StoreName)
		assert.True(t, pubStore.IsOpenNow)

		// Troca manual_status para closed -> is_open_now=false
		rec = doJSONRequest(t, router, http.MethodPatch, "/api/v1/admin/settings", adminToken, map[string]any{
			"manual_status": "closed",
		})
		require.Equal(t, http.StatusOK, rec.Code)
		rec = doJSONRequest(t, router, http.MethodGet, "/api/v1/public/store", "", nil)
		require.Equal(t, http.StatusOK, rec.Code)
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &pubStore))
		assert.False(t, pubStore.IsOpenNow)

		// Restaura manual_status=auto
		rec = doJSONRequest(t, router, http.MethodPatch, "/api/v1/admin/settings", adminToken, map[string]any{
			"manual_status": "auto",
		})
		require.Equal(t, http.StatusOK, rec.Code)
	})

	// 2. Categorias: CRUD completo, auto-slug pt-BR, 409 slug duplicado, 409 delete com produtos.
	var catBrigadeirosID uuid.UUID
	var catBolosID uuid.UUID
	var catInativaID uuid.UUID

	t.Run("categories CRUD, pt-BR auto-slug and conflict checks", func(t *testing.T) {
		rec := doJSONRequest(t, router, http.MethodPost, "/api/v1/admin/categories", adminToken, map[string]any{
			"name":       "Pão de Mel & Maçã",
			"sort_order": 1,
			"active":     true,
		})
		require.Equal(t, http.StatusCreated, rec.Code)
		var cat1 oapi.CategoryResponse
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &cat1))
		assert.Equal(t, "pao-de-mel-maca", cat1.Slug)
		catBrigadeirosID = uuid.UUID(cat1.Id)

		// Slug duplicado (case-insensitive) -> 409
		rec = doJSONRequest(t, router, http.MethodPost, "/api/v1/admin/categories", adminToken, map[string]any{
			"name": "Outra Categoria",
			"slug": "PAO-DE-MEL-MACA",
		})
		assert.Equal(t, http.StatusConflict, rec.Code)

		// Nome que gera slug vazio -> 400
		rec = doJSONRequest(t, router, http.MethodPost, "/api/v1/admin/categories", adminToken, map[string]any{
			"name": "!!!@@@",
		})
		assert.Equal(t, http.StatusBadRequest, rec.Code)

		// Cria mais duas categorias (uma ativa e uma inativa)
		rec = doJSONRequest(t, router, http.MethodPost, "/api/v1/admin/categories", adminToken, map[string]any{
			"name":       "Bolos & Tortas",
			"sort_order": 2,
			"active":     true,
		})
		require.Equal(t, http.StatusCreated, rec.Code)
		var cat2 oapi.CategoryResponse
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &cat2))
		catBolosID = uuid.UUID(cat2.Id)

		rec = doJSONRequest(t, router, http.MethodPost, "/api/v1/admin/categories", adminToken, map[string]any{
			"name":       "Sazonais Inativos",
			"sort_order": 3,
			"active":     false,
		})
		require.Equal(t, http.StatusCreated, rec.Code)
		var cat3 oapi.CategoryResponse
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &cat3))
		catInativaID = uuid.UUID(cat3.Id)

		// Listagem paginada e filtro active=true
		rec = doJSONRequest(t, router, http.MethodGet, "/api/v1/admin/categories?active=true&page=1&page_size=1", adminToken, nil)
		require.Equal(t, http.StatusOK, rec.Code)
		var listResp oapi.PaginatedCategoriesResponse
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &listResp))
		assert.Len(t, listResp.Data, 1)
		assert.Equal(t, 2, listResp.Meta.TotalItems)
		assert.Equal(t, 2, listResp.Meta.TotalPages)

		// GetCategory 200 e 404
		rec = doJSONRequest(t, router, http.MethodGet, "/api/v1/admin/categories/"+catBrigadeirosID.String(), adminToken, nil)
		require.Equal(t, http.StatusOK, rec.Code)

		missingID, idErr := uuid.NewV7()
		require.NoError(t, idErr)
		rec = doJSONRequest(t, router, http.MethodGet, "/api/v1/admin/categories/"+missingID.String(), adminToken, nil)
		assert.Equal(t, http.StatusNotFound, rec.Code)

		// UpdateCategory (sucesso + conflito 409)
		rec = doJSONRequest(t, router, http.MethodPatch, "/api/v1/admin/categories/"+catBrigadeirosID.String(), adminToken, map[string]any{
			"name": "Brigadeiros & Doces",
			"slug": "brigadeiros-doces",
		})
		require.Equal(t, http.StatusOK, rec.Code)
		var updated oapi.CategoryResponse
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &updated))
		assert.Equal(t, "Brigadeiros & Doces", updated.Name)
		assert.Equal(t, "brigadeiros-doces", updated.Slug)

		rec = doJSONRequest(t, router, http.MethodPatch, "/api/v1/admin/categories/"+catBrigadeirosID.String(), adminToken, map[string]any{
			"slug": "bolos-tortas",
		})
		assert.Equal(t, http.StatusConflict, rec.Code)

		// Cria e remove categoria vazia -> 204; segundo delete -> 404
		rec = doJSONRequest(t, router, http.MethodPost, "/api/v1/admin/categories", adminToken, map[string]any{
			"name": "Temporária",
		})
		require.Equal(t, http.StatusCreated, rec.Code)
		var tempCat oapi.CategoryResponse
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &tempCat))

		rec = doJSONRequest(t, router, http.MethodDelete, "/api/v1/admin/categories/"+uuid.UUID(tempCat.Id).String(), adminToken, nil)
		assert.Equal(t, http.StatusNoContent, rec.Code)
		rec = doJSONRequest(t, router, http.MethodDelete, "/api/v1/admin/categories/"+uuid.UUID(tempCat.Id).String(), adminToken, nil)
		assert.Equal(t, http.StatusNotFound, rec.Code)
	})

	// 3. Produtos, variações, busca com % e _ literais, upload de imagem e limpeza de arquivo.
	var prodReadyID uuid.UUID
	var prodReadyOutOfStockID uuid.UUID
	var prodMadeToOrderID uuid.UUID
	var prodToDeleteID uuid.UUID

	t.Run("products CRUD, batch variants, search LIKE escaping, and category delete conflict", func(t *testing.T) {
		// Criar sem variações -> 400
		rec := doJSONRequest(t, router, http.MethodPost, "/api/v1/admin/products", adminToken, map[string]any{
			"category_id":  catBrigadeirosID.String(),
			"name":         "Sem Variante",
			"service_type": "pronta_entrega",
			"variants":     []any{},
		})
		assert.Equal(t, http.StatusBadRequest, rec.Code)

		// SKU duplicado na mesma requisição (case-insensitive) -> 400
		rec = doJSONRequest(t, router, http.MethodPost, "/api/v1/admin/products", adminToken, map[string]any{
			"category_id":  catBrigadeirosID.String(),
			"name":         "SKU Repetido",
			"service_type": "pronta_entrega",
			"variants": []map[string]any{
				{"name": "Unidade", "sku": "BRIG-01", "price_cents": 500, "current_stock": 10},
				{"name": "Caixa", "sku": "brig-01", "price_cents": 2500, "current_stock": 5},
			},
		})
		assert.Equal(t, http.StatusBadRequest, rec.Code)

		// Categoria inexistente -> 404
		missingCatID, idErr := uuid.NewV7()
		require.NoError(t, idErr)
		rec = doJSONRequest(t, router, http.MethodPost, "/api/v1/admin/products", adminToken, map[string]any{
			"category_id":  missingCatID.String(),
			"name":         "Produto Órfão",
			"service_type": "pronta_entrega",
			"variants":     []map[string]any{{"name": "Padrão", "price_cents": 1000}},
		})
		assert.Equal(t, http.StatusNotFound, rec.Code)

		// Produto 1: pronta_entrega em estoque, com 70% e _ no nome/descrição para testar escape do LIKE
		rec = doJSONRequest(t, router, http.MethodPost, "/api/v1/admin/products", adminToken, map[string]any{
			"category_id":       catBrigadeirosID.String(),
			"name":              "Brigadeiro 70% Cacau_Belga",
			"description":       "Chocolate 70% puro com granulado belga.",
			"service_type":      "pronta_entrega",
			"prep_time_minutes": 10,
			"active":            true,
			"available_online":  true,
			"featured":          true,
			"sort_order":        1,
			"variants": []map[string]any{
				{"name": "Caixa com 6", "sku": "BRIG-70-CX6", "price_cents": 2990, "current_stock": 15, "min_stock": 3, "sort_order": 1, "active": true},
				{"name": "Caixa com 12", "sku": "BRIG-70-CX12", "price_cents": 5490, "current_stock": 0, "min_stock": 2, "sort_order": 2, "active": true},
			},
		})
		require.Equal(t, http.StatusCreated, rec.Code)
		var p1 oapi.ProductResponse
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &p1))
		assert.Equal(t, "brigadeiro-70-cacau-belga", p1.Slug)
		assert.Len(t, p1.Variants, 2)
		prodReadyID = uuid.UUID(p1.Id)

		// Conflito de slug no banco -> 409
		rec = doJSONRequest(t, router, http.MethodPost, "/api/v1/admin/products", adminToken, map[string]any{
			"category_id":  catBrigadeirosID.String(),
			"name":         "Outro Brigadeiro",
			"slug":         "BRIGADEIRO-70-CACAU-BELGA",
			"service_type": "pronta_entrega",
			"variants":     []map[string]any{{"name": "Unidade", "price_cents": 500}},
		})
		assert.Equal(t, http.StatusConflict, rec.Code)

		// Conflito de SKU no banco -> 409
		rec = doJSONRequest(t, router, http.MethodPost, "/api/v1/admin/products", adminToken, map[string]any{
			"category_id":  catBrigadeirosID.String(),
			"name":         "Outro Produto SKU",
			"service_type": "pronta_entrega",
			"variants":     []map[string]any{{"name": "Unidade", "sku": "brig-70-cx6", "price_cents": 500}},
		})
		assert.Equal(t, http.StatusConflict, rec.Code)

		// Produto 2: pronta_entrega esgotado (current_stock = 0)
		rec = doJSONRequest(t, router, http.MethodPost, "/api/v1/admin/products", adminToken, map[string]any{
			"category_id":      catBrigadeirosID.String(),
			"name":             "Beijinho Clássico",
			"description":      "Coco fresco ralado.",
			"service_type":     "pronta_entrega",
			"active":           true,
			"available_online": true,
			"featured":         false,
			"sort_order":       2,
			"variants": []map[string]any{
				{"name": "Caixa com 6", "sku": "BEIJ-CX6", "price_cents": 2490, "current_stock": 0, "active": true},
			},
		})
		require.Equal(t, http.StatusCreated, rec.Code)
		var p2 oapi.ProductResponse
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &p2))
		prodReadyOutOfStockID = uuid.UUID(p2.Id)

		// Produto 3: sob_encomenda com current_stock = 0 (deve continuar visível no menu público!)
		rec = doJSONRequest(t, router, http.MethodPost, "/api/v1/admin/products", adminToken, map[string]any{
			"category_id":       catBolosID.String(),
			"name":              "Bolo Red Velvet",
			"description":       "Massa aveludada com cream cheese.",
			"service_type":      "sob_encomenda",
			"prep_time_minutes": 180,
			"active":            true,
			"available_online":  true,
			"featured":          true,
			"sort_order":        1,
			"variants": []map[string]any{
				{"name": "2kg (20 fatias)", "sku": "BOLO-RV-2KG", "price_cents": 18900, "current_stock": 0, "active": true},
			},
		})
		require.Equal(t, http.StatusCreated, rec.Code)
		var p3 oapi.ProductResponse
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &p3))
		prodMadeToOrderID = uuid.UUID(p3.Id)

		// Produto 4: em categoria inativa (não pode aparecer no menu ou detalhe público)
		rec = doJSONRequest(t, router, http.MethodPost, "/api/v1/admin/products", adminToken, map[string]any{
			"category_id":      catInativaID.String(),
			"name":             "Panetone Trufado",
			"service_type":     "pronta_entrega",
			"active":           true,
			"available_online": true,
			"variants": []map[string]any{
				{"name": "500g", "price_cents": 6990, "current_stock": 10, "active": true},
			},
		})
		require.Equal(t, http.StatusCreated, rec.Code)

		// Produto 5: para testar delete + remoção da imagem do disco
		rec = doJSONRequest(t, router, http.MethodPost, "/api/v1/admin/products", adminToken, map[string]any{
			"category_id":      catBolosID.String(),
			"name":             "Torta Temporária",
			"service_type":     "pronta_entrega",
			"active":           false,
			"available_online": false,
			"variants": []map[string]any{
				{"name": "Fatia", "price_cents": 1500, "current_stock": 5, "active": true},
			},
		})
		require.Equal(t, http.StatusCreated, rec.Code)
		var p5 oapi.ProductResponse
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &p5))
		prodToDeleteID = uuid.UUID(p5.Id)

		// Delete categoria com produtos vinculados -> 409
		rec = doJSONRequest(t, router, http.MethodDelete, "/api/v1/admin/categories/"+catBrigadeirosID.String(), adminToken, nil)
		assert.Equal(t, http.StatusConflict, rec.Code)

		// Listagem admin com paginação e batch loading de variações
		rec = doJSONRequest(t, router, http.MethodGet, "/api/v1/admin/products?page=1&page_size=2", adminToken, nil)
		require.Equal(t, http.StatusOK, rec.Code)
		var prodList oapi.PaginatedProductsResponse
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &prodList))
		assert.Len(t, prodList.Data, 2)
		assert.Equal(t, 5, prodList.Meta.TotalItems)
		assert.Equal(t, 3, prodList.Meta.TotalPages)
		for _, item := range prodList.Data {
			assert.NotEmpty(t, item.Variants)
		}

		// Busca com '%' e '_' literais (não devem atuar como wildcard para todos os produtos!)
		rec = doJSONRequest(t, router, http.MethodGet, "/api/v1/admin/products?search=70%25", adminToken, nil)
		require.Equal(t, http.StatusOK, rec.Code)
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &prodList))
		require.Len(t, prodList.Data, 1)
		assert.Equal(t, prodReadyID, uuid.UUID(prodList.Data[0].Id))

		rec = doJSONRequest(t, router, http.MethodGet, "/api/v1/admin/products?search=Cacau_Belga", adminToken, nil)
		require.Equal(t, http.StatusOK, rec.Code)
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &prodList))
		require.Len(t, prodList.Data, 1)
		assert.Equal(t, prodReadyID, uuid.UUID(prodList.Data[0].Id))

		// UpdateProduct
		rec = doJSONRequest(t, router, http.MethodPatch, "/api/v1/admin/products/"+prodReadyID.String(), adminToken, map[string]any{
			"prep_time_minutes": 15,
		})
		require.Equal(t, http.StatusOK, rec.Code)
		var updatedProd oapi.ProductResponse
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &updatedProd))
		assert.Equal(t, 15, updatedProd.PrepTimeMinutes)
	})

	// 4. Upload de imagem, substituição (remove antiga), validações e static file server.
	t.Run("product image upload, replacement, static serving and delete cleanup", func(t *testing.T) {
		// JPEG válido -> 200
		rec := doMultipartUploadRequest(
			t, router,
			fmt.Sprintf("/api/v1/admin/products/%s/image", prodReadyID),
			adminToken, "file", "brigadeiro.jpg", testJPEGBytes(),
		)
		require.Equal(t, http.StatusOK, rec.Code)
		var prodWithJPEG oapi.ProductResponse
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &prodWithJPEG))
		require.NotNil(t, prodWithJPEG.ImageUrl)
		assert.True(t, strings.HasPrefix(*prodWithJPEG.ImageUrl, "http://localhost:8080/uploads/products/"))
		assert.True(t, strings.HasSuffix(*prodWithJPEG.ImageUrl, ".jpg"))

		firstRelPath := strings.TrimPrefix(*prodWithJPEG.ImageUrl, "http://localhost:8080/uploads/")
		firstDiskPath := filepath.Join(uploadDir, filepath.FromSlash(firstRelPath))
		_, statErr := os.Stat(firstDiskPath)
		require.NoError(t, statErr)

		// Serve arquivo estático via router (/uploads/*)
		staticReq := httptest.NewRequest(http.MethodGet, "/uploads/"+firstRelPath, nil)
		staticRec := httptest.NewRecorder()
		router.ServeHTTP(staticRec, staticReq)
		assert.Equal(t, http.StatusOK, staticRec.Code)
		assert.Equal(t, "nosniff", staticRec.Header().Get("X-Content-Type-Options"))
		assert.Equal(t, "public, max-age=31536000, immutable", staticRec.Header().Get("Cache-Control"))
		assert.Equal(t, testJPEGBytes(), staticRec.Body.Bytes())

		// Upload PNG substitui JPEG e remove arquivo anterior do disco
		rec = doMultipartUploadRequest(
			t, router,
			fmt.Sprintf("/api/v1/admin/products/%s/image", prodReadyID),
			adminToken, "file", "brigadeiro.png", testPNGBytes(),
		)
		require.Equal(t, http.StatusOK, rec.Code)
		var prodWithPNG oapi.ProductResponse
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &prodWithPNG))
		require.NotNil(t, prodWithPNG.ImageUrl)
		assert.True(t, strings.HasSuffix(*prodWithPNG.ImageUrl, ".png"))

		_, statErr = os.Stat(firstDiskPath)
		assert.True(t, os.IsNotExist(statErr), "old image file should have been deleted")

		// Upload WebP válido também funciona
		rec = doMultipartUploadRequest(
			t, router,
			fmt.Sprintf("/api/v1/admin/products/%s/image", prodToDeleteID),
			adminToken, "file", "torta.webp", testWebPBytes(),
		)
		require.Equal(t, http.StatusOK, rec.Code)
		var prodToDelResp oapi.ProductResponse
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &prodToDelResp))
		require.NotNil(t, prodToDelResp.ImageUrl)
		delRelPath := strings.TrimPrefix(*prodToDelResp.ImageUrl, "http://localhost:8080/uploads/")
		delDiskPath := filepath.Join(uploadDir, filepath.FromSlash(delRelPath))
		_, statErr = os.Stat(delDiskPath)
		require.NoError(t, statErr)

		// Deletar produto também remove imagem armazenada do disco!
		rec = doJSONRequest(t, router, http.MethodDelete, "/api/v1/admin/products/"+prodToDeleteID.String(), adminToken, nil)
		assert.Equal(t, http.StatusNoContent, rec.Code)
		_, statErr = os.Stat(delDiskPath)
		assert.True(t, os.IsNotExist(statErr), "deleted product image should have been removed from disk")

		// Texto disfarçado de .jpg -> 400
		rec = doMultipartUploadRequest(
			t, router,
			fmt.Sprintf("/api/v1/admin/products/%s/image", prodReadyID),
			adminToken, "file", "fake.jpg", []byte("not really a jpeg file"),
		)
		assert.Equal(t, http.StatusBadRequest, rec.Code)

		// SVG disfarçado de .jpg -> 400
		rec = doMultipartUploadRequest(
			t, router,
			fmt.Sprintf("/api/v1/admin/products/%s/image", prodReadyID),
			adminToken, "file", "xss.jpg", []byte(`<svg xmlns="http://www.w3.org/2000/svg"><script>alert(1)</script></svg>`),
		)
		assert.Equal(t, http.StatusBadRequest, rec.Code)

		// Upload maior que MaxUploadBytes (1024) -> 400
		oversized := append(testPNGBytes(), bytes.Repeat([]byte("A"), int(maxUploadBytes))...)
		rec = doMultipartUploadRequest(
			t, router,
			fmt.Sprintf("/api/v1/admin/products/%s/image", prodReadyID),
			adminToken, "file", "huge.png", oversized,
		)
		assert.Equal(t, http.StatusBadRequest, rec.Code)

		// Sem campo "file" -> 400
		rec = doMultipartUploadRequest(
			t, router,
			fmt.Sprintf("/api/v1/admin/products/%s/image", prodReadyID),
			adminToken, "image", "wrong-field.png", testPNGBytes(),
		)
		assert.Equal(t, http.StatusBadRequest, rec.Code)

		// Arquivo vazio -> 400
		rec = doMultipartUploadRequest(
			t, router,
			fmt.Sprintf("/api/v1/admin/products/%s/image", prodReadyID),
			adminToken, "file", "empty.png", []byte{},
		)
		assert.Equal(t, http.StatusBadRequest, rec.Code)

		// Produto inexistente -> 404
		missingProdID, idErr := uuid.NewV7()
		require.NoError(t, idErr)
		rec = doMultipartUploadRequest(
			t, router,
			fmt.Sprintf("/api/v1/admin/products/%s/image", missingProdID),
			adminToken, "file", "ok.png", testPNGBytes(),
		)
		assert.Equal(t, http.StatusNotFound, rec.Code)
	})

	// 5. Variações: CRUD, últimas regras de variante e SKU.
	t.Run("variants CRUD, last variant protection, last active protection and SKU clearing", func(t *testing.T) {
		// Busca produto sob_encomenda (tem 1 variante ativa: BOLO-RV-2KG)
		rec := doJSONRequest(t, router, http.MethodGet, "/api/v1/admin/products/"+prodMadeToOrderID.String(), adminToken, nil)
		require.Equal(t, http.StatusOK, rec.Code)
		var prod3 oapi.ProductResponse
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &prod3))
		require.Len(t, prod3.Variants, 1)
		v1ID := uuid.UUID(prod3.Variants[0].Id)

		// Não permite deletar a última variante -> 409
		rec = doJSONRequest(t, router, http.MethodDelete, fmt.Sprintf("/api/v1/admin/products/%s/variants/%s", prodMadeToOrderID, v1ID), adminToken, nil)
		assert.Equal(t, http.StatusConflict, rec.Code)

		// Não permite desativar a última variante ativa de um produto ativo -> 409
		rec = doJSONRequest(t, router, http.MethodPatch, fmt.Sprintf("/api/v1/admin/products/%s/variants/%s", prodMadeToOrderID, v1ID), adminToken, map[string]any{
			"active": false,
		})
		assert.Equal(t, http.StatusConflict, rec.Code)

		// Adiciona segunda variante ao produto
		rec = doJSONRequest(t, router, http.MethodPost, fmt.Sprintf("/api/v1/admin/products/%s/variants", prodMadeToOrderID), adminToken, map[string]any{
			"name":        "1kg (10 fatias)",
			"sku":         "BOLO-RV-1KG",
			"price_cents": 10900,
			"sort_order":  2,
			"active":      true,
		})
		require.Equal(t, http.StatusCreated, rec.Code)
		var v2 oapi.ProductVariantResponse
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &v2))
		v2ID := uuid.UUID(v2.Id)

		// Conflito de SKU ao criar ou atualizar variante -> 409
		rec = doJSONRequest(t, router, http.MethodPost, fmt.Sprintf("/api/v1/admin/products/%s/variants", prodMadeToOrderID), adminToken, map[string]any{
			"name":        "Duplicado",
			"sku":         "bolo-rv-1kg",
			"price_cents": 9900,
		})
		assert.Equal(t, http.StatusConflict, rec.Code)

		rec = doJSONRequest(t, router, http.MethodPatch, fmt.Sprintf("/api/v1/admin/products/%s/variants/%s", prodMadeToOrderID, v2ID), adminToken, map[string]any{
			"sku": "BOLO-RV-2KG",
		})
		assert.Equal(t, http.StatusConflict, rec.Code)

		// Limpa SKU enviando string vazia "" -> sku fica nil
		rec = doJSONRequest(t, router, http.MethodPatch, fmt.Sprintf("/api/v1/admin/products/%s/variants/%s", prodMadeToOrderID, v2ID), adminToken, map[string]any{
			"sku":         "",
			"price_cents": 11500,
		})
		require.Equal(t, http.StatusOK, rec.Code)
		var v2Updated oapi.ProductVariantResponse
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &v2Updated))
		assert.Nil(t, v2Updated.Sku)
		assert.Equal(t, int64(11500), v2Updated.PriceCents)

		// Agora que há 2 variações ativas, consegue desativar v2 e depois removê-la -> 204
		rec = doJSONRequest(t, router, http.MethodPatch, fmt.Sprintf("/api/v1/admin/products/%s/variants/%s", prodMadeToOrderID, v2ID), adminToken, map[string]any{
			"active": false,
		})
		require.Equal(t, http.StatusOK, rec.Code)

		rec = doJSONRequest(t, router, http.MethodDelete, fmt.Sprintf("/api/v1/admin/products/%s/variants/%s", prodMadeToOrderID, v2ID), adminToken, nil)
		assert.Equal(t, http.StatusNoContent, rec.Code)
	})

	// 6. Catálogo público: menu, filtros, auto_hide_out_of_stock, detalhe por UUID e por slug.
	t.Run("public menu and product details", func(t *testing.T) {
		// Com auto_hide_out_of_stock=true:
		// - Brigadeiro 70% aparece apenas com a variante CX6 (CX12 tem stock 0 e é ocultada)
		// - Beijinho Clássico (pronta_entrega, stock 0) é ocultado
		// - Bolo Red Velvet (sob_encomenda, stock 0) é mantido visível!
		// - Panetone (categoria inativa) é ocultado
		rec := doJSONRequest(t, router, http.MethodGet, "/api/v1/public/menu", "", nil)
		require.Equal(t, http.StatusOK, rec.Code)
		var menu oapi.PublicMenuResponse
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &menu))
		require.Len(t, menu.Categories, 2)

		assert.Equal(t, "brigadeiros-doces", menu.Categories[0].Slug)
		require.Len(t, menu.Categories[0].Products, 1)
		assert.Equal(t, "brigadeiro-70-cacau-belga", menu.Categories[0].Products[0].Slug)
		assert.True(t, menu.Categories[0].Products[0].InStock)
		require.Len(t, menu.Categories[0].Products[0].Variants, 1)
		assert.Equal(t, 15, menu.Categories[0].Products[0].Variants[0].AvailableStock)

		assert.Equal(t, "bolos-tortas", menu.Categories[1].Slug)
		require.Len(t, menu.Categories[1].Products, 1)
		assert.Equal(t, "bolo-red-velvet", menu.Categories[1].Products[0].Slug)
		assert.True(t, menu.Categories[1].Products[0].InStock)
		require.Len(t, menu.Categories[1].Products[0].Variants, 1)
		assert.True(t, menu.Categories[1].Products[0].Variants[0].InStock)
		assert.Equal(t, 0, menu.Categories[1].Products[0].Variants[0].AvailableStock)

		// Filtros do menu: category_slug, service_type, search, featured_only
		rec = doJSONRequest(t, router, http.MethodGet, "/api/v1/public/menu?category_slug=bolos-tortas&service_type=sob_encomenda&featured_only=true&search=velvet", "", nil)
		require.Equal(t, http.StatusOK, rec.Code)
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &menu))
		require.Len(t, menu.Categories, 1)
		require.Len(t, menu.Categories[0].Products, 1)
		assert.Equal(t, prodMadeToOrderID, uuid.UUID(menu.Categories[0].Products[0].Id))

		// Filtro service_type inválido -> 400
		rec = doJSONRequest(t, router, http.MethodGet, "/api/v1/public/menu?service_type=invalido", "", nil)
		assert.Equal(t, http.StatusBadRequest, rec.Code)

		// GET /api/v1/public/products/{slugOrId} por UUID e por slug:
		// Mesmo com auto_hide_out_of_stock=true, detalhe direto do Beijinho esgotado retorna 200 com in_stock=false!
		rec = doJSONRequest(t, router, http.MethodGet, "/api/v1/public/products/"+prodReadyOutOfStockID.String(), "", nil)
		require.Equal(t, http.StatusOK, rec.Code)
		var pubByID oapi.PublicProductResponse
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &pubByID))
		assert.False(t, pubByID.InStock)
		require.Len(t, pubByID.Variants, 1)
		assert.False(t, pubByID.Variants[0].InStock)

		rec = doJSONRequest(t, router, http.MethodGet, "/api/v1/public/products/BEIJINHO-CLASSICO", "", nil)
		require.Equal(t, http.StatusOK, rec.Code)
		var pubBySlug oapi.PublicProductResponse
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &pubBySlug))
		assert.Equal(t, prodReadyOutOfStockID, uuid.UUID(pubBySlug.Id))
		assert.False(t, pubBySlug.InStock)

		// Produto em categoria inativa não é acessível publicamente -> 404
		rec = doJSONRequest(t, router, http.MethodGet, "/api/v1/public/products/panetone-trufado", "", nil)
		assert.Equal(t, http.StatusNotFound, rec.Code)

		// Ao desativar auto_hide_out_of_stock, Beijinho passa a aparecer no menu com in_stock=false
		rec = doJSONRequest(t, router, http.MethodPatch, "/api/v1/admin/settings", adminToken, map[string]any{
			"auto_hide_out_of_stock": false,
		})
		require.Equal(t, http.StatusOK, rec.Code)

		rec = doJSONRequest(t, router, http.MethodGet, "/api/v1/public/menu?category_slug=brigadeiros-doces", "", nil)
		require.Equal(t, http.StatusOK, rec.Code)
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &menu))
		require.Len(t, menu.Categories, 1)
		require.Len(t, menu.Categories[0].Products, 2)
		assert.Equal(t, "brigadeiro-70-cacau-belga", menu.Categories[0].Products[0].Slug)
		assert.True(t, menu.Categories[0].Products[0].InStock)
		assert.Equal(t, "beijinho-classico", menu.Categories[0].Products[1].Slug)
		assert.False(t, menu.Categories[0].Products[1].InStock)
	})

	// 7. RBAC ponta-a-ponta com JWT real (401 sem token, 200 leituras para atendente/cozinha, 403 escritas/uploads/deleções).
	t.Run("end-to-end RBAC across all Phase 3 endpoints", func(t *testing.T) {
		readEndpoints := []string{
			"/api/v1/admin/settings",
			"/api/v1/admin/categories",
			"/api/v1/admin/categories/" + catBrigadeirosID.String(),
			"/api/v1/admin/products",
			"/api/v1/admin/products/" + prodReadyID.String(),
		}

		for _, path := range readEndpoints {
			// Sem token -> 401
			rec := doJSONRequest(t, router, http.MethodGet, path, "", nil)
			assert.Equal(t, http.StatusUnauthorized, rec.Code, "no token GET %s", path)

			// Atendente e Cozinha -> 200
			rec = doJSONRequest(t, router, http.MethodGet, path, atendenteToken, nil)
			assert.Equal(t, http.StatusOK, rec.Code, "atendente GET %s", path)

			rec = doJSONRequest(t, router, http.MethodGet, path, cozinhaToken, nil)
			assert.Equal(t, http.StatusOK, rec.Code, "cozinha GET %s", path)
		}

		dummyID, idErr := uuid.NewV7()
		require.NoError(t, idErr)

		writeRequests := []struct {
			method string
			path   string
			body   any
		}{
			{http.MethodPatch, "/api/v1/admin/settings", map[string]any{"store_name": "Hack"}},
			{http.MethodPost, "/api/v1/admin/categories", map[string]any{"name": "Hack"}},
			{http.MethodPatch, "/api/v1/admin/categories/" + catBrigadeirosID.String(), map[string]any{"name": "Hack"}},
			{http.MethodDelete, "/api/v1/admin/categories/" + dummyID.String(), nil},
			{http.MethodPost, "/api/v1/admin/products", map[string]any{
				"category_id":  catBrigadeirosID.String(),
				"name":         "Hack",
				"service_type": "pronta_entrega",
				"variants":     []map[string]any{{"name": "U", "price_cents": 100}},
			}},
			{http.MethodPatch, "/api/v1/admin/products/" + prodReadyID.String(), map[string]any{"name": "Hack"}},
			{http.MethodDelete, "/api/v1/admin/products/" + dummyID.String(), nil},
			{http.MethodPost, fmt.Sprintf("/api/v1/admin/products/%s/variants", prodReadyID), map[string]any{"name": "Hack", "price_cents": 100}},
			{http.MethodPatch, fmt.Sprintf("/api/v1/admin/products/%s/variants/%s", prodReadyID, dummyID), map[string]any{"name": "Hack"}},
			{http.MethodDelete, fmt.Sprintf("/api/v1/admin/products/%s/variants/%s", prodReadyID, dummyID), nil},
		}

		for _, reqSpec := range writeRequests {
			rec := doJSONRequest(t, router, reqSpec.method, reqSpec.path, "", reqSpec.body)
			assert.Equal(t, http.StatusUnauthorized, rec.Code, "no token %s %s", reqSpec.method, reqSpec.path)

			rec = doJSONRequest(t, router, reqSpec.method, reqSpec.path, atendenteToken, reqSpec.body)
			assert.Equal(t, http.StatusForbidden, rec.Code, "atendente %s %s", reqSpec.method, reqSpec.path)

			rec = doJSONRequest(t, router, reqSpec.method, reqSpec.path, cozinhaToken, reqSpec.body)
			assert.Equal(t, http.StatusForbidden, rec.Code, "cozinha %s %s", reqSpec.method, reqSpec.path)
		}

		// Upload endpoint RBAC (multipart)
		uploadPath := fmt.Sprintf("/api/v1/admin/products/%s/image", prodReadyID)
		rec := doMultipartUploadRequest(t, router, uploadPath, "", "file", "a.png", testPNGBytes())
		assert.Equal(t, http.StatusUnauthorized, rec.Code)

		rec = doMultipartUploadRequest(t, router, uploadPath, atendenteToken, "file", "a.png", testPNGBytes())
		assert.Equal(t, http.StatusForbidden, rec.Code)

		rec = doMultipartUploadRequest(t, router, uploadPath, cozinhaToken, "file", "a.png", testPNGBytes())
		assert.Equal(t, http.StatusForbidden, rec.Code)
	})
}
