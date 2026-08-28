package main

import (
	"errors"
	"fmt"
	"log/slog"
	"net"
	"os"
	"os/signal"
	"strconv"
	"syscall"

	telemetryserver "github.com/juanchopalen/tallerp-telemetry/internal/server"
	"github.com/juanchopalen/tallerp-telemetry/internal/telemetry"
)

const defaultPort = 8899

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
	port, err := configuredPort(os.Getenv("TALLERP_TELEMETRY_PORT"))
	if err != nil {
		return err
	}
	logger := telemetry.NewLogger(os.Stdout, slog.LevelInfo)
	address := net.JoinHostPort("0.0.0.0", strconv.Itoa(port))
	listener, err := net.Listen("tcp", address)
	if err != nil {
		return fmt.Errorf("listen on %s: %w", address, err)
	}

	server := telemetryserver.New(telemetryserver.DefaultConfig(), logger)
	serveErrors := make(chan error, 1)
	go func() { serveErrors <- server.Serve(listener) }()
	logger.Info("telemetry server listening", "event", "server_listening", "address", address)

	signals := make(chan os.Signal, 1)
	signal.Notify(signals, syscall.SIGINT, syscall.SIGTERM)
	defer signal.Stop(signals)
	select {
	case receivedSignal := <-signals:
		logger.Info("telemetry server stopping", "event", "server_stopping", "signal", receivedSignal.String())
		if err := server.Close(); err != nil {
			return fmt.Errorf("close server: %w", err)
		}
		if err := <-serveErrors; err != nil && !errors.Is(err, net.ErrClosed) {
			return fmt.Errorf("serve TCP: %w", err)
		}
		return nil
	case err := <-serveErrors:
		_ = server.Close()
		if err != nil && !errors.Is(err, net.ErrClosed) {
			return fmt.Errorf("serve TCP: %w", err)
		}
		return nil
	}
}

func configuredPort(raw string) (int, error) {
	if raw == "" {
		return defaultPort, nil
	}
	port, err := strconv.Atoi(raw)
	if err != nil || port < 1 || port > 65535 {
		return 0, fmt.Errorf("TALLERP_TELEMETRY_PORT must be an integer from 1 to 65535")
	}
	return port, nil
}
