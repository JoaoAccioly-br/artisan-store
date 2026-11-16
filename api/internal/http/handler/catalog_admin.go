package handler

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/google/uuid"
	openapi_types "github.com/oapi-codegen/runtime/types"

	"github.com/joao-accioly/artisan-store/api/internal/domain"
	"github.com/joao-accioly/artisan-store/api/internal/http/oapi"
	"github.com/joao-accioly/artisan-store/api/internal/service"
)

// AdminGetSettings retorna as configurações atuais da loja.
func (s *Server) AdminGetSettings(ctx context.Context, _ oapi.AdminGetSettingsRequestObject) (oapi.AdminGetSettingsResponseObject, error) {
	if s.catalog == nil {
		return nil, domain.NewInternalError("Serviço de catálogo não configurado", nil)
	}

	settings, err := s.catalog.GetSettings(ctx)
	if err != nil {
		return nil, err
	}

	return oapi.AdminGetSettings200JSONResponse(toOAPISettings(*settings)), nil
}

// AdminUpdateSettings atualiza configurações operacionais e públicas da loja.
func (s *Server) AdminUpdateSettings(ctx context.Context, request oapi.AdminUpdateSettingsRequestObject) (oapi.AdminUpdateSettingsResponseObject, error) {
	if s.catalog == nil {
		return nil, domain.NewInternalError("Serviço de catálogo não configurado", nil)
	}
	if request.Body == nil {
		return nil, domain.NewValidationError("Corpo da requisição é obrigatório", nil)
	}

	var hours *domain.OpeningHours
	if request.Body.OpeningHours != nil {
		converted := fromOAPIOpeningHours(*request.Body.OpeningHours)
		hours = &converted
	}

	var methods *[]domain.PaymentMethod
	if request.Body.AcceptedPaymentMethods != nil {
		list := make([]domain.PaymentMethod, 0, len(*request.Body.AcceptedPaymentMethods))
		for _, m := range *request.Body.AcceptedPaymentMethods {
			list = append(list, domain.PaymentMethod(m))
		}
		methods = &list
	}

	var manualStatus *domain.StoreManualStatus
	if request.Body.ManualStatus != nil {
		st := domain.StoreManualStatus(*request.Body.ManualStatus)
		manualStatus = &st
	}

	settings, err := s.catalog.UpdateSettings(ctx, service.UpdateStoreSettingsInput{
		StoreName:                request.Body.StoreName,
		Description:              request.Body.Description,
		Phone:                    request.Body.Phone,
		WhatsApp:                 request.Body.Whatsapp,
		Email:                    request.Body.Email,
		AddressLine:              request.Body.AddressLine,
		OpeningHours:             hours,
		AcceptedPaymentMethods:   methods,
		ManualStatus:             manualStatus,
		AllowScheduling:          request.Body.AllowScheduling,
		PixReservationTTLMinutes: request.Body.PixReservationTtlMinutes,
		AutoHideOutOfStock:       request.Body.AutoHideOutOfStock,
	})
	if err != nil {
		return nil, err
	}

	return oapi.AdminUpdateSettings200JSONResponse(toOAPISettings(*settings)), nil
}

// AdminListCategories lista categorias paginadas com filtro opcional de status ativo.
func (s *Server) AdminListCategories(ctx context.Context, request oapi.AdminListCategoriesRequestObject) (oapi.AdminListCategoriesResponseObject, error) {
	if s.catalog == nil {
		return nil, domain.NewInternalError("Serviço de catálogo não configurado", nil)
	}

	page := 1
	if request.Params.Page != nil {
		page = *request.Params.Page
	}
	pageSize := 20
	if request.Params.PageSize != nil {
		pageSize = *request.Params.PageSize
	}

	categories, meta, err := s.catalog.ListCategories(ctx, request.Params.Active, page, pageSize)
	if err != nil {
		return nil, err
	}

	items := make([]oapi.CategoryResponse, 0, len(categories))
	for _, c := range categories {
		items = append(items, toOAPICategory(c))
	}

	return oapi.AdminListCategories200JSONResponse{
		Data: items,
		Meta: toOAPIPaginationMeta(meta),
	}, nil
}

// AdminCreateCategory cadastra uma nova categoria.
func (s *Server) AdminCreateCategory(ctx context.Context, request oapi.AdminCreateCategoryRequestObject) (oapi.AdminCreateCategoryResponseObject, error) {
	if s.catalog == nil {
		return nil, domain.NewInternalError("Serviço de catálogo não configurado", nil)
	}
	if request.Body == nil {
		return nil, domain.NewValidationError("Corpo da requisição é obrigatório", nil)
	}

	sortOrder := 0
	if request.Body.SortOrder != nil {
		sortOrder = *request.Body.SortOrder
	}
	active := true
	if request.Body.Active != nil {
		active = *request.Body.Active
	}

	cat, err := s.catalog.CreateCategory(ctx, service.CreateCategoryInput{
		Name:      request.Body.Name,
		Slug:      request.Body.Slug,
		SortOrder: sortOrder,
		Active:    active,
	})
	if err != nil {
		return nil, err
	}

	return oapi.AdminCreateCategory201JSONResponse(toOAPICategory(*cat)), nil
}

// AdminGetCategory busca uma categoria por UUIDv7.
func (s *Server) AdminGetCategory(ctx context.Context, request oapi.AdminGetCategoryRequestObject) (oapi.AdminGetCategoryResponseObject, error) {
	if s.catalog == nil {
		return nil, domain.NewInternalError("Serviço de catálogo não configurado", nil)
	}

	cat, err := s.catalog.GetCategory(ctx, uuid.UUID(request.Id))
	if err != nil {
		return nil, err
	}

	return oapi.AdminGetCategory200JSONResponse(toOAPICategory(*cat)), nil
}

// AdminUpdateCategory altera dados de uma categoria.
func (s *Server) AdminUpdateCategory(ctx context.Context, request oapi.AdminUpdateCategoryRequestObject) (oapi.AdminUpdateCategoryResponseObject, error) {
	if s.catalog == nil {
		return nil, domain.NewInternalError("Serviço de catálogo não configurado", nil)
	}
	if request.Body == nil {
		return nil, domain.NewValidationError("Corpo da requisição é obrigatório", nil)
	}

	cat, err := s.catalog.UpdateCategory(ctx, uuid.UUID(request.Id), service.UpdateCategoryInput{
		Name:      request.Body.Name,
		Slug:      request.Body.Slug,
		SortOrder: request.Body.SortOrder,
		Active:    request.Body.Active,
	})
	if err != nil {
		return nil, err
	}

	return oapi.AdminUpdateCategory200JSONResponse(toOAPICategory(*cat)), nil
}

// AdminDeleteCategory remove uma categoria sem produtos vinculados.
func (s *Server) AdminDeleteCategory(ctx context.Context, request oapi.AdminDeleteCategoryRequestObject) (oapi.AdminDeleteCategoryResponseObject, error) {
	if s.catalog == nil {
		return nil, domain.NewInternalError("Serviço de catálogo não configurado", nil)
	}

	if err := s.catalog.DeleteCategory(ctx, uuid.UUID(request.Id)); err != nil {
		return nil, err
	}

	return oapi.AdminDeleteCategory204Response{}, nil
}

// AdminListProducts lista produtos paginados com variações e filtros administrativos.
func (s *Server) AdminListProducts(ctx context.Context, request oapi.AdminListProductsRequestObject) (oapi.AdminListProductsResponseObject, error) {
	if s.catalog == nil {
		return nil, domain.NewInternalError("Serviço de catálogo não configurado", nil)
	}

	page := 1
	if request.Params.Page != nil {
		page = *request.Params.Page
	}
	pageSize := 20
	if request.Params.PageSize != nil {
		pageSize = *request.Params.PageSize
	}

	var catID *uuid.UUID
	if request.Params.CategoryId != nil {
		parsed := uuid.UUID(*request.Params.CategoryId)
		catID = &parsed
	}

	var serviceType *domain.ServiceType
	if request.Params.ServiceType != nil {
		parsed, err := domain.ParseServiceType(string(*request.Params.ServiceType))
		if err != nil {
			return nil, err
		}
		serviceType = &parsed
	}

	products, meta, err := s.catalog.ListProducts(ctx, service.ProductListFilter{
		CategoryID:      catID,
		ServiceType:     serviceType,
		Active:          request.Params.Active,
		AvailableOnline: request.Params.AvailableOnline,
		Search:          request.Params.Search,
	}, page, pageSize)
	if err != nil {
		return nil, err
	}

	items := make([]oapi.ProductResponse, 0, len(products))
	for _, p := range products {
		items = append(items, toOAPIProduct(p))
	}

	return oapi.AdminListProducts200JSONResponse{
		Data: items,
		Meta: toOAPIPaginationMeta(meta),
	}, nil
}

// AdminCreateProduct cria um produto com ao menos uma variante inicial.
func (s *Server) AdminCreateProduct(ctx context.Context, request oapi.AdminCreateProductRequestObject) (oapi.AdminCreateProductResponseObject, error) {
	if s.catalog == nil {
		return nil, domain.NewInternalError("Serviço de catálogo não configurado", nil)
	}
	if request.Body == nil {
		return nil, domain.NewValidationError("Corpo da requisição é obrigatório", nil)
	}

	serviceType, err := domain.ParseServiceType(string(request.Body.ServiceType))
	if err != nil {
		return nil, err
	}

	variants := make([]service.CreateVariantInput, 0, len(request.Body.Variants))
	for _, v := range request.Body.Variants {
		variants = append(variants, fromOAPICreateVariant(v))
	}

	description := ""
	if request.Body.Description != nil {
		description = *request.Body.Description
	}
	prepTime := 0
	if request.Body.PrepTimeMinutes != nil {
		prepTime = *request.Body.PrepTimeMinutes
	}
	active := true
	if request.Body.Active != nil {
		active = *request.Body.Active
	}
	availableOnline := true
	if request.Body.AvailableOnline != nil {
		availableOnline = *request.Body.AvailableOnline
	}
	featured := false
	if request.Body.Featured != nil {
		featured = *request.Body.Featured
	}
	sortOrder := 0
	if request.Body.SortOrder != nil {
		sortOrder = *request.Body.SortOrder
	}

	product, err := s.catalog.CreateProduct(ctx, service.CreateProductInput{
		CategoryID:      uuid.UUID(request.Body.CategoryId),
		Name:            request.Body.Name,
		Slug:            request.Body.Slug,
		Description:     description,
		ServiceType:     serviceType,
		PrepTimeMinutes: prepTime,
		Active:          active,
		AvailableOnline: availableOnline,
		Featured:        featured,
		SortOrder:       sortOrder,
		Variants:        variants,
	})
	if err != nil {
		return nil, err
	}

	return oapi.AdminCreateProduct201JSONResponse(toOAPIProduct(*product)), nil
}

// AdminGetProduct busca um produto e suas variações por ID.
func (s *Server) AdminGetProduct(ctx context.Context, request oapi.AdminGetProductRequestObject) (oapi.AdminGetProductResponseObject, error) {
	if s.catalog == nil {
		return nil, domain.NewInternalError("Serviço de catálogo não configurado", nil)
	}

	product, err := s.catalog.GetProduct(ctx, uuid.UUID(request.Id))
	if err != nil {
		return nil, err
	}

	return oapi.AdminGetProduct200JSONResponse(toOAPIProduct(*product)), nil
}

// AdminUpdateProduct atualiza dados cadastrais de um produto.
func (s *Server) AdminUpdateProduct(ctx context.Context, request oapi.AdminUpdateProductRequestObject) (oapi.AdminUpdateProductResponseObject, error) {
	if s.catalog == nil {
		return nil, domain.NewInternalError("Serviço de catálogo não configurado", nil)
	}
	if request.Body == nil {
		return nil, domain.NewValidationError("Corpo da requisição é obrigatório", nil)
	}

	var catID *uuid.UUID
	if request.Body.CategoryId != nil {
		parsed := uuid.UUID(*request.Body.CategoryId)
		catID = &parsed
	}

	var serviceType *domain.ServiceType
	if request.Body.ServiceType != nil {
		parsed, err := domain.ParseServiceType(string(*request.Body.ServiceType))
		if err != nil {
			return nil, err
		}
		serviceType = &parsed
	}

	product, err := s.catalog.UpdateProduct(ctx, uuid.UUID(request.Id), service.UpdateProductInput{
		CategoryID:      catID,
		Name:            request.Body.Name,
		Slug:            request.Body.Slug,
		Description:     request.Body.Description,
		ServiceType:     serviceType,
		PrepTimeMinutes: request.Body.PrepTimeMinutes,
		Active:          request.Body.Active,
		AvailableOnline: request.Body.AvailableOnline,
		Featured:        request.Body.Featured,
		SortOrder:       request.Body.SortOrder,
	})
	if err != nil {
		return nil, err
	}

	return oapi.AdminUpdateProduct200JSONResponse(toOAPIProduct(*product)), nil
}

// AdminDeleteProduct remove um produto e suas variações.
func (s *Server) AdminDeleteProduct(ctx context.Context, request oapi.AdminDeleteProductRequestObject) (oapi.AdminDeleteProductResponseObject, error) {
	if s.catalog == nil {
		return nil, domain.NewInternalError("Serviço de catálogo não configurado", nil)
	}

	if err := s.catalog.DeleteProduct(ctx, uuid.UUID(request.Id)); err != nil {
		return nil, err
	}

	return oapi.AdminDeleteProduct204Response{}, nil
}

// AdminUploadProductImage lê a parte "file" do multipart respeitando o limite de bytes e atualiza a foto do produto.
func (s *Server) AdminUploadProductImage(ctx context.Context, request oapi.AdminUploadProductImageRequestObject) (oapi.AdminUploadProductImageResponseObject, error) {
	if s.catalog == nil {
		return nil, domain.NewInternalError("Serviço de catálogo não configurado", nil)
	}
	if request.Body == nil {
		return nil, domain.NewValidationError("Corpo multipart/form-data é obrigatório", nil)
	}

	var fileBytes []byte
	var found bool

	for {
		part, err := request.Body.NextPart()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, domain.NewValidationError("Falha ao ler requisição multipart", map[string]any{
				"file": "envie um formulário multipart/form-data válido",
			})
		}

		if part.FormName() != "file" {
			if closeErr := part.Close(); closeErr != nil {
				return nil, domain.NewValidationError("Falha ao processar parte do formulário multipart", nil)
			}
			continue
		}

		limitedReader := io.LimitReader(part, s.maxUploadBytes+1)
		data, readErr := io.ReadAll(limitedReader)
		closeErr := part.Close()
		if readErr != nil || closeErr != nil {
			return nil, domain.NewValidationError("Falha ao ler arquivo enviado", nil)
		}
		if int64(len(data)) > s.maxUploadBytes {
			return nil, domain.NewValidationError(
				fmt.Sprintf("Arquivo excede o limite máximo permitido de %d bytes", s.maxUploadBytes),
				map[string]any{"file": "tamanho máximo excedido"},
			)
		}

		fileBytes = data
		found = true
		break
	}

	if !found {
		return nil, domain.NewValidationError("Campo de arquivo 'file' é obrigatório", map[string]any{
			"file": "campo multipart 'file' não informado",
		})
	}

	product, err := s.catalog.UploadProductImage(ctx, uuid.UUID(request.Id), fileBytes)
	if err != nil {
		return nil, err
	}

	return oapi.AdminUploadProductImage200JSONResponse(toOAPIProduct(*product)), nil
}

// AdminCreateProductVariant adiciona uma variante a um produto existente.
func (s *Server) AdminCreateProductVariant(ctx context.Context, request oapi.AdminCreateProductVariantRequestObject) (oapi.AdminCreateProductVariantResponseObject, error) {
	if s.catalog == nil {
		return nil, domain.NewInternalError("Serviço de catálogo não configurado", nil)
	}
	if request.Body == nil {
		return nil, domain.NewValidationError("Corpo da requisição é obrigatório", nil)
	}

	variant, err := s.catalog.CreateVariant(ctx, uuid.UUID(request.Id), fromOAPICreateVariant(*request.Body))
	if err != nil {
		return nil, err
	}

	return oapi.AdminCreateProductVariant201JSONResponse(toOAPIVariant(*variant)), nil
}

// AdminUpdateProductVariant atualiza dados de uma variante de produto.
func (s *Server) AdminUpdateProductVariant(ctx context.Context, request oapi.AdminUpdateProductVariantRequestObject) (oapi.AdminUpdateProductVariantResponseObject, error) {
	if s.catalog == nil {
		return nil, domain.NewInternalError("Serviço de catálogo não configurado", nil)
	}
	if request.Body == nil {
		return nil, domain.NewValidationError("Corpo da requisição é obrigatório", nil)
	}

	clearSKU := request.Body.Sku != nil && strings.TrimSpace(*request.Body.Sku) == ""
	variant, err := s.catalog.UpdateVariant(ctx, uuid.UUID(request.Id), uuid.UUID(request.VariantId), service.UpdateVariantInput{
		Name:       request.Body.Name,
		SKU:        request.Body.Sku,
		ClearSKU:   clearSKU,
		PriceCents: request.Body.PriceCents,
		MinStock:   request.Body.MinStock,
		SortOrder:  request.Body.SortOrder,
		Active:     request.Body.Active,
	})
	if err != nil {
		return nil, err
	}

	return oapi.AdminUpdateProductVariant200JSONResponse(toOAPIVariant(*variant)), nil
}

// AdminDeleteProductVariant remove uma variante desde que o produto mantenha ao menos uma.
func (s *Server) AdminDeleteProductVariant(ctx context.Context, request oapi.AdminDeleteProductVariantRequestObject) (oapi.AdminDeleteProductVariantResponseObject, error) {
	if s.catalog == nil {
		return nil, domain.NewInternalError("Serviço de catálogo não configurado", nil)
	}

	if err := s.catalog.DeleteVariant(ctx, uuid.UUID(request.Id), uuid.UUID(request.VariantId)); err != nil {
		return nil, err
	}

	return oapi.AdminDeleteProductVariant204Response{}, nil
}

func fromOAPICreateVariant(v oapi.CreateVariantRequest) service.CreateVariantInput {
	currentStock := 0
	if v.CurrentStock != nil {
		currentStock = *v.CurrentStock
	}
	minStock := 0
	if v.MinStock != nil {
		minStock = *v.MinStock
	}
	sortOrder := 0
	if v.SortOrder != nil {
		sortOrder = *v.SortOrder
	}
	active := true
	if v.Active != nil {
		active = *v.Active
	}

	return service.CreateVariantInput{
		Name:         v.Name,
		SKU:          v.Sku,
		PriceCents:   v.PriceCents,
		CurrentStock: currentStock,
		MinStock:     minStock,
		SortOrder:    sortOrder,
		Active:       active,
	}
}

func fromOAPIOpeningHours(h oapi.OpeningHours) domain.OpeningHours {
	return domain.OpeningHours{
		Monday:    domain.DaySchedule{Open: h.Monday.Open, Close: h.Monday.Close, Closed: h.Monday.Closed},
		Tuesday:   domain.DaySchedule{Open: h.Tuesday.Open, Close: h.Tuesday.Close, Closed: h.Tuesday.Closed},
		Wednesday: domain.DaySchedule{Open: h.Wednesday.Open, Close: h.Wednesday.Close, Closed: h.Wednesday.Closed},
		Thursday:  domain.DaySchedule{Open: h.Thursday.Open, Close: h.Thursday.Close, Closed: h.Thursday.Closed},
		Friday:    domain.DaySchedule{Open: h.Friday.Open, Close: h.Friday.Close, Closed: h.Friday.Closed},
		Saturday:  domain.DaySchedule{Open: h.Saturday.Open, Close: h.Saturday.Close, Closed: h.Saturday.Closed},
		Sunday:    domain.DaySchedule{Open: h.Sunday.Open, Close: h.Sunday.Close, Closed: h.Sunday.Closed},
	}
}

func toOAPIOpeningHours(h domain.OpeningHours) oapi.OpeningHours {
	return oapi.OpeningHours{
		Monday:    oapi.DaySchedule{Open: h.Monday.Open, Close: h.Monday.Close, Closed: h.Monday.Closed},
		Tuesday:   oapi.DaySchedule{Open: h.Tuesday.Open, Close: h.Tuesday.Close, Closed: h.Tuesday.Closed},
		Wednesday: oapi.DaySchedule{Open: h.Wednesday.Open, Close: h.Wednesday.Close, Closed: h.Wednesday.Closed},
		Thursday:  oapi.DaySchedule{Open: h.Thursday.Open, Close: h.Thursday.Close, Closed: h.Thursday.Closed},
		Friday:    oapi.DaySchedule{Open: h.Friday.Open, Close: h.Friday.Close, Closed: h.Friday.Closed},
		Saturday:  oapi.DaySchedule{Open: h.Saturday.Open, Close: h.Saturday.Close, Closed: h.Saturday.Closed},
		Sunday:    oapi.DaySchedule{Open: h.Sunday.Open, Close: h.Sunday.Close, Closed: h.Sunday.Closed},
	}
}

func toOAPIPaymentMethods(methods []domain.PaymentMethod) []oapi.PaymentMethod {
	result := make([]oapi.PaymentMethod, 0, len(methods))
	for _, m := range methods {
		result = append(result, oapi.PaymentMethod(m))
	}
	return result
}

func toOAPISettings(s domain.StoreSettings) oapi.StoreSettingsResponse {
	return oapi.StoreSettingsResponse{
		Id:                       openapi_types.UUID(s.ID),
		StoreName:                s.StoreName,
		Description:              s.Description,
		Phone:                    s.Phone,
		Whatsapp:                 s.WhatsApp,
		Email:                    s.Email,
		AddressLine:              s.AddressLine,
		OpeningHours:             toOAPIOpeningHours(s.OpeningHours),
		AcceptedPaymentMethods:   toOAPIPaymentMethods(s.AcceptedPaymentMethods),
		ManualStatus:             oapi.StoreManualStatus(s.ManualStatus),
		AllowScheduling:          s.AllowScheduling,
		PixReservationTtlMinutes: s.PixReservationTTLMinutes,
		AutoHideOutOfStock:       s.AutoHideOutOfStock,
		CreatedAt:                s.CreatedAt,
		UpdatedAt:                s.UpdatedAt,
	}
}

func toOAPICategory(c domain.Category) oapi.CategoryResponse {
	return oapi.CategoryResponse{
		Id:        openapi_types.UUID(c.ID),
		Name:      c.Name,
		Slug:      c.Slug,
		SortOrder: c.SortOrder,
		Active:    c.Active,
		CreatedAt: c.CreatedAt,
		UpdatedAt: c.UpdatedAt,
	}
}

func toOAPIVariant(v domain.ProductVariant) oapi.ProductVariantResponse {
	return oapi.ProductVariantResponse{
		Id:             openapi_types.UUID(v.ID),
		ProductId:      openapi_types.UUID(v.ProductID),
		Name:           v.Name,
		Sku:            v.SKU,
		PriceCents:     v.PriceCents,
		CurrentStock:   v.CurrentStock,
		ReservedStock:  v.ReservedStock,
		AvailableStock: v.AvailableStock(),
		MinStock:       v.MinStock,
		SortOrder:      v.SortOrder,
		Active:         v.Active,
		CreatedAt:      v.CreatedAt,
		UpdatedAt:      v.UpdatedAt,
	}
}

func toOAPIProduct(p domain.Product) oapi.ProductResponse {
	variants := make([]oapi.ProductVariantResponse, 0, len(p.Variants))
	for _, v := range p.Variants {
		variants = append(variants, toOAPIVariant(v))
	}

	return oapi.ProductResponse{
		Id:              openapi_types.UUID(p.ID),
		CategoryId:      openapi_types.UUID(p.CategoryID),
		Name:            p.Name,
		Slug:            p.Slug,
		Description:     p.Description,
		ImageUrl:        p.ImageURL,
		ServiceType:     oapi.ServiceType(p.ServiceType),
		PrepTimeMinutes: p.PrepTimeMinutes,
		Active:          p.Active,
		AvailableOnline: p.AvailableOnline,
		Featured:        p.Featured,
		SortOrder:       p.SortOrder,
		Variants:        variants,
		CreatedAt:       p.CreatedAt,
		UpdatedAt:       p.UpdatedAt,
	}
}

func toOAPIPaginationMeta(meta domain.PaginationMeta) oapi.PaginationMeta {
	return oapi.PaginationMeta{
		Page:       meta.Page,
		PageSize:   meta.PageSize,
		TotalItems: int(meta.TotalItems),
		TotalPages: meta.TotalPages,
	}
}
