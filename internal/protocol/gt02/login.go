package gt02

import (
	"encoding/binary"
	"fmt"

	"github.com/juanchopalen/tallerp-telemetry/internal/protocol"
)

func ParseLogin(frame protocol.Frame) (LoginPacket, error) {
	if frame.ProtocolNumber != 0x01 {
		return LoginPacket{}, fmt.Errorf("unexpected GT02 login protocol 0x%02x", frame.ProtocolNumber)
	}
	if len(frame.Content) != 12 {
		return LoginPacket{}, fmt.Errorf("GT02 login content requires 12 bytes, got %d", len(frame.Content))
	}
	imei, err := decodeIMEI(frame.Content[:8])
	if err != nil {
		return LoginPacket{}, err
	}
	timezoneLanguage := binary.BigEndian.Uint16(frame.Content[10:12])
	offsetHundredths := int(timezoneLanguage >> 4)
	hours, minutes := offsetHundredths/100, offsetHundredths%100
	if hours > 14 || minutes > 59 {
		return LoginPacket{}, fmt.Errorf("invalid timezone value %d", offsetHundredths)
	}
	offsetMinutes := hours*60 + minutes
	if timezoneLanguage&(1<<3) != 0 {
		offsetMinutes = -offsetMinutes
	}
	return LoginPacket{
		IMEI: imei, DeviceType: binary.BigEndian.Uint16(frame.Content[8:10]),
		TimezoneOffsetMinutes: offsetMinutes, Language: uint8(timezoneLanguage & 1), Serial: frame.Serial,
	}, nil
}
