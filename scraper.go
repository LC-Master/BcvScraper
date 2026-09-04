package main

import (
	"crypto/tls"
	"encoding/json"
	"errors"
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
)

type ResponseApiColombia struct {
	Valor         string `json:"valor"`
	Unidad        string `json:"unidad"`
	VigenciaDesde string `json:"vigenciadesde"`
	VigenciaHasta string `json:"vigenciahasta"`
}

func scrapeCoins[T any](url string, searchString string, scraper func(*colly.HTMLElement) (T, error)) ([]T, error) {
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

func fetchPesosColombia() (models.Coin, error) {
	client := &http.Client{Timeout: 15 * time.Second}
	res, err := client.Get("https://www.datos.gov.co/resource/32sa-8pi3.json?$limit=1&$order=vigenciahasta%20DESC")
	if err != nil {
		return models.Coin{}, fmt.Errorf("failed to fetch pesos from api: %w", err)
	}
	defer res.Body.Close()

	if res.StatusCode != http.StatusOK {
		return models.Coin{}, fmt.Errorf("unexpected status code from colombia api: %d", res.StatusCode)
	}

	var data []ResponseApiColombia
	if err := json.NewDecoder(res.Body).Decode(&data); err != nil {
		return models.Coin{}, fmt.Errorf("failed to decode pesos response: %w", err)
	}

	if len(data) == 0 {
		return models.Coin{}, errors.New("empty response from colombia api")
	}

	item := data[0]
	value, err := decimal.NewFromString(item.Valor)
	if err != nil {
		return models.Coin{}, fmt.Errorf("failed to parse colombia valor: %w", err)
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

	return models.Coin{
		Moneda:      "Pesos",
		Valor:       value,
		Fecha:       time.Now().Local(),
		Simbolo:     "$",
		FechaValida: validDate,
	}, nil
}

func scrapeBCV() ([]models.Coin, error) {
	coins, err := scrapeCoins("https://bcv.org.ve/", "#dolar, #euro", func(e *colly.HTMLElement) (models.Coin, error) {
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
			return models.Coin{}, fmt.Errorf("moneda no identificada de forma segura en el texto: %s", name)
		}

		return models.Coin{
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

func scrapeTasaCambio() (*models.TasaCambio, error) {
	var tasa models.TasaCambio

	bcvCoins, bcvErr := scrapeBCV()
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
