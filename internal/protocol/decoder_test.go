package protocol

import (
	"encoding/binary"
	"encoding/hex"
	"errors"
	"testing"
)

func TestDecoderCompleteAndFragmentedFrames(t *testing.T) {
	t.Parallel()
	raw := mustDecodeHex(t, "78780d01012345678901234500018cdd0d0a")

	complete := NewDecoder(0, 0)
	frames, decodeErrors := complete.Push(raw)
	assertSingleLoginFrame(t, frames, decodeErrors)

	fragmented := NewDecoder(0, 0)
	for _, part := range [][]byte{raw[:1], raw[1:4], raw[4:11]} {
		frames, decodeErrors = fragmented.Push(part)
		if len(frames) != 0 || len(decodeErrors) != 0 {
			t.Fatalf("incomplete fragment emitted frames=%d errors=%d", len(frames), len(decodeErrors))
		}
	}
	frames, decodeErrors = fragmented.Push(raw[11:])
	assertSingleLoginFrame(t, frames, decodeErrors)
}

func TestDecoderMultipleFramesAndNoise(t *testing.T) {
	t.Parallel()
	login := mustDecodeHex(t, "78780d01012345678901234500018cdd0d0a")
	heartbeat := mustDecodeHex(t, "78780a134406030b02001fb4e20d0a")
	stream := append([]byte{0xaa, 0xbb, 0xcc}, append(login, heartbeat...)...)
	decoder := NewDecoder(0, 0)
	frames, decodeErrors := decoder.Push(stream)
	if len(frames) != 2 {
		t.Fatalf("got %d frames, want 2", len(frames))
	}
	if frames[0].ProtocolNumber != 0x01 || frames[1].ProtocolNumber != 0x13 {
		t.Fatalf("unexpected protocols: %02x, %02x", frames[0].ProtocolNumber, frames[1].ProtocolNumber)
	}
	if len(decodeErrors) == 0 || !errors.Is(decodeErrors[0], ErrInvalidHeader) {
		t.Fatalf("expected invalid-header noise error, got %#v", decodeErrors)
	}
}

func TestDecoderSupports7979Length(t *testing.T) {
	t.Parallel()
	raw := buildTestFrame(t, []byte{0x79, 0x79}, 0x94, []byte{0x0a, 0x12, 0x34}, 9)
	frames, decodeErrors := NewDecoder(0, 0).Push(raw)
	if len(decodeErrors) != 0 || len(frames) != 1 {
		t.Fatalf("frames=%d errors=%v", len(frames), decodeErrors)
	}
	frame := frames[0]
	if frame.Length != 8 || frame.ProtocolNumber != 0x94 || frame.Serial != 9 {
		t.Fatalf("unexpected frame: %+v", frame)
	}
	if got := hex.EncodeToString(frame.Header); got != "7979" {
		t.Fatalf("header=%s", got)
	}
}

func TestDecoderRejectsAndRecovers(t *testing.T) {
	t.Parallel()
	valid := mustDecodeHex(t, "78780d01012345678901234500018cdd0d0a")
	tests := []struct {
		name string
		bad  []byte
		kind error
	}{
		{name: "length", bad: []byte{0x78, 0x78, 0x04}, kind: ErrInvalidLength},
		{name: "oversize", bad: []byte{0x79, 0x79, 0xff, 0xff}, kind: ErrInvalidLength},
		{name: "stop", bad: mutateCopy(valid, len(valid)-1, 0xff), kind: ErrInvalidStop},
		{name: "crc", bad: mutateCopy(valid, len(valid)-4, valid[len(valid)-4]^0xff), kind: ErrInvalidCRC},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			stream := append(append([]byte(nil), test.bad...), valid...)
			frames, decodeErrors := NewDecoder(64*1024, 128*1024).Push(stream)
			if len(frames) != 1 || frames[0].ProtocolNumber != 0x01 {
				t.Fatalf("decoder did not recover: frames=%d errors=%v", len(frames), decodeErrors)
			}
			found := false
			for _, decodeError := range decodeErrors {
				if errors.Is(decodeError, test.kind) {
					found = true
					break
				}
			}
			if !found {
				t.Fatalf("missing %v in errors %v", test.kind, decodeErrors)
			}
		})
	}
}

func assertSingleLoginFrame(t *testing.T, frames []Frame, decodeErrors []DecodeError) {
	t.Helper()
	if len(decodeErrors) != 0 || len(frames) != 1 {
		t.Fatalf("frames=%d errors=%v", len(frames), decodeErrors)
	}
	frame := frames[0]
	if frame.Length != 13 || frame.ProtocolNumber != 0x01 || frame.Serial != 1 || frame.CRC != 0x8cdd {
		t.Fatalf("unexpected frame: %+v", frame)
	}
	if got := hex.EncodeToString(frame.Content); got != "0123456789012345" {
		t.Fatalf("content=%s", got)
	}
}

func buildTestFrame(t *testing.T, header []byte, protocolNumber byte, content []byte, serial uint16) []byte {
	t.Helper()
	declaredLength := 1 + len(content) + 2 + 2
	lengthBytes := 1
	if header[0] == 0x79 {
		lengthBytes = 2
	}
	raw := append([]byte(nil), header...)
	if lengthBytes == 1 {
		raw = append(raw, byte(declaredLength))
	} else {
		raw = append(raw, byte(declaredLength>>8), byte(declaredLength))
	}
	raw = append(raw, protocolNumber)
	raw = append(raw, content...)
	raw = binary.BigEndian.AppendUint16(raw, serial)
	crcStart := 2
	raw = binary.BigEndian.AppendUint16(raw, CalculateCRC(raw[crcStart:]))
	raw = append(raw, 0x0d, 0x0a)
	return raw
}

func mustDecodeHex(t *testing.T, value string) []byte {
	t.Helper()
	data, err := hex.DecodeString(value)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func mutateCopy(data []byte, at int, value byte) []byte {
	copyOfData := append([]byte(nil), data...)
	copyOfData[at] = value
	return copyOfData
}
