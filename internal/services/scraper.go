package services

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"scraperbcv/internal/config"
	"scraperbcv/internal/domain"
	"strings"
	"sync/atomic"
	"time"

	"github.com/gocolly/colly/v2"
	"github.com/shopspring/decimal"
)

type ResponseApiColombia struct {
	Valor         string `json:"valor"`
	Unidad        string `json:"unidad"`
	VigenciaDesde string `json:"vigenciadesde"`
	VigenciaHasta string `json:"vigenciahasta"`
}

type ScraperService struct {
	cfg              *config.Config
	repo             domain.RepositoryCoin
	scrapeInProgress atomic.Int32
}

func NewScraperService(cfg *config.Config, repo domain.RepositoryCoin) *ScraperService {
	return &ScraperService{cfg: cfg, repo: repo}
}

func (s *ScraperService) ScrapeCoins[T any](url string, searchString string, scraper func(*colly.HTMLElement) (T, error)) ([]T, error) {
	c := colly.NewCollector(
		colly.UserAgent("Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/124.0.0.0 Safari/537.36"),
		colly.AllowedDomains("bcv.org.ve", "www.bcv.org.ve"),
	)

	var coins []T
	var scrapeErr error

	c.SetRequestTimeout(30 * time.Second)

	allowInsecure := strings.EqualFold(os.Getenv("ALLOW_INSECURE_TLS"), "true")

	c.WithTransport(&http.Transport{
		Proxy: http.ProxyFromEnvironment,
		DialContext: (&net.Dialer{
			Timeout:   30 * time.Second,
			KeepAlive: 30 * time.Second,
		}).DialContext,
		TLSClientConfig: &tls.Config{InsecureSkipVerify: allowInsecure},
		IdleConnTimeout: 90 * time.Second,
	})

	c.OnRequest(func(r *colly.Request) {
		r.Headers.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,image/avif,image/webp,image/apng,*/*;q=0.8")
		r.Headers.Set("Accept-Language", "es-ES,es;q=0.9,en;q=0.8")
		r.Headers.Set("Referer", "https://www.google.com/")
		r.Headers.Set("Sec-Fetch-Dest", "document")
		r.Headers.Set("Sec-Fetch-Mode", "navigate")
		r.Headers.Set("Sec-Fetch-Site", "cross-site")
		r.Headers.Set("Upgrade-Insecure-Requests", "1")
	})

	c.OnResponse(func(r *colly.Response) {
		slog.Info("request completed", "status", r.StatusCode, "url", r.Request.URL.String())
	})

	c.OnError(func(r *colly.Response, err error) {
		if r != nil && r.Request != nil {
			slog.Error("request failed", "status", r.StatusCode, "url", r.Request.URL.String(), "error", err)
		} else {
			slog.Error("request failed", "error", err)
		}
	})

	c.OnHTML(searchString, func(e *colly.HTMLElement) {
		coin, err := scraper(e)
		if err != nil {
			if scrapeErr == nil {
				scrapeErr = err
			}
			slog.Error("failed to parse coin", "url", url, "selector", searchString, "error", err)
			return
		}
		coins = append(coins, coin)
	})

	err := c.Visit(url)
	if err != nil {
		slog.Error("failed to visit url", "error", err)
		return nil, err
	}

	if scrapeErr != nil {
		return coins, scrapeErr
	}

	if len(coins) == 0 {
		return nil, fmt.Errorf("no results for selector: %s", searchString)
	}

	return coins, nil
}

func fetchPesosColombia() (domain.Coin, error) {
	client := &http.Client{Timeout: 15 * time.Second}
	res, err := client.Get("https://www.datos.gov.co/resource/32sa-8pi3.json?$limit=1&$order=vigenciahasta%20DESC")
	if err != nil {
		return domain.Coin{}, fmt.Errorf("failed to fetch pesos from api: %w", err)
	}
	defer res.Body.Close()

	if res.StatusCode != http.StatusOK {
		return domain.Coin{}, fmt.Errorf("unexpected status code from colombia api: %d", res.StatusCode)
	}

	var data []ResponseApiColombia
	if err := json.NewDecoder(res.Body).Decode(&data); err != nil {
		return domain.Coin{}, fmt.Errorf("failed to decode pesos response: %w", err)
	}

	if len(data) == 0 {
		return domain.Coin{}, errors.New("empty response from colombia api")
	}

	item := data[0]
	value, err := decimal.NewFromString(item.Valor)
	if err != nil {
		return domain.Coin{}, fmt.Errorf("failed to parse colombia valor: %w", err)
	}
	value = value.Truncate(4)

	dateStr := item.VigenciaDesde
	if dateStr == "" {
		dateStr = item.VigenciaHasta
	}

	var tm time.Time
	for _, layout := range []string{"2006-01-02T15:04:05.000", "2006-01-02T15:04:05", "2006-01-02"} {
		if t, err := time.Parse(layout, dateStr); err == nil {
			tm = t
			break
		}
	}

	if tm.IsZero() {
		slog.Warn("Failed to parse date from colombia api, using UTC now", "raw_date", dateStr)
		tm = time.Now().UTC()
	}

	validDate := time.Date(
		tm.Year(), tm.Month(), tm.Day(), 0, 0, 0, 0, time.FixedZone("UTC", 0),
	)

	return domain.Coin{
		Moneda:      "Pesos",
		Valor:       value,
		Fecha:       time.Now().Local(),
		Simbolo:     "$",
		FechaValida: validDate,
	}, nil
}

func (s *ScraperService) scrapeBCV() ([]domain.Coin, error) {
	coins, err := s.ScrapeCoins("https://bcv.org.ve/", "#dolar, #euro", func(e *colly.HTMLElement) (domain.Coin, error) {
		name := strings.TrimSpace(e.ChildText("span"))
		priceRaw := strings.TrimSpace(e.ChildText(".centrado strong"))

		priceClean := strings.ReplaceAll(priceRaw, ".", "")
		priceClean = strings.ReplaceAll(priceClean, ",", ".")
		priceClean = strings.TrimSpace(priceClean)
		var fechaValida time.Time

		valor, err := decimal.NewFromString(priceClean)
		if err != nil {
			return domain.Coin{}, fmt.Errorf("failed to parse %s price: %w", name, err)
		}

		valor = valor.Truncate(4)

		fechaRaw := e.DOM.Closest(".views-row").Find(".date-display-single").AttrOr("content", "")

		if fechaRaw != "" {
			if t, err := time.Parse(time.RFC3339, fechaRaw); err == nil {
				fechaValida = time.Date(t.Year(), t.Month(), t.Day(), t.Hour(), t.Minute(), t.Second(), 0, time.UTC)
			}
		}

		if fechaValida.IsZero() {
			slog.Warn("Missing date in content, using current date")
			now := time.Now().UTC()
			fechaValida = time.Date(now.Year(), now.Month(), now.Day(), now.Hour(), now.Minute(), now.Second(), 0, time.UTC)
		}

		nameClean := strings.ToLower(name)
		var moneda string
		var simbolo string

		if strings.Contains(nameClean, "eur") {
			moneda = "Euro"
			simbolo = "€"
		} else if strings.Contains(nameClean, "dol") || strings.Contains(nameClean, "usd") {
			moneda = "Dolar"
			simbolo = "$"
		} else {
			return domain.Coin{}, fmt.Errorf("moneda no identificada de forma segura en el texto: %s", name)
		}

		return domain.Coin{
			Moneda:      moneda,
			Valor:       valor,
			Fecha:       time.Now().Local(),
			Simbolo:     simbolo,
			FechaValida: fechaValida,
		}, nil
	})

	if err != nil {
		return nil, err
	}

	if len(coins) == 0 {
		return nil, errors.New("no BCV data scraped")
	}

	return coins, nil
}

func (s *ScraperService) scrapeTasaCambio() (*domain.TasaCambio, error) {
	var tasa domain.TasaCambio

	bcvCoins, bcvErr := s.scrapeBCV()
	if bcvErr != nil {
		slog.Error("Failed to scrape BCV", "error", bcvErr)
	} else {
		for _, c := range bcvCoins {
			switch strings.ToLower(c.Moneda) {
			case "dolar":
				tasa.Dolar = c
			case "euro":
				tasa.Euro = c
			}
		}
	}

	pesos, pesosErr := fetchPesosColombia()
	if pesosErr != nil {
		slog.Error("Failed to fetch Colombia TRM", "error", pesosErr)
	} else {
		tasa.Pesos = pesos
	}

	if bcvErr != nil && pesosErr != nil {
		return nil, fmt.Errorf("both BCV and Colombia failed: bcv=%v, pesos=%v", bcvErr, pesosErr)
	}

	return &tasa, nil
}
func (s *ScraperService) RunScrapeTask(trigger string) {
	if !s.scrapeInProgress.CompareAndSwap(0, 1) {
		slog.Warn("Scrape skipped; another scrape is running", "trigger", trigger)
		return
	}
	defer s.scrapeInProgress.Store(0)

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

	err := withRetry("scrape_and_save", s.cfg.Scraper.MaxAttempts, s.cfg.Scraper.BaseDelay, s.cfg.Scraper.MaxDelay, func() error {
		if !bcvDone {
			bcvCoins, err := s.scrapeBCV()
			if err != nil {
				slog.Error("Failed to scrape BCV", "error", err)
			} else {
				saveFailed := false
				for _, coin := range bcvCoins {
					if err := s.repo.SaveCoin(context.Background(), coin); err != nil {
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
				if err := s.repo.SaveCoin(context.Background(), pesos); err != nil {
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
