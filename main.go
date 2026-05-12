package main

import (
	"log/slog"
	"os"
	"scraperbcv/database"

	"github.com/robfig/cron/v3"
	"gorm.io/gorm"
)

type App struct {
	db *gorm.DB
}

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{}))
	slog.SetDefault(logger)

	db := database.InitDB("sqlserver://SA:Sistemas01@localhost:1433?database=TasaCambio&encrypt=disable")

	app := &App{db: db}

	cron := cron.New()

	_, err := cron.AddFunc("0 1 * * *", func() {
		tasa, err := scrapeTasaCambio()
		if err != nil {
			slog.Error("Error scraping tasa de cambio", "error", err)
			return
		}

		err = database.SaveTasaCambio(app.db, *tasa)
		if err != nil {
			slog.Error("Error saving tasa de cambio to database", "error", err)
			return
		}
	})

	if err != nil {
		slog.Error("Error adding cron job", "error", err)
		return
	}

	cron.Start()
	defer cron.Stop()

	tasa, err := scrapeTasaCambio()
	if err != nil {
		slog.Error("Error scraping tasa de cambio", "error", err)
		return
	}
	err = database.SaveTasaCambio(app.db, *tasa)
	if err != nil {
		slog.Error("Error saving tasa de cambio to database", "error", err)
		return
	}
	select {}
}
