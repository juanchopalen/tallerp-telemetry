package jt808

import (
	"bytes"
	"testing"
)

func buildRegistrationBody(t *testing.T, manufacturer, model, terminalID string, plateColor byte, vehicleID string) []byte {
	t.Helper()
	body := make([]byte, 0, minRegistrationBodyLength+len(vehicleID))
	body = append(body, 0x00, 0x00) // province
	body = append(body, 0x00, 0x00) // city/county
	body = append(body, padRight(t, manufacturer, 5)...)
	body = append(body, padRight(t, model, 20)...)
	body = append(body, padRight(t, terminalID, 7)...)
	body = append(body, plateColor)
	body = append(body, vehicleID...)
	return body
}

func padRight(t *testing.T, value string, size int) []byte {
	t.Helper()
	if len(value) > size {
		t.Fatalf("value %q longer than %d bytes", value, size)
	}
	out := make([]byte, size)
	copy(out, value)
	return out
}

func TestParseRegistration(t *testing.T) {
	t.Parallel()
	body := buildRegistrationBody(t, "SZRXT", "RXTmt2503d32m", "", 0, "12345")
	packet, err := ParseRegistration(body)
	if err != nil {
		t.Fatal(err)
	}
	if packet.Manufacturer != "SZRXT" {
		t.Errorf("Manufacturer = %q, want SZRXT", packet.Manufacturer)
	}
	if packet.Model != "RXTmt2503d32m" {
		t.Errorf("Model = %q, want RXTmt2503d32m", packet.Model)
	}
	if packet.VehicleID != "12345" {
		t.Errorf("VehicleID = %q, want 12345", packet.VehicleID)
	}
}

func TestParseRegistrationTooShort(t *testing.T) {
	t.Parallel()
	if _, err := ParseRegistration(make([]byte, 10)); err == nil {
		t.Fatal("expected an error for a too-short registration body")
	}
}

func TestBuildRegistrationACKSuccessIncludesAuthCode(t *testing.T) {
	t.Parallel()
	frame, err := BuildRegistrationACK("013800100034", 1, 5, RegistrationSuccess, "deadbeef")
	if err != nil {
		t.Fatal(err)
	}
	decoder := NewDecoder(0, 0)
	frames, errs := decoder.Push(frame)
	if len(errs) != 0 || len(frames) != 1 {
		t.Fatalf("failed to decode built ACK: frames=%d errs=%v", len(frames), errs)
	}
	got := frames[0]
	if got.MessageID != MsgRegistrationResponse {
		t.Fatalf("MessageID = 0x%04x, want 0x%04x", got.MessageID, MsgRegistrationResponse)
	}
	if !bytes.Contains(got.Body, []byte("deadbeef")) {
		t.Fatalf("expected body to contain auth code, got %x", got.Body)
	}
}

func TestBuildRegistrationACKFailureOmitsAuthCode(t *testing.T) {
	t.Parallel()
	frame, err := BuildRegistrationACK("013800100034", 1, 5, RegistrationNoSuchVehicle, "should-not-appear")
	if err != nil {
		t.Fatal(err)
	}
	decoder := NewDecoder(0, 0)
	frames, errs := decoder.Push(frame)
	if len(errs) != 0 || len(frames) != 1 {
		t.Fatalf("failed to decode built ACK: frames=%d errs=%v", len(frames), errs)
	}
	if len(frames[0].Body) != 3 {
		t.Fatalf("expected a 3-byte body (no auth code) on failure, got %d bytes: %x", len(frames[0].Body), frames[0].Body)
	}
}
