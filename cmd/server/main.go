package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	appconfig "github.com/juanchopalen/tallerp-telemetry/internal/config"
	"github.com/juanchopalen/tallerp-telemetry/internal/delivery"
	"github.com/juanchopalen/tallerp-telemetry/internal/health"
	telemetryserver "github.com/juanchopalen/tallerp-telemetry/internal/server"
	"github.com/juanchopalen/tallerp-telemetry/internal/spool"
	"github.com/juanchopalen/tallerp-telemetry/internal/telemetry"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
	configuration, err := appconfig.Load()
	if err != nil {
		return err
	}
	logger := telemetry.NewLogger(os.Stdout, slog.LevelInfo)
	queue, err := spool.Open(configuration.SpoolPath)
	if err != nil {
		return fmt.Errorf("open durable spool: %w", err)
	}
	defer func() {
		if queue != nil {
			_ = queue.Close()
		}
	}()
	metrics := &telemetry.Metrics{}
	serverConfig := telemetryserver.DefaultConfig()
	serverConfig.EventSink = queue
	serverConfig.Metrics = metrics
	server := telemetryserver.New(serverConfig, logger)
	address := fmt.Sprintf("0.0.0.0:%d", configuration.TelemetryPort)
	listener, err := net.Listen("tcp", address)
	if err != nil {
		return fmt.Errorf("listen on %s: %w", address, err)
	}
	healthAddress := fmt.Sprintf("0.0.0.0:%d", configuration.HealthPort)
	healthServer, err := health.New(healthAddress, server.Ready, queue)
	if err != nil {
		_ = listener.Close()
		return fmt.Errorf("listen health on %s: %w", healthAddress, err)
	}
	worker := delivery.New(delivery.Config{APIURL: configuration.APIURL, Token: configuration.TelemetryToken, BatchSize: configuration.DeliveryBatchSize, Interval: configuration.DeliveryInterval, Retention: configuration.SpoolRetention, CriticalPending: configuration.CriticalPending}, queue, &http.Client{Timeout: configuration.HTTPTimeout}, logger, metrics)
	worker.Start(context.Background())
	serveErrors := make(chan error, 1)
	go func() { serveErrors <- server.Serve(listener) }()
	healthErrors := make(chan error, 1)
	go func() { healthErrors <- healthServer.Serve() }()
	logger.Info("telemetry server listening", "event", "server_listening", "address", address)
	logger.Info("health server listening", "event", "health_listening", "address", healthAddress)

	signals := make(chan os.Signal, 1)
	signal.Notify(signals, syscall.SIGINT, syscall.SIGTERM)
	defer signal.Stop(signals)
	var runErr error
	select {
	case receivedSignal := <-signals:
		logger.Info("telemetry server stopping", "event", "server_stopping", "signal", receivedSignal.String())
	case err := <-serveErrors:
		if err != nil && !errors.Is(err, net.ErrClosed) {
			runErr = fmt.Errorf("serve TCP: %w", err)
		}
	case err := <-healthErrors:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			runErr = fmt.Errorf("serve health: %w", err)
		}
	}
	if err := server.Close(); err != nil && runErr == nil {
		runErr = fmt.Errorf("close server: %w", err)
	}
	shutdownContext, cancel := context.WithTimeout(context.Background(), max(15*time.Second, configuration.HTTPTimeout+5*time.Second))
	defer cancel()
	if err := worker.Shutdown(shutdownContext); err != nil && runErr == nil {
		runErr = fmt.Errorf("stop delivery worker: %w", err)
	}
	if err := healthServer.Shutdown(shutdownContext); err != nil && runErr == nil {
		runErr = fmt.Errorf("stop health server: %w", err)
	}
	closeSpoolErr := queue.Close()
	queue = nil
	if closeSpoolErr != nil && runErr == nil {
		runErr = fmt.Errorf("close durable spool: %w", closeSpoolErr)
	}
	return runErr
}
