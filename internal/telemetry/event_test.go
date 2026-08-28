package telemetry

import (
	"testing"
	"time"
)

func TestEventIDIsDeterministic(t *testing.T) {
	gpsAt := time.Date(2026, 8, 28, 16, 37, 42, 0, time.UTC)
	first := TelemetryEvent{IMEI: "867111066918621", Protocol: "0x12", Serial: 14, GPSAt: &gpsAt, ReceivedAt: gpsAt, RawHex: "7878AABB"}
	second := first
	second.ReceivedAt = second.ReceivedAt.Add(time.Hour)
	second.RawHex = "7878aabb"
	first.SetID()
	second.SetID()
	if first.EventID != second.EventID {
		t.Fatalf("same tracker packet produced different IDs: %s != %s", first.EventID, second.EventID)
	}
	second.Serial++
	second.SetID()
	if first.EventID == second.EventID {
		t.Fatal("different tracker packets produced the same ID")
	}
}
