package database

import (
	"fmt"
	"log/slog"
	"scraperbcv/models"

	"gorm.io/driver/sqlserver"
	"gorm.io/gorm"
)

func InitDB(connString string) *gorm.DB {
	db, err := gorm.Open(sqlserver.Open(connString), &gorm.Config{})
	if err != nil {
		panic(err)
	}

	db.AutoMigrate(&models.Coin{})

	return db
}

func SaveTasaCambio(db *gorm.DB, tasa models.TasaCambio) error {
	if db == nil {
		return fmt.Errorf("database connection is nil")
	}

	var ultimosRegistros []models.Coin
	subquery := db.Table("TasasCambio").Select("Moneda, MAX(FechaValida) as FechaValida").Group("Moneda")

	if err := db.Table("TasasCambio as t").
		Joins("JOIN (?) s ON s.Moneda = t.Moneda AND s.FechaValida = t.FechaValida", subquery).
		Find(&ultimosRegistros).Error; err != nil {
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
			c.Fecha = c.Fecha.UTC()
			c.FechaValida = c.FechaValida.UTC()

			nuevas = append(nuevas, c)
		}
	}

	if len(nuevas) > 0 {
		if err := db.Create(&nuevas).Error; err != nil {
			return err
		}
		slog.Info("Registros nuevos guardados", "count", len(nuevas))
	} else {
		slog.Info("Tasas al día. No se requiere guardado.")
	}

	return nil
}
