package jt808

import (
	"bytes"
	"errors"
	"fmt"
)

const (
	DefaultMaxFrameSize     = 64 * 1024
	DefaultMaxBufferedBytes = 128 * 1024
)

var (
	ErrInvalidChecksum = errors.New("invalid jt808 checksum")
	ErrTruncatedFrame  = errors.New("truncated or malformed jt808 frame")
	ErrBufferLimit     = errors.New("jt808 connection buffer limit exceeded")
)

// DecodeError describes bytes rejected by the streaming decoder. It never
// causes the decoder to panic or the caller to disconnect on its own; the
// caller decides what to do (log and continue, per the protocol's
// robustness requirements).
type DecodeError struct {
	Kind error
	Raw  []byte
	Err  error
}

func (e DecodeError) Error() string {
	if e.Err != nil {
		return e.Err.Error()
	}
	return e.Kind.Error()
}

func (e DecodeError) Unwrap() error { return e.Kind }

// Decoder incrementally extracts JT808 frames (0x7e-delimited, byte-stuffed,
// single-byte XOR checksum) from an arbitrary TCP byte stream. It is
// independent of protocol.Decoder, which only understands GT06/GT02 framing.
type Decoder struct {
	buffer           []byte
	maxFrameSize     int
	maxBufferedBytes int
}

func NewDecoder(maxFrameSize, maxBufferedBytes int) *Decoder {
	if maxFrameSize <= 0 {
		maxFrameSize = DefaultMaxFrameSize
	}
	if maxBufferedBytes <= 0 {
		maxBufferedBytes = DefaultMaxBufferedBytes
	}
	if maxBufferedBytes < maxFrameSize {
		maxBufferedBytes = maxFrameSize
	}
	return &Decoder{maxFrameSize: maxFrameSize, maxBufferedBytes: maxBufferedBytes}
}

func (d *Decoder) BufferedBytes() int { return len(d.buffer) }

// Push adds stream bytes and returns every complete, valid frame found.
// Recoverable framing and validation failures are returned separately, one
// per rejected frame; the decoder keeps scanning afterwards.
//
// A frame's closing 0x7e delimiter is intentionally left in the buffer
// (rather than consumed) so it can double as the next frame's opening
// delimiter: some terminals emit back-to-back messages sharing a single
// delimiter (0x7e msg1 0x7e msg2 0x7e) while others emit two adjacent
// delimiters (0x7e msg1 0x7e 0x7e msg2 0x7e); both are handled.
func (d *Decoder) Push(data []byte) ([]Frame, []DecodeError) {
	d.buffer = append(d.buffer, data...)
	var frames []Frame
	var decodeErrors []DecodeError

	for {
		start := bytes.IndexByte(d.buffer, delimiter)
		if start < 0 {
			break
		}
		if start > 0 {
			raw := cloneBytes(d.buffer[:start])
			decodeErrors = append(decodeErrors, newDecodeError(ErrTruncatedFrame, raw,
				fmt.Errorf("%w: discarded %d bytes before frame delimiter", ErrTruncatedFrame, len(raw))))
			d.buffer = d.buffer[start:]
		}

		relativeEnd := bytes.IndexByte(d.buffer[1:], delimiter)
		if relativeEnd < 0 {
			break // wait for more data
		}
		end := relativeEnd + 1

		if end == 1 {
			// Two adjacent delimiters: an empty frame. Drop the leading one
			// and let the next iteration treat the other as a fresh start.
			d.buffer = d.buffer[1:]
			continue
		}

		frameRaw := cloneBytes(d.buffer[:end+1])
		if end+1 > d.maxFrameSize {
			decodeErrors = append(decodeErrors, newDecodeError(ErrTruncatedFrame, frameRaw,
				fmt.Errorf("%w: frame too large: %d bytes", ErrTruncatedFrame, end+1)))
			d.buffer = d.buffer[end:]
			continue
		}

		unescaped, err := Unescape(d.buffer[1:end])
		if err != nil {
			decodeErrors = append(decodeErrors, newDecodeError(ErrInvalidEscape, frameRaw, err))
			d.buffer = d.buffer[end:]
			continue
		}
		if len(unescaped) < minHeaderLength+1 {
			decodeErrors = append(decodeErrors, newDecodeError(ErrTruncatedFrame, frameRaw,
				fmt.Errorf("%w: unescaped length %d too short", ErrTruncatedFrame, len(unescaped))))
			d.buffer = d.buffer[end:]
			continue
		}

		payload := unescaped[:len(unescaped)-1]
		gotChecksum := unescaped[len(unescaped)-1]
		wantChecksum := CalculateChecksum(payload)
		if gotChecksum != wantChecksum {
			err := fmt.Errorf("%w: got=%02x want=%02x", ErrInvalidChecksum, gotChecksum, wantChecksum)
			decodeErrors = append(decodeErrors, newDecodeError(ErrInvalidChecksum, frameRaw, err))
			d.buffer = d.buffer[end:]
			continue
		}

		frame, err := parseHeader(payload)
		if err != nil {
			decodeErrors = append(decodeErrors, newDecodeError(ErrTruncatedFrame, frameRaw, err))
			d.buffer = d.buffer[end:]
			continue
		}
		frame.Checksum = gotChecksum
		frame.Raw = frameRaw
		frames = append(frames, frame)
		d.buffer = d.buffer[end:]
	}

	if len(d.buffer) > d.maxBufferedBytes {
		raw := cloneBytes(d.buffer)
		decodeErrors = append(decodeErrors, newDecodeError(ErrBufferLimit, raw, ErrBufferLimit))
		d.buffer = nil
	}
	if len(d.buffer) == 0 {
		d.buffer = nil
	}
	return frames, decodeErrors
}

func cloneBytes(data []byte) []byte { return append([]byte(nil), data...) }

func newDecodeError(kind error, raw []byte, err error) DecodeError {
	return DecodeError{Kind: kind, Raw: raw, Err: err}
}
