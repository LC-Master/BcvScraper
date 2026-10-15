package routes

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"scraperbcv/internal/domain"

	"github.com/gofiber/fiber/v3"
	"github.com/shopspring/decimal"
)

type mockRepo struct {
	latestRate   domain.TasaCambio
	historyRates []domain.Coin
	specificCoin domain.Coin
	specificErr  error
}

func (m *mockRepo) SaveCoin(ctx context.Context, c domain.Coin) error {
	return nil
}

func (m *mockRepo) GetLatestExchangeRate(ctx context.Context) (domain.TasaCambio, error) {
	return m.latestRate, nil
}

func (m *mockRepo) GetHistory(ctx context.Context, filter domain.HistoryFilter) ([]domain.Coin, error) {
	return m.historyRates, nil
}

func (m *mockRepo) GetSpecificRate(ctx context.Context, currency string, targetDate *time.Time) (domain.Coin, error) {
	if m.specificErr != nil {
		return domain.Coin{}, m.specificErr
	}
	return m.specificCoin, nil
}

func setupTestApp(repo domain.RepositoryCoin) *fiber.App {
	app := fiber.New()
	Register(app, repo, nil)
	return app
}

func TestGetLatest(t *testing.T) {
	repo := &mockRepo{
		latestRate: domain.TasaCambio{
			Dolar: domain.Coin{Moneda: "Dolar", Valor: decimal.NewFromFloat(50.0)},
			Euro:  domain.Coin{Moneda: "Euro", Valor: decimal.NewFromFloat(55.0)},
			Pesos: domain.Coin{Moneda: "Pesos", Valor: decimal.NewFromFloat(4000.0)},
		},
	}
	app := setupTestApp(repo)

	req := httptest.NewRequest(http.MethodGet, "/exchange-rates", nil)
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected status 200, got %d", resp.StatusCode)
	}

	body, _ := io.ReadAll(resp.Body)
	var result domain.TasaCambio
	if err := json.Unmarshal(body, &result); err != nil {
		t.Fatalf("failed to parse json: %v", err)
	}

	if !result.Dolar.Valor.Equal(decimal.NewFromFloat(50.0)) {
		t.Errorf("expected dolar 50.0, got %s", result.Dolar.Valor)
	}
}

func TestGetSpecificRate(t *testing.T) {
	repo := &mockRepo{
		specificCoin: domain.Coin{
			Moneda: "Dolar",
			Valor:  decimal.NewFromFloat(52.5),
		},
	}
	app := setupTestApp(repo)

	req := httptest.NewRequest(http.MethodGet, "/exchange-rates/specific/Dolar?date=2026-10-15", nil)
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected status 200, got %d", resp.StatusCode)
	}
}

func TestGetDifferential(t *testing.T) {
	repo := &mockRepo{
		specificCoin: domain.Coin{
			Moneda: "Dolar",
			Valor:  decimal.NewFromFloat(100.0),
		},
	}
	app := setupTestApp(repo)

	req := httptest.NewRequest(http.MethodGet, "/exchange-rates/differential?currency=Dolar&issue_date=2026-10-01&payment_date=2026-10-15", nil)
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected status 200, got %d", resp.StatusCode)
	}

	body, _ := io.ReadAll(resp.Body)
	var diff domain.DifferentialResponse
	if err := json.Unmarshal(body, &diff); err != nil {
		t.Fatalf("failed to parse differential json: %v", err)
	}

	if diff.Currency != "Dolar" {
		t.Errorf("expected Dolar, got %s", diff.Currency)
	}
}
