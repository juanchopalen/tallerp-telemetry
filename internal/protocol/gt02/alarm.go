package gt02

import (
	"encoding/binary"
	"fmt"

	"github.com/juanchopalen/tallerp-telemetry/internal/protocol"
)

func ParseAlarm(frame protocol.Frame, imei string) (AlarmPacket, error) {
	if frame.ProtocolNumber != 0x32 {
		return AlarmPacket{}, fmt.Errorf("unexpected GT02 alarm protocol 0x%02x", frame.ProtocolNumber)
	}
	if len(frame.Content) != 34 {
		return AlarmPacket{}, fmt.Errorf("GT02 alarm content requires 34 bytes, got %d", len(frame.Content))
	}
	content := frame.Content
	gpsAt, err := decodeTimestamp(content[:6])
	if err != nil {
		return AlarmPacket{}, err
	}
	if content[6]>>4 != 0x0c {
		return AlarmPacket{}, fmt.Errorf("invalid GPS information length %d", content[6]>>4)
	}
	if content[18] != 0x08 && content[18] != 0x0a {
		return AlarmPacket{}, fmt.Errorf("invalid LBS information length %d", content[18])
	}
	status, err := decodeGPSStatus(content[16:18])
	if err != nil {
		return AlarmPacket{}, err
	}
	latitude, err := decodeLatitude(binary.BigEndian.Uint32(content[7:11]), status.North)
	if err != nil {
		return AlarmPacket{}, err
	}
	longitude, err := decodeLongitude(binary.BigEndian.Uint32(content[11:15]), status.East)
	if err != nil {
		return AlarmPacket{}, err
	}
	power, err := decodePower(content[30], content[33])
	if err != nil {
		return AlarmPacket{}, err
	}
	if content[31] > 4 {
		return AlarmPacket{}, fmt.Errorf("invalid GT02 GSM signal %d", content[31])
	}
	terminal := content[29]
	location := LocationPacket{
		IMEI: imei, GPSAt: gpsAt, Latitude: latitude, Longitude: longitude, SpeedKmh: content[15],
		Heading: status.Heading, Satellites: content[6] & 0x0f, GPSLocated: status.Located,
		RealtimeGPS: status.Realtime, ACC: terminal&(1<<1) != 0,
		MCC: binary.BigEndian.Uint16(content[19:21]), MNC: binary.BigEndian.Uint16(content[21:23]),
		LAC: binary.BigEndian.Uint16(content[23:25]), CellID: binary.BigEndian.Uint32(content[25:29]),
		Serial: frame.Serial,
	}
	return AlarmPacket{
		Location: location, TerminalInformation: terminal, ExternalPower: terminal&(1<<2) != 0,
		Power: power, GSMSignal: content[31], AlarmCode: content[32],
		AlarmType: AlarmTypeName(content[32]), Language: content[33],
	}, nil
}

func AlarmTypeName(value byte) string {
	names := map[byte]string{
		0x00: "normal", 0x01: "sos", 0x02: "power_failure", 0x03: "vibration",
		0x04: "enter_geofence", 0x05: "exit_geofence", 0x06: "overspeed",
		0x09: "displacement", 0x0a: "enter_gps_blind_area", 0x0b: "leave_gps_blind_area",
		0x0c: "startup", 0x0e: "low_external_power", 0x0f: "external_power_low_point_protection",
		0x11: "shutdown", 0x13: "removal", 0x14: "door", 0x15: "low_power_shutdown",
		0x28: "rapid_deceleration", 0x29: "rapid_acceleration", 0x2c: "collision",
		0x2d: "rollover", 0x2e: "sharp_turn",
	}
	if name, ok := names[value]; ok {
		return name
	}
	return fmt.Sprintf("unknown_0x%02x", value)
}
