package main

import (
	"crypto/tls"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"scraperbcv/models"
	"strings"
	"time"

	"github.com/gocolly/colly/v2"
	"github.com/shopspring/decimal"
	"golang.org/x/sync/errgroup"
)

func scrapeCoins[T any](url string, searchString string, scraper func(*colly.HTMLElement) (T, error)) ([]T, error) {
	c := colly.NewCollector(
		colly.UserAgent("Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/124.0.0.0 Safari/537.36"),
		colly.AllowedDomains("www.banrep.gov.co", "banrep.gov.co", "bcv.org.ve", "www.bcv.org.ve"),
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

func scrapeTasaCambio() (*models.TasaCambio, error) {
	g := new(errgroup.Group)

	var pesos []models.Coin
	g.Go(func() error {
		var err error
		pesos, err = scrapeCoins("https://www.banrep.gov.co/es", ".indicator--trm", func(e *colly.HTMLElement) (models.Coin, error) {
			price := e.ChildAttr("#trm", "value")
			dateRaw := strings.TrimSpace(e.ChildText(".indicator__comment"))

			tm, err := time.Parse("02/01/2006", dateRaw)

			if err != nil {
				slog.Warn("Failed to parse date", "error", err, "raw_date", dateRaw)
				tm = time.Now().UTC()
			}

			valor, err := decimal.NewFromString(price)
			if err != nil {
				return models.Coin{}, fmt.Errorf("failed to parse pesos price: %w", err)
			}

			date := time.Date(
				tm.Year(), tm.Month(), tm.Day(), 0, 0, 0, 0, time.FixedZone("UTC", 0),
			)

			return models.Coin{
				Moneda:      "Pesos",
				Valor:       valor,
				Fecha:       time.Now().UTC(),
				Simbolo:     "$",
				FechaValida: date,
			}, nil
		})
		return err
	})

	var bcvCoins []models.Coin
	g.Go(func() error {
		var err error
		bcvCoins, err = scrapeCoins("https://bcv.org.ve/", "#dolar, #euro", func(e *colly.HTMLElement) (models.Coin, error) {
			name := strings.TrimSpace(e.ChildText("span"))
			priceRaw := strings.TrimSpace(e.ChildText(".centrado strong"))

			priceClean := strings.ReplaceAll(priceRaw, ".", "")
			priceClean = strings.ReplaceAll(priceClean, ",", ".")
			priceClean = strings.TrimSpace(priceClean)
			var fechaValida time.Time

			valor, err := decimal.NewFromString(priceClean)
			if err != nil {
				return models.Coin{}, fmt.Errorf("failed to parse %s price: %w", name, err)
			}

			fechaRaw := e.DOM.Closest(".views-row").Find(".date-display-single").AttrOr("content", "")

			if fechaRaw != "" {
				if t, err := time.Parse(time.RFC3339, fechaRaw); err == nil {
					fechaValida = time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
				}
			}

			if fechaValida.IsZero() {
				slog.Warn("Missing date in content, using current date")
				now := time.Now().UTC()
				fechaValida = time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
			}

			moneda := "Dolar"
			if strings.Contains(strings.ToLower(name), "eur") {
				moneda = "Euro"
			}

			return models.Coin{
				Moneda:      moneda,
				Valor:       valor,
				Fecha:       time.Now().UTC(),
				Simbolo:     "",
				FechaValida: fechaValida,
			}, nil
		})
		return err
	})

	if waitErr := g.Wait(); waitErr != nil {
		slog.Error("Scrape group failed", "error", waitErr)
		return nil, waitErr
	}

	if len(pesos) == 0 {
		return nil, fmt.Errorf("no pesos data scraped")
	}
	if len(bcvCoins) == 0 {
		return nil, fmt.Errorf("no BCV data scraped")
	}

	var tasa models.TasaCambio

	for _, c := range bcvCoins {
		switch strings.ToLower(c.Moneda) {
		case "dolar":
			tasa.Dolar = c
			tasa.Dolar.Simbolo = "$"
		case "euro":
			tasa.Euro = c
			tasa.Euro.Simbolo = "€"
		}
	}

	if len(pesos) > 0 {
		c := pesos[0]
		tasa.Pesos = c
		tasa.Pesos.Simbolo = "$"
	}

	if tasa.Dolar.Moneda == "" || tasa.Euro.Moneda == "" || tasa.Pesos.Moneda == "" {
		return nil, fmt.Errorf("missing currency data in scrape result")
	}

	slog.Info("Scrape completed", "pesos_count", len(pesos), "bcv_count", len(bcvCoins))
	return &tasa, nil
}
