package routes

import (
	"scraperbcv/internal/domain"

	"github.com/gofiber/fiber/v3"
	"gorm.io/gorm"
)

// Register vincula todos los endpoints de salud y tasas de cambio en el servidor Fiber
func Register(app *fiber.App, repo domain.RepositoryCoin, db *gorm.DB) {
	// Endpoints de salud
	healthHandler := NewHealthHandler(db)
	app.Get("/healthz", healthHandler.Healthz)
	app.Get("/readyz", healthHandler.Readyz)

	// Endpoints de tasas de cambio
	ratesHandler := NewRatesHandler(repo)
	app.Get("/exchange-rates", ratesHandler.GetLatest)
	app.Get("/exchange-rates/history", ratesHandler.GetHistory)
	app.Get("/exchange-rates/specific/:currency", ratesHandler.GetSpecific)
	app.Get("/exchange-rates/differential", ratesHandler.GetDifferential)

	// Alias retrocompatible
	app.Get("/exchange-rate", ratesHandler.GetLatest)
}
