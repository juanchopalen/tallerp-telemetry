package jt808

import (
	"bytes"
	"errors"
	"testing"
)

func mustEncodeFrame(t *testing.T, messageID uint16, terminalSN string, serial uint16, body []byte) []byte {
	t.Helper()
	frame, err := EncodeFrame(messageID, terminalSN, serial, body)
	if err != nil {
		t.Fatal(err)
	}
	return frame
}

func assertFrame(t *testing.T, got Frame, messageID uint16, terminalSN string, serial uint16, body []byte) {
	t.Helper()
	if got.MessageID != messageID {
		t.Errorf("MessageID = 0x%04x, want 0x%04x", got.MessageID, messageID)
	}
	if got.TerminalSN != terminalSN {
		t.Errorf("TerminalSN = %q, want %q", got.TerminalSN, terminalSN)
	}
	if got.MessageSerial != serial {
		t.Errorf("MessageSerial = %d, want %d", got.MessageSerial, serial)
	}
	if !bytes.Equal(got.Body, body) {
		t.Errorf("Body = %x, want %x", got.Body, body)
	}
}

func TestDecoderHappyPath(t *testing.T) {
	t.Parallel()
	frame := mustEncodeFrame(t, 0x0002, "013800100034", 7, nil)
	decoder := NewDecoder(0, 0)
	frames, errs := decoder.Push(frame)
	if len(errs) != 0 {
		t.Fatalf("unexpected decode errors: %v", errs)
	}
	if len(frames) != 1 {
		t.Fatalf("got %d frames, want 1", len(frames))
	}
	assertFrame(t, frames[0], 0x0002, "013800100034", 7, nil)
}

func TestDecoderFragmentedAcrossPushCalls(t *testing.T) {
	t.Parallel()
	frame := mustEncodeFrame(t, 0x0200, "013800100034", 42, []byte{0x01, 0x02, 0x03, 0x04})
	decoder := NewDecoder(0, 0)

	var all []Frame
	for i := 0; i < len(frame); i++ {
		frames, errs := decoder.Push(frame[i : i+1])
		if len(errs) != 0 {
			t.Fatalf("unexpected decode errors at byte %d: %v", i, errs)
		}
		all = append(all, frames...)
	}
	if len(all) != 1 {
		t.Fatalf("got %d frames, want 1", len(all))
	}
	assertFrame(t, all[0], 0x0200, "013800100034", 42, []byte{0x01, 0x02, 0x03, 0x04})
}

func TestDecoderConcatenatedFramesSharedDelimiter(t *testing.T) {
	t.Parallel()
	frame1 := mustEncodeFrame(t, 0x0002, "013800100034", 1, nil)
	frame2 := mustEncodeFrame(t, 0x0002, "013800100034", 2, nil)
	// Simulate a terminal that reuses one 0x7e between back-to-back
	// messages instead of sending two adjacent delimiters.
	shared := append(append([]byte{}, frame1[:len(frame1)-1]...), frame2...)

	decoder := NewDecoder(0, 0)
	frames, errs := decoder.Push(shared)
	if len(errs) != 0 {
		t.Fatalf("unexpected decode errors: %v", errs)
	}
	if len(frames) != 2 {
		t.Fatalf("got %d frames, want 2", len(frames))
	}
	if frames[0].MessageSerial != 1 || frames[1].MessageSerial != 2 {
		t.Fatalf("got serials %d,%d, want 1,2", frames[0].MessageSerial, frames[1].MessageSerial)
	}
}

func TestDecoderConcatenatedFramesDoubleDelimiter(t *testing.T) {
	t.Parallel()
	frame1 := mustEncodeFrame(t, 0x0002, "013800100034", 1, nil)
	frame2 := mustEncodeFrame(t, 0x0002, "013800100034", 2, nil)
	doubled := append(append([]byte{}, frame1...), frame2...)

	decoder := NewDecoder(0, 0)
	frames, errs := decoder.Push(doubled)
	if len(errs) != 0 {
		t.Fatalf("unexpected decode errors: %v", errs)
	}
	if len(frames) != 2 {
		t.Fatalf("got %d frames, want 2", len(frames))
	}
}

func TestDecoderMultipleFramesInOnePush(t *testing.T) {
	t.Parallel()
	frame1 := mustEncodeFrame(t, 0x0100, "013800100034", 5, []byte{0xaa})
	frame2 := mustEncodeFrame(t, 0x0102, "013800100034", 6, []byte{0xbb})
	combined := append(append([]byte{}, frame1...), frame2...)

	decoder := NewDecoder(0, 0)
	frames, errs := decoder.Push(combined)
	if len(errs) != 0 {
		t.Fatalf("unexpected decode errors: %v", errs)
	}
	if len(frames) != 2 {
		t.Fatalf("got %d frames, want 2", len(frames))
	}
	if frames[0].MessageID != 0x0100 || frames[1].MessageID != 0x0102 {
		t.Fatalf("unexpected message IDs: %04x, %04x", frames[0].MessageID, frames[1].MessageID)
	}
}

func TestDecoderRejectsBadChecksum(t *testing.T) {
	t.Parallel()
	frame := mustEncodeFrame(t, 0x0002, "013800100034", 1, nil)
	frame[len(frame)-2] ^= 0xff // flip the checksum byte (just before closing delimiter)

	decoder := NewDecoder(0, 0)
	frames, errs := decoder.Push(frame)
	if len(frames) != 0 {
		t.Fatalf("expected no frames, got %d", len(frames))
	}
	if len(errs) != 1 || !errors.Is(errs[0], ErrInvalidChecksum) {
		t.Fatalf("expected a single ErrInvalidChecksum, got %v", errs)
	}
}

func TestDecoderRejectsTruncatedFrame(t *testing.T) {
	t.Parallel()
	// A single delimiter with too little content, then a good frame, to
	// prove the decoder recovers.
	good := mustEncodeFrame(t, 0x0002, "013800100034", 9, nil)
	truncated := append([]byte{delimiter, 0x01, 0x02, delimiter}, good...)

	decoder := NewDecoder(0, 0)
	frames, errs := decoder.Push(truncated)
	if len(frames) != 1 {
		t.Fatalf("got %d frames, want 1", len(frames))
	}
	if len(errs) == 0 {
		t.Fatal("expected at least one decode error for the truncated frame")
	}
}

func TestDecoderRecoversAfterGarbageBeforeDelimiter(t *testing.T) {
	t.Parallel()
	good := mustEncodeFrame(t, 0x0002, "013800100034", 11, nil)
	noisy := append([]byte{0x00, 0x11, 0x22}, good...)

	decoder := NewDecoder(0, 0)
	frames, errs := decoder.Push(noisy)
	if len(frames) != 1 {
		t.Fatalf("got %d frames, want 1", len(frames))
	}
	if len(errs) != 1 {
		t.Fatalf("got %d decode errors, want 1 (for the leading garbage)", len(errs))
	}
}

func TestDecoderRejectsInvalidEscape(t *testing.T) {
	t.Parallel()
	frame := mustEncodeFrame(t, 0x0002, "013800100034", 1, []byte{0x00})
	// Corrupt an escape byte inside the frame body, if present; otherwise
	// inject one directly.
	corrupted := append([]byte{delimiter, escapeByte, 0x03}, frame[1:]...)

	decoder := NewDecoder(0, 0)
	_, errs := decoder.Push(corrupted)
	foundInvalidEscape := false
	for _, err := range errs {
		if errors.Is(err, ErrInvalidEscape) {
			foundInvalidEscape = true
		}
	}
	if !foundInvalidEscape {
		t.Fatalf("expected an ErrInvalidEscape among %v", errs)
	}
}

func TestDecoderBufferLimitDropsOverflow(t *testing.T) {
	t.Parallel()
	decoder := NewDecoder(64, 128)
	garbage := bytes.Repeat([]byte{0x01}, 200)
	// No delimiter at all: everything just accumulates until the buffer
	// limit is exceeded, then it must be dropped rather than grow forever.
	frames, errs := decoder.Push(garbage)
	if len(frames) != 0 {
		t.Fatalf("expected no frames, got %d", len(frames))
	}
	foundBufferLimit := false
	for _, err := range errs {
		if errors.Is(err, ErrBufferLimit) {
			foundBufferLimit = true
		}
	}
	if !foundBufferLimit {
		t.Fatalf("expected ErrBufferLimit among %v", errs)
	}
	if decoder.BufferedBytes() != 0 {
		t.Fatalf("buffer should be cleared after limit overflow, got %d bytes", decoder.BufferedBytes())
	}
}

func TestDecoderNeverPanicsOnRandomBytes(t *testing.T) {
	t.Parallel()
	inputs := [][]byte{
		{},
		{delimiter},
		{delimiter, delimiter},
		{delimiter, escapeByte},
		{delimiter, escapeByte, 0xff, delimiter},
		bytes.Repeat([]byte{delimiter}, 50),
		{delimiter, 0x00, 0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08, 0x09, 0x0a, 0x0b, 0x0c, 0x0d, delimiter},
	}
	for _, input := range inputs {
		decoder := NewDecoder(0, 0)
		func() {
			defer func() {
				if recovered := recover(); recovered != nil {
					t.Fatalf("Push(%x) panicked: %v", input, recovered)
				}
			}()
			decoder.Push(input)
		}()
	}
}

func FuzzDecoderPush(f *testing.F) {
	if seed, err := EncodeFrame(0x0200, "013800100034", 99, []byte{0x01, 0x02, 0x03}); err == nil {
		f.Add(seed)
	}
	f.Add([]byte{delimiter, delimiter})
	f.Add([]byte{delimiter, escapeByte, 0x00})
	f.Fuzz(func(t *testing.T, data []byte) {
		decoder := NewDecoder(0, 0)
		defer func() {
			if recovered := recover(); recovered != nil {
				t.Fatalf("Push panicked on %x: %v", data, recovered)
			}
		}()
		decoder.Push(data)
	})
}
