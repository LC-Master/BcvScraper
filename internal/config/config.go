package config

import (
	"log/slog"
	"net/url"
	"os"
	"strconv"
	"time"

	"github.com/joho/godotenv"
)

type Config struct {
	AppName            string
	AppURL             string
	Port               int
	ServerReadTimeout  time.Duration
	ServerWriteTimeout time.Duration
	ServerIdleTimeout  time.Duration
	LogFile            LogFileConfig
	SqlServer          SqlServerConfig
	Scraper            ScraperConfig
	CorsOrigins        string
}

type ScraperConfig struct {
	CronSpec    string
	MaxAttempts int
	BaseDelay   time.Duration
	MaxDelay    time.Duration
}

type LogFileConfig struct {
	FileName   string
	MaxSizeMB  int
	MaxBackups int
	MaxAge     int
	Level      string
	Compress   bool
}

type SqlServerConfig struct {
	Host                   string
	Port                   string
	User                   string
	Password               string
	Database               string
	Encrypt                string
	TrustServerCertificate string
	ConnectionTimeout      string
	ReconnectionAttempts   int
}

func LoadConfig() (*Config, error) {
	if err := godotenv.Load(); err != nil {
		slog.Info("No .env file found, reading from system environment")
	}
	cfg := &Config{
		AppName:            getEnv("APP_NAME", "facturacion-sap"),
		Port:               getEnv("PORT", 8080),
		AppURL:             getEnv("APP_URL", "http://localhost:3000"),
		ServerReadTimeout:  getEnv("SERVER_READ_TIMEOUT", 10*time.Second),
		ServerWriteTimeout: getEnv("SERVER_WRITE_TIMEOUT", 20*time.Second),
		ServerIdleTimeout:  getEnv("SERVER_IDLE_TIMEOUT", 30*time.Second),
		Scraper: ScraperConfig{
			CronSpec:    getEnv("CRON_SPEC", "0 1 * * *"),
			MaxAttempts: getEnv("SCRAPER_MAX_ATTEMPTS", 3),
			BaseDelay:   getEnv("SCRAPER_BASE_DELAY", 1*time.Second),
			MaxDelay:    getEnv("SCRAPER_MAX_DELAY", 5*time.Second),
		},
		LogFile: LogFileConfig{
			FileName:   getEnv("LOG_FILE_NAME", "logs/app.log"),
			MaxSizeMB:  getEnv("LOG_MAX_SIZE_MB", 10),
			MaxBackups: getEnv("LOG_MAX_BACKUPS", 3),
			MaxAge:     getEnv("LOG_MAX_AGE", 7),
			Compress:   getEnv("LOG_COMPRESS", true),
			Level:      getEnv("LOG_LEVEL", "info"),
		},
		CorsOrigins: getEnv("CORS_ORIGINS", "http://localhost:3000"),
		SqlServer: SqlServerConfig{
			Host:                   getEnv("SQLSERVER_HOST", "localhost"),
			Port:                   getEnv("SQLSERVER_PORT", "1433"),
			User:                   getEnv("SQLSERVER_USER", "sa"),
			Password:               getEnv("SQLSERVER_PASSWORD", ""),
			Database:               getEnv("SQLSERVER_DATABASE", ""),
			Encrypt:                getEnv("SQLSERVER_ENCRYPT", "true"),
			TrustServerCertificate: getEnv("SQLSERVER_TRUST_SERVER_CERTIFICATE", "false"),
			ConnectionTimeout:      getEnv("SQLSERVER_CONNECTION_TIMEOUT", "30"),
			ReconnectionAttempts:   getEnv("SQLSERVER_RECONNECTION_ATTEMPTS", 3),
		},
	}
	return cfg, nil
}

func getEnv[T string | int | bool | url.URL | time.Duration](key string, defaultValue T) T {
	value := os.Getenv(key)
	if value == "" {
		return defaultValue
	}
	switch any(defaultValue).(type) {
	case string:
		return any(value).(T)
	case int:
		i, err := strconv.Atoi(value)
		if err != nil {
			return defaultValue
		}
		return any(i).(T)
	case bool:
		b, err := strconv.ParseBool(value)
		if err != nil {
			return defaultValue
		}
		return any(b).(T)
	case url.URL:
		u, err := url.Parse(value)
		if err != nil {
			return defaultValue
		}
		return any(*u).(T)
	case time.Duration:
		d, err := time.ParseDuration(value)
		if err != nil {
			return defaultValue
		}
		return any(d).(T)
	default:
		return defaultValue
	}
}
