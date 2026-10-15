package logger

import (
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"

	"scraperbcv/internal/config"

	"gopkg.in/natefinch/lumberjack.v2"
)

var defaultJack *lumberjack.Logger

func parseLogLevel(levelStr string) slog.Level {
	switch strings.ToLower(strings.TrimSpace(levelStr)) {
	case "debug":
		return slog.LevelDebug
	case "warn", "warning":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}

func NewLogger(cfg *config.Config) {
	jack := &lumberjack.Logger{
		Filename:   cfg.LogFile.FileName,
		MaxSize:    cfg.LogFile.MaxSizeMB,
		MaxBackups: cfg.LogFile.MaxBackups,
		MaxAge:     cfg.LogFile.MaxAge,
		Compress:   cfg.LogFile.Compress,
	}
	defaultJack = jack

	level := parseLogLevel(cfg.LogFile.Level)

	handlerOpts := &slog.HandlerOptions{
		Level:     level,
		AddSource: level == slog.LevelDebug,
	}

	output := io.MultiWriter(os.Stdout, jack)
	handler := slog.NewJSONHandler(output, handlerOpts)

	slog.SetDefault(slog.New(handler))
}

func Close() error {
	if defaultJack != nil {
		if err := defaultJack.Close(); err != nil {
			return fmt.Errorf("failed to close lumberjack logger: %w", err)
		}
	}
	return nil
}
