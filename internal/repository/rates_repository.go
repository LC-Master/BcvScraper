package repository

import (
	"context"
	"fmt"
	"log/slog"
	"scraperbcv/internal/domain"
	"strings"
	"time"

	"gorm.io/gorm"
)

const tableIdentifier = "[dbo].[LOCATEL.TasasCambio]"

type ratesRepository struct {
	db *gorm.DB
}

func NewRatesRepository(db *gorm.DB) domain.RepositoryCoin {
	return &ratesRepository{db: db}
}

func (r *ratesRepository) SaveCoin(ctx context.Context, c domain.Coin) error {
	if r.db == nil {
		return fmt.Errorf("database connection is nil")
	}
	if c.Moneda == "" {
		return nil
	}

	fechaEntranteStr := c.FechaValida.Format("2006-01-02 15:04:05")
	var existente domain.Coin

	querySQL := fmt.Sprintf("SELECT Moneda, Valor, Fecha, Simbolo, FechaValida FROM %s WHERE Moneda = ? AND FechaValida = ?", tableIdentifier)
	if err := r.db.WithContext(ctx).Raw(querySQL, c.Moneda, fechaEntranteStr).Scan(&existente).Error; err != nil {
		return err
	}

	if existente.Moneda == "" {
		cols := "(Moneda, Valor, Fecha, Simbolo, FechaValida)"
		insertSQL := fmt.Sprintf("INSERT INTO %s %s VALUES (?, ?, ?, ?, ?)", tableIdentifier, cols)
		if err := r.db.WithContext(ctx).Exec(insertSQL, c.Moneda, c.Valor.String(), c.Fecha.Format("2006-01-02 15:04:05"), c.Simbolo, fechaEntranteStr).Error; err != nil {
			return err
		}
		slog.Info("Saved new record", "moneda", c.Moneda, "valor", c.Valor.String(), "fechaValida", fechaEntranteStr)
	} else if !existente.Valor.Equal(c.Valor) {
		updateSQL := fmt.Sprintf("UPDATE %s SET Valor = ?, Fecha = ? WHERE Moneda = ? AND FechaValida = ?", tableIdentifier)
		if err := r.db.WithContext(ctx).Exec(updateSQL, c.Valor.String(), c.Fecha.Format("2006-01-02 15:04:05"), c.Moneda, fechaEntranteStr).Error; err != nil {
			return err
		}
		slog.Info("Updated existing record with new value", "moneda", c.Moneda, "valor", c.Valor.String(), "fechaValida", fechaEntranteStr)
	} else {
		slog.Info("Rates are up to date; no save required", "moneda", c.Moneda, "fechaValida", fechaEntranteStr)
	}

	return nil
}

func (r *ratesRepository) GetLatestExchangeRate(ctx context.Context) (domain.TasaCambio, error) {
	if r.db == nil {
		return domain.TasaCambio{}, fmt.Errorf("database connection is nil")
	}

	var rows []domain.Coin
	querySQL := fmt.Sprintf(`SELECT Moneda, Valor, Fecha, Simbolo, FechaValida 
		FROM (
			SELECT Moneda, Valor, Fecha, Simbolo, FechaValida, 
				ROW_NUMBER() OVER (PARTITION BY Moneda ORDER BY FechaValida DESC, Fecha DESC) as rn 
			FROM %s
		) t 
		WHERE t.rn = 1`, tableIdentifier)

	if err := r.db.WithContext(ctx).Raw(querySQL).Scan(&rows).Error; err != nil {
		return domain.TasaCambio{}, err
	}

	if len(rows) == 0 {
		return domain.TasaCambio{}, fmt.Errorf("no rates found")
	}

	var tasa domain.TasaCambio
	for _, row := range rows {
		switch strings.ToLower(strings.TrimSpace(row.Moneda)) {
		case "dolar":
			tasa.Dolar = row
		case "euro":
			tasa.Euro = row
		case "pesos":
			tasa.Pesos = row
		}
	}

	if tasa.Dolar.Moneda == "" && tasa.Euro.Moneda == "" && tasa.Pesos.Moneda == "" {
		return domain.TasaCambio{}, fmt.Errorf("no recognized currencies in latest rates")
	}

	return tasa, nil
}

func (r *ratesRepository) GetHistory(ctx context.Context, filter domain.HistoryFilter) ([]domain.Coin, error) {
	if r.db == nil {
		return nil, fmt.Errorf("database connection is nil")
	}

	query := fmt.Sprintf("SELECT Moneda, Valor, Fecha, Simbolo, FechaValida FROM %s WHERE 1=1", tableIdentifier)
	var args []any

	if strings.TrimSpace(filter.Currency) != "" {
		query += " AND LOWER(Moneda) = LOWER(?)"
		args = append(args, strings.TrimSpace(filter.Currency))
	}

	// Regla: Si no mandan ni date, ni from, ni to -> últimos 30 días respecto a la fecha actual
	if filter.Date == nil && filter.From == nil && filter.To == nil {
		thirtyDaysAgo := time.Now().AddDate(0, 0, -30)
		start := time.Date(thirtyDaysAgo.Year(), thirtyDaysAgo.Month(), thirtyDaysAgo.Day(), 0, 0, 0, 0, time.UTC)
		query += " AND FechaValida >= ?"
		args = append(args, start.Format("2006-01-02 15:04:05"))
	} else {
		if filter.Date != nil {
			start := time.Date(filter.Date.Year(), filter.Date.Month(), filter.Date.Day(), 0, 0, 0, 0, time.UTC)
			end := time.Date(filter.Date.Year(), filter.Date.Month(), filter.Date.Day(), 23, 59, 59, 0, time.UTC)
			query += " AND FechaValida >= ? AND FechaValida <= ?"
			args = append(args, start.Format("2006-01-02 15:04:05"), end.Format("2006-01-02 15:04:05"))
		} else {
			if filter.From != nil {
				start := time.Date(filter.From.Year(), filter.From.Month(), filter.From.Day(), 0, 0, 0, 0, time.UTC)
				query += " AND FechaValida >= ?"
				args = append(args, start.Format("2006-01-02 15:04:05"))
			}
			if filter.To != nil {
				end := time.Date(filter.To.Year(), filter.To.Month(), filter.To.Day(), 23, 59, 59, 0, time.UTC)
				query += " AND FechaValida <= ?"
				args = append(args, end.Format("2006-01-02 15:04:05"))
			}
		}
	}

	orderDir := "DESC"
	if strings.ToUpper(strings.TrimSpace(filter.Order)) == "ASC" {
		orderDir = "ASC"
	}
	query += fmt.Sprintf(" ORDER BY FechaValida %s, Fecha %s", orderDir, orderDir)

	rows := make([]domain.Coin, 0)
	if err := r.db.WithContext(ctx).Raw(query, args...).Scan(&rows).Error; err != nil {
		return nil, err
	}

	return rows, nil
}

func (r *ratesRepository) GetSpecificRate(ctx context.Context, currency string, targetDate *time.Time) (domain.Coin, error) {
	if r.db == nil {
		return domain.Coin{}, fmt.Errorf("database connection is nil")
	}

	var coin domain.Coin
	var query string
	var args []any

	if targetDate == nil {
		query = fmt.Sprintf("SELECT TOP 1 Moneda, Valor, Fecha, Simbolo, FechaValida FROM %s WHERE LOWER(Moneda) = LOWER(?) ORDER BY FechaValida DESC, Fecha DESC", tableIdentifier)
		args = append(args, strings.TrimSpace(currency))
	} else {
		endOfDay := time.Date(targetDate.Year(), targetDate.Month(), targetDate.Day(), 23, 59, 59, 0, time.UTC)
		query = fmt.Sprintf("SELECT TOP 1 Moneda, Valor, Fecha, Simbolo, FechaValida FROM %s WHERE LOWER(Moneda) = LOWER(?) AND FechaValida <= ? ORDER BY FechaValida DESC, Fecha DESC", tableIdentifier)
		args = append(args, strings.TrimSpace(currency), endOfDay.Format("2006-01-02 15:04:05"))
	}

	if err := r.db.WithContext(ctx).Raw(query, args...).Scan(&coin).Error; err != nil {
		return domain.Coin{}, err
	}

	if coin.Moneda == "" {
		return domain.Coin{}, domain.ErrRateNotFound
	}

	return coin, nil
}
