package gt02

import (
	"fmt"
	"time"

	"github.com/juanchopalen/tallerp-telemetry/internal/protocol"
	"github.com/juanchopalen/tallerp-telemetry/internal/telemetry"
)

type Handler struct{}

func NewHandler() *Handler { return &Handler{} }

func (*Handler) Name() protocol.Family { return protocol.FamilyGT02 }

func (*Handler) CanHandle(frame protocol.Frame, context *protocol.SessionContext) bool {
	if context.Family != protocol.FamilyUnknown {
		return context.Family == protocol.FamilyGT02
	}
	if frame.ProtocolNumber == 0x01 {
		return len(frame.Content) == 12
	}
	switch frame.ProtocolNumber {
	case 0x21, 0x31, 0x32, 0x33, 0x34, 0x94:
		return true
	default:
		return false
	}
}

func (*Handler) Handle(frame protocol.Frame, context *protocol.SessionContext, receivedAt time.Time) (protocol.HandleResult, error) {
	rawHex := protocol.Hex(frame.Raw)
	switch frame.ProtocolNumber {
	case 0x01:
		packet, err := ParseLogin(frame)
		if err != nil {
			return protocol.HandleResult{}, err
		}
		context.IMEI, context.DeviceType = packet.IMEI, packet.DeviceType
		return protocol.HandleResult{ACK: BuildLoginACK(packet.Serial), Events: []telemetry.TelemetryEvent{{
			EventType: "login", IMEI: packet.IMEI, Protocol: "0x01", ProtocolFamily: "gt02",
			Serial: packet.Serial, ReceivedAt: receivedAt, RawHex: rawHex,
		}}}, nil
	case 0x31:
		packet, err := ParseLocation(frame, context.IMEI)
		if err != nil {
			return protocol.HandleResult{}, err
		}
		speed := float64(packet.SpeedKmh)
		return protocol.HandleResult{Events: []telemetry.TelemetryEvent{{
			EventType: "location", IMEI: packet.IMEI, Protocol: "0x31", ProtocolFamily: "gt02",
			Serial: packet.Serial, GPSAt: &packet.GPSAt, ReceivedAt: receivedAt,
			Latitude: &packet.Latitude, Longitude: &packet.Longitude, SpeedKmh: &speed,
			Heading: &packet.Heading, Satellites: &packet.Satellites, GPSLocated: &packet.GPSLocated,
			RealtimeGPS: &packet.RealtimeGPS, ACC: &packet.ACC, IsRetransmission: &packet.IsRetransmission,
			MCC: &packet.MCC, MNC: &packet.MNC,
			Cells: []telemetry.CellObservation{{LAC: packet.LAC, CellID: packet.CellID}}, RawHex: rawHex,
		}}}, nil
	case 0x13:
		packet, err := ParseHeartbeat(frame, context.IMEI)
		if err != nil {
			return protocol.HandleResult{}, err
		}
		return protocol.HandleResult{ACK: BuildHeartbeatACK(packet.Serial), Events: []telemetry.TelemetryEvent{{
			EventType: "heartbeat", IMEI: packet.IMEI, Protocol: "0x13", ProtocolFamily: "gt02",
			Serial: packet.Serial, ReceivedAt: receivedAt, GPSLocated: &packet.GPSLocated,
			ACC: &packet.ACC, ExternalPower: &packet.ExternalPower, GSMSignal: &packet.GSMSignal,
			VoltageLevel: packet.Power.VoltageLevel, BatteryPercent: packet.Power.BatteryPercent,
			ExternalVoltage: packet.Power.ExternalVoltage, RawHex: rawHex,
		}}}, nil
	case 0x32:
		packet, err := ParseAlarm(frame, context.IMEI)
		if err != nil {
			return protocol.HandleResult{}, err
		}
		location := packet.Location
		speed := float64(location.SpeedKmh)
		return protocol.HandleResult{ACK: BuildAlarmACK(location.Serial), Events: []telemetry.TelemetryEvent{{
			EventType: "alarm", IMEI: location.IMEI, Protocol: "0x32", ProtocolFamily: "gt02",
			Serial: location.Serial, GPSAt: &location.GPSAt, ReceivedAt: receivedAt,
			Latitude: &location.Latitude, Longitude: &location.Longitude, SpeedKmh: &speed,
			Heading: &location.Heading, Satellites: &location.Satellites, GPSLocated: &location.GPSLocated,
			RealtimeGPS: &location.RealtimeGPS, ACC: &location.ACC, ExternalPower: &packet.ExternalPower,
			GSMSignal: &packet.GSMSignal, VoltageLevel: packet.Power.VoltageLevel,
			BatteryPercent: packet.Power.BatteryPercent, ExternalVoltage: packet.Power.ExternalVoltage,
			AlarmType: &packet.AlarmType, MCC: &location.MCC, MNC: &location.MNC,
			Cells: []telemetry.CellObservation{{LAC: location.LAC, CellID: location.CellID}}, RawHex: rawHex,
		}}}, nil
	case 0x34:
		packet, err := ParseLBS(frame, context.IMEI)
		if err != nil {
			return protocol.HandleResult{}, err
		}
		return protocol.HandleResult{Events: []telemetry.TelemetryEvent{{
			EventType: "lbs", IMEI: packet.IMEI, Protocol: "0x34", ProtocolFamily: "gt02",
			Serial: packet.Serial, GPSAt: &packet.GPSAt, ReceivedAt: receivedAt, MCC: &packet.MCC,
			MNC: &packet.MNC, TimingAdvance: &packet.TimingAdvance, Cells: packet.Cells, RawHex: rawHex,
		}}}, nil
	case 0x33:
		packet, err := ParseWiFi(frame, context.IMEI)
		if err != nil {
			return protocol.HandleResult{}, err
		}
		return protocol.HandleResult{Events: []telemetry.TelemetryEvent{{
			EventType: "wifi", IMEI: packet.IMEI, Protocol: "0x33", ProtocolFamily: "gt02",
			Serial: packet.Serial, GPSAt: &packet.GPSAt, ReceivedAt: receivedAt, MCC: &packet.MCC,
			MNC: &packet.MNC, TimingAdvance: &packet.TimingAdvance, Cells: packet.Cells,
			WiFiAccessPoints: packet.AccessPoints, RawHex: rawHex,
		}}}, nil
	case 0x94:
		packet, err := ParseGeneral(frame, context.IMEI)
		if err != nil {
			return protocol.HandleResult{}, err
		}
		return protocol.HandleResult{Events: []telemetry.TelemetryEvent{{
			EventType: "general_info", IMEI: packet.IMEI, Protocol: "0x94", ProtocolFamily: "gt02",
			Serial: packet.Serial, ReceivedAt: receivedAt, GeneralInfo: &telemetry.GeneralInformation{
				Subtype: packet.Subtype, IMEI: packet.DeviceIMEI, IMSI: packet.IMSI, ICCID: packet.ICCID, DataHex: packet.DataHex,
			}, RawHex: rawHex,
		}}}, nil
	case 0x21:
		packet, err := ParseCommandResponse(frame, context.IMEI)
		if err != nil {
			return protocol.HandleResult{}, err
		}
		return protocol.HandleResult{Events: []telemetry.TelemetryEvent{{
			EventType: "command_response", IMEI: packet.IMEI, Protocol: "0x21", ProtocolFamily: "gt02",
			Serial: packet.Serial, ReceivedAt: receivedAt, CommandResponse: &telemetry.CommandResponse{
				ServerFlag: packet.ServerFlag, Encoding: packet.Encoding, Content: packet.Content,
			}, RawHex: rawHex,
		}}}, nil
	default:
		return protocol.HandleResult{}, fmt.Errorf("%w: GT02 0x%02x", protocol.ErrUnsupportedMessage, frame.ProtocolNumber)
	}
}
