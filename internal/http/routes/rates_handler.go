package routes

import (
	"errors"
	"fmt"
	"log/slog"
	"scraperbcv/internal/domain"
	"strings"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/shopspring/decimal"
)

type RatesHandler struct {
	repo domain.RepositoryCoin
}

func NewRatesHandler(repo domain.RepositoryCoin) *RatesHandler {
	return &RatesHandler{repo: repo}
}

// GetLatest devuelve la última tasa vigente registrada para cada una de las tres divisas (Dolar, Euro, Pesos)
// GET /exchange-rates
func (h *RatesHandler) GetLatest(c fiber.Ctx) error {
	ctx := c.Context()
	rates, err := h.repo.GetLatestExchangeRate(ctx)
	if err != nil {
		slog.Error("Failed to fetch latest exchange rates", "error", err)
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error": "failed to fetch latest exchange rates",
		})
	}
	return c.JSON(rates)
}

// GetHistory consulta el histórico de tasas según filtros opcionales
// GET /exchange-rates/history
func (h *RatesHandler) GetHistory(c fiber.Ctx) error {
	ctx := c.Context()
	var filter domain.HistoryFilter

	filter.Currency = strings.TrimSpace(c.Query("currency"))
	filter.Order = strings.TrimSpace(c.Query("order", "DESC"))

	const dateFormat = "2006-01-02"

	if dateStr := strings.TrimSpace(c.Query("date")); dateStr != "" {
		d, err := time.Parse(dateFormat, dateStr)
		if err != nil {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
				"error": "invalid 'date' query parameter format, expected YYYY-MM-DD",
			})
		}
		filter.Date = &d
	}

	if fromStr := strings.TrimSpace(c.Query("from")); fromStr != "" {
		f, err := time.Parse(dateFormat, fromStr)
		if err != nil {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
				"error": "invalid 'from' query parameter format, expected YYYY-MM-DD",
			})
		}
		filter.From = &f
	}

	if toStr := strings.TrimSpace(c.Query("to")); toStr != "" {
		t, err := time.Parse(dateFormat, toStr)
		if err != nil {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
				"error": "invalid 'to' query parameter format, expected YYYY-MM-DD",
			})
		}
		filter.To = &t
	}

	rates, err := h.repo.GetHistory(ctx, filter)
	if err != nil {
		slog.Error("Failed to fetch exchange rates history", "error", err)
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error": "failed to fetch exchange rates history",
		})
	}

	return c.JSON(rates)
}

// GetSpecific consulta puntual para una sola moneda con fallback a día hábil anterior si no hay registro exacto
// GET /exchange-rates/specific/:currency
func (h *RatesHandler) GetSpecific(c fiber.Ctx) error {
	ctx := c.Context()
	currency := strings.TrimSpace(c.Params("currency"))
	if currency == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": "currency path parameter is required",
		})
	}

	var targetDate *time.Time
	if dateStr := strings.TrimSpace(c.Query("date")); dateStr != "" {
		d, err := time.Parse("2006-01-02", dateStr)
		if err != nil {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
				"error": "invalid 'date' query parameter format, expected YYYY-MM-DD",
			})
		}
		targetDate = &d
	}

	coin, err := h.repo.GetSpecificRate(ctx, currency, targetDate)
	if err != nil {
		if errors.Is(err, domain.ErrRateNotFound) {
			return c.Status(fiber.StatusNotFound).JSON(fiber.Map{
				"error": fmt.Sprintf("no rate found for currency '%s' on or prior to the requested date", currency),
			})
		}
		slog.Error("Failed to fetch specific exchange rate", "currency", currency, "error", err)
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error": "failed to fetch specific exchange rate",
		})
	}

	return c.JSON(coin)
}

// GetDifferential calcula la variación absoluta y porcentual entre dos fechas para una divisa
// GET /exchange-rates/differential
func (h *RatesHandler) GetDifferential(c fiber.Ctx) error {
	ctx := c.Context()
	currency := strings.TrimSpace(c.Query("currency"))
	if currency == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": "query parameter 'currency' is required",
		})
	}

	issueDateStr := strings.TrimSpace(c.Query("issue_date"))
	if issueDateStr == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": "query parameter 'issue_date' is required",
		})
	}
	issueDate, err := time.Parse("2006-01-02", issueDateStr)
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": "invalid 'issue_date' query parameter format, expected YYYY-MM-DD",
		})
	}

	paymentDateStr := strings.TrimSpace(c.Query("payment_date"))
	if paymentDateStr == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": "query parameter 'payment_date' is required",
		})
	}
	paymentDate, err := time.Parse("2006-01-02", paymentDateStr)
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": "invalid 'payment_date' query parameter format, expected YYYY-MM-DD",
		})
	}

	issueCoin, err := h.repo.GetSpecificRate(ctx, currency, &issueDate)
	if err != nil {
		if errors.Is(err, domain.ErrRateNotFound) {
			return c.Status(fiber.StatusNotFound).JSON(fiber.Map{
				"error": fmt.Sprintf("no rate found for currency '%s' on or prior to issue_date '%s'", currency, issueDateStr),
			})
		}
		slog.Error("Failed to fetch issue exchange rate", "currency", currency, "date", issueDateStr, "error", err)
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error": "failed to fetch issue exchange rate",
		})
	}

	paymentCoin, err := h.repo.GetSpecificRate(ctx, currency, &paymentDate)
	if err != nil {
		if errors.Is(err, domain.ErrRateNotFound) {
			return c.Status(fiber.StatusNotFound).JSON(fiber.Map{
				"error": fmt.Sprintf("no rate found for currency '%s' on or prior to payment_date '%s'", currency, paymentDateStr),
			})
		}
		slog.Error("Failed to fetch payment exchange rate", "currency", currency, "date", paymentDateStr, "error", err)
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error": "failed to fetch payment exchange rate",
		})
	}

	diffAbs := paymentCoin.Valor.Sub(issueCoin.Valor).Round(4)
	diffPct := decimal.Zero
	if !issueCoin.Valor.IsZero() {
		diffPct = paymentCoin.Valor.Sub(issueCoin.Valor).Div(issueCoin.Valor).Mul(decimal.NewFromInt(100)).Round(4)
	}

	response := domain.DifferentialResponse{
		Currency:             currency,
		IssueRate:            issueCoin,
		PaymentRate:          paymentCoin,
		AbsoluteDifference:   diffAbs,
		PercentageDifference: diffPct,
	}

	return c.JSON(response)
}
