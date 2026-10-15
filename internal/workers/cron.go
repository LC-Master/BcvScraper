package workers

import (
	"context"
	"fmt"
	"log/slog"
	"scraperbcv/internal/config"
	"scraperbcv/internal/services"

	"github.com/robfig/cron/v3"
)

type CronWorker struct {
	cron           *cron.Cron
	cfg            *config.Config
	scraperService *services.ScraperService
}

func NewCron(cfg *config.Config, scraperService *services.ScraperService) *CronWorker {
	return &CronWorker{
		cron:           cron.New(cron.WithChain(cron.Recover(cron.DefaultLogger))),
		cfg:            cfg,
		scraperService: scraperService,
	}
}

func (c *CronWorker) Start() error {
	_, err := c.cron.AddFunc(c.cfg.Scraper.CronSpec, func() {
		c.scraperService.RunScrapeTask("cron")
	})
	if err != nil {
		return fmt.Errorf("failed to schedule scraper job: %w", err)
	}

	c.cron.Start()
	slog.Info("Scheduler started", "schedule", c.cfg.Scraper.CronSpec)
	return nil
}

func (c *CronWorker) Stop(ctx context.Context) error {
	slog.Info("Scheduler stopping")
	stopCtx := c.cron.Stop()

	select {
	case <-stopCtx.Done():
		slog.Info("All cron jobs finished gracefully")
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
