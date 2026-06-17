package database

import (
	"fmt"
	"log/slog"
	"net/url"
	"os"
	"scraperbcv/models"
	"strings"

	"gorm.io/driver/sqlserver"
	"gorm.io/gorm"
)

const tableIdentifier = "[dbo].[LOCATEL.TasasCambio]"

func BuildConnString() string {
	u := &url.URL{
		Scheme: "sqlserver",
		User: url.UserPassword(
			os.Getenv("DB_USER"),
			os.Getenv("DB_PASSWORD"),
		),
		Host: fmt.Sprintf(
			"%s:%s",
			os.Getenv("DB_HOST"),
			os.Getenv("DB_PORT"),
		),
	}

	q := u.Query()
	q.Set("database", os.Getenv("DB_NAME"))
	q.Set("encrypt", os.Getenv("DB_ENCRYPT"))
	q.Set("TrustServerCertificate", os.Getenv("DB_TRUST_SERVER_CERTIFICATE"))
	q.Set("connection timeout", os.Getenv("DB_CONNECTION_TIMEOUT"))

	u.RawQuery = q.Encode()
	return u.String()
}

func InitDB() (*gorm.DB, error) {
	connString := BuildConnString()
	db, err := gorm.Open(sqlserver.Open(connString), &gorm.Config{})
	if err != nil {
		return nil, err
	}
	return db, nil
}

func SaveTasaCambio(db *gorm.DB, tasa models.TasaCambio) error {
	if db == nil {
		return fmt.Errorf("database connection is nil")
	}

	var ultimosRegistros []models.Coin
	querySQL := fmt.Sprintf("SELECT Moneda, Valor, Fecha, Simbolo, FechaValida FROM (SELECT Moneda, Valor, Fecha, Simbolo, FechaValida, ROW_NUMBER() OVER (PARTITION BY Moneda ORDER BY Fecha DESC) as rn FROM %s) t WHERE t.rn = 1", tableIdentifier)

	if err := db.Raw(querySQL).Scan(&ultimosRegistros).Error; err != nil {
		return err
	}

	ultimosMap := make(map[string]models.Coin)
	for _, r := range ultimosRegistros {
		ultimosMap[r.Moneda] = r
	}

	nuevas := []models.Coin{}
	candidatas := []models.Coin{tasa.Dolar, tasa.Euro, tasa.Pesos}

	for _, c := range candidatas {
		if c.Moneda == "" {
			continue
		}

		fechaEntranteStr := c.FechaValida.Format("2006-01-02 15:04:05")
		ultimo, existe := ultimosMap[c.Moneda]

		if !existe || ultimo.FechaValida.Format("2006-01-02 15:04:05") != fechaEntranteStr {
			nuevas = append(nuevas, c)
		} else if !ultimo.Valor.Equal(c.Valor) {
			updateSQL := fmt.Sprintf("UPDATE %s SET Valor = ?, Fecha = ? WHERE Moneda = ? AND FechaValida = ?", tableIdentifier)
			if err := db.Exec(updateSQL, c.Valor.String(), c.Fecha.Format("2006-01-02 15:04:05"), c.Moneda, fechaEntranteStr).Error; err != nil {
				return err
			}
			slog.Info("Updated existing record with new value", "moneda", c.Moneda, "valor", c.Valor.String(), "fechaValida", fechaEntranteStr)
		}
	}

	if len(nuevas) > 0 {
		cols := "(Moneda, Valor, Fecha, Simbolo, FechaValida)"
		var values strings.Builder
		args := []any{}

		for i, r := range nuevas {
			if i > 0 {
				values.WriteString(",")
			}
			values.WriteString("(?, ?, ?, ?, ?)")
			args = append(args, r.Moneda, r.Valor.String(), r.Fecha.Format("2006-01-02 15:04:05"), r.Simbolo, r.FechaValida.Format("2006-01-02 15:04:05"))
		}

		insertSQL := fmt.Sprintf("INSERT INTO %s %s VALUES %s", tableIdentifier, cols, values.String())
		if err := db.Exec(insertSQL, args...).Error; err != nil {
			return err
		}

		recordsSummary := make([]string, len(nuevas))
		for i, r := range nuevas {
			recordsSummary[i] = fmt.Sprintf("%s=%s (FechaValida=%s)", r.Moneda, r.Valor.String(), r.FechaValida.Format("2006-01-02 15:04:05"))
		}
		slog.Info("Saved new records", "count", len(nuevas), "records", strings.Join(recordsSummary, "; "))
	} else if len(nuevas) == 0 {
		slog.Info("Rates are up to date; no save required")
	}

	return nil
}

func GetUltimaTasaCambio(db *gorm.DB) (models.TasaCambio, error) {
	if db == nil {
		return models.TasaCambio{}, fmt.Errorf("database connection is nil")
	}

	var rows []models.Coin
	querySQL := fmt.Sprintf("SELECT Moneda, Valor, Fecha, Simbolo, FechaValida FROM (SELECT Moneda, Valor, Fecha, Simbolo, FechaValida, ROW_NUMBER() OVER (PARTITION BY Moneda ORDER BY Fecha DESC) as rn FROM %s) t WHERE t.rn = 1", tableIdentifier)

	if err := db.Raw(querySQL).Scan(&rows).Error; err != nil {
		return models.TasaCambio{}, err
	}

	if len(rows) == 0 {
		return models.TasaCambio{}, fmt.Errorf("no rates found")
	}

	var tasa models.TasaCambio
	for _, r := range rows {
		switch strings.ToLower(r.Moneda) {
		case "dolar":
			tasa.Dolar = r
		case "euro":
			tasa.Euro = r
		case "pesos":
			tasa.Pesos = r
		}
	}

	if tasa.Dolar.Moneda == "" && tasa.Euro.Moneda == "" && tasa.Pesos.Moneda == "" {
		return models.TasaCambio{}, fmt.Errorf("no recognized currencies in latest rates")
	}

	return tasa, nil
}

func Ping(db *gorm.DB) error {
	if db == nil {
		return fmt.Errorf("database connection is nil")
	}

	var one int
	if err := db.Raw("SELECT 1").Scan(&one).Error; err != nil {
		return err
	}
	return nil
}
