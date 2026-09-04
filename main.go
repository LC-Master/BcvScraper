package main

import (
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"scraperbcv/database"
	"strings"
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
	scrapeInProgress atomic.Int32
}

const (
	scrapeMaxAttempts  = 15
	scrapeBaseDelay    = 5 * time.Second
	scrapeMaxDelay     = 1 * time.Minute
	dbConnectBaseDelay = 5 * time.Second
	dbConnectMaxDelay  = 1 * time.Minute
	serverReadTimeout  = 10 * time.Second
	serverWriteTimeout = 20 * time.Second
	serverIdleTimeout  = 30 * time.Second
)

func initLogger() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{}))
	slog.SetDefault(logger)
}

func main() {
	initLogger()

	slog.Info("=== BCV SCRAPER SERVICE STARTED VIA WINSW ===")

	defer func() {
		if r := recover(); r != nil {
			slog.Error("panic recovered in main", "panic", r)
		}
	}()

	err := godotenv.Load()
	if err != nil {
		slog.Warn("Failed to load .env file, attempting default load", "error", err)
		_ = godotenv.Load()
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

	db := connectWithRetry()

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

	slog.Info("Starting Fiber server in console mode...")
	if err := server.Listen(":"+port, fiber.ListenConfig{DisableStartupMessage: true}); err != nil {
		slog.Error("Failed to start server", "error", err)
	}
}

func runScrapeTask(app *App, trigger string) {
	if !app.scrapeInProgress.CompareAndSwap(0, 1) {
		slog.Warn("Scrape skipped; another scrape is running", "trigger", trigger)
		return
	}
	defer app.scrapeInProgress.Store(0)

	defer func() {
		if r := recover(); r != nil {
			slog.Error("panic recovered in scrape task", "trigger", trigger, "panic", r)
		}
	}()

	slog.Info("Scrape cycle started", "trigger", trigger)

	var (
		bcvDone      bool
		colombiaDone bool
	)

	err := withRetry("scrape_and_save", scrapeMaxAttempts, scrapeBaseDelay, scrapeMaxDelay, func() error {
		if !bcvDone {
			bcvCoins, err := scrapeBCV()
			if err != nil {
				slog.Error("Failed to scrape BCV", "error", err)
			} else {
				saveFailed := false
				for _, coin := range bcvCoins {
					if err := database.SaveCoin(app.db, coin); err != nil {
						slog.Error("Failed to save BCV rate to database", "moneda", coin.Moneda, "error", err)
						saveFailed = true
					}
				}
				if !saveFailed {
					bcvDone = true
					slog.Info("BCV rates successfully obtained and saved")
				}
			}
		}

		if !colombiaDone {
			pesos, err := fetchPesosColombia()
			if err != nil {
				slog.Error("Failed to fetch Colombia TRM", "error", err)
			} else {
				if err := database.SaveCoin(app.db, pesos); err != nil {
					slog.Error("Failed to save Colombia TRM to database", "error", err)
				} else {
					colombiaDone = true
					slog.Info("Colombia TRM successfully obtained and saved")
				}
			}
		}

		if bcvDone && colombiaDone {
			return nil
		}

		var pendientes []string
		if !bcvDone {
			pendientes = append(pendientes, "BCV")
		}
		if !colombiaDone {
			pendientes = append(pendientes, "Colombia")
		}

		return fmt.Errorf("tasas incompletas (pendientes: %s); reintentando", strings.Join(pendientes, ", "))
	})

	if err != nil {
		slog.Error("Scrape cycle failed after retries", "trigger", trigger, "error", err)
		return
	}

	slog.Info("Scrape cycle completed: all rates saved", "trigger", trigger)
}

func connectWithRetry() *gorm.DB {
	var db *gorm.DB

	_ = withRetry("db_connect", 0, dbConnectBaseDelay, dbConnectMaxDelay, func() error {
		var err error
		db, err = database.InitDB()
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
