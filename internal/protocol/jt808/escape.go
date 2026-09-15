package jt808

import "errors"

const (
	delimiter        byte = 0x7e
	escapeByte       byte = 0x7d
	escapedDelimiter byte = 0x02
	escapedEscape    byte = 0x01
)

// ErrInvalidEscape signals a malformed byte-stuffing sequence: a bare 0x7e
// inside the escaped region, a trailing 0x7d with nothing after it, or a
// 0x7d followed by a byte other than 0x01/0x02.
var ErrInvalidEscape = errors.New("invalid jt808 escape sequence")

// Escape byte-stuffs raw header+body+checksum bytes for transmission between
// the two 0x7e frame delimiters: 0x7e -> 0x7d 0x02, 0x7d -> 0x7d 0x01.
func Escape(data []byte) []byte {
	escaped := make([]byte, 0, len(data))
	for _, value := range data {
		switch value {
		case delimiter:
			escaped = append(escaped, escapeByte, escapedDelimiter)
		case escapeByte:
			escaped = append(escaped, escapeByte, escapedEscape)
		default:
			escaped = append(escaped, value)
		}
	}
	return escaped
}

// Unescape reverses Escape. The input must be the bytes strictly between the
// two 0x7e frame delimiters (delimiters themselves must not be passed in).
func Unescape(data []byte) ([]byte, error) {
	unescaped := make([]byte, 0, len(data))
	for i := 0; i < len(data); i++ {
		value := data[i]
		if value == delimiter {
			return nil, ErrInvalidEscape
		}
		if value != escapeByte {
			unescaped = append(unescaped, value)
			continue
		}
		if i+1 >= len(data) {
			return nil, ErrInvalidEscape
		}
		switch data[i+1] {
		case escapedDelimiter:
			unescaped = append(unescaped, delimiter)
		case escapedEscape:
			unescaped = append(unescaped, escapeByte)
		default:
			return nil, ErrInvalidEscape
		}
		i++
	}
	return unescaped, nil
}
