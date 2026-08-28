package delivery

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/juanchopalen/tallerp-telemetry/internal/spool"
	"github.com/juanchopalen/tallerp-telemetry/internal/telemetry"
)

func TestBackoffSchedule(t *testing.T) {
	want := []time.Duration{5 * time.Second, 15 * time.Second, 30 * time.Second, time.Minute, 5 * time.Minute, 15 * time.Minute, 30 * time.Minute, 30 * time.Minute}
	for index, expected := range want {
		if actual := Backoff(index + 1); actual != expected {
			t.Fatalf("Backoff(%d)=%v, want %v", index+1, actual, expected)
		}
	}
}

func TestDeliveryAcceptedDuplicateAndPartial(t *testing.T) {
	var firstID, secondID, thirdID string
	api := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Header.Get("Authorization") != "Bearer secret" {
			t.Error("missing bearer token")
		}
		if request.Header.Get("User-Agent") != UserAgent {
			t.Error("unexpected user agent")
		}
		_ = json.NewEncoder(writer).Encode(response{Accepted: []string{firstID}, Duplicates: []string{secondID}, Rejected: []string{thirdID}})
	}))
	defer api.Close()
	queue, events := testQueue(t, 3)
	defer queue.Close()
	firstID = events[0].EventID
	secondID = events[1].EventID
	thirdID = events[2].EventID
	worker := testWorker(queue, api.Client(), api.URL)
	worker.config.Token = "secret"
	worker.deliverOnce(context.Background())
	items, err := queue.Pending(context.Background(), 10, time.Now().Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].Event.EventID != thirdID || items[0].Attempts != 1 {
		t.Fatalf("partial result pending=%+v", items)
	}
}

func TestDeliveryRetryStatuses(t *testing.T) {
	for _, status := range []int{http.StatusInternalServerError, http.StatusTooManyRequests} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			api := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) { http.Error(writer, "retry", status) }))
			defer api.Close()
			queue, _ := testQueue(t, 1)
			defer queue.Close()
			worker := testWorker(queue, api.Client(), api.URL)
			worker.deliverOnce(context.Background())
			items, err := queue.Pending(context.Background(), 10, time.Now().Add(time.Hour))
			if err != nil {
				t.Fatal(err)
			}
			if len(items) != 1 || items[0].Attempts != 1 {
				t.Fatalf("retry pending=%+v", items)
			}
		})
	}
}

func TestDeliveryTimeoutRetries(t *testing.T) {
	api := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) { time.Sleep(100 * time.Millisecond) }))
	defer api.Close()
	queue, _ := testQueue(t, 1)
	defer queue.Close()
	client := api.Client()
	client.Timeout = 10 * time.Millisecond
	worker := testWorker(queue, client, api.URL)
	worker.deliverOnce(context.Background())
	items, err := queue.Pending(context.Background(), 10, time.Now().Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].Attempts != 1 {
		t.Fatalf("timeout pending=%+v", items)
	}
}

func TestShutdownFinishesBatchInProgress(t *testing.T) {
	queue, events := testQueue(t, 1)
	defer queue.Close()
	started := make(chan struct{})
	release := make(chan struct{})
	var once sync.Once
	api := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		once.Do(func() { close(started) })
		<-release
		_ = json.NewEncoder(writer).Encode(response{Accepted: []string{events[0].EventID}})
	}))
	defer api.Close()
	worker := testWorker(queue, api.Client(), api.URL)
	worker.config.Interval = time.Millisecond
	worker.Start(context.Background())
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("delivery did not start")
	}
	shutdownDone := make(chan error, 1)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	go func() { shutdownDone <- worker.Shutdown(ctx) }()
	select {
	case err := <-shutdownDone:
		t.Fatalf("shutdown returned before batch completed: %v", err)
	case <-time.After(20 * time.Millisecond):
	}
	close(release)
	if err := <-shutdownDone; err != nil {
		t.Fatal(err)
	}
	items, err := queue.Pending(context.Background(), 10, time.Now().Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 0 {
		t.Fatalf("completed batch remained pending: %+v", items)
	}
}

func testQueue(t *testing.T, count int) (*spool.Spool, []telemetry.TelemetryEvent) {
	t.Helper()
	queue, err := spool.Open(filepath.Join(t.TempDir(), "spool.db"))
	if err != nil {
		t.Fatal(err)
	}
	events := make([]telemetry.TelemetryEvent, count)
	for index := range events {
		events[index] = telemetry.TelemetryEvent{EventType: "heartbeat", IMEI: "867111066918621", Protocol: "0x13", Serial: uint16(index + 1), ReceivedAt: time.Now().UTC(), RawHex: string(rune('a' + index))}
		events[index].SetID()
		if _, err := queue.Store(context.Background(), events[index]); err != nil {
			t.Fatal(err)
		}
	}
	return queue, events
}

func testWorker(queue *spool.Spool, client *http.Client, apiURL string) *Worker {
	worker := New(Config{APIURL: apiURL, BatchSize: 100, Interval: time.Hour}, queue, client, nil, nil)
	worker.jitter = func(value time.Duration) time.Duration { return value }
	return worker
}
