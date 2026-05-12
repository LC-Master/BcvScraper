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
	subquery := db.Table("TasasCambio").Select("MAX(ID)").Group("Moneda")
	if err := db.Where("ID IN (?)", subquery).Find(&ultimosRegistros).Error; err != nil {
		return err
	}

	ultimasFechas := make(map[string]string)
	for _, r := range ultimosRegistros {
		ultimasFechas[r.Moneda] = r.FechaValida.Format("2006-01-02 15:04")
	}

	nuevas := []models.Coin{}
	candidatas := []models.Coin{tasa.Dolar, tasa.Euro, tasa.Pesos}

	for _, c := range candidatas {
		if c.Moneda == "" {
			continue
		}

		fechaEntranteStr := c.FechaValida.Format("2006-01-02 15:04")

		ultimaFechaStr, existe := ultimasFechas[c.Moneda]

		if !existe || ultimaFechaStr != fechaEntranteStr {
			c.Fecha = c.Fecha.Local()
			c.FechaValida = c.FechaValida.Local()

			nuevas = append(nuevas, c)
		}
	}

	if len(nuevas) > 0 {
		if err := db.Create(&nuevas).Error; err != nil {
			slog.Error("Error al insertar nuevas tasas", "error", err)
			return err
		}
		slog.Info("Registros nuevos guardados", "count", len(nuevas))
	} else {
		slog.Info("No hay cambios en las tasas, nada que guardar")
	}

	return nil
}
