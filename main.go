package main

import (
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"scraperbcv/database"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/gofiber/fiber/v3"
	fiberrecover "github.com/gofiber/fiber/v3/middleware/recover"
	"github.com/joho/godotenv"
	"github.com/robfig/cron/v3"
	"gorm.io/gorm"
)

type App struct {
	db               *gorm.DB
	scrapeInProgress int32
}

const (
	scrapeMaxAttempts  = 5
	scrapeBaseDelay    = 5 * time.Second
	scrapeMaxDelay     = 1 * time.Minute
	dbConnectBaseDelay = 5 * time.Second
	dbConnectMaxDelay  = 1 * time.Minute
	serverReadTimeout  = 10 * time.Second
	serverWriteTimeout = 20 * time.Second
	serverIdleTimeout  = 30 * time.Second
)

func main() {
	defer func() {
		if r := recover(); r != nil {
			slog.Error("panic recovered in main", "panic", r)
		}
	}()

	exePath, err := os.Executable()
	if err == nil {
		importPath := filepath.Dir(exePath)
		_ = os.Chdir(importPath)
	}

	var logFile *os.File
	var logWriter io.Writer = os.Stdout
	if f, ferr := os.OpenFile("app.log", os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644); ferr == nil {
		logFile = f
		logWriter = io.MultiWriter(os.Stdout, logFile)
		defer func() { _ = logFile.Close() }()
	}

	logger := slog.New(slog.NewJSONHandler(logWriter, &slog.HandlerOptions{}))
	slog.SetDefault(logger)

	err = godotenv.Load()
	if err != nil {
		slog.Warn("Failed to load .env file", "error", err)
	}

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
		slog.Info("PORT not set, using default", "port", port)
	}

	cronSpec := os.Getenv("CRON_SPEC")
	if cronSpec == "" {
		cronSpec = "0 1 * * *"
		slog.Info("CRON_SPEC not set, using default", "spec", cronSpec)
	}

	connString := loadConnStringWithRetry()
	db := connectWithRetry(connString)

	app := &App{db: db}

	cron := cron.New()

	_, err = cron.AddFunc(cronSpec, func() {
		runScrapeTask(app, "cron")
	})

	if err != nil {
		slog.Error("Failed to add cron job", "error", err)
	}

	cron.Start()
	defer cron.Stop()
	slog.Info("Scheduler started", "schedule", cronSpec)

	go runScrapeTask(app, "startup")

	server := fiber.New(fiber.Config{
		ReadTimeout:  serverReadTimeout,
		WriteTimeout: serverWriteTimeout,
		IdleTimeout:  serverIdleTimeout,
	})
	server.Use(fiberrecover.New())

	server.Get("/healthz", func(c fiber.Ctx) error {
		return c.SendStatus(fiber.StatusOK)
	})

	server.Get("/readyz", func(c fiber.Ctx) error {
		if err := database.Ping(app.db); err != nil {
			slog.Warn("Readiness check failed", "error", err)
			return c.Status(fiber.StatusServiceUnavailable).JSON(fiber.Map{
				"status": "not_ready",
			})
		}
		return c.JSON(fiber.Map{
			"status": "ready",
		})
	})

	server.Get("/tasa-cambio", func(c fiber.Ctx) error {
		tasa, err := database.GetUltimaTasaCambio(app.db)
		if err != nil {
			slog.Error("Failed to fetch latest rates", "error", err)
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
				"error": "failed to fetch latest rates",
			})
		}

		return c.JSON(tasa)
	})

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)

	go func() {
		<-stop
		slog.Info("Shutdown signal received")
		if err := server.Shutdown(); err != nil {
			slog.Error("Server shutdown failed", "error", err)
		}
	}()

	slog.Info(fmt.Sprintf("INFO Server on Url 127.0.0.1:%s", port))
	slog.Info(fmt.Sprintf("INFO PID: %d", os.Getpid()))

	if err := RunServer(server, port); err != nil {
		slog.Error("Failed to start server", "error", err)
	}
}

func runScrapeTask(app *App, trigger string) {
	if !atomic.CompareAndSwapInt32(&app.scrapeInProgress, 0, 1) {
		slog.Warn("Scrape skipped; another scrape is running", "trigger", trigger)
		return
	}
	defer atomic.StoreInt32(&app.scrapeInProgress, 0)

	defer func() {
		if r := recover(); r != nil {
			slog.Error("panic recovered in scrape task", "trigger", trigger, "panic", r)
		}
	}()

	slog.Info("Scrape cycle started", "trigger", trigger)

	err := withRetry("scrape_and_save", scrapeMaxAttempts, scrapeBaseDelay, scrapeMaxDelay, func() error {
		tasa, err := scrapeTasaCambio()
		if err != nil {
			return err
		}

		return database.SaveTasaCambio(app.db, *tasa)
	})

	if err != nil {
		slog.Error("Scrape cycle failed after retries", "trigger", trigger, "error", err)
		return
	}

	slog.Info("Scrape cycle completed", "trigger", trigger)
}

func loadConnStringWithRetry() string {
	var connString string

	_ = withRetry("load_config", 0, dbConnectBaseDelay, dbConnectMaxDelay, func() error {
		if err := godotenv.Load(); err != nil {
			slog.Warn("Failed to load .env file", "error", err)
		}

		connString = os.Getenv("DB_CONNECTION_STRING")
		if connString == "" {
			return errors.New("DB_CONNECTION_STRING is not set")
		}

		return nil
	})

	return connString
}

func connectWithRetry(connString string) *gorm.DB {
	var db *gorm.DB

	_ = withRetry("db_connect", 0, dbConnectBaseDelay, dbConnectMaxDelay, func() error {
		var err error
		db, err = database.InitDB(connString)
		return err
	})

	slog.Info("Database connection established")
	return db
}

func withRetry(operation string, maxAttempts int, baseDelay, maxDelay time.Duration, fn func() error) error {
	var err error

	for attempt := 1; maxAttempts <= 0 || attempt <= maxAttempts; attempt++ {
		err = fn()
		if err == nil {
			if attempt > 1 {
				slog.Info("Operation succeeded after retries", "operation", operation, "attempt", attempt)
			}
			return nil
		}

		if maxAttempts > 0 && attempt >= maxAttempts {
			break
		}

		wait := backoff(baseDelay, maxDelay, attempt)
		slog.Warn("Operation failed, retrying", "operation", operation, "attempt", attempt, "error", err, "wait", wait.String())
		time.Sleep(wait)
	}

	return err
}

func backoff(baseDelay, maxDelay time.Duration, attempt int) time.Duration {
	if attempt <= 1 {
		return baseDelay
	}

	delay := baseDelay
	for i := 1; i < attempt; i++ {
		delay *= 2
		if delay >= maxDelay {
			return maxDelay
		}
	}

	if delay > maxDelay {
		return maxDelay
	}

	return delay
}
