package jt808

import "testing"

func TestBuildTerminalControl(t *testing.T) {
	t.Parallel()
	frame, err := BuildTerminalControl("013800100034", 1, ControlOilElectricCutoff, "")
	if err != nil {
		t.Fatal(err)
	}
	decoded := decodeSingleFrame(t, frame)
	if decoded.MessageID != MsgTerminalControl {
		t.Fatalf("MessageID = 0x%04x, want 0x%04x", decoded.MessageID, MsgTerminalControl)
	}
	if len(decoded.Body) != 1 || decoded.Body[0] != ControlOilElectricCutoff {
		t.Fatalf("Body = %x, want [0x64]", decoded.Body)
	}
}

func TestBuildTerminalControlWithParams(t *testing.T) {
	t.Parallel()
	params := "1;auth;apn;user;pass;telemetry.tallerp.com;8899;;0"
	frame, err := BuildTerminalControl("013800100034", 1, ControlSetServer, params)
	if err != nil {
		t.Fatal(err)
	}
	decoded := decodeSingleFrame(t, frame)
	if string(decoded.Body[1:]) != params {
		t.Fatalf("Body params = %q, want %q", decoded.Body[1:], params)
	}
}
