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

func TestEventIDPreservesGT06AndSeparatesGT02(t *testing.T) {
	base := TelemetryEvent{IMEI: "867111066918621", Protocol: "0x13", Serial: 1, RawHex: "7878aabb"}
	legacy := base
	legacy.SetID()
	gt06 := base
	gt06.ProtocolFamily = "gt06"
	gt06.SetID()
	if legacy.EventID != gt06.EventID {
		t.Fatal("adding the GT06 family changed the legacy event ID")
	}
	gt02 := base
	gt02.ProtocolFamily = "gt02"
	gt02.SetID()
	if gt02.EventID == legacy.EventID {
		t.Fatal("GT02 family was not included in the event ID")
	}

	jt808 := base
	jt808.ProtocolFamily = "jt808"
	jt808.SetID()
	if jt808.EventID == legacy.EventID || jt808.EventID == gt02.EventID {
		t.Fatal("JT808 family was not included in the event ID")
	}
}

func TestEventIDRetransmissionIsIdempotent(t *testing.T) {
	gpsAt := time.Date(2026, 8, 28, 16, 37, 42, 0, time.UTC)
	original := TelemetryEvent{
		IMEI: "013800100034", Protocol: "0x0200", ProtocolFamily: "jt808",
		Serial: 7, GPSAt: &gpsAt, ReceivedAt: gpsAt, RawHex: "7e0200aabb7e",
	}
	original.SetID()

	// A terminal retransmitting a stored location report after reconnecting
	// resends the same message serial, GPS time, and raw body, but arrives
	// at a later received_at. The event ID must not change.
	retransmitted := original
	retransmitted.ReceivedAt = gpsAt.Add(2 * time.Hour)
	retransmitted.SetID()
	if original.EventID != retransmitted.EventID {
		t.Fatalf("retransmission produced a different event ID: %s != %s", original.EventID, retransmitted.EventID)
	}
}
