package jt808

import (
	"encoding/binary"
	"fmt"
)

// EncodeFrame builds a complete on-wire JT808 frame (header + body +
// checksum, escaped, delimited) for a simple, non-subpackaged message. It is
// used both by response builders (0x8001, 0x8100, ...) and by tests that
// need to construct request frames.
func EncodeFrame(messageID uint16, terminalSN string, serial uint16, body []byte) ([]byte, error) {
	if len(body) > int(bodyLengthMask) {
		return nil, fmt.Errorf("jt808 body too long: %d bytes exceeds %d", len(body), bodyLengthMask)
	}
	snBytes, err := encodeBCD(terminalSN, 6)
	if err != nil {
		return nil, fmt.Errorf("terminal SN: %w", err)
	}

	payload := make([]byte, 0, minHeaderLength+len(body))
	payload = binary.BigEndian.AppendUint16(payload, messageID)
	payload = binary.BigEndian.AppendUint16(payload, uint16(len(body))&bodyLengthMask)
	payload = append(payload, snBytes...)
	payload = binary.BigEndian.AppendUint16(payload, serial)
	payload = append(payload, body...)

	checksum := CalculateChecksum(payload)
	payload = append(payload, checksum)

	escaped := Escape(payload)
	frame := make([]byte, 0, len(escaped)+2)
	frame = append(frame, delimiter)
	frame = append(frame, escaped...)
	frame = append(frame, delimiter)
	return frame, nil
}
