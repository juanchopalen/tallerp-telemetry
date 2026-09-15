package delivery

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"math/rand/v2"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/juanchopalen/tallerp-telemetry/internal/spool"
	"github.com/juanchopalen/tallerp-telemetry/internal/telemetry"
)

const UserAgent = "TallERP-Telemetry/2"

type Config struct {
	APIURL          string
	Token           string
	BatchSize       int
	Interval        time.Duration
	Retention       time.Duration
	CriticalPending int64
}

type Worker struct {
	config  Config
	spool   *spool.Spool
	client  *http.Client
	logger  *slog.Logger
	metrics *telemetry.Metrics
	now     func() time.Time
	jitter  func(time.Duration) time.Duration
	cancel  context.CancelFunc
	done    chan struct{}
	once    sync.Once
}

type response struct {
	Accepted   []string `json:"accepted"`
	Duplicates []string `json:"duplicates"`
	Rejected   []string `json:"rejected"`
}

func New(config Config, queue *spool.Spool, client *http.Client, logger *slog.Logger, metrics *telemetry.Metrics) *Worker {
	if config.BatchSize <= 0 {
		config.BatchSize = 100
	}
	if config.Interval <= 0 {
		config.Interval = 5 * time.Second
	}
	if config.Retention <= 0 {
		config.Retention = 24 * time.Hour
	}
	if config.CriticalPending <= 0 {
		config.CriticalPending = 100000
	}
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Second}
	}
	if logger == nil {
		logger = slog.Default()
	}
	if metrics == nil {
		metrics = &telemetry.Metrics{}
	}
	return &Worker{config: config, spool: queue, client: client, logger: logger, metrics: metrics, now: func() time.Time { return time.Now().UTC() }, jitter: randomJitter, done: make(chan struct{})}
}

func (w *Worker) Start(parent context.Context) {
	w.once.Do(func() {
		ctx, cancel := context.WithCancel(parent)
		w.cancel = cancel
		go w.run(ctx)
	})
}

func (w *Worker) Shutdown(ctx context.Context) error {
	if w.cancel == nil {
		return nil
	}
	w.cancel()
	select {
	case <-w.done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (w *Worker) run(ctx context.Context) {
	defer close(w.done)
	ticker := time.NewTicker(w.config.Interval)
	defer ticker.Stop()
	maintenance := time.NewTicker(time.Minute)
	defer maintenance.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			w.deliverOnce(context.Background())
		case <-maintenance.C:
			w.maintain(context.Background())
		}
	}
}

func (w *Worker) deliverOnce(ctx context.Context) {
	now := w.now()
	items, err := w.spool.Pending(ctx, w.config.BatchSize, now)
	if err != nil {
		w.failure("read_pending", err)
		return
	}
	if len(items) == 0 {
		return
	}
	events := make([]telemetry.TelemetryEvent, len(items))
	ids := make([]string, len(items))
	maxAttempts := 0
	for index, item := range items {
		events[index] = item.Event
		ids[index] = item.Event.EventID
		if item.Attempts > maxAttempts {
			maxAttempts = item.Attempts
		}
	}

	result, err := w.post(ctx, events)
	if err != nil {
		next := now.Add(w.jitter(Backoff(maxAttempts + 1)))
		if markErr := w.spool.MarkRetry(ctx, ids, next, err.Error()); markErr != nil {
			w.failure("mark_retry", markErr)
		}
		w.failure("request", err)
		return
	}
	deliveredSet := make(map[string]struct{}, len(result.Accepted)+len(result.Duplicates))
	for _, id := range append(result.Accepted, result.Duplicates...) {
		deliveredSet[id] = struct{}{}
	}
	delivered := make([]string, 0, len(deliveredSet))
	remaining := make([]string, 0)
	for _, id := range ids {
		if _, ok := deliveredSet[id]; ok {
			delivered = append(delivered, id)
		} else {
			remaining = append(remaining, id)
		}
	}
	if err := w.spool.MarkDelivered(ctx, delivered, now); err != nil {
		w.failure("mark_delivered", err)
		return
	}
	if len(remaining) > 0 {
		next := now.Add(w.jitter(Backoff(maxAttempts + 1)))
		message := "event was not accepted (rejected or absent from response)"
		if err := w.spool.MarkRetry(ctx, remaining, next, message); err != nil {
			w.failure("mark_partial_retry", err)
		}
	}
	w.metrics.DeliverySuccess.Add(uint64(len(delivered)))
	w.logger.Info("telemetry delivery completed", "event", "delivery_success", "delivered", len(delivered), "pending_retry", len(remaining))
}

func (w *Worker) post(ctx context.Context, events []telemetry.TelemetryEvent) (response, error) {
	payload, err := json.Marshal(struct {
		Events []telemetry.TelemetryEvent `json:"events"`
	}{events})
	if err != nil {
		return response{}, err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, w.config.APIURL+"/api/internal/telemetry/events", bytes.NewReader(payload))
	if err != nil {
		return response{}, err
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "application/json")
	request.Header.Set("User-Agent", UserAgent)
	if w.config.Token != "" {
		request.Header.Set("Authorization", "Bearer "+w.config.Token)
	}
	httpResponse, err := w.client.Do(request)
	if err != nil {
		return response{}, err
	}
	defer httpResponse.Body.Close()
	body, readErr := io.ReadAll(io.LimitReader(httpResponse.Body, 1<<20))
	if readErr != nil {
		return response{}, readErr
	}
	if httpResponse.StatusCode < 200 || httpResponse.StatusCode >= 300 {
		return response{}, fmt.Errorf("Laravel returned HTTP %d: %s", httpResponse.StatusCode, strings.TrimSpace(string(body)))
	}
	var result response
	if err := json.Unmarshal(body, &result); err != nil {
		return response{}, fmt.Errorf("decode Laravel response: %w", err)
	}
	return result, nil
}

func (w *Worker) maintain(ctx context.Context) {
	now := w.now()
	deleted, err := w.spool.Cleanup(ctx, now.Add(-w.config.Retention))
	if err != nil {
		w.failure("cleanup", err)
	} else if deleted > 0 {
		w.logger.Info("delivered spool events cleaned", "event", "spool_cleanup", "deleted", deleted)
	}
	stats, err := w.spool.Stats(ctx, now)
	if err != nil {
		w.failure("stats", err)
		return
	}
	level := slog.LevelInfo
	if stats.Pending > w.config.CriticalPending {
		level = slog.LevelError
	} else if stats.Pending > 10000 {
		level = slog.LevelWarn
	}
	w.logger.Log(ctx, level, "telemetry metrics", "event", "telemetry_metrics",
		"connections_active", w.metrics.ConnectionsActive.Load(),
		"frames_received", w.metrics.FramesReceived.Load(),
		"frames_received_gt06", w.metrics.FramesReceivedGT06.Load(),
		"frames_received_gt02", w.metrics.FramesReceivedGT02.Load(),
		"locations_received", w.metrics.LocationsReceived.Load(),
		"locations_received_gt06", w.metrics.LocationsGT06.Load(),
		"locations_received_gt02", w.metrics.LocationsGT02.Load(),
		"heartbeats_received", w.metrics.HeartbeatsReceived.Load(),
		"invalid_crc", w.metrics.InvalidCRC.Load(),
		"unsupported_protocol", w.metrics.UnsupportedProtocol.Load(),
		"protocol_detection_failure", w.metrics.ProtocolDetectionFailure.Load(),
		"spool_pending", stats.Pending,
		"spool_pending_events", stats.Pending,
		"oldest_pending_event_age_seconds", stats.OldestPendingAge.Seconds(),
		"spool_database_size", stats.DatabaseSize,
		"delivery_success", w.metrics.DeliverySuccess.Load(),
		"delivery_failure", w.metrics.DeliveryFailure.Load(),
	)
}

func (w *Worker) failure(operation string, err error) {
	w.metrics.DeliveryFailure.Add(1)
	w.logger.Error("telemetry delivery failure", "event", "delivery_failure", "operation", operation, "error", err)
}

func Backoff(attempt int) time.Duration {
	delays := []time.Duration{5 * time.Second, 15 * time.Second, 30 * time.Second, time.Minute, 5 * time.Minute, 15 * time.Minute, 30 * time.Minute}
	if attempt < 1 {
		attempt = 1
	}
	if attempt > len(delays) {
		attempt = len(delays)
	}
	return delays[attempt-1]
}

func randomJitter(base time.Duration) time.Duration {
	// Add up to 20% so concurrent workers do not retry in lockstep.
	return base + time.Duration(rand.Int64N(max(1, int64(base/5))))
}
