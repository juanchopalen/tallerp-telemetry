package protocol

import (
	"encoding/binary"
	"fmt"
	"time"
)

const (
	AlarmNormal       byte = 0x00
	AlarmSOS          byte = 0x01
	AlarmPowerFailure byte = 0x02
	AlarmVibration    byte = 0x03
	AlarmEnterFence   byte = 0x04
	AlarmExitFence    byte = 0x05
	AlarmOverspeed    byte = 0x06
	AlarmDisplacement byte = 0x09
	AlarmLowBattery   byte = 0x0e
	AlarmACCFlameout  byte = 0xfe
	AlarmACCIgnition  byte = 0xff
)

type AlarmPacket struct {
	IMEI                string
	GPSAt               time.Time
	Latitude            float64
	Longitude           float64
	SpeedKmh            uint8
	Heading             uint16
	Satellites          uint8
	AlarmType           byte
	TerminalInformation byte
	ACC                 bool
	GPSLocated          bool
	VoltageLevel        uint8
	GSMSignal           uint8
	Language            uint8
	MCC                 uint16
	MNC                 uint8
	LAC                 uint16
	CellID              uint32
	Serial              uint16
}

func ParseAlarm(frame Frame, imei string) (AlarmPacket, error) {
	if frame.ProtocolNumber != 0x16 {
		return AlarmPacket{}, fmt.Errorf("unexpected alarm protocol 0x%02x", frame.ProtocolNumber)
	}
	if len(frame.Content) != 32 {
		return AlarmPacket{}, fmt.Errorf("alarm content requires 32 bytes, got %d", len(frame.Content))
	}
	content := frame.Content
	gpsAt, err := decodeTimestamp(content[:6])
	if err != nil {
		return AlarmPacket{}, err
	}
	if content[6]>>4 != 0x0c {
		return AlarmPacket{}, fmt.Errorf("invalid GPS information length %d", content[6]>>4)
	}
	status, err := decodeGPSStatus(content[16], content[17])
	if err != nil {
		return AlarmPacket{}, err
	}
	if content[18] != 0x09 {
		return AlarmPacket{}, fmt.Errorf("invalid alarm LBS length %d", content[18])
	}
	latitudeRaw := binary.BigEndian.Uint32(content[7:11])
	longitudeRaw := binary.BigEndian.Uint32(content[11:15])
	if latitudeRaw > 162_000_000 || longitudeRaw > 324_000_000 {
		return AlarmPacket{}, fmt.Errorf("alarm coordinates out of range: latitude=%d longitude=%d", latitudeRaw, longitudeRaw)
	}
	if content[28] > 6 {
		return AlarmPacket{}, fmt.Errorf("invalid voltage level %d", content[28])
	}
	if content[29] > 100 {
		return AlarmPacket{}, fmt.Errorf("invalid GSM signal %d", content[29])
	}
	terminalInformation := content[27]
	return AlarmPacket{
		IMEI:                imei,
		GPSAt:               gpsAt,
		Latitude:            DecodeLatitude(latitudeRaw, status.North),
		Longitude:           DecodeLongitude(longitudeRaw, status.East),
		SpeedKmh:            content[15],
		Heading:             status.Heading,
		Satellites:          content[6] & 0x0f,
		AlarmType:           content[30],
		TerminalInformation: terminalInformation,
		ACC:                 terminalInformation&(1<<1) != 0,
		GPSLocated:          terminalInformation&(1<<6) != 0,
		VoltageLevel:        content[28],
		GSMSignal:           content[29],
		Language:            content[31],
		MCC:                 binary.BigEndian.Uint16(content[19:21]),
		MNC:                 content[21],
		LAC:                 binary.BigEndian.Uint16(content[22:24]),
		CellID:              uint32(content[24])<<16 | uint32(content[25])<<8 | uint32(content[26]),
		Serial:              frame.Serial,
	}, nil
}

func AlarmTypeName(value byte) string {
	switch value {
	case AlarmNormal:
		return "normal"
	case AlarmSOS:
		return "sos"
	case AlarmPowerFailure:
		return "power_failure"
	case AlarmVibration:
		return "vibration"
	case AlarmEnterFence:
		return "enter_geofence"
	case AlarmExitFence:
		return "exit_geofence"
	case AlarmOverspeed:
		return "overspeed"
	case AlarmDisplacement:
		return "displacement"
	case AlarmLowBattery:
		return "low_battery"
	case AlarmACCFlameout:
		return "acc_flameout"
	case AlarmACCIgnition:
		return "acc_ignition"
	default:
		return "unknown"
	}
}
