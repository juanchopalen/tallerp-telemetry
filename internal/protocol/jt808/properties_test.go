package jt808

import "testing"

func buildPropertiesResponseBody(t *testing.T, manufacturer, model, terminalID string, iccid string, hw, fw string, gnss, comms byte) []byte {
	t.Helper()
	body := make([]byte, 0, 64)
	body = append(body, 0x00, 0x00) // terminal type
	body = append(body, padRight(t, manufacturer, 5)...)
	body = append(body, padRight(t, model, 20)...)
	body = append(body, padRight(t, terminalID, 7)...)
	iccidBytes, err := encodeBCD(iccid, 10)
	if err != nil {
		t.Fatal(err)
	}
	body = append(body, iccidBytes...)
	body = append(body, byte(len(hw)))
	body = append(body, hw...)
	body = append(body, byte(len(fw)))
	body = append(body, fw...)
	body = append(body, gnss, comms)
	return body
}

func TestBuildQueryPropertiesHasEmptyBody(t *testing.T) {
	t.Parallel()
	frame, err := BuildQueryProperties("013800100034", 1)
	if err != nil {
		t.Fatal(err)
	}
	decoded := decodeSingleFrame(t, frame)
	if len(decoded.Body) != 0 {
		t.Fatalf("expected empty body, got %d bytes", len(decoded.Body))
	}
}

func TestParsePropertiesResponse(t *testing.T) {
	t.Parallel()
	body := buildPropertiesResponseBody(t, "SZRXT", "RXTmt2503d32m", "", "89860000000000000123", "RXTmt2503dV1.0", "201V1.0.1", 0x01, 0x02)
	resp, err := ParsePropertiesResponse(body)
	if err != nil {
		t.Fatal(err)
	}
	if resp.Manufacturer != "SZRXT" {
		t.Errorf("Manufacturer = %q, want SZRXT", resp.Manufacturer)
	}
	if resp.Model != "RXTmt2503d32m" {
		t.Errorf("Model = %q, want RXTmt2503d32m", resp.Model)
	}
	if resp.ICCID != "89860000000000000123" {
		t.Errorf("ICCID = %q, want 89860000000000000123", resp.ICCID)
	}
	if resp.HardwareVersion != "RXTmt2503dV1.0" {
		t.Errorf("HardwareVersion = %q, want RXTmt2503dV1.0", resp.HardwareVersion)
	}
	if resp.FirmwareVersion != "201V1.0.1" {
		t.Errorf("FirmwareVersion = %q, want 201V1.0.1", resp.FirmwareVersion)
	}
	if resp.GNSSCapability != 0x01 || resp.CommsCapability != 0x02 {
		t.Errorf("capabilities = %02x/%02x, want 01/02", resp.GNSSCapability, resp.CommsCapability)
	}
}

func TestParsePropertiesResponseRejectsTooShort(t *testing.T) {
	t.Parallel()
	if _, err := ParsePropertiesResponse(make([]byte, 10)); err == nil {
		t.Fatal("expected an error for a too-short properties response body")
	}
}
