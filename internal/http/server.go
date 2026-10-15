package http

import (
	"scraperbcv/internal/config"

	"github.com/gofiber/fiber/v3"
	"github.com/gofiber/fiber/v3/middleware/recover"
)

func NewServer(cfg config.Config) *fiber.App {
	server := fiber.New(fiber.Config{
		ReadTimeout:  cfg.ServerReadTimeout,
		WriteTimeout: cfg.ServerWriteTimeout,
		IdleTimeout:  cfg.ServerIdleTimeout,
	})
	server.Use(recover.New())

	return server
}
