package protocol

import "fmt"

type HeartbeatPacket struct {
	IMEI                string
	TerminalInformation byte
	ACC                 bool
	GPSLocated          bool
	ExternalPower       bool
	VoltageLevel        uint8
	GSMSignal           uint8
	ExternalVoltage     uint8
	Language            uint8
	Serial              uint16
}

func ParseHeartbeat(frame Frame, imei string) (HeartbeatPacket, error) {
	if frame.ProtocolNumber != 0x13 {
		return HeartbeatPacket{}, fmt.Errorf("unexpected heartbeat protocol 0x%02x", frame.ProtocolNumber)
	}
	if len(frame.Content) != 5 {
		return HeartbeatPacket{}, fmt.Errorf("heartbeat content requires 5 bytes, got %d", len(frame.Content))
	}
	content := frame.Content
	if content[1] > 6 {
		return HeartbeatPacket{}, fmt.Errorf("invalid voltage level %d", content[1])
	}
	if content[2] > 100 {
		return HeartbeatPacket{}, fmt.Errorf("invalid GSM signal %d", content[2])
	}
	return HeartbeatPacket{
		IMEI:                imei,
		TerminalInformation: content[0],
		ACC:                 content[0]&(1<<1) != 0,
		GPSLocated:          content[0]&(1<<6) != 0,
		ExternalPower:       content[0]&(1<<2) != 0,
		VoltageLevel:        content[1],
		GSMSignal:           content[2],
		ExternalVoltage:     content[3],
		Language:            content[4],
		Serial:              frame.Serial,
	}, nil
}
