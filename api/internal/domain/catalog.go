package domain

import (
	"fmt"
	"regexp"
	"strings"
	"time"
	_ "time/tzdata" // Garante fuso America/Sao_Paulo mesmo em containers minimalistas.
	"unicode"

	"github.com/google/uuid"
	"golang.org/x/text/runes"
	"golang.org/x/text/transform"
	"golang.org/x/text/unicode/norm"
)

// ServiceType define como o produto é atendido e tem estoque controlado.
type ServiceType string

// Tipos de atendimento suportados para produtos.
const (
	ServiceTypeReadyStock  ServiceType = "pronta_entrega"
	ServiceTypeMadeToOrder ServiceType = "sob_encomenda"
)

// IsValid informa se o tipo de atendimento é permitido no domínio.
func (s ServiceType) IsValid() bool {
	return s == ServiceTypeReadyStock || s == ServiceTypeMadeToOrder
}

// ParseServiceType converte e valida uma string para domain.ServiceType.
func ParseServiceType(raw string) (ServiceType, error) {
	parsed := ServiceType(strings.ToLower(strings.TrimSpace(raw)))
	if !parsed.IsValid() {
		return "", NewValidationError(fmt.Sprintf("Tipo de atendimento inválido: %q", raw), map[string]any{
			"service_type": "deve ser pronta_entrega ou sob_encomenda",
		})
	}
	return parsed, nil
}

// PaymentMethod define os meios de pagamento configuráveis da loja.
type PaymentMethod string

// Métodos de pagamento aceitos pelo sistema.
const (
	PaymentMethodPixOnline     PaymentMethod = "pix_online"
	PaymentMethodCardDelivery  PaymentMethod = "cartao_entrega"
	PaymentMethodCashDelivery  PaymentMethod = "dinheiro_entrega"
	PaymentMethodPickupPayment PaymentMethod = "pagamento_retirada"
)

// IsValid informa se o método de pagamento é suportado.
func (p PaymentMethod) IsValid() bool {
	switch p {
	case PaymentMethodPixOnline, PaymentMethodCardDelivery, PaymentMethodCashDelivery, PaymentMethodPickupPayment:
		return true
	default:
		return false
	}
}

// StoreManualStatus controla se a loja segue a tabela de horários ou abre/fecha manualmente.
type StoreManualStatus string

// Modos de funcionamento manual da loja.
const (
	StoreManualStatusAuto   StoreManualStatus = "auto"
	StoreManualStatusOpen   StoreManualStatus = "open"
	StoreManualStatusClosed StoreManualStatus = "closed"
)

// IsValid informa se o status manual é válido.
func (s StoreManualStatus) IsValid() bool {
	return s == StoreManualStatusAuto || s == StoreManualStatusOpen || s == StoreManualStatusClosed
}

// DaySchedule define o horário de funcionamento de um dia da semana (formato HH:MM).
type DaySchedule struct {
	Open   string `json:"open"`
	Close  string `json:"close"`
	Closed bool   `json:"closed"`
}

// OpeningHours agrupa a agenda semanal da loja.
type OpeningHours struct {
	Monday    DaySchedule `json:"monday"`
	Tuesday   DaySchedule `json:"tuesday"`
	Wednesday DaySchedule `json:"wednesday"`
	Thursday  DaySchedule `json:"thursday"`
	Friday    DaySchedule `json:"friday"`
	Saturday  DaySchedule `json:"saturday"`
	Sunday    DaySchedule `json:"sunday"`
}

// StoreSettings representa as configurações operacionais e públicas da loja.
type StoreSettings struct {
	ID                       uuid.UUID
	StoreName                string
	Description              string
	Phone                    string
	WhatsApp                 string
	Email                    string
	AddressLine              string
	OpeningHours             OpeningHours
	AcceptedPaymentMethods   []PaymentMethod
	ManualStatus             StoreManualStatus
	AllowScheduling          bool
	PixReservationTTLMinutes int
	AutoHideOutOfStock       bool
	CreatedAt                time.Time
	UpdatedAt                time.Time
}

// IsOpenAt avalia se a loja está aberta no instante informado considerando fuso America/Sao_Paulo.
func (s StoreSettings) IsOpenAt(now time.Time) bool {
	switch s.ManualStatus {
	case StoreManualStatusOpen:
		return true
	case StoreManualStatusClosed:
		return false
	}

	loc, err := time.LoadLocation("America/Sao_Paulo")
	if err != nil {
		loc = time.FixedZone("BRT", -3*3600)
	}
	localNow := now.In(loc)

	var sched DaySchedule
	switch localNow.Weekday() {
	case time.Monday:
		sched = s.OpeningHours.Monday
	case time.Tuesday:
		sched = s.OpeningHours.Tuesday
	case time.Wednesday:
		sched = s.OpeningHours.Wednesday
	case time.Thursday:
		sched = s.OpeningHours.Thursday
	case time.Friday:
		sched = s.OpeningHours.Friday
	case time.Saturday:
		sched = s.OpeningHours.Saturday
	case time.Sunday:
		sched = s.OpeningHours.Sunday
	}

	if sched.Closed {
		return false
	}

	openMin, okOpen := parseHHMM(sched.Open)
	closeMin, okClose := parseHHMM(sched.Close)
	if !okOpen || !okClose || closeMin <= openMin {
		return false
	}

	currentMin := localNow.Hour()*60 + localNow.Minute()
	return currentMin >= openMin && currentMin < closeMin
}

var hhmmRegex = regexp.MustCompile(`^([01][0-9]|2[0-3]):([0-5][0-9])$`)

// IsValidHHMM verifica se a string está no formato estrito HH:MM (00:00..23:59).
func IsValidHHMM(v string) bool {
	return hhmmRegex.MatchString(strings.TrimSpace(v))
}

func parseHHMM(v string) (int, bool) {
	v = strings.TrimSpace(v)
	if !hhmmRegex.MatchString(v) {
		return 0, false
	}
	var hour, min int
	if _, err := fmt.Sscanf(v, "%d:%d", &hour, &min); err != nil {
		return 0, false
	}
	return hour*60 + min, true
}

// Category organiza os produtos da loja e do cardápio.
type Category struct {
	ID        uuid.UUID
	Name      string
	Slug      string
	SortOrder int
	Active    bool
	CreatedAt time.Time
	UpdatedAt time.Time
}

// Product representa um item do catálogo, vinculado a uma categoria e uma ou mais variações.
type Product struct {
	ID              uuid.UUID
	CategoryID      uuid.UUID
	Name            string
	Slug            string
	Description     string
	ImageURL        *string
	ImageKey        *string
	ServiceType     ServiceType
	PrepTimeMinutes int
	Active          bool
	AvailableOnline bool
	Featured        bool
	SortOrder       int
	Variants        []ProductVariant
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

// ProductVariant representa uma variação vendável de produto (preço e estoque de pronta-entrega).
type ProductVariant struct {
	ID            uuid.UUID
	ProductID     uuid.UUID
	Name          string
	SKU           *string
	PriceCents    int64
	CurrentStock  int
	ReservedStock int
	MinStock      int
	SortOrder     int
	Active        bool
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

// AvailableStock calcula o saldo efetivamente disponível para venda na variante.
func (v ProductVariant) AvailableStock() int {
	avail := v.CurrentStock - v.ReservedStock
	if avail < 0 {
		return 0
	}
	return avail
}

// IsInStockFor indica se a variante está disponível para pedido dado o tipo de atendimento do produto.
func (v ProductVariant) IsInStockFor(serviceType ServiceType) bool {
	if !v.Active {
		return false
	}
	if serviceType == ServiceTypeMadeToOrder {
		return true
	}
	return v.AvailableStock() > 0
}

// PublicProduct representa a visão de vitrine de um produto ativo para a loja online.
type PublicProduct struct {
	ID              uuid.UUID
	CategoryID      uuid.UUID
	CategoryName    string
	CategorySlug    string
	Name            string
	Slug            string
	Description     string
	ImageURL        *string
	ServiceType     ServiceType
	PrepTimeMinutes int
	Featured        bool
	InStock         bool
	Variants        []PublicVariant
}

// PublicVariant representa a visão pública de uma variante no cardápio.
type PublicVariant struct {
	ID             uuid.UUID
	Name           string
	SKU            *string
	PriceCents     int64
	InStock        bool
	AvailableStock int
	SortOrder      int
}

// PublicMenuCategory agrupa produtos públicos por categoria ordenada.
type PublicMenuCategory struct {
	ID        uuid.UUID
	Name      string
	Slug      string
	SortOrder int
	Products  []PublicProduct
}

var (
	slugInvalidChars = regexp.MustCompile(`[^a-z0-9]+`)
	slugValidRegex   = regexp.MustCompile(`^[a-z0-9]+(?:-[a-z0-9]+)*$`)
)

// Slugify gera um slug URL-friendly em minúsculas removendo acentuação e símbolos.
func Slugify(input string) string {
	t := transform.Chain(norm.NFD, runes.Remove(runes.In(unicode.Mn)), norm.NFC)
	normalized, _, err := transform.String(t, strings.TrimSpace(input))
	if err != nil {
		normalized = strings.TrimSpace(input)
	}
	lower := strings.ToLower(normalized)
	replaced := slugInvalidChars.ReplaceAllString(lower, "-")
	return strings.Trim(replaced, "-")
}

// ValidateSlug verifica se um slug já está no formato canônico permitido (letras minúsculas, dígitos e hífens).
func ValidateSlug(slug string) error {
	if slug == "" || len(slug) > 120 || !slugValidRegex.MatchString(slug) {
		return NewValidationError("Slug inválido: use apenas letras minúsculas, números e hífens", map[string]any{
			"slug": "deve seguir o formato url-friendly (ex.: brigadeiro-belga)",
		})
	}
	return nil
}
