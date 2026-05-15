//go:build windows
// +build windows

package main

import (
	"log/slog"

	"github.com/gofiber/fiber/v3"
	"golang.org/x/sys/windows/svc"
)

type windowsServiceHandler struct {
	server *fiber.App
	port   string
}

func (h *windowsServiceHandler) Execute(args []string, r <-chan svc.ChangeRequest, s chan<- svc.Status) (bool, uint32) {
	const cmdsAccepted = svc.AcceptStop | svc.AcceptShutdown

	s <- svc.Status{State: svc.StartPending}

	go func() {
		if err := h.server.Listen(":"+h.port, fiber.ListenConfig{DisableStartupMessage: true}); err != nil {
			slog.Error("server.Listen failed", "error", err)
		}
	}()

	s <- svc.Status{State: svc.Running, Accepts: cmdsAccepted}

	for c := range r {
		switch c.Cmd {
		case svc.Interrogate:
			s <- c.CurrentStatus
		case svc.Stop, svc.Shutdown:
			s <- svc.Status{State: svc.StopPending}
			_ = h.server.Shutdown()
			return false, 0
		default:
			slog.Warn("Unhandled windows service command", "cmd", c.Cmd)
		}
	}

	return false, 0
}

func RunServer(server *fiber.App, port string) error {
	isService, err := svc.IsWindowsService()
	if err != nil {
		slog.Warn("failed to determine execution environment", "error", err)
	}

	if !isService {
		slog.Info("Running in interactive console mode", "port", port)
		return server.Listen(":"+port, fiber.ListenConfig{DisableStartupMessage: true})
	}

	slog.Info("Running as Windows Service", "port", port)
	handler := &windowsServiceHandler{server: server, port: port}
	svcName := "BcvScraperService"

	if err := svc.Run(svcName, handler); err != nil {
		slog.Error("Failed to start service loop", "error", err)
		return err
	}
	return nil
}
