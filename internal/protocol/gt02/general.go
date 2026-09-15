package gt02

import (
	"fmt"
	"strings"

	"github.com/juanchopalen/tallerp-telemetry/internal/protocol"
)

func ParseGeneral(frame protocol.Frame, imei string) (GeneralPacket, error) {
	if frame.ProtocolNumber != 0x94 {
		return GeneralPacket{}, fmt.Errorf("unexpected GT02 general protocol 0x%02x", frame.ProtocolNumber)
	}
	if len(frame.Content) < 1 {
		return GeneralPacket{}, fmt.Errorf("GT02 general content requires a subtype")
	}
	packet := GeneralPacket{
		IMEI: imei, Subtype: frame.Content[0], DataHex: protocol.Hex(frame.Content[1:]), Serial: frame.Serial,
	}
	if packet.Subtype != 0x0a {
		return packet, nil
	}
	if len(frame.Content) != 27 {
		return GeneralPacket{}, fmt.Errorf("GT02 general subtype 0x0A requires 27 bytes, got %d", len(frame.Content))
	}
	deviceIMEI, err := decodeBCD(frame.Content[1:9])
	if err != nil {
		return GeneralPacket{}, fmt.Errorf("decode general IMEI: %w", err)
	}
	if len(deviceIMEI) == 16 && strings.HasPrefix(deviceIMEI, "0") {
		deviceIMEI = deviceIMEI[1:]
	}
	imsi, err := decodeBCD(frame.Content[9:17])
	if err != nil {
		return GeneralPacket{}, fmt.Errorf("decode IMSI: %w", err)
	}
	iccid, err := decodeBCD(frame.Content[17:27])
	if err != nil {
		return GeneralPacket{}, fmt.Errorf("decode ICCID: %w", err)
	}
	packet.DeviceIMEI, packet.IMSI, packet.ICCID = deviceIMEI, imsi, iccid
	packet.DataHex = ""
	return packet, nil
}
