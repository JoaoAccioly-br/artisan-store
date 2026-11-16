package domain_test

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/joao-accioly/artisan-store/api/internal/domain"
)

func TestServiceType(t *testing.T) {
	assert.True(t, domain.ServiceTypeReadyStock.IsValid())
	assert.True(t, domain.ServiceTypeMadeToOrder.IsValid())
	assert.False(t, domain.ServiceType("").IsValid())
	assert.False(t, domain.ServiceType("delivery").IsValid())
	assert.False(t, domain.ServiceType("PRONTA_ENTREGA").IsValid())

	parsed, err := domain.ParseServiceType("  PRONTA_ENTREGA ")
	require.NoError(t, err)
	assert.Equal(t, domain.ServiceTypeReadyStock, parsed)

	parsed, err = domain.ParseServiceType("Sob_Encomenda")
	require.NoError(t, err)
	assert.Equal(t, domain.ServiceTypeMadeToOrder, parsed)

	_, err = domain.ParseServiceType("invalido")
	require.Error(t, err)
	domErr, ok := domain.AsDomainError(err)
	require.True(t, ok)
	assert.Equal(t, domain.CodeValidation, domErr.Code)
	assert.Contains(t, domErr.Details, "service_type")
}

func TestPaymentMethod_IsValid(t *testing.T) {
	valid := []domain.PaymentMethod{
		domain.PaymentMethodPixOnline,
		domain.PaymentMethodCardDelivery,
		domain.PaymentMethodCashDelivery,
		domain.PaymentMethodPickupPayment,
	}
	for _, m := range valid {
		assert.True(t, m.IsValid(), "expected %q to be valid", m)
	}

	invalid := []domain.PaymentMethod{"", "boleto", "PIX_ONLINE", "pix"}
	for _, m := range invalid {
		assert.False(t, m.IsValid(), "expected %q to be invalid", m)
	}
}

func TestStoreManualStatus_IsValid(t *testing.T) {
	assert.True(t, domain.StoreManualStatusAuto.IsValid())
	assert.True(t, domain.StoreManualStatusOpen.IsValid())
	assert.True(t, domain.StoreManualStatusClosed.IsValid())

	for _, s := range []domain.StoreManualStatus{"", "forced_open", "forced_closed", "OPEN"} {
		assert.False(t, s.IsValid(), "expected %q to be invalid", s)
	}
}

func TestSlugifyAndValidateSlug(t *testing.T) {
	t.Run("slugify pt-BR accents, symbols, multiple spaces and empty fallback", func(t *testing.T) {
		tests := []struct {
			name     string
			input    string
			expected string
		}{
			{
				name:     "pt-BR accents and cedilla",
				input:    "Pão de Mel & Maçã D'Água",
				expected: "pao-de-mel-maca-d-agua",
			},
			{
				name:     "acai and special punctuation",
				input:    "Açaí com Cupuaçu — Edição Especial!",
				expected: "acai-com-cupuacu-edicao-especial",
			},
			{
				name:     "multiple spaces and surrounding symbols",
				input:    "  ---Brigadeiro   Belga   70%!!!  ",
				expected: "brigadeiro-belga-70",
			},
			{
				name:     "empty when only whitespace",
				input:    "     ",
				expected: "",
			},
			{
				name:     "empty when only symbols or emojis",
				input:    "!!!@@@### 🎂🍫",
				expected: "",
			},
		}

		for _, tc := range tests {
			t.Run(tc.name, func(t *testing.T) {
				assert.Equal(t, tc.expected, domain.Slugify(tc.input))
			})
		}
	})

	t.Run("validate slug accepts valid canonical slugs and rejects invalid ones", func(t *testing.T) {
		for _, valid := range []string{"a", "brigadeiro-belga", "bolo-de-cenoura-2026", strings.Repeat("a", 120)} {
			assert.NoError(t, domain.ValidateSlug(valid), "expected %q to be valid", valid)
		}

		for _, invalid := range []string{
			"",
			"Brigadeiro",
			"brigadeiro_belga",
			"-brigadeiro",
			"brigadeiro-",
			"brigadeiro--belga",
			"pão-de-mel",
			"bolo belga",
			strings.Repeat("a", 121),
		} {
			err := domain.ValidateSlug(invalid)
			require.Error(t, err, "expected %q to be invalid", invalid)
			domErr, ok := domain.AsDomainError(err)
			require.True(t, ok)
			assert.Equal(t, domain.CodeValidation, domErr.Code)
			assert.Contains(t, domErr.Details, "slug")
		}
	})
}

func TestIsValidHHMM(t *testing.T) {
	for _, valid := range []string{"00:00", "09:00", "18:30", "23:59", " 08:15 "} {
		assert.True(t, domain.IsValidHHMM(valid), "expected %q to be valid", valid)
	}

	for _, invalid := range []string{"", "9:00", "24:00", "12:60", "12:5", "ab:cd", "09:00:00"} {
		assert.False(t, domain.IsValidHHMM(invalid), "expected %q to be invalid", invalid)
	}
}

func TestStoreSettings_IsOpenAt(t *testing.T) {
	loc, err := time.LoadLocation("America/Sao_Paulo")
	require.NoError(t, err)

	baseHours := domain.OpeningHours{
		Monday:    domain.DaySchedule{Open: "09:00", Close: "18:00", Closed: false},
		Tuesday:   domain.DaySchedule{Open: "09:00", Close: "18:00", Closed: false},
		Wednesday: domain.DaySchedule{Open: "09:00", Close: "18:00", Closed: false},
		Thursday:  domain.DaySchedule{Open: "09:00", Close: "18:00", Closed: false},
		Friday:    domain.DaySchedule{Open: "09:00", Close: "19:00", Closed: false},
		Saturday:  domain.DaySchedule{Open: "09:00", Close: "16:00", Closed: false},
		Sunday:    domain.DaySchedule{Open: "09:00", Close: "13:00", Closed: true},
	}

	t.Run("manual_status overrides schedule", func(t *testing.T) {
		sundayClosedInstant := time.Date(2026, 11, 15, 3, 0, 0, 0, loc) // Domingo 03:00
		mondayOpenInstant := time.Date(2026, 11, 16, 12, 0, 0, 0, loc)  // Segunda 12:00

		forcedOpen := domain.StoreSettings{
			OpeningHours: baseHours,
			ManualStatus: domain.StoreManualStatusOpen,
		}
		assert.True(t, forcedOpen.IsOpenAt(sundayClosedInstant))

		forcedClosed := domain.StoreSettings{
			OpeningHours: baseHours,
			ManualStatus: domain.StoreManualStatusClosed,
		}
		assert.False(t, forcedClosed.IsOpenAt(mondayOpenInstant))
	})

	t.Run("auto follows opening hours in America/Sao_Paulo", func(t *testing.T) {
		settings := domain.StoreSettings{
			OpeningHours: baseHours,
			ManualStatus: domain.StoreManualStatusAuto,
		}

		// Segunda-feira 08:59 BRT -> fechada
		assert.False(t, settings.IsOpenAt(time.Date(2026, 11, 16, 8, 59, 0, 0, loc)))
		// Segunda-feira 09:00 BRT -> aberta (limite inferior inclusivo)
		assert.True(t, settings.IsOpenAt(time.Date(2026, 11, 16, 9, 0, 0, 0, loc)))
		// Segunda-feira 17:59 BRT -> aberta
		assert.True(t, settings.IsOpenAt(time.Date(2026, 11, 16, 17, 59, 0, 0, loc)))
		// Segunda-feira 18:00 BRT -> fechada (limite superior exclusivo)
		assert.False(t, settings.IsOpenAt(time.Date(2026, 11, 16, 18, 0, 0, 0, loc)))

		// Converte UTC para America/Sao_Paulo (UTC-3):
		// 2026-11-16T12:00:00Z = Segunda 09:00 BRT -> aberta
		assert.True(t, settings.IsOpenAt(time.Date(2026, 11, 16, 12, 0, 0, 0, time.UTC)))
		// 2026-11-16T01:00:00Z = Domingo 22:00 BRT -> fechada (domingo fechado)
		assert.False(t, settings.IsOpenAt(time.Date(2026, 11, 16, 1, 0, 0, 0, time.UTC)))

		// Todos os demais dias da semana no horário comercial
		assert.True(t, settings.IsOpenAt(time.Date(2026, 11, 17, 10, 0, 0, 0, loc)))  // Terça
		assert.True(t, settings.IsOpenAt(time.Date(2026, 11, 18, 10, 0, 0, 0, loc)))  // Quarta
		assert.True(t, settings.IsOpenAt(time.Date(2026, 11, 19, 10, 0, 0, 0, loc)))  // Quinta
		assert.True(t, settings.IsOpenAt(time.Date(2026, 11, 20, 18, 30, 0, 0, loc))) // Sexta (até 19:00)
		assert.True(t, settings.IsOpenAt(time.Date(2026, 11, 21, 15, 59, 0, 0, loc))) // Sábado (até 16:00)
		assert.False(t, settings.IsOpenAt(time.Date(2026, 11, 15, 11, 0, 0, 0, loc))) // Domingo (Closed: true)
	})

	t.Run("invalid schedule formats or close <= open return false", func(t *testing.T) {
		mondayNoon := time.Date(2026, 11, 16, 12, 0, 0, 0, loc)

		badSchedules := []domain.DaySchedule{
			{Open: "invalid", Close: "18:00", Closed: false},
			{Open: "09:00", Close: "25:00", Closed: false},
			{Open: "18:00", Close: "09:00", Closed: false},
			{Open: "12:00", Close: "12:00", Closed: false},
		}

		for _, sched := range badSchedules {
			settings := domain.StoreSettings{
				ManualStatus: domain.StoreManualStatusAuto,
				OpeningHours: domain.OpeningHours{Monday: sched},
			}
			assert.False(t, settings.IsOpenAt(mondayNoon), "expected false for schedule %+v", sched)
		}
	})
}

func TestProductVariant_StockHelpers(t *testing.T) {
	assert.Equal(t, 7, domain.ProductVariant{CurrentStock: 10, ReservedStock: 3}.AvailableStock())
	assert.Equal(t, 0, domain.ProductVariant{CurrentStock: 5, ReservedStock: 5}.AvailableStock())
	assert.Equal(t, 0, domain.ProductVariant{CurrentStock: 2, ReservedStock: 5}.AvailableStock())

	inactive := domain.ProductVariant{Active: false, CurrentStock: 10, ReservedStock: 0}
	assert.False(t, inactive.IsInStockFor(domain.ServiceTypeReadyStock))
	assert.False(t, inactive.IsInStockFor(domain.ServiceTypeMadeToOrder))

	readyOutOfStock := domain.ProductVariant{Active: true, CurrentStock: 3, ReservedStock: 3}
	assert.False(t, readyOutOfStock.IsInStockFor(domain.ServiceTypeReadyStock))

	readyInStock := domain.ProductVariant{Active: true, CurrentStock: 4, ReservedStock: 3}
	assert.True(t, readyInStock.IsInStockFor(domain.ServiceTypeReadyStock))

	madeToOrderZeroStock := domain.ProductVariant{Active: true, CurrentStock: 0, ReservedStock: 0}
	assert.True(t, madeToOrderZeroStock.IsInStockFor(domain.ServiceTypeMadeToOrder))
}
