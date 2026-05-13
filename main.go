package main

import (
	"log/slog"
	"os"
	"scraperbcv/database"

	"github.com/joho/godotenv"
	"github.com/robfig/cron/v3"
	"gorm.io/gorm"
)

type App struct {
	db *gorm.DB
}

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{}))
	slog.SetDefault(logger)

	err := godotenv.Load()

	if err != nil {
		slog.Error("Error loading .env file", "error", err)
	}

	connString := os.Getenv("DB_CONNECTION_STRING")
	if connString == "" {
		slog.Error("DB_CONNECTION_STRING environment variable is not set")
		return
	}

	db := database.InitDB(connString)

	app := &App{db: db}

	cron := cron.New()

	_, err = cron.AddFunc("0 1 * * *", func() {
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
