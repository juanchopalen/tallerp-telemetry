package gt02

import (
	"fmt"

	"github.com/juanchopalen/tallerp-telemetry/internal/protocol"
)

func ParseHeartbeat(frame protocol.Frame, imei string) (HeartbeatPacket, error) {
	if frame.ProtocolNumber != 0x13 {
		return HeartbeatPacket{}, fmt.Errorf("unexpected GT02 heartbeat protocol 0x%02x", frame.ProtocolNumber)
	}
	if len(frame.Content) != 5 {
		return HeartbeatPacket{}, fmt.Errorf("GT02 heartbeat content requires 5 bytes, got %d", len(frame.Content))
	}
	content := frame.Content
	if content[2] > 4 {
		return HeartbeatPacket{}, fmt.Errorf("invalid GT02 GSM signal %d", content[2])
	}
	power, err := decodePower(content[1], content[3])
	if err != nil {
		return HeartbeatPacket{}, err
	}
	terminal := content[0]
	return HeartbeatPacket{
		IMEI: imei, TerminalInformation: terminal, OilElectricCut: terminal&(1<<7) != 0,
		GPSLocated: terminal&(1<<6) != 0, AlarmState: (terminal >> 3) & 0x07,
		ExternalPower: terminal&(1<<2) != 0, ACC: terminal&(1<<1) != 0,
		Armed: terminal&1 != 0, Power: power, GSMSignal: content[2], Language: content[4],
		Serial: frame.Serial,
	}, nil
}
