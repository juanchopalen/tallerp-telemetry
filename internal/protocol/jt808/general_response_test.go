package jt808

import "testing"

func TestBuildAndParseGeneralResponse(t *testing.T) {
	t.Parallel()
	frame, err := BuildGeneralResponse("013800100034", 3, 42, MsgLocationReport, ResultSuccess)
	if err != nil {
		t.Fatal(err)
	}
	decoder := NewDecoder(0, 0)
	frames, errs := decoder.Push(frame)
	if len(errs) != 0 || len(frames) != 1 {
		t.Fatalf("failed to decode built response: frames=%d errs=%v", len(frames), errs)
	}
	if frames[0].MessageID != MsgPlatformGeneralResponse {
		t.Fatalf("MessageID = 0x%04x, want 0x%04x", frames[0].MessageID, MsgPlatformGeneralResponse)
	}

	packet, err := ParseGeneralResponse(frames[0].Body)
	if err != nil {
		t.Fatal(err)
	}
	if packet.ResponseSerial != 42 || packet.ReplyID != MsgLocationReport || packet.Result != ResultSuccess {
		t.Fatalf("unexpected packet: %+v", packet)
	}
}

func TestParseGeneralResponseTooShort(t *testing.T) {
	t.Parallel()
	if _, err := ParseGeneralResponse([]byte{0x00, 0x01}); err == nil {
		t.Fatal("expected an error for a too-short general response body")
	}
}
