//go:build !windows
// +build !windows

package main

import (
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/gofiber/fiber/v3"
)

func RunServer(server *fiber.App, port string) error {
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGTERM)

	errCh := make(chan error, 1)
	go func() {
		errCh <- server.Listen(":"+port, fiber.ListenConfig{DisableStartupMessage: true})
	}()

	select {
	case sig := <-stop:
		slog.Info("Shutdown signal received", "signal", sig)
		if err := server.Shutdown(); err != nil {
			slog.Error("Server shutdown failed", "error", err)
		}
		return nil
	case err := <-errCh:
		if err != nil {
			slog.Error("Server failed", "error", err)
		}
		return err
	}
}
