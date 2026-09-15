package jt808

import (
	"encoding/binary"
	"errors"
	"fmt"
)

const (
	bodyLengthMask     uint16 = 0x03ff // bits 0-9
	bodyEncryptionMask uint16 = 0x1c00 // bits 10-12
	bodySubpackageMask uint16 = 0x2000 // bit 13

	minHeaderLength        = 12 // without subpackage fields
	subpackageHeaderLength = 16 // with total-packets + packet-serial
)

// ErrInvalidBCD signals a BCD-encoded field with a nibble outside 0-9.
var ErrInvalidBCD = errors.New("invalid jt808 BCD digit")

// decodeBCD converts an 8421 BCD byte string into its decimal digit string
// (two digits per byte, e.g. terminal SN BCD[6] -> a 12-digit string).
func decodeBCD(data []byte) (string, error) {
	digits := make([]byte, 0, len(data)*2)
	for _, value := range data {
		high := value >> 4
		low := value & 0x0f
		if high > 9 || low > 9 {
			return "", fmt.Errorf("%w: byte 0x%02x", ErrInvalidBCD, value)
		}
		digits = append(digits, '0'+high, '0'+low)
	}
	return string(digits), nil
}

// encodeBCD is the inverse of decodeBCD: it packs a decimal digit string
// into byteLen BCD bytes, left-padding with zeros as needed.
func encodeBCD(digits string, byteLen int) ([]byte, error) {
	want := byteLen * 2
	if len(digits) > want {
		return nil, fmt.Errorf("digit string %q longer than %d BCD bytes can hold", digits, byteLen)
	}
	if len(digits) < want {
		padding := make([]byte, want-len(digits))
		for i := range padding {
			padding[i] = '0'
		}
		digits = string(padding) + digits
	}
	out := make([]byte, byteLen)
	for i := 0; i < byteLen; i++ {
		hi := digits[i*2]
		lo := digits[i*2+1]
		if hi < '0' || hi > '9' || lo < '0' || lo > '9' {
			return nil, fmt.Errorf("%w: digit string %q is not decimal", ErrInvalidBCD, digits)
		}
		out[i] = (hi-'0')<<4 | (lo - '0')
	}
	return out, nil
}

// parseHeader decodes the message header (and, if present, body) from an
// unescaped, checksum-verified payload (header+body, checksum already
// stripped).
func parseHeader(payload []byte) (Frame, error) {
	if len(payload) < minHeaderLength {
		return Frame{}, fmt.Errorf("%w: header requires at least %d bytes, got %d", ErrTruncatedFrame, minHeaderLength, len(payload))
	}
	messageID := binary.BigEndian.Uint16(payload[0:2])
	bodyProps := binary.BigEndian.Uint16(payload[2:4])
	terminalSN, err := decodeBCD(payload[4:10])
	if err != nil {
		return Frame{}, fmt.Errorf("terminal SN: %w", err)
	}
	serial := binary.BigEndian.Uint16(payload[10:12])

	headerLength := minHeaderLength
	hasSubpackage := bodyProps&bodySubpackageMask != 0
	var totalPackets, packetSerial uint16
	if hasSubpackage {
		if len(payload) < subpackageHeaderLength {
			return Frame{}, fmt.Errorf("%w: subpackaged header requires at least %d bytes, got %d", ErrTruncatedFrame, subpackageHeaderLength, len(payload))
		}
		totalPackets = binary.BigEndian.Uint16(payload[12:14])
		packetSerial = binary.BigEndian.Uint16(payload[14:16])
		headerLength = subpackageHeaderLength
	}

	declaredBodyLength := int(bodyProps & bodyLengthMask)
	body := payload[headerLength:]
	if len(body) != declaredBodyLength {
		return Frame{}, fmt.Errorf("%w: declared body length %d, got %d", ErrTruncatedFrame, declaredBodyLength, len(body))
	}

	return Frame{
		MessageID:     messageID,
		BodyProps:     bodyProps,
		Encryption:    uint8((bodyProps & bodyEncryptionMask) >> 10),
		HasSubpackage: hasSubpackage,
		TerminalSN:    terminalSN,
		MessageSerial: serial,
		TotalPackets:  totalPackets,
		PacketSerial:  packetSerial,
		Body:          cloneBytes(body),
	}, nil
}
