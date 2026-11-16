package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/mail"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/joao-accioly/artisan-store/api/internal/domain"
	"github.com/joao-accioly/artisan-store/api/internal/platform/storage"
	"github.com/joao-accioly/artisan-store/api/internal/repository"
	"github.com/joao-accioly/artisan-store/api/internal/repository/sqlc"
)

const pgForeignKeyViolationCode = "23503"

// UpdateStoreSettingsInput agrupa os campos editáveis nas configurações da loja.
type UpdateStoreSettingsInput struct {
	StoreName                *string
	Description              *string
	Phone                    *string
	WhatsApp                 *string
	Email                    *string
	AddressLine              *string
	OpeningHours             *domain.OpeningHours
	AcceptedPaymentMethods   *[]domain.PaymentMethod
	ManualStatus             *domain.StoreManualStatus
	AllowScheduling          *bool
	PixReservationTTLMinutes *int
	AutoHideOutOfStock       *bool
}

// CreateCategoryInput agrupa os dados para criação de categoria.
type CreateCategoryInput struct {
	Name      string
	Slug      *string
	SortOrder int
	Active    bool
}

// UpdateCategoryInput agrupa os campos editáveis de uma categoria.
type UpdateCategoryInput struct {
	Name      *string
	Slug      *string
	SortOrder *int
	Active    *bool
}

// CreateVariantInput agrupa os dados para criação de variante de produto.
type CreateVariantInput struct {
	Name         string
	SKU          *string
	PriceCents   int64
	CurrentStock int
	MinStock     int
	SortOrder    int
	Active       bool
}

// UpdateVariantInput agrupa os campos editáveis de uma variante de produto.
type UpdateVariantInput struct {
	Name       *string
	SKU        *string
	ClearSKU   bool
	PriceCents *int64
	MinStock   *int
	SortOrder  *int
	Active     *bool
}

// ProductListFilter define filtros administrativos de listagem de produtos.
type ProductListFilter struct {
	CategoryID      *uuid.UUID
	ServiceType     *domain.ServiceType
	Active          *bool
	AvailableOnline *bool
	Search          *string
}

// CreateProductInput agrupa os dados para criação de produto com variações iniciais.
type CreateProductInput struct {
	CategoryID      uuid.UUID
	Name            string
	Slug            *string
	Description     string
	ServiceType     domain.ServiceType
	PrepTimeMinutes int
	Active          bool
	AvailableOnline bool
	Featured        bool
	SortOrder       int
	Variants        []CreateVariantInput
}

// UpdateProductInput agrupa os campos editáveis de um produto.
type UpdateProductInput struct {
	CategoryID      *uuid.UUID
	Name            *string
	Slug            *string
	Description     *string
	ServiceType     *domain.ServiceType
	PrepTimeMinutes *int
	Active          *bool
	AvailableOnline *bool
	Featured        *bool
	SortOrder       *int
}

// PublicMenuFilter define filtros opcionais de busca no cardápio público.
type PublicMenuFilter struct {
	CategorySlug *string
	ServiceType  *domain.ServiceType
	Search       *string
	FeaturedOnly *bool
}

// CatalogService implementa as regras de catálogo, configurações da loja e upload de imagens.
type CatalogService struct {
	pool           *pgxpool.Pool
	queries        *sqlc.Queries
	storage        storage.Storage
	maxUploadBytes int64
	log            *slog.Logger
	nowFunc        func() time.Time
}

// NewCatalogService constrói o serviço de catálogo com banco, storage e limite de upload.
func NewCatalogService(
	pool *pgxpool.Pool,
	store storage.Storage,
	maxUploadBytes int64,
	log *slog.Logger,
) (*CatalogService, error) {
	if pool == nil {
		return nil, errors.New("database pool is required")
	}
	if maxUploadBytes <= 0 {
		maxUploadBytes = 5 * 1024 * 1024
	}
	if log == nil {
		log = slog.Default()
	}

	return &CatalogService{
		pool:           pool,
		queries:        sqlc.New(pool),
		storage:        store,
		maxUploadBytes: maxUploadBytes,
		log:            log,
		nowFunc:        time.Now,
	}, nil
}

// GetSettings retorna as configurações operacionais da loja.
func (s *CatalogService) GetSettings(ctx context.Context) (*domain.StoreSettings, error) {
	row, err := s.queries.GetStoreSettings(ctx)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domain.NewNotFoundError("Configurações da loja não encontradas")
		}
		return nil, domain.NewInternalError("Erro ao buscar configurações da loja", err)
	}

	settings, err := mapStoreSettings(row)
	if err != nil {
		return nil, domain.NewInternalError("Erro ao decodificar configurações da loja", err)
	}
	return &settings, nil
}

// GetPublicStore retorna as informações públicas da loja e se ela está aberta no instante.
func (s *CatalogService) GetPublicStore(ctx context.Context) (*domain.StoreSettings, bool, error) {
	settings, err := s.GetSettings(ctx)
	if err != nil {
		return nil, false, err
	}
	return settings, settings.IsOpenAt(s.nowFunc()), nil
}

// UpdateSettings valida e atualiza as configurações gerais da loja.
func (s *CatalogService) UpdateSettings(ctx context.Context, input UpdateStoreSettingsInput) (*domain.StoreSettings, error) {
	if input.StoreName == nil &&
		input.Description == nil &&
		input.Phone == nil &&
		input.WhatsApp == nil &&
		input.Email == nil &&
		input.AddressLine == nil &&
		input.OpeningHours == nil &&
		input.AcceptedPaymentMethods == nil &&
		input.ManualStatus == nil &&
		input.AllowScheduling == nil &&
		input.PixReservationTTLMinutes == nil &&
		input.AutoHideOutOfStock == nil {
		return nil, domain.NewValidationError("Informe ao menos um campo para atualização", nil)
	}

	details := make(map[string]any)
	if input.StoreName != nil && utf8.RuneCountInString(strings.TrimSpace(*input.StoreName)) < 2 {
		details["store_name"] = "deve ter pelo menos 2 caracteres"
	}
	if input.Email != nil && strings.TrimSpace(*input.Email) != "" && !isValidOptionalEmail(*input.Email) {
		details["email"] = "formato de e-mail inválido"
	}
	if input.OpeningHours != nil {
		validateOpeningHours(*input.OpeningHours, details)
	}
	if input.AcceptedPaymentMethods != nil {
		if len(*input.AcceptedPaymentMethods) == 0 {
			details["accepted_payment_methods"] = "informe ao menos um método de pagamento"
		} else {
			for _, m := range *input.AcceptedPaymentMethods {
				if !m.IsValid() {
					details["accepted_payment_methods"] = fmt.Sprintf("método inválido: %q", m)
					break
				}
			}
		}
	}
	if input.ManualStatus != nil && !input.ManualStatus.IsValid() {
		details["manual_status"] = "deve ser auto, open ou closed"
	}
	if input.PixReservationTTLMinutes != nil && (*input.PixReservationTTLMinutes < 1 || *input.PixReservationTTLMinutes > 1440) {
		details["pix_reservation_ttl_minutes"] = "deve estar entre 1 e 1440 minutos"
	}
	if len(details) > 0 {
		return nil, domain.NewValidationError("Dados inválidos nas configurações da loja", details)
	}

	var updated domain.StoreSettings
	err := repository.WithTx(ctx, s.pool, func(q *sqlc.Queries) error {
		curRow, err := q.GetStoreSettings(ctx)
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return domain.NewNotFoundError("Configurações da loja não encontradas")
			}
			return domain.NewInternalError("Erro ao buscar configurações da loja", err)
		}

		current, err := mapStoreSettings(curRow)
		if err != nil {
			return domain.NewInternalError("Erro ao decodificar configurações da loja", err)
		}

		if input.StoreName != nil {
			current.StoreName = strings.TrimSpace(*input.StoreName)
		}
		if input.Description != nil {
			current.Description = strings.TrimSpace(*input.Description)
		}
		if input.Phone != nil {
			current.Phone = strings.TrimSpace(*input.Phone)
		}
		if input.WhatsApp != nil {
			current.WhatsApp = strings.TrimSpace(*input.WhatsApp)
		}
		if input.Email != nil {
			current.Email = strings.TrimSpace(*input.Email)
		}
		if input.AddressLine != nil {
			current.AddressLine = strings.TrimSpace(*input.AddressLine)
		}
		if input.OpeningHours != nil {
			current.OpeningHours = *input.OpeningHours
		}
		if input.AcceptedPaymentMethods != nil {
			current.AcceptedPaymentMethods = deduplicatePaymentMethods(*input.AcceptedPaymentMethods)
		}
		if input.ManualStatus != nil {
			current.ManualStatus = *input.ManualStatus
		}
		if input.AllowScheduling != nil {
			current.AllowScheduling = *input.AllowScheduling
		}
		if input.PixReservationTTLMinutes != nil {
			current.PixReservationTTLMinutes = *input.PixReservationTTLMinutes
		}
		if input.AutoHideOutOfStock != nil {
			current.AutoHideOutOfStock = *input.AutoHideOutOfStock
		}

		hoursJSON, err := json.Marshal(current.OpeningHours)
		if err != nil {
			return domain.NewInternalError("Erro ao serializar horários de funcionamento", err)
		}

		methodsStr := make([]string, 0, len(current.AcceptedPaymentMethods))
		for _, m := range current.AcceptedPaymentMethods {
			methodsStr = append(methodsStr, string(m))
		}

		savedRow, err := q.UpdateStoreSettings(ctx, sqlc.UpdateStoreSettingsParams{
			StoreName:                current.StoreName,
			Description:              current.Description,
			Phone:                    current.Phone,
			Whatsapp:                 current.WhatsApp,
			Email:                    current.Email,
			AddressLine:              current.AddressLine,
			OpeningHours:             hoursJSON,
			AcceptedPaymentMethods:   methodsStr,
			ManualStatus:             string(current.ManualStatus),
			AllowScheduling:          current.AllowScheduling,
			PixReservationTtlMinutes: int32(current.PixReservationTTLMinutes),
			AutoHideOutOfStock:       current.AutoHideOutOfStock,
		})
		if err != nil {
			return domain.NewInternalError("Erro ao salvar configurações da loja", err)
		}

		updated, err = mapStoreSettings(savedRow)
		if err != nil {
			return domain.NewInternalError("Erro ao decodificar configurações salvas", err)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	return &updated, nil
}

// ListCategories lista categorias paginadas com filtro opcional por status ativo.
func (s *CatalogService) ListCategories(ctx context.Context, active *bool, page, pageSize int) ([]domain.Category, domain.PaginationMeta, error) {
	var err error
	page, pageSize, err = normalizePagination(page, pageSize)
	if err != nil {
		return nil, domain.PaginationMeta{}, err
	}

	activeFilter := toPGTypeBool(active)
	total, err := s.queries.CountCategoriesByFilter(ctx, activeFilter)
	if err != nil {
		return nil, domain.PaginationMeta{}, domain.NewInternalError("Erro ao contar categorias", err)
	}

	offset := int32((page - 1) * pageSize)
	rows, err := s.queries.ListCategories(ctx, sqlc.ListCategoriesParams{
		Active: activeFilter,
		Limit:  int32(pageSize),
		Offset: offset,
	})
	if err != nil {
		return nil, domain.PaginationMeta{}, domain.NewInternalError("Erro ao listar categorias", err)
	}

	items := make([]domain.Category, 0, len(rows))
	for _, row := range rows {
		items = append(items, mapSQLCCategory(row))
	}

	return items, domain.NewPaginationMeta(page, pageSize, total), nil
}

// GetCategory busca uma categoria pelo UUIDv7.
func (s *CatalogService) GetCategory(ctx context.Context, id uuid.UUID) (*domain.Category, error) {
	row, err := s.queries.GetCategoryByID(ctx, id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domain.NewNotFoundError("Categoria não encontrada")
		}
		return nil, domain.NewInternalError("Erro ao buscar categoria", err)
	}
	cat := mapSQLCCategory(row)
	return &cat, nil
}

// CreateCategory valida e cria uma nova categoria de catálogo.
func (s *CatalogService) CreateCategory(ctx context.Context, input CreateCategoryInput) (*domain.Category, error) {
	name := strings.TrimSpace(input.Name)
	if utf8.RuneCountInString(name) < 2 {
		return nil, domain.NewValidationError("Dados inválidos para criação de categoria", map[string]any{
			"name": "deve ter pelo menos 2 caracteres",
		})
	}

	slug, err := resolveAndValidateSlug(name, input.Slug)
	if err != nil {
		return nil, err
	}

	id, err := uuid.NewV7()
	if err != nil {
		return nil, domain.NewInternalError("Erro ao gerar identificador da categoria", err)
	}

	row, err := s.queries.CreateCategory(ctx, sqlc.CreateCategoryParams{
		ID:        id,
		Name:      name,
		Slug:      slug,
		SortOrder: int32(input.SortOrder),
		Active:    input.Active,
	})
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == pgUniqueViolationCode {
			return nil, domain.NewConflictError("Slug de categoria já cadastrado")
		}
		return nil, domain.NewInternalError("Erro ao criar categoria", err)
	}

	cat := mapSQLCCategory(row)
	return &cat, nil
}

// UpdateCategory atualiza nome, slug, ordem ou status de uma categoria.
func (s *CatalogService) UpdateCategory(ctx context.Context, id uuid.UUID, input UpdateCategoryInput) (*domain.Category, error) {
	if input.Name == nil && input.Slug == nil && input.SortOrder == nil && input.Active == nil {
		return nil, domain.NewValidationError("Informe ao menos um campo para atualização", nil)
	}

	if input.Name != nil && utf8.RuneCountInString(strings.TrimSpace(*input.Name)) < 2 {
		return nil, domain.NewValidationError("Dados inválidos para atualização de categoria", map[string]any{
			"name": "deve ter pelo menos 2 caracteres",
		})
	}
	if input.Slug != nil {
		normalizedSlug := strings.ToLower(strings.TrimSpace(*input.Slug))
		if err := domain.ValidateSlug(normalizedSlug); err != nil {
			return nil, err
		}
	}

	var updated domain.Category
	err := repository.WithTx(ctx, s.pool, func(q *sqlc.Queries) error {
		cur, err := q.GetCategoryByID(ctx, id)
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return domain.NewNotFoundError("Categoria não encontrada")
			}
			return domain.NewInternalError("Erro ao buscar categoria", err)
		}

		name := cur.Name
		if input.Name != nil {
			name = strings.TrimSpace(*input.Name)
		}
		slug := cur.Slug
		if input.Slug != nil {
			slug = strings.ToLower(strings.TrimSpace(*input.Slug))
		}
		sortOrder := cur.SortOrder
		if input.SortOrder != nil {
			sortOrder = int32(*input.SortOrder)
		}
		active := cur.Active
		if input.Active != nil {
			active = *input.Active
		}

		saved, err := q.UpdateCategory(ctx, sqlc.UpdateCategoryParams{
			ID:        id,
			Name:      name,
			Slug:      slug,
			SortOrder: sortOrder,
			Active:    active,
		})
		if err != nil {
			var pgErr *pgconn.PgError
			if errors.As(err, &pgErr) && pgErr.Code == pgUniqueViolationCode {
				return domain.NewConflictError("Slug de categoria já cadastrado")
			}
			return domain.NewInternalError("Erro ao atualizar categoria", err)
		}

		updated = mapSQLCCategory(saved)
		return nil
	})
	if err != nil {
		return nil, err
	}

	return &updated, nil
}

// DeleteCategory remove uma categoria caso não haja produtos vinculados.
func (s *CatalogService) DeleteCategory(ctx context.Context, id uuid.UUID) error {
	rows, err := s.queries.DeleteCategory(ctx, id)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == pgForeignKeyViolationCode {
			return domain.NewConflictError("Categoria possui produtos vinculados")
		}
		return domain.NewInternalError("Erro ao remover categoria", err)
	}
	if rows == 0 {
		return domain.NewNotFoundError("Categoria não encontrada")
	}
	return nil
}

// ListProducts lista produtos paginados com variações carregadas em lote (sem N+1).
func (s *CatalogService) ListProducts(ctx context.Context, filter ProductListFilter, page, pageSize int) ([]domain.Product, domain.PaginationMeta, error) {
	var err error
	page, pageSize, err = normalizePagination(page, pageSize)
	if err != nil {
		return nil, domain.PaginationMeta{}, err
	}

	var catID pgtype.UUID
	if filter.CategoryID != nil {
		catID = pgtype.UUID{Bytes: *filter.CategoryID, Valid: true}
	}

	var serviceType pgtype.Text
	if filter.ServiceType != nil {
		if !filter.ServiceType.IsValid() {
			return nil, domain.PaginationMeta{}, domain.NewValidationError("Tipo de atendimento inválido no filtro", map[string]any{
				"service_type": "deve ser pronta_entrega ou sob_encomenda",
			})
		}
		serviceType = pgtype.Text{String: string(*filter.ServiceType), Valid: true}
	}

	var searchFilter pgtype.Text
	if filter.Search != nil && strings.TrimSpace(*filter.Search) != "" {
		searchFilter = pgtype.Text{String: escapeLikePattern(strings.TrimSpace(*filter.Search)), Valid: true}
	}

	activeFilter := toPGTypeBool(filter.Active)
	onlineFilter := toPGTypeBool(filter.AvailableOnline)

	total, err := s.queries.CountProductsByFilter(ctx, sqlc.CountProductsByFilterParams{
		CategoryID:      catID,
		ServiceType:     serviceType,
		Active:          activeFilter,
		AvailableOnline: onlineFilter,
		Search:          searchFilter,
	})
	if err != nil {
		return nil, domain.PaginationMeta{}, domain.NewInternalError("Erro ao contar produtos", err)
	}

	offset := int32((page - 1) * pageSize)
	rows, err := s.queries.ListProducts(ctx, sqlc.ListProductsParams{
		CategoryID:      catID,
		ServiceType:     serviceType,
		Active:          activeFilter,
		AvailableOnline: onlineFilter,
		Search:          searchFilter,
		Limit:           int32(pageSize),
		Offset:          offset,
	})
	if err != nil {
		return nil, domain.PaginationMeta{}, domain.NewInternalError("Erro ao listar produtos", err)
	}

	if len(rows) == 0 {
		return []domain.Product{}, domain.NewPaginationMeta(page, pageSize, total), nil
	}

	productIDs := make([]uuid.UUID, 0, len(rows))
	for _, r := range rows {
		productIDs = append(productIDs, r.ID)
	}

	variantsByProduct, err := s.loadVariantsByProductIDs(ctx, s.queries, productIDs)
	if err != nil {
		return nil, domain.PaginationMeta{}, err
	}

	products := make([]domain.Product, 0, len(rows))
	for _, r := range rows {
		p := mapSQLCProduct(r, variantsByProduct[r.ID])
		products = append(products, p)
	}

	return products, domain.NewPaginationMeta(page, pageSize, total), nil
}

// GetProduct busca detalhes completos de um produto e suas variações.
func (s *CatalogService) GetProduct(ctx context.Context, id uuid.UUID) (*domain.Product, error) {
	row, err := s.queries.GetProductByID(ctx, id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domain.NewNotFoundError("Produto não encontrado")
		}
		return nil, domain.NewInternalError("Erro ao buscar produto", err)
	}

	variantsMap, err := s.loadVariantsByProductIDs(ctx, s.queries, []uuid.UUID{id})
	if err != nil {
		return nil, err
	}

	product := mapSQLCProduct(row, variantsMap[id])
	return &product, nil
}

// CreateProduct cria um novo produto e suas variações iniciais em uma única transação.
func (s *CatalogService) CreateProduct(ctx context.Context, input CreateProductInput) (*domain.Product, error) {
	name := strings.TrimSpace(input.Name)
	details := make(map[string]any)
	if utf8.RuneCountInString(name) < 2 {
		details["name"] = "deve ter pelo menos 2 caracteres"
	}
	if !input.ServiceType.IsValid() {
		details["service_type"] = "deve ser pronta_entrega ou sob_encomenda"
	}
	if input.PrepTimeMinutes < 0 {
		details["prep_time_minutes"] = "não pode ser negativo"
	}
	if len(input.Variants) == 0 {
		details["variants"] = "informe ao menos uma variante inicial"
	}
	if len(details) > 0 {
		return nil, domain.NewValidationError("Dados inválidos para criação de produto", details)
	}

	slug, err := resolveAndValidateSlug(name, input.Slug)
	if err != nil {
		return nil, err
	}

	seenSKUs := make(map[string]struct{}, len(input.Variants))
	for i, v := range input.Variants {
		if valErr := validateVariantValues(v.Name, v.PriceCents, v.CurrentStock, v.MinStock); valErr != nil {
			return nil, valErr
		}
		if v.SKU != nil {
			trimmed := strings.ToLower(strings.TrimSpace(*v.SKU))
			if trimmed != "" {
				if _, dup := seenSKUs[trimmed]; dup {
					return nil, domain.NewValidationError("SKU duplicado na lista de variações", map[string]any{
						fmt.Sprintf("variants[%d].sku", i): "SKU repetido no mesmo produto",
					})
				}
				seenSKUs[trimmed] = struct{}{}
			}
		}
	}

	var created domain.Product
	err = repository.WithTx(ctx, s.pool, func(q *sqlc.Queries) error {
		if _, catErr := q.GetCategoryByID(ctx, input.CategoryID); catErr != nil {
			if errors.Is(catErr, pgx.ErrNoRows) {
				return domain.NewNotFoundError("Categoria não encontrada")
			}
			return domain.NewInternalError("Erro ao verificar categoria", catErr)
		}

		productID, idErr := uuid.NewV7()
		if idErr != nil {
			return domain.NewInternalError("Erro ao gerar identificador do produto", idErr)
		}

		prodRow, prodErr := q.CreateProduct(ctx, sqlc.CreateProductParams{
			ID:              productID,
			CategoryID:      input.CategoryID,
			Name:            name,
			Slug:            slug,
			Description:     strings.TrimSpace(input.Description),
			ServiceType:     string(input.ServiceType),
			PrepTimeMinutes: int32(input.PrepTimeMinutes),
			Active:          input.Active,
			AvailableOnline: input.AvailableOnline,
			Featured:        input.Featured,
			SortOrder:       int32(input.SortOrder),
		})
		if prodErr != nil {
			return mapProductDBError(prodErr)
		}

		variants := make([]domain.ProductVariant, 0, len(input.Variants))
		for _, v := range input.Variants {
			varID, varIDErr := uuid.NewV7()
			if varIDErr != nil {
				return domain.NewInternalError("Erro ao gerar identificador da variação", varIDErr)
			}

			varRow, varErr := q.CreateProductVariant(ctx, sqlc.CreateProductVariantParams{
				ID:            varID,
				ProductID:     productID,
				Name:          strings.TrimSpace(v.Name),
				Sku:           toOptionalSKU(v.SKU),
				PriceCents:    v.PriceCents,
				CurrentStock:  int32(v.CurrentStock),
				ReservedStock: 0,
				MinStock:      int32(v.MinStock),
				SortOrder:     int32(v.SortOrder),
				Active:        v.Active,
			})
			if varErr != nil {
				return mapProductDBError(varErr)
			}
			variants = append(variants, mapSQLCVariant(varRow))
		}

		created = mapSQLCProduct(prodRow, variants)
		return nil
	})
	if err != nil {
		return nil, err
	}

	return &created, nil
}

// UpdateProduct altera dados cadastrais de um produto existente.
func (s *CatalogService) UpdateProduct(ctx context.Context, id uuid.UUID, input UpdateProductInput) (*domain.Product, error) {
	if input.CategoryID == nil &&
		input.Name == nil &&
		input.Slug == nil &&
		input.Description == nil &&
		input.ServiceType == nil &&
		input.PrepTimeMinutes == nil &&
		input.Active == nil &&
		input.AvailableOnline == nil &&
		input.Featured == nil &&
		input.SortOrder == nil {
		return nil, domain.NewValidationError("Informe ao menos um campo para atualização", nil)
	}

	details := make(map[string]any)
	if input.Name != nil && utf8.RuneCountInString(strings.TrimSpace(*input.Name)) < 2 {
		details["name"] = "deve ter pelo menos 2 caracteres"
	}
	if input.ServiceType != nil && !input.ServiceType.IsValid() {
		details["service_type"] = "deve ser pronta_entrega ou sob_encomenda"
	}
	if input.PrepTimeMinutes != nil && *input.PrepTimeMinutes < 0 {
		details["prep_time_minutes"] = "não pode ser negativo"
	}
	if len(details) > 0 {
		return nil, domain.NewValidationError("Dados inválidos para atualização de produto", details)
	}
	if input.Slug != nil {
		normalizedSlug := strings.ToLower(strings.TrimSpace(*input.Slug))
		if err := domain.ValidateSlug(normalizedSlug); err != nil {
			return nil, err
		}
	}

	var updated domain.Product
	err := repository.WithTx(ctx, s.pool, func(q *sqlc.Queries) error {
		cur, err := q.GetProductByID(ctx, id)
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return domain.NewNotFoundError("Produto não encontrado")
			}
			return domain.NewInternalError("Erro ao buscar produto", err)
		}

		categoryID := cur.CategoryID
		if input.CategoryID != nil && *input.CategoryID != cur.CategoryID {
			if _, catErr := q.GetCategoryByID(ctx, *input.CategoryID); catErr != nil {
				if errors.Is(catErr, pgx.ErrNoRows) {
					return domain.NewNotFoundError("Categoria não encontrada")
				}
				return domain.NewInternalError("Erro ao verificar categoria", catErr)
			}
			categoryID = *input.CategoryID
		}

		name := cur.Name
		if input.Name != nil {
			name = strings.TrimSpace(*input.Name)
		}
		slug := cur.Slug
		if input.Slug != nil {
			slug = strings.ToLower(strings.TrimSpace(*input.Slug))
		}
		description := cur.Description
		if input.Description != nil {
			description = strings.TrimSpace(*input.Description)
		}
		serviceType := cur.ServiceType
		if input.ServiceType != nil {
			serviceType = string(*input.ServiceType)
		}
		prepTime := cur.PrepTimeMinutes
		if input.PrepTimeMinutes != nil {
			prepTime = int32(*input.PrepTimeMinutes)
		}
		active := cur.Active
		if input.Active != nil {
			active = *input.Active
		}
		availableOnline := cur.AvailableOnline
		if input.AvailableOnline != nil {
			availableOnline = *input.AvailableOnline
		}
		featured := cur.Featured
		if input.Featured != nil {
			featured = *input.Featured
		}
		sortOrder := cur.SortOrder
		if input.SortOrder != nil {
			sortOrder = int32(*input.SortOrder)
		}

		saved, err := q.UpdateProduct(ctx, sqlc.UpdateProductParams{
			ID:              id,
			CategoryID:      categoryID,
			Name:            name,
			Slug:            slug,
			Description:     description,
			ServiceType:     serviceType,
			PrepTimeMinutes: prepTime,
			Active:          active,
			AvailableOnline: availableOnline,
			Featured:        featured,
			SortOrder:       sortOrder,
		})
		if err != nil {
			return mapProductDBError(err)
		}

		variantsMap, err := s.loadVariantsByProductIDs(ctx, q, []uuid.UUID{id})
		if err != nil {
			return err
		}

		updated = mapSQLCProduct(saved, variantsMap[id])
		return nil
	})
	if err != nil {
		return nil, err
	}

	return &updated, nil
}

// DeleteProduct remove um produto (e suas variações em cascata) e limpa a imagem no storage.
func (s *CatalogService) DeleteProduct(ctx context.Context, id uuid.UUID) error {
	var oldImageKey string
	err := repository.WithTx(ctx, s.pool, func(q *sqlc.Queries) error {
		cur, err := q.GetProductByID(ctx, id)
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return domain.NewNotFoundError("Produto não encontrado")
			}
			return domain.NewInternalError("Erro ao buscar produto", err)
		}
		if cur.ImageKey.Valid {
			oldImageKey = cur.ImageKey.String
		}

		rows, err := q.DeleteProduct(ctx, id)
		if err != nil {
			var pgErr *pgconn.PgError
			if errors.As(err, &pgErr) && pgErr.Code == pgForeignKeyViolationCode {
				return domain.NewConflictError("Produto possui registros vinculados e não pode ser removido")
			}
			return domain.NewInternalError("Erro ao remover produto", err)
		}
		if rows == 0 {
			return domain.NewNotFoundError("Produto não encontrado")
		}
		return nil
	})
	if err != nil {
		return err
	}

	if oldImageKey != "" && s.storage != nil {
		if delErr := s.storage.Delete(ctx, oldImageKey); delErr != nil {
			s.log.Warn("failed to delete product image from storage",
				slog.String("product_id", id.String()),
				slog.String("image_key", oldImageKey),
				slog.String("error", delErr.Error()),
			)
		}
	}
	return nil
}

// UploadProductImage valida o MIME real (magic bytes) e tamanho, salva no storage e atualiza o produto.
func (s *CatalogService) UploadProductImage(ctx context.Context, productID uuid.UUID, data []byte) (*domain.Product, error) {
	if s.storage == nil {
		return nil, domain.NewInternalError("Storage de imagens não configurado", nil)
	}
	if int64(len(data)) == 0 {
		return nil, domain.NewValidationError("Arquivo de imagem vazio ou não enviado", map[string]any{
			"file": "envie uma imagem JPEG, PNG ou WebP",
		})
	}
	if int64(len(data)) > s.maxUploadBytes {
		return nil, domain.NewValidationError(
			fmt.Sprintf("Arquivo excede o limite máximo permitido de %d bytes", s.maxUploadBytes),
			map[string]any{"file": "tamanho máximo excedido"},
		)
	}

	contentType, ext, ok := storage.DetectImageType(data)
	if !ok {
		return nil, domain.NewValidationError("Formato de imagem inválido: envie JPEG, PNG ou WebP válido", map[string]any{
			"file": fmt.Sprintf("tipo detectado %q não permitido", contentType),
		})
	}

	if _, err := s.queries.GetProductByID(ctx, productID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domain.NewNotFoundError("Produto não encontrado")
		}
		return nil, domain.NewInternalError("Erro ao verificar produto", err)
	}

	newKey, err := storage.BuildProductImageKey(productID.String(), ext)
	if err != nil {
		return nil, domain.NewInternalError("Erro ao gerar chave de imagem", err)
	}

	publicURL, err := s.storage.Save(ctx, newKey, contentType, data)
	if err != nil {
		return nil, domain.NewInternalError("Erro ao salvar imagem no storage", err)
	}

	var oldKey string
	var updated domain.Product
	txErr := repository.WithTx(ctx, s.pool, func(q *sqlc.Queries) error {
		cur, err := q.GetProductByID(ctx, productID)
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return domain.NewNotFoundError("Produto não encontrado")
			}
			return domain.NewInternalError("Erro ao buscar produto", err)
		}
		if cur.ImageKey.Valid {
			oldKey = cur.ImageKey.String
		}

		saved, err := q.UpdateProductImage(ctx, sqlc.UpdateProductImageParams{
			ID:       productID,
			ImageUrl: pgtype.Text{String: publicURL, Valid: true},
			ImageKey: pgtype.Text{String: newKey, Valid: true},
		})
		if err != nil {
			return domain.NewInternalError("Erro ao atualizar imagem do produto", err)
		}

		variantsMap, err := s.loadVariantsByProductIDs(ctx, q, []uuid.UUID{productID})
		if err != nil {
			return err
		}
		updated = mapSQLCProduct(saved, variantsMap[productID])
		return nil
	})
	if txErr != nil {
		if delErr := s.storage.Delete(ctx, newKey); delErr != nil {
			s.log.Warn("failed to rollback uploaded image after db error",
				slog.String("image_key", newKey),
				slog.String("error", delErr.Error()),
			)
		}
		return nil, txErr
	}

	if oldKey != "" && oldKey != newKey {
		if delErr := s.storage.Delete(ctx, oldKey); delErr != nil {
			s.log.Warn("failed to remove replaced product image",
				slog.String("old_key", oldKey),
				slog.String("error", delErr.Error()),
			)
		}
	}

	return &updated, nil
}

// CreateVariant adiciona uma nova variante a um produto existente.
func (s *CatalogService) CreateVariant(ctx context.Context, productID uuid.UUID, input CreateVariantInput) (*domain.ProductVariant, error) {
	if err := validateVariantValues(input.Name, input.PriceCents, input.CurrentStock, input.MinStock); err != nil {
		return nil, err
	}

	if _, err := s.queries.GetProductByID(ctx, productID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domain.NewNotFoundError("Produto não encontrado")
		}
		return nil, domain.NewInternalError("Erro ao verificar produto", err)
	}

	varID, err := uuid.NewV7()
	if err != nil {
		return nil, domain.NewInternalError("Erro ao gerar identificador da variante", err)
	}

	row, err := s.queries.CreateProductVariant(ctx, sqlc.CreateProductVariantParams{
		ID:            varID,
		ProductID:     productID,
		Name:          strings.TrimSpace(input.Name),
		Sku:           toOptionalSKU(input.SKU),
		PriceCents:    input.PriceCents,
		CurrentStock:  int32(input.CurrentStock),
		ReservedStock: 0,
		MinStock:      int32(input.MinStock),
		SortOrder:     int32(input.SortOrder),
		Active:        input.Active,
	})
	if err != nil {
		return nil, mapProductDBError(err)
	}

	variant := mapSQLCVariant(row)
	return &variant, nil
}

// UpdateVariant altera atributos de uma variante garantindo que produto ativo mantenha ao menos uma variante ativa.
func (s *CatalogService) UpdateVariant(ctx context.Context, productID, variantID uuid.UUID, input UpdateVariantInput) (*domain.ProductVariant, error) {
	if input.Name == nil &&
		input.SKU == nil &&
		!input.ClearSKU &&
		input.PriceCents == nil &&
		input.MinStock == nil &&
		input.SortOrder == nil &&
		input.Active == nil {
		return nil, domain.NewValidationError("Informe ao menos um campo para atualização", nil)
	}

	details := make(map[string]any)
	if input.Name != nil && strings.TrimSpace(*input.Name) == "" {
		details["name"] = "não pode ser vazio"
	}
	if input.PriceCents != nil && *input.PriceCents < 0 {
		details["price_cents"] = "não pode ser negativo"
	}
	if input.MinStock != nil && *input.MinStock < 0 {
		details["min_stock"] = "não pode ser negativo"
	}
	if len(details) > 0 {
		return nil, domain.NewValidationError("Dados inválidos para atualização de variante", details)
	}

	var updated domain.ProductVariant
	err := repository.WithTx(ctx, s.pool, func(q *sqlc.Queries) error {
		prod, err := q.GetProductByID(ctx, productID)
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return domain.NewNotFoundError("Produto não encontrado")
			}
			return domain.NewInternalError("Erro ao buscar produto", err)
		}

		allVariants, err := q.ListVariantsByProductIDForUpdate(ctx, productID)
		if err != nil {
			return domain.NewInternalError("Erro ao listar variações do produto", err)
		}

		var target *sqlc.ProductVariant
		activeOthers := 0
		for i := range allVariants {
			v := &allVariants[i]
			if v.ID == variantID {
				target = v
			} else if v.Active {
				activeOthers++
			}
		}
		if target == nil {
			return domain.NewNotFoundError("Variante não encontrada")
		}

		if prod.Active && input.Active != nil && !*input.Active && activeOthers == 0 {
			return domain.NewConflictError("Produto ativo precisa manter ao menos uma variante ativa")
		}

		name := target.Name
		if input.Name != nil {
			name = strings.TrimSpace(*input.Name)
		}
		sku := target.Sku
		if input.ClearSKU {
			sku = pgtype.Text{Valid: false}
		} else if input.SKU != nil {
			sku = toOptionalSKU(input.SKU)
		}
		priceCents := target.PriceCents
		if input.PriceCents != nil {
			priceCents = *input.PriceCents
		}
		minStock := target.MinStock
		if input.MinStock != nil {
			minStock = int32(*input.MinStock)
		}
		sortOrder := target.SortOrder
		if input.SortOrder != nil {
			sortOrder = int32(*input.SortOrder)
		}
		active := target.Active
		if input.Active != nil {
			active = *input.Active
		}

		saved, err := q.UpdateProductVariant(ctx, sqlc.UpdateProductVariantParams{
			ID:         variantID,
			ProductID:  productID,
			Name:       name,
			Sku:        sku,
			PriceCents: priceCents,
			MinStock:   minStock,
			SortOrder:  sortOrder,
			Active:     active,
		})
		if err != nil {
			return mapProductDBError(err)
		}

		updated = mapSQLCVariant(saved)
		return nil
	})
	if err != nil {
		return nil, err
	}

	return &updated, nil
}

// DeleteVariant remove uma variante desde que não seja a última do produto.
func (s *CatalogService) DeleteVariant(ctx context.Context, productID, variantID uuid.UUID) error {
	return repository.WithTx(ctx, s.pool, func(q *sqlc.Queries) error {
		if _, err := q.GetProductByID(ctx, productID); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return domain.NewNotFoundError("Produto não encontrado")
			}
			return domain.NewInternalError("Erro ao buscar produto", err)
		}

		variants, err := q.ListVariantsByProductIDForUpdate(ctx, productID)
		if err != nil {
			return domain.NewInternalError("Erro ao verificar variações do produto", err)
		}

		found := false
		for _, v := range variants {
			if v.ID == variantID {
				found = true
				break
			}
		}
		if !found {
			return domain.NewNotFoundError("Variante não encontrada")
		}
		if len(variants) <= 1 {
			return domain.NewConflictError("Produto precisa manter ao menos uma variante")
		}

		rows, err := q.DeleteProductVariant(ctx, sqlc.DeleteProductVariantParams{
			ID:        variantID,
			ProductID: productID,
		})
		if err != nil {
			var pgErr *pgconn.PgError
			if errors.As(err, &pgErr) && pgErr.Code == pgForeignKeyViolationCode {
				return domain.NewConflictError("Variante possui registros vinculados e não pode ser removida")
			}
			return domain.NewInternalError("Erro ao remover variante", err)
		}
		if rows == 0 {
			return domain.NewNotFoundError("Variante não encontrada")
		}
		return nil
	})
}

// GetPublicMenu monta o cardápio público agrupado por categoria considerando disponibilidade em estoque.
func (s *CatalogService) GetPublicMenu(ctx context.Context, filter PublicMenuFilter) ([]domain.PublicMenuCategory, error) {
	settings, err := s.GetSettings(ctx)
	if err != nil {
		return nil, err
	}

	var catSlug pgtype.Text
	if filter.CategorySlug != nil && strings.TrimSpace(*filter.CategorySlug) != "" {
		catSlug = pgtype.Text{String: strings.ToLower(strings.TrimSpace(*filter.CategorySlug)), Valid: true}
	}

	var serviceType pgtype.Text
	if filter.ServiceType != nil {
		if !filter.ServiceType.IsValid() {
			return nil, domain.NewValidationError("Tipo de atendimento inválido no filtro", map[string]any{
				"service_type": "deve ser pronta_entrega ou sob_encomenda",
			})
		}
		serviceType = pgtype.Text{String: string(*filter.ServiceType), Valid: true}
	}

	var search pgtype.Text
	if filter.Search != nil && strings.TrimSpace(*filter.Search) != "" {
		search = pgtype.Text{String: escapeLikePattern(strings.TrimSpace(*filter.Search)), Valid: true}
	}

	var featuredOnly pgtype.Bool
	if filter.FeaturedOnly != nil {
		featuredOnly = pgtype.Bool{Bool: *filter.FeaturedOnly, Valid: true}
	}

	rows, err := s.queries.ListPublicMenuProducts(ctx, sqlc.ListPublicMenuProductsParams{
		CategorySlug: catSlug,
		ServiceType:  serviceType,
		FeaturedOnly: featuredOnly,
		Search:       search,
	})
	if err != nil {
		return nil, domain.NewInternalError("Erro ao listar cardápio público", err)
	}
	if len(rows) == 0 {
		return []domain.PublicMenuCategory{}, nil
	}

	productIDs := make([]uuid.UUID, 0, len(rows))
	for _, r := range rows {
		productIDs = append(productIDs, r.ID)
	}

	variantsMap, err := s.loadVariantsByProductIDs(ctx, s.queries, productIDs)
	if err != nil {
		return nil, err
	}

	activeCats, err := s.queries.ListActiveCategories(ctx)
	if err != nil {
		return nil, domain.NewInternalError("Erro ao carregar categorias ativas", err)
	}
	catOrderMap := make(map[uuid.UUID]int, len(activeCats))
	for _, c := range activeCats {
		catOrderMap[c.ID] = int(c.SortOrder)
	}

	grouped := make([]domain.PublicMenuCategory, 0)
	catIndex := make(map[uuid.UUID]int)

	for _, r := range rows {
		pubProduct, hasVisible := buildPublicProduct(
			r.ID, r.CategoryID, r.CategoryName, r.CategorySlug,
			r.Name, r.Slug, r.Description, pgTextToPtr(r.ImageUrl),
			domain.ServiceType(r.ServiceType), int(r.PrepTimeMinutes), r.Featured,
			variantsMap[r.ID], settings.AutoHideOutOfStock,
		)
		if !hasVisible {
			continue
		}

		idx, exists := catIndex[r.CategoryID]
		if !exists {
			idx = len(grouped)
			catIndex[r.CategoryID] = idx
			grouped = append(grouped, domain.PublicMenuCategory{
				ID:        r.CategoryID,
				Name:      r.CategoryName,
				Slug:      r.CategorySlug,
				SortOrder: catOrderMap[r.CategoryID],
				Products:  make([]domain.PublicProduct, 0),
			})
		}
		grouped[idx].Products = append(grouped[idx].Products, pubProduct)
	}

	return grouped, nil
}

// GetPublicProduct busca os detalhes públicos de um produto ativo por slug ou UUID.
func (s *CatalogService) GetPublicProduct(ctx context.Context, slugOrID string) (*domain.PublicProduct, error) {
	trimmed := strings.TrimSpace(slugOrID)
	if trimmed == "" {
		return nil, domain.NewNotFoundError("Produto não encontrado")
	}

	var (
		id              uuid.UUID
		categoryID      uuid.UUID
		categoryName    string
		categorySlug    string
		name            string
		slug            string
		description     string
		imageURL        pgtype.Text
		serviceType     string
		prepTimeMinutes int32
		featured        bool
	)

	if parsedID, parseErr := uuid.Parse(trimmed); parseErr == nil {
		row, err := s.queries.GetPublicProductByID(ctx, parsedID)
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return nil, domain.NewNotFoundError("Produto não encontrado")
			}
			return nil, domain.NewInternalError("Erro ao buscar produto público", err)
		}
		id, categoryID, categoryName, categorySlug = row.ID, row.CategoryID, row.CategoryName, row.CategorySlug
		name, slug, description, imageURL = row.Name, row.Slug, row.Description, row.ImageUrl
		serviceType, prepTimeMinutes, featured = row.ServiceType, row.PrepTimeMinutes, row.Featured
	} else {
		row, err := s.queries.GetPublicProductBySlug(ctx, strings.ToLower(trimmed))
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return nil, domain.NewNotFoundError("Produto não encontrado")
			}
			return nil, domain.NewInternalError("Erro ao buscar produto público", err)
		}
		id, categoryID, categoryName, categorySlug = row.ID, row.CategoryID, row.CategoryName, row.CategorySlug
		name, slug, description, imageURL = row.Name, row.Slug, row.Description, row.ImageUrl
		serviceType, prepTimeMinutes, featured = row.ServiceType, row.PrepTimeMinutes, row.Featured
	}

	variantsMap, err := s.loadVariantsByProductIDs(ctx, s.queries, []uuid.UUID{id})
	if err != nil {
		return nil, err
	}

	// No detalhe direto do produto exibimos todas as variações ativas sinalizando in_stock=false quando esgotado.
	pubProduct, _ := buildPublicProduct(
		id, categoryID, categoryName, categorySlug,
		name, slug, description, pgTextToPtr(imageURL),
		domain.ServiceType(serviceType), int(prepTimeMinutes), featured,
		variantsMap[id], false,
	)
	if len(pubProduct.Variants) == 0 {
		return nil, domain.NewNotFoundError("Produto não encontrado")
	}
	return &pubProduct, nil
}

func (s *CatalogService) loadVariantsByProductIDs(ctx context.Context, q *sqlc.Queries, productIDs []uuid.UUID) (map[uuid.UUID][]domain.ProductVariant, error) {
	result := make(map[uuid.UUID][]domain.ProductVariant, len(productIDs))
	if len(productIDs) == 0 {
		return result, nil
	}

	rows, err := q.ListVariantsByProductIDs(ctx, productIDs)
	if err != nil {
		return nil, domain.NewInternalError("Erro ao carregar variações dos produtos", err)
	}

	for _, r := range rows {
		result[r.ProductID] = append(result[r.ProductID], mapSQLCVariant(r))
	}
	return result, nil
}

func buildPublicProduct(
	id, categoryID uuid.UUID,
	categoryName, categorySlug, name, slug, description string,
	imageURL *string,
	serviceType domain.ServiceType,
	prepTimeMinutes int,
	featured bool,
	variants []domain.ProductVariant,
	hideOutOfStock bool,
) (domain.PublicProduct, bool) {
	pubVariants := make([]domain.PublicVariant, 0, len(variants))
	productInStock := false

	for _, v := range variants {
		if !v.Active {
			continue
		}
		inStock := v.IsInStockFor(serviceType)
		if hideOutOfStock && serviceType == domain.ServiceTypeReadyStock && !inStock {
			continue
		}
		if inStock {
			productInStock = true
		}

		avail := v.AvailableStock()
		if serviceType == domain.ServiceTypeMadeToOrder {
			avail = 0
		}

		pubVariants = append(pubVariants, domain.PublicVariant{
			ID:             v.ID,
			Name:           v.Name,
			SKU:            v.SKU,
			PriceCents:     v.PriceCents,
			InStock:        inStock,
			AvailableStock: avail,
			SortOrder:      v.SortOrder,
		})
	}

	return domain.PublicProduct{
		ID:              id,
		CategoryID:      categoryID,
		CategoryName:    categoryName,
		CategorySlug:    categorySlug,
		Name:            name,
		Slug:            slug,
		Description:     description,
		ImageURL:        imageURL,
		ServiceType:     serviceType,
		PrepTimeMinutes: prepTimeMinutes,
		Featured:        featured,
		InStock:         productInStock,
		Variants:        pubVariants,
	}, len(pubVariants) > 0
}

func normalizePagination(page, pageSize int) (int, int, error) {
	if page <= 0 {
		page = 1
	}
	if pageSize <= 0 {
		pageSize = 20
	}
	if pageSize > 100 {
		return 0, 0, domain.NewValidationError("page_size não pode exceder 100", map[string]any{
			"page_size": "máximo permitido é 100",
		})
	}
	return page, pageSize, nil
}

func resolveAndValidateSlug(name string, rawSlug *string) (string, error) {
	var slug string
	if rawSlug != nil && strings.TrimSpace(*rawSlug) != "" {
		slug = strings.ToLower(strings.TrimSpace(*rawSlug))
	} else {
		slug = domain.Slugify(name)
	}
	if err := domain.ValidateSlug(slug); err != nil {
		return "", err
	}
	return slug, nil
}

func validateVariantValues(name string, priceCents int64, currentStock, minStock int) error {
	details := make(map[string]any)
	if strings.TrimSpace(name) == "" {
		details["name"] = "nome da variante é obrigatório"
	}
	if priceCents < 0 {
		details["price_cents"] = "preço em centavos não pode ser negativo"
	}
	if currentStock < 0 {
		details["current_stock"] = "estoque inicial não pode ser negativo"
	}
	if minStock < 0 {
		details["min_stock"] = "estoque mínimo não pode ser negativo"
	}
	if len(details) > 0 {
		return domain.NewValidationError("Dados inválidos para variante de produto", details)
	}
	return nil
}

func validateOpeningHours(hours domain.OpeningHours, details map[string]any) {
	days := []struct {
		name string
		day  domain.DaySchedule
	}{
		{"monday", hours.Monday},
		{"tuesday", hours.Tuesday},
		{"wednesday", hours.Wednesday},
		{"thursday", hours.Thursday},
		{"friday", hours.Friday},
		{"saturday", hours.Saturday},
		{"sunday", hours.Sunday},
	}

	for _, item := range days {
		if !domain.IsValidHHMM(item.day.Open) || !domain.IsValidHHMM(item.day.Close) {
			details["opening_hours."+item.name] = "horário deve usar formato HH:MM"
			continue
		}
		if !item.day.Closed && strings.TrimSpace(item.day.Close) <= strings.TrimSpace(item.day.Open) {
			details["opening_hours."+item.name] = "horário de fechamento deve ser posterior ao de abertura"
		}
	}
}

func deduplicatePaymentMethods(methods []domain.PaymentMethod) []domain.PaymentMethod {
	seen := make(map[domain.PaymentMethod]struct{}, len(methods))
	result := make([]domain.PaymentMethod, 0, len(methods))
	for _, m := range methods {
		if _, ok := seen[m]; ok {
			continue
		}
		seen[m] = struct{}{}
		result = append(result, m)
	}
	return result
}

func isValidOptionalEmail(raw string) bool {
	trimmed := strings.TrimSpace(raw)
	addr, err := mail.ParseAddress(trimmed)
	return err == nil && addr.Address == trimmed
}

func escapeLikePattern(raw string) string {
	r := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)
	return r.Replace(raw)
}

func toPGTypeBool(v *bool) pgtype.Bool {
	if v == nil {
		return pgtype.Bool{}
	}
	return pgtype.Bool{Bool: *v, Valid: true}
}

func toOptionalSKU(sku *string) pgtype.Text {
	if sku == nil {
		return pgtype.Text{}
	}
	trimmed := strings.TrimSpace(*sku)
	if trimmed == "" {
		return pgtype.Text{}
	}
	return pgtype.Text{String: trimmed, Valid: true}
}

func pgTextToPtr(v pgtype.Text) *string {
	if !v.Valid {
		return nil
	}
	s := v.String
	return &s
}

func mapProductDBError(err error) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		switch pgErr.Code {
		case pgUniqueViolationCode:
			if strings.Contains(pgErr.ConstraintName, "sku") {
				return domain.NewConflictError("SKU de variante já cadastrado")
			}
			return domain.NewConflictError("Slug de produto já cadastrado")
		case pgForeignKeyViolationCode:
			return domain.NewNotFoundError("Categoria não encontrada")
		}
	}
	return domain.NewInternalError("Erro ao persistir dados de catálogo", err)
}

func mapStoreSettings(row sqlc.StoreSetting) (domain.StoreSettings, error) {
	var hours domain.OpeningHours
	if len(row.OpeningHours) > 0 {
		if err := json.Unmarshal(row.OpeningHours, &hours); err != nil {
			return domain.StoreSettings{}, err
		}
	}

	methods := make([]domain.PaymentMethod, 0, len(row.AcceptedPaymentMethods))
	for _, m := range row.AcceptedPaymentMethods {
		methods = append(methods, domain.PaymentMethod(m))
	}

	return domain.StoreSettings{
		ID:                       row.ID,
		StoreName:                row.StoreName,
		Description:              row.Description,
		Phone:                    row.Phone,
		WhatsApp:                 row.Whatsapp,
		Email:                    row.Email,
		AddressLine:              row.AddressLine,
		OpeningHours:             hours,
		AcceptedPaymentMethods:   methods,
		ManualStatus:             domain.StoreManualStatus(row.ManualStatus),
		AllowScheduling:          row.AllowScheduling,
		PixReservationTTLMinutes: int(row.PixReservationTtlMinutes),
		AutoHideOutOfStock:       row.AutoHideOutOfStock,
		CreatedAt:                row.CreatedAt.UTC(),
		UpdatedAt:                row.UpdatedAt.UTC(),
	}, nil
}

func mapSQLCCategory(row sqlc.Category) domain.Category {
	return domain.Category{
		ID:        row.ID,
		Name:      row.Name,
		Slug:      row.Slug,
		SortOrder: int(row.SortOrder),
		Active:    row.Active,
		CreatedAt: row.CreatedAt.UTC(),
		UpdatedAt: row.UpdatedAt.UTC(),
	}
}

func mapSQLCVariant(row sqlc.ProductVariant) domain.ProductVariant {
	return domain.ProductVariant{
		ID:            row.ID,
		ProductID:     row.ProductID,
		Name:          row.Name,
		SKU:           pgTextToPtr(row.Sku),
		PriceCents:    row.PriceCents,
		CurrentStock:  int(row.CurrentStock),
		ReservedStock: int(row.ReservedStock),
		MinStock:      int(row.MinStock),
		SortOrder:     int(row.SortOrder),
		Active:        row.Active,
		CreatedAt:     row.CreatedAt.UTC(),
		UpdatedAt:     row.UpdatedAt.UTC(),
	}
}

func mapSQLCProduct(row sqlc.Product, variants []domain.ProductVariant) domain.Product {
	if variants == nil {
		variants = []domain.ProductVariant{}
	}
	return domain.Product{
		ID:              row.ID,
		CategoryID:      row.CategoryID,
		Name:            row.Name,
		Slug:            row.Slug,
		Description:     row.Description,
		ImageURL:        pgTextToPtr(row.ImageUrl),
		ImageKey:        pgTextToPtr(row.ImageKey),
		ServiceType:     domain.ServiceType(row.ServiceType),
		PrepTimeMinutes: int(row.PrepTimeMinutes),
		Active:          row.Active,
		AvailableOnline: row.AvailableOnline,
		Featured:        row.Featured,
		SortOrder:       int(row.SortOrder),
		Variants:        variants,
		CreatedAt:       row.CreatedAt.UTC(),
		UpdatedAt:       row.UpdatedAt.UTC(),
	}
}
