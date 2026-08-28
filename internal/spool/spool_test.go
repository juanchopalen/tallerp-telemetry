package spool

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/juanchopalen/tallerp-telemetry/internal/telemetry"
)

func TestSpoolLifecycleAndRestart(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "spool.db")
	queue, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	event := telemetry.TelemetryEvent{EventType: "heartbeat", IMEI: "867111066918621", Protocol: "0x13", Serial: 31, ReceivedAt: now, RawHex: "abcd"}
	event.SetID()
	inserted, err := queue.Store(ctx, event)
	if err != nil || !inserted {
		t.Fatalf("first Store() inserted=%v error=%v", inserted, err)
	}
	inserted, err = queue.Store(ctx, event)
	if err != nil || inserted {
		t.Fatalf("duplicate Store() inserted=%v error=%v", inserted, err)
	}
	if err := queue.Close(); err != nil {
		t.Fatal(err)
	}

	queue, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer queue.Close()
	items, err := queue.Pending(ctx, 10, now.Add(time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].Event.EventID != event.EventID {
		t.Fatalf("pending after restart=%+v", items)
	}
	if err := queue.MarkRetry(ctx, []string{event.EventID}, now.Add(time.Minute), "HTTP 500"); err != nil {
		t.Fatal(err)
	}
	items, err = queue.Pending(ctx, 10, now.Add(2*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].Attempts != 1 {
		t.Fatalf("retried items=%+v", items)
	}
	if err := queue.MarkDelivered(ctx, []string{event.EventID}, now.Add(2*time.Minute)); err != nil {
		t.Fatal(err)
	}
	items, err = queue.Pending(ctx, 10, now.Add(3*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 0 {
		t.Fatalf("delivered event is still pending: %+v", items)
	}
}

func TestCleanupOnlyDeletesOldDeliveredEvents(t *testing.T) {
	ctx := context.Background()
	queue, err := Open(filepath.Join(t.TempDir(), "spool.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer queue.Close()
	now := time.Now().UTC()
	pending := telemetry.TelemetryEvent{EventType: "location", IMEI: "1", Protocol: "0x12", Serial: 1, ReceivedAt: now, RawHex: "01"}
	pending.SetID()
	delivered := telemetry.TelemetryEvent{EventType: "location", IMEI: "1", Protocol: "0x12", Serial: 2, ReceivedAt: now, RawHex: "02"}
	delivered.SetID()
	_, _ = queue.Store(ctx, pending)
	_, _ = queue.Store(ctx, delivered)
	if err := queue.MarkDelivered(ctx, []string{delivered.EventID}, now.Add(-48*time.Hour)); err != nil {
		t.Fatal(err)
	}
	deleted, err := queue.Cleanup(ctx, now.Add(-24*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if deleted != 1 {
		t.Fatalf("deleted=%d, want 1", deleted)
	}
	stats, err := queue.Stats(ctx, now)
	if err != nil {
		t.Fatal(err)
	}
	if stats.Pending != 1 {
		t.Fatalf("pending=%d, want 1", stats.Pending)
	}
}

func TestSpoolReadyChecksWritableDatabase(t *testing.T) {
	queue, err := Open(filepath.Join(t.TempDir(), "spool.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer queue.Close()
	if err := queue.Ready(context.Background()); err != nil {
		t.Fatal(err)
	}
}
