package jt808

import (
	"encoding/binary"
	"testing"
	"time"

	"github.com/juanchopalen/tallerp-telemetry/internal/protocol"
)

func encodeBCDTime(t *testing.T, when time.Time) []byte {
	t.Helper()
	digits := []int{
		when.Year() % 100, int(when.Month()), when.Day(),
		when.Hour(), when.Minute(), when.Second(),
	}
	out := make([]byte, 0, 6)
	for _, d := range digits {
		out = append(out, byte((d/10)<<4|(d%10)))
	}
	return out
}

func buildLocationBody(t *testing.T, alarmFlags, status uint32, rawLat, rawLon uint32, altitude int16, rawSpeed, heading uint16, when time.Time, extra []byte) []byte {
	t.Helper()
	body := make([]byte, 0, locationFixedBodyLength+len(extra))
	body = binary.BigEndian.AppendUint32(body, alarmFlags)
	body = binary.BigEndian.AppendUint32(body, status)
	body = binary.BigEndian.AppendUint32(body, rawLat)
	body = binary.BigEndian.AppendUint32(body, rawLon)
	body = binary.BigEndian.AppendUint16(body, uint16(altitude))
	body = binary.BigEndian.AppendUint16(body, rawSpeed)
	body = binary.BigEndian.AppendUint16(body, heading)
	body = append(body, encodeBCDTime(t, when)...)
	body = append(body, extra...)
	return body
}

func TestParseLocationBasicFields(t *testing.T) {
	t.Parallel()
	when := time.Date(2026, 3, 14, 9, 26, 53, 0, time.UTC)
	// status: ACC on (bit0), positioned (bit1), south latitude (bit2),
	// west longitude (bit3), external power (bit22).
	status := uint32(1<<0 | 1<<1 | 1<<2 | 1<<3 | 1<<22)
	body := buildLocationBody(t, 0, status, 10_500_000, 66_900_000, 120, 425, 90, when, nil)

	packet, err := ParseLocation(body)
	if err != nil {
		t.Fatal(err)
	}
	if packet.Latitude != -10.5 {
		t.Errorf("Latitude = %v, want -10.5", packet.Latitude)
	}
	if packet.Longitude != -66.9 {
		t.Errorf("Longitude = %v, want -66.9", packet.Longitude)
	}
	if packet.AltitudeMeters != 120 {
		t.Errorf("AltitudeMeters = %v, want 120", packet.AltitudeMeters)
	}
	if packet.SpeedKmh != 42.5 {
		t.Errorf("SpeedKmh = %v, want 42.5", packet.SpeedKmh)
	}
	if packet.Heading != 90 {
		t.Errorf("Heading = %v, want 90", packet.Heading)
	}
	if !packet.GPSAt.Equal(when) {
		t.Errorf("GPSAt = %v, want %v", packet.GPSAt, when)
	}
	if !packet.ACC || !packet.GPSLocated || !packet.ExternalPower {
		t.Errorf("expected ACC/GPSLocated/ExternalPower all true, got %+v", packet)
	}
	if len(packet.AlarmTypes) != 0 {
		t.Errorf("expected no alarms, got %v", packet.AlarmTypes)
	}
}

func TestParseLocationNorthEastHemisphere(t *testing.T) {
	t.Parallel()
	when := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	body := buildLocationBody(t, 0, 0, 10_500_000, 66_900_000, 0, 0, 0, when, nil)
	packet, err := ParseLocation(body)
	if err != nil {
		t.Fatal(err)
	}
	if packet.Latitude != 10.5 || packet.Longitude != 66.9 {
		t.Fatalf("expected positive N/E coordinates, got lat=%v lon=%v", packet.Latitude, packet.Longitude)
	}
}

func TestParseLocationMultipleAlarms(t *testing.T) {
	t.Parallel()
	when := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	alarms := uint32(1<<alarmBitEmergency | 1<<alarmBitCollision | 1<<alarmBitRollover)
	body := buildLocationBody(t, alarms, 0, 0, 0, 0, 0, 0, when, nil)
	packet, err := ParseLocation(body)
	if err != nil {
		t.Fatal(err)
	}
	if len(packet.AlarmTypes) != 3 {
		t.Fatalf("expected 3 simultaneous alarms, got %v", packet.AlarmTypes)
	}
}

func TestParseLocationRejectsShortBody(t *testing.T) {
	t.Parallel()
	if _, err := ParseLocation(make([]byte, 10)); err == nil {
		t.Fatal("expected an error for a too-short location body")
	}
}

func TestDispatchLocationCarriesAlarmTypes(t *testing.T) {
	t.Parallel()
	handler := NewHandler(newMemoryAuthStore())
	session := &protocol.SessionContext{Authenticated: true}
	when := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	alarms := uint32(1 << alarmBitCollision)
	body := buildLocationBody(t, alarms, 0, 0, 0, 0, 0, 0, when, nil)
	raw, err := EncodeFrame(MsgLocationReport, "013800100034", 1, body)
	if err != nil {
		t.Fatal(err)
	}
	result, dispatchErr := handler.Dispatch(decodeSingleFrame(t, raw), session, time.Now().UTC())
	if dispatchErr != nil {
		t.Fatal(dispatchErr)
	}
	if len(result.Events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(result.Events))
	}
	event := result.Events[0]
	if event.EventType != "location" {
		t.Fatalf("EventType = %q, want location", event.EventType)
	}
	if len(event.AlarmTypes) != 1 || event.AlarmTypes[0] != "collision" {
		t.Fatalf("AlarmTypes = %v, want [collision]", event.AlarmTypes)
	}
	if len(result.ACK) == 0 {
		t.Fatal("expected an ACK for the location report")
	}
}
