package jt808

import (
	"encoding/binary"
	"fmt"
)

// BuildGeneralResponse builds a 0x8001 platform general response acking a
// terminal message. It is reused for auth, heartbeat, location, and any
// other message that requires a plain ACK.
func BuildGeneralResponse(terminalSN string, serial uint16, responseSerial uint16, replyID uint16, result byte) ([]byte, error) {
	body := make([]byte, 0, 5)
	body = binary.BigEndian.AppendUint16(body, responseSerial)
	body = binary.BigEndian.AppendUint16(body, replyID)
	body = append(body, result)
	return EncodeFrame(MsgPlatformGeneralResponse, terminalSN, serial, body)
}

// ParseGeneralResponse decodes a 0x0001 terminal general response (a
// terminal acknowledging a platform-initiated message, e.g. 0x8103/0x8105).
// Nothing in this codebase sends those platform-initiated messages
// automatically yet, so this is parse-only, for logging/future use.
func ParseGeneralResponse(body []byte) (GeneralResponsePacket, error) {
	if len(body) < 5 {
		return GeneralResponsePacket{}, fmt.Errorf("%w: general response body requires at least 5 bytes, got %d", ErrMalformedBody, len(body))
	}
	return GeneralResponsePacket{
		ResponseSerial: binary.BigEndian.Uint16(body[0:2]),
		ReplyID:        binary.BigEndian.Uint16(body[2:4]),
		Result:         body[4],
	}, nil
}
