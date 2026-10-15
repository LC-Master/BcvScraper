package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"runtime/debug"
	"syscall"
	"time"

	"scraperbcv/internal/config"
	internalHttp "scraperbcv/internal/http"
	"scraperbcv/internal/http/routes"
	"scraperbcv/internal/logger"
	"scraperbcv/internal/platform/database"
	"scraperbcv/internal/repository"
	"scraperbcv/internal/services"
	"scraperbcv/internal/workers"

	"github.com/gofiber/fiber/v3"
)

func main() {
	cfg, err := config.LoadConfig()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error loading config: %v\n", err)
		os.Exit(1)
	}

	logger.NewLogger(cfg)
	defer func() {
		if err := logger.Close(); err != nil {
			fmt.Fprintf(os.Stderr, "Error closing logger: %v\n", err)
		}
	}()

	slog.Info("=== BCV SCRAPER SERVICE STARTED VIA WINSW ===")

	defer func() {
		if r := recover(); r != nil {
			slog.Error("panic recovered in main",
				"panic", r,
				"stack", string(debug.Stack()),
			)
			_ = logger.Close()
			os.Exit(1)
		}
	}()

	db, err := database.NewSqlServerConnection(cfg)
	if err != nil {
		slog.Error("Error initializing database", "error", err)
		os.Exit(1)
	}
	defer database.Close(db)

	ratesRepo := repository.NewRatesRepository(db)

	scraperSvc := services.NewScraperService(cfg, ratesRepo)

	cron := workers.NewCron(cfg, scraperSvc)
	if err := cron.Start(); err != nil {
		slog.Error("Failed to start cron worker", "error", err)
		os.Exit(1)
	}

	go func() {
		defer func() {
			if r := recover(); r != nil {
				slog.Error("panic in startup scrape task",
					"panic", r,
					"stack", string(debug.Stack()),
				)
			}
		}()
		scraperSvc.RunScrapeTask("startup")
	}()

	server := internalHttp.NewServer(*cfg)

	routes.Register(server, ratesRepo, db)

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)

	addr := fmt.Sprintf(":%d", cfg.Port)
	slog.Info(fmt.Sprintf("INFO Server on Url 127.0.0.1:%d", cfg.Port))
	slog.Info(fmt.Sprintf("INFO PID: %d", os.Getpid()))
	slog.Info("Starting Fiber server in console mode...")

	go func() {
		listenCfg := fiber.ListenConfig{DisableStartupMessage: true}
		if err := server.Listen(addr, listenCfg); err != nil && !errors.Is(err, http.ErrServerClosed) {
			slog.Error("Failed to start server", "error", err)
			stop <- syscall.SIGTERM
		}
	}()

	<-stop
	slog.Info("Shutdown signal received")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	if err := server.ShutdownWithTimeout(10 * time.Second); err != nil {
		slog.Error("Fiber server shutdown failed", "error", err)
	} else {
		slog.Info("Fiber server shutdown completed")
	}

	if err := cron.Stop(shutdownCtx); err != nil {
		slog.Error("Cron worker shutdown failed", "error", err)
	}

	slog.Info("Service stopped gracefully")
}
