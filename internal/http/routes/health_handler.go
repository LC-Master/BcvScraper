package routes

import (
	"log/slog"
	"scraperbcv/internal/platform/database"

	"github.com/gofiber/fiber/v3"
	"gorm.io/gorm"
)

type HealthHandler struct {
	db *gorm.DB
}

func NewHealthHandler(db *gorm.DB) *HealthHandler {
	return &HealthHandler{db: db}
}

func (h *HealthHandler) Healthz(c fiber.Ctx) error {
	return c.SendStatus(fiber.StatusOK)
}

func (h *HealthHandler) Readyz(c fiber.Ctx) error {
	if err := database.Ping(h.db); err != nil {
		slog.Warn("Readiness check failed", "error", err)
		return c.Status(fiber.StatusServiceUnavailable).JSON(fiber.Map{
			"status": "not_ready",
		})
	}
	return c.JSON(fiber.Map{
		"status": "ready",
	})
}
