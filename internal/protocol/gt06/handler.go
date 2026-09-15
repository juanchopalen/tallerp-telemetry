package gt06

import (
	"fmt"
	"time"

	"github.com/juanchopalen/tallerp-telemetry/internal/protocol"
	"github.com/juanchopalen/tallerp-telemetry/internal/telemetry"
)

type Handler struct{}

func NewHandler() *Handler { return &Handler{} }

func (*Handler) Name() protocol.Family { return protocol.FamilyGT06 }

func (*Handler) CanHandle(frame protocol.Frame, context *protocol.SessionContext) bool {
	if context.Family != protocol.FamilyUnknown {
		return context.Family == protocol.FamilyGT06
	}
	return (frame.ProtocolNumber == 0x01 && len(frame.Content) == 8) ||
		frame.ProtocolNumber == 0x12 || frame.ProtocolNumber == 0x16
}

func (*Handler) Handle(frame protocol.Frame, context *protocol.SessionContext, receivedAt time.Time) (protocol.HandleResult, error) {
	rawHex := protocol.Hex(frame.Raw)
	switch frame.ProtocolNumber {
	case 0x01:
		packet, err := protocol.ParseLogin(frame)
		if err != nil {
			return protocol.HandleResult{}, err
		}
		context.IMEI = packet.IMEI
		ack, err := protocol.BuildLoginACK(packet.Serial)
		if err != nil {
			return protocol.HandleResult{}, err
		}
		return protocol.HandleResult{ACK: ack, Events: []telemetry.TelemetryEvent{{
			EventType: "login", IMEI: packet.IMEI, Protocol: "0x01", ProtocolFamily: "gt06",
			Serial: packet.Serial, ReceivedAt: receivedAt, RawHex: rawHex,
		}}}, nil
	case 0x12:
		packet, err := protocol.ParseLocation(frame, context.IMEI)
		if err != nil {
			return protocol.HandleResult{}, err
		}
		speed := float64(packet.SpeedKmh)
		return protocol.HandleResult{Events: []telemetry.TelemetryEvent{{
			EventType: "location", IMEI: packet.IMEI, Protocol: "0x12", ProtocolFamily: "gt06",
			Serial: packet.Serial, GPSAt: &packet.GPSAt, ReceivedAt: receivedAt,
			Latitude: &packet.Latitude, Longitude: &packet.Longitude, SpeedKmh: &speed,
			Heading: &packet.Heading, Satellites: &packet.Satellites, GPSLocated: &packet.GPSLocated,
			RealtimeGPS: &packet.RealtimeGPS, ACC: packet.ACC, RawHex: rawHex,
		}}}, nil
	case 0x13:
		packet, err := protocol.ParseHeartbeat(frame, context.IMEI)
		if err != nil {
			return protocol.HandleResult{}, err
		}
		ack, err := protocol.BuildHeartbeatACK(packet.Serial)
		if err != nil {
			return protocol.HandleResult{}, err
		}
		return protocol.HandleResult{ACK: ack, Events: []telemetry.TelemetryEvent{{
			EventType: "heartbeat", IMEI: packet.IMEI, Protocol: "0x13", ProtocolFamily: "gt06",
			Serial: packet.Serial, ReceivedAt: receivedAt, GPSLocated: &packet.GPSLocated,
			ACC: &packet.ACC, ExternalPower: &packet.ExternalPower, GSMSignal: &packet.GSMSignal,
			VoltageLevel: &packet.VoltageLevel, RawHex: rawHex,
		}}}, nil
	case 0x16:
		packet, err := protocol.ParseAlarm(frame, context.IMEI)
		if err != nil {
			return protocol.HandleResult{}, err
		}
		ack, err := protocol.BuildAlarmACK(packet.Serial)
		if err != nil {
			return protocol.HandleResult{}, err
		}
		speed := float64(packet.SpeedKmh)
		alarmType := protocol.AlarmTypeName(packet.AlarmType)
		return protocol.HandleResult{ACK: ack, Events: []telemetry.TelemetryEvent{{
			EventType: "alarm", IMEI: packet.IMEI, Protocol: "0x16", ProtocolFamily: "gt06",
			Serial: packet.Serial, GPSAt: &packet.GPSAt, ReceivedAt: receivedAt,
			Latitude: &packet.Latitude, Longitude: &packet.Longitude, SpeedKmh: &speed,
			Heading: &packet.Heading, Satellites: &packet.Satellites, GPSLocated: &packet.GPSLocated,
			ACC: &packet.ACC, GSMSignal: &packet.GSMSignal, VoltageLevel: &packet.VoltageLevel,
			AlarmType: &alarmType, RawHex: rawHex,
		}}}, nil
	default:
		return protocol.HandleResult{}, fmt.Errorf("%w: GT06 0x%02x", protocol.ErrUnsupportedMessage, frame.ProtocolNumber)
	}
}
