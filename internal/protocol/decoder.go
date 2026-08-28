package protocol

import (
	"encoding/binary"
	"errors"
	"fmt"
)

const (
	DefaultMaxFrameSize     = 64 * 1024
	DefaultMaxBufferedBytes = 128 * 1024
)

var (
	ErrInvalidHeader = errors.New("invalid frame header")
	ErrInvalidLength = errors.New("invalid packet length")
	ErrInvalidStop   = errors.New("invalid stop bits")
	ErrInvalidCRC    = errors.New("invalid CRC")
	ErrBufferLimit   = errors.New("connection buffer limit exceeded")
)

// DecodeError describes bytes rejected by the streaming decoder.
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

// Decoder incrementally extracts frames from an arbitrary TCP byte stream.
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
// Recoverable framing and validation failures are returned separately.
func (d *Decoder) Push(data []byte) ([]Frame, []DecodeError) {
	d.buffer = append(d.buffer, data...)
	var frames []Frame
	var decodeErrors []DecodeError

	for {
		headerAt := findHeader(d.buffer)
		if headerAt < 0 {
			keep := 0
			if len(d.buffer) > 0 && (d.buffer[len(d.buffer)-1] == 0x78 || d.buffer[len(d.buffer)-1] == 0x79) {
				keep = 1
			}
			if len(d.buffer) > keep {
				raw := cloneBytes(d.buffer[:len(d.buffer)-keep])
				decodeErrors = append(decodeErrors, newDecodeError(ErrInvalidHeader, raw, ErrInvalidHeader))
				d.buffer = cloneBytes(d.buffer[len(d.buffer)-keep:])
			}
			break
		}
		if headerAt > 0 {
			raw := cloneBytes(d.buffer[:headerAt])
			decodeErrors = append(decodeErrors, newDecodeError(ErrInvalidHeader, raw, ErrInvalidHeader))
			d.buffer = d.buffer[headerAt:]
		}

		lengthBytes := 1
		if d.buffer[0] == 0x79 {
			lengthBytes = 2
		}
		prefixLength := 2 + lengthBytes
		if len(d.buffer) < prefixLength {
			break
		}

		declaredLength := int(d.buffer[2])
		if lengthBytes == 2 {
			declaredLength = int(binary.BigEndian.Uint16(d.buffer[2:4]))
		}
		totalLength := prefixLength + declaredLength + 2
		if declaredLength < 5 || totalLength > d.maxFrameSize {
			raw := cloneBytes(d.buffer[:prefixLength])
			err := fmt.Errorf("%w: declared=%d total=%d", ErrInvalidLength, declaredLength, totalLength)
			decodeErrors = append(decodeErrors, newDecodeError(ErrInvalidLength, raw, err))
			d.buffer = d.buffer[1:]
			continue
		}
		if len(d.buffer) < totalLength {
			break
		}

		candidate := d.buffer[:totalLength]
		if candidate[totalLength-2] != 0x0d || candidate[totalLength-1] != 0x0a {
			raw := cloneBytes(candidate)
			decodeErrors = append(decodeErrors, newDecodeError(ErrInvalidStop, raw, ErrInvalidStop))
			d.buffer = d.buffer[1:]
			continue
		}

		serialAt := totalLength - 6
		crcAt := totalLength - 4
		serial := binary.BigEndian.Uint16(candidate[serialAt:crcAt])
		gotCRC := binary.BigEndian.Uint16(candidate[crcAt : crcAt+2])
		wantCRC := CalculateCRC(candidate[2:crcAt])
		if gotCRC != wantCRC {
			raw := cloneBytes(candidate)
			err := fmt.Errorf("%w: got=%04x want=%04x", ErrInvalidCRC, gotCRC, wantCRC)
			decodeErrors = append(decodeErrors, newDecodeError(ErrInvalidCRC, raw, err))
			d.buffer = d.buffer[totalLength:]
			continue
		}

		protocolAt := prefixLength
		frames = append(frames, Frame{
			Header:         cloneBytes(candidate[:2]),
			Length:         declaredLength,
			ProtocolNumber: candidate[protocolAt],
			Content:        cloneBytes(candidate[protocolAt+1 : serialAt]),
			Serial:         serial,
			CRC:            gotCRC,
			Raw:            cloneBytes(candidate),
		})
		d.buffer = d.buffer[totalLength:]
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

func findHeader(data []byte) int {
	for i := 0; i+1 < len(data); i++ {
		if (data[i] == 0x78 && data[i+1] == 0x78) || (data[i] == 0x79 && data[i+1] == 0x79) {
			return i
		}
	}
	return -1
}

func cloneBytes(data []byte) []byte { return append([]byte(nil), data...) }

func newDecodeError(kind error, raw []byte, err error) DecodeError {
	return DecodeError{Kind: kind, Raw: raw, Err: err}
}
