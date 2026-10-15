package database

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"scraperbcv/internal/config"
	"time"

	"gorm.io/driver/sqlserver"
	"gorm.io/gorm"
)

func BuildConnString(host, port, user, password, dbName, encrypt, trustCert, connTimeout string) string {
	u := &url.URL{
		Scheme: "sqlserver",
		User:   url.UserPassword(user, password),
		Host:   fmt.Sprintf("%s:%s", host, port),
	}

	q := url.Values{
		"database":               []string{dbName},
		"encrypt":                []string{encrypt},
		"TrustServerCertificate": []string{trustCert},
		"connection timeout":     []string{connTimeout},
	}

	u.RawQuery = q.Encode()
	return u.String()
}

func NewSqlServerConnection(cfg *config.Config) (*gorm.DB, error) {
	dsn := BuildConnString(
		cfg.SqlServer.Host,
		cfg.SqlServer.Port,
		cfg.SqlServer.User,
		cfg.SqlServer.Password,
		cfg.SqlServer.Database,
		cfg.SqlServer.Encrypt,
		cfg.SqlServer.TrustServerCertificate,
		cfg.SqlServer.ConnectionTimeout,
	)
	ctx := context.Background()
	maxAttempts := cfg.SqlServer.ReconnectionAttempts
	backoff := 2 * time.Second

	for attempt := 1; attempt <= maxAttempts; attempt++ {
		db, err := gorm.Open(sqlserver.Open(dsn), &gorm.Config{})
		if err == nil {
			if sqlDB, errCtx := db.DB(); errCtx == nil {
				if err = sqlDB.Ping(); err == nil {
					slog.Info("Database connection established successfully")
					sqlDB.SetMaxIdleConns(10)
					sqlDB.SetMaxOpenConns(100)
					sqlDB.SetConnMaxLifetime(time.Hour)
					sqlDB.SetConnMaxIdleTime(15 * time.Minute)

					return db, nil
				}
			}
		}
		slog.Warn("Failed to connect to database, retrying...",
			"attempt", attempt,
			"max_attempts", maxAttempts,
			"backoff_delay", backoff,
			"error", err,
		)
		if attempt == maxAttempts {
			break
		}
		select {
		case <-ctx.Done():
			return nil, fmt.Errorf("database connection aborted due to context cancellation: %w", ctx.Err())
		case <-time.After(backoff):
			backoff *= 2
		}

	}

	return nil, errors.New("failed to connect to database after multiple attempts")
}

func Close(db *gorm.DB) error {
	sqlDB, err := db.DB()
	if err != nil {
		return fmt.Errorf("error al obtener la conexion nativa de gorm: %w", err)
	}

	if err := sqlDB.Close(); err != nil {
		return fmt.Errorf("error al cerrar el pool de conexiones: %w", err)
	}

	return nil
}

func Ping(db *gorm.DB) error {
	if db == nil {
		return fmt.Errorf("database connection is nil")
	}

	var one int
	if err := db.Raw("SELECT 1").Scan(&one).Error; err != nil {
		return err
	}
	return nil
}
