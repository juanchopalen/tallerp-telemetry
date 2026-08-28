package protocol

import "testing"

func TestParseLoginExample(t *testing.T) {
	t.Parallel()
	frames, decodeErrors := NewDecoder(0, 0).Push(mustDecodeHex(t, "78780d01012345678901234500018cdd0d0a"))
	if len(decodeErrors) != 0 || len(frames) != 1 {
		t.Fatalf("frames=%d errors=%v", len(frames), decodeErrors)
	}
	packet, err := ParseLogin(frames[0])
	if err != nil {
		t.Fatal(err)
	}
	if packet.IMEI != "123456789012345" || packet.Serial != 1 {
		t.Fatalf("unexpected login: %+v", packet)
	}
}

func TestParseLoginRejectsInvalidBCD(t *testing.T) {
	t.Parallel()
	frame := Frame{ProtocolNumber: 0x01, Content: []byte{0x01, 0x23, 0x45, 0x6a, 0x89, 0x01, 0x23, 0x45}}
	if _, err := ParseLogin(frame); err == nil {
		t.Fatal("expected invalid BCD error")
	}
	frame.Content[0] = 0x11
	frame.Content[3] = 0x67
	if _, err := ParseLogin(frame); err == nil {
		t.Fatal("expected missing padding error")
	}
}
