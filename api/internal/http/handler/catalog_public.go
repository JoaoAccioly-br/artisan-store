package handler

import (
	"context"

	openapi_types "github.com/oapi-codegen/runtime/types"

	"github.com/joao-accioly/artisan-store/api/internal/domain"
	"github.com/joao-accioly/artisan-store/api/internal/http/oapi"
	"github.com/joao-accioly/artisan-store/api/internal/service"
)

// PublicGetStore retorna os dados públicos da loja, horários e status de funcionamento atual.
func (s *Server) PublicGetStore(ctx context.Context, _ oapi.PublicGetStoreRequestObject) (oapi.PublicGetStoreResponseObject, error) {
	if s.catalog == nil {
		return nil, domain.NewInternalError("Serviço de catálogo não configurado", nil)
	}

	settings, isOpenNow, err := s.catalog.GetPublicStore(ctx)
	if err != nil {
		return nil, err
	}

	return oapi.PublicGetStore200JSONResponse{
		StoreName:                settings.StoreName,
		Description:              settings.Description,
		Phone:                    settings.Phone,
		Whatsapp:                 settings.WhatsApp,
		Email:                    settings.Email,
		AddressLine:              settings.AddressLine,
		OpeningHours:             toOAPIOpeningHours(settings.OpeningHours),
		AcceptedPaymentMethods:   toOAPIPaymentMethods(settings.AcceptedPaymentMethods),
		ManualStatus:             oapi.StoreManualStatus(settings.ManualStatus),
		IsOpenNow:                isOpenNow,
		AllowScheduling:          settings.AllowScheduling,
		PixReservationTtlMinutes: settings.PixReservationTTLMinutes,
		AutoHideOutOfStock:       settings.AutoHideOutOfStock,
	}, nil
}

// PublicGetMenu retorna o cardápio público organizado por categoria com filtros opcionais.
func (s *Server) PublicGetMenu(ctx context.Context, request oapi.PublicGetMenuRequestObject) (oapi.PublicGetMenuResponseObject, error) {
	if s.catalog == nil {
		return nil, domain.NewInternalError("Serviço de catálogo não configurado", nil)
	}

	var serviceType *domain.ServiceType
	if request.Params.ServiceType != nil {
		parsed, err := domain.ParseServiceType(string(*request.Params.ServiceType))
		if err != nil {
			return nil, err
		}
		serviceType = &parsed
	}

	categories, err := s.catalog.GetPublicMenu(ctx, service.PublicMenuFilter{
		CategorySlug: request.Params.CategorySlug,
		ServiceType:  serviceType,
		Search:       request.Params.Search,
		FeaturedOnly: request.Params.FeaturedOnly,
	})
	if err != nil {
		return nil, err
	}

	respCategories := make([]oapi.PublicMenuCategory, 0, len(categories))
	for _, cat := range categories {
		products := make([]oapi.PublicProductResponse, 0, len(cat.Products))
		for _, p := range cat.Products {
			products = append(products, toOAPIPublicProduct(p))
		}
		respCategories = append(respCategories, oapi.PublicMenuCategory{
			Id:        openapi_types.UUID(cat.ID),
			Name:      cat.Name,
			Slug:      cat.Slug,
			SortOrder: cat.SortOrder,
			Products:  products,
		})
	}

	return oapi.PublicGetMenu200JSONResponse{
		Categories: respCategories,
	}, nil
}

// PublicGetProduct retorna detalhes públicos de um produto ativo por slug ou UUID.
func (s *Server) PublicGetProduct(ctx context.Context, request oapi.PublicGetProductRequestObject) (oapi.PublicGetProductResponseObject, error) {
	if s.catalog == nil {
		return nil, domain.NewInternalError("Serviço de catálogo não configurado", nil)
	}

	product, err := s.catalog.GetPublicProduct(ctx, request.SlugOrId)
	if err != nil {
		return nil, err
	}

	return oapi.PublicGetProduct200JSONResponse(toOAPIPublicProduct(*product)), nil
}

func toOAPIPublicProduct(p domain.PublicProduct) oapi.PublicProductResponse {
	variants := make([]oapi.PublicVariantResponse, 0, len(p.Variants))
	for _, v := range p.Variants {
		variants = append(variants, oapi.PublicVariantResponse{
			Id:             openapi_types.UUID(v.ID),
			Name:           v.Name,
			Sku:            v.SKU,
			PriceCents:     v.PriceCents,
			InStock:        v.InStock,
			AvailableStock: v.AvailableStock,
			SortOrder:      v.SortOrder,
		})
	}

	return oapi.PublicProductResponse{
		Id:              openapi_types.UUID(p.ID),
		CategoryId:      openapi_types.UUID(p.CategoryID),
		CategoryName:    p.CategoryName,
		CategorySlug:    p.CategorySlug,
		Name:            p.Name,
		Slug:            p.Slug,
		Description:     p.Description,
		ImageUrl:        p.ImageURL,
		ServiceType:     oapi.ServiceType(p.ServiceType),
		PrepTimeMinutes: p.PrepTimeMinutes,
		Featured:        p.Featured,
		InStock:         p.InStock,
		Variants:        variants,
	}
}
