package database

import (
	"fmt"
	"log/slog"
	"net/url"
	"scraperbcv/models"
	"strings"

	"gorm.io/driver/sqlserver"
	"gorm.io/gorm"
)

const tableIdentifier = "[dbo].[LOCATEL.TasasCambio]"

func EscapeConnString(rawString string) string {
	if !strings.HasPrefix(rawString, "sqlserver://") {
		return rawString
	}

	str := strings.TrimPrefix(rawString, "sqlserver://")

	queryParams := ""
	if idx := strings.Index(str, "?"); idx != -1 {
		queryParams = str[idx:]
		str = str[:idx]
	}

	lastAt := strings.LastIndex(str, "@")
	if lastAt == -1 {
		return rawString
	}

	credentials := str[:lastAt]
	host := str[lastAt+1:]

	username := credentials
	password := ""
	if colonIdx := strings.Index(credentials, ":"); colonIdx != -1 {
		username = credentials[:colonIdx]
		password = credentials[colonIdx+1:]
	}

	escapedPassword := url.PathEscape(password)

	return fmt.Sprintf("sqlserver://%s:%s@%s%s", username, escapedPassword, host, queryParams)
}

func InitDB(connString string) (*gorm.DB, error) {
	connString = EscapeConnString(connString)
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

	subquerySQL := fmt.Sprintf("(SELECT Moneda, MAX(FechaValida) as FechaValida FROM %s GROUP BY Moneda)", tableIdentifier)
	querySQL := fmt.Sprintf("SELECT t.Moneda, t.Valor, t.Fecha, t.Simbolo, t.FechaValida FROM %s t JOIN %s s ON s.Moneda = t.Moneda AND s.FechaValida = t.FechaValida", tableIdentifier, subquerySQL)

	if err := db.Raw(querySQL).Scan(&ultimosRegistros).Error; err != nil {
		return err
	}

	ultimasFechas := make(map[string]string)
	for _, r := range ultimosRegistros {
		ultimasFechas[r.Moneda] = r.FechaValida.Format("2006-01-02")
	}

	nuevas := []models.Coin{}
	candidatas := []models.Coin{tasa.Dolar, tasa.Euro, tasa.Pesos}

	for _, c := range candidatas {
		if c.Moneda == "" {
			continue
		}

		fechaEntranteStr := c.FechaValida.Format("2006-01-02")
		ultimaFechaStr, existe := ultimasFechas[c.Moneda]

		if !existe || ultimaFechaStr != fechaEntranteStr {
			c.FechaValida = c.FechaValida.UTC()

			nuevas = append(nuevas, c)
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
			recordsSummary[i] = fmt.Sprintf("%s=%s (FechaValida=%s)", r.Moneda, r.Valor.String(), r.FechaValida.Format("2006-01-02"))
		}
		slog.Info("Saved new records", "count", len(nuevas), "records", strings.Join(recordsSummary, "; "))
	} else {
		slog.Info("Rates are up to date; no save required")
	}

	return nil
}

func GetUltimaTasaCambio(db *gorm.DB) (models.TasaCambio, error) {
	if db == nil {
		return models.TasaCambio{}, fmt.Errorf("database connection is nil")
	}

	var rows []models.Coin

	subquerySQL := fmt.Sprintf("(SELECT Moneda, MAX(FechaValida) as FechaValida FROM %s GROUP BY Moneda)", tableIdentifier)
	querySQL := fmt.Sprintf("SELECT t.Moneda, t.Valor, t.Fecha, t.Simbolo, t.FechaValida FROM %s t JOIN %s s ON s.Moneda = t.Moneda AND s.FechaValida = t.FechaValida", tableIdentifier, subquerySQL)

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
