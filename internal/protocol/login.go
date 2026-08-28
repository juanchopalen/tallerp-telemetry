package protocol

import "fmt"

type LoginPacket struct {
	IMEI   string
	Serial uint16
}

func ParseLogin(frame Frame) (LoginPacket, error) {
	if frame.ProtocolNumber != 0x01 {
		return LoginPacket{}, fmt.Errorf("unexpected login protocol 0x%02x", frame.ProtocolNumber)
	}
	if len(frame.Content) != 8 {
		return LoginPacket{}, fmt.Errorf("login content requires 8 bytes, got %d", len(frame.Content))
	}

	digits := make([]byte, 0, 16)
	for _, value := range frame.Content {
		high, low := value>>4, value&0x0f
		if high > 9 || low > 9 {
			return LoginPacket{}, fmt.Errorf("invalid BCD terminal ID")
		}
		digits = append(digits, '0'+high, '0'+low)
	}
	if digits[0] != '0' {
		return LoginPacket{}, fmt.Errorf("15-digit IMEI must use a leading BCD zero")
	}
	return LoginPacket{IMEI: string(digits[1:]), Serial: frame.Serial}, nil
}
