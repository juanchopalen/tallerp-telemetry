package protocol

import (
	"encoding/binary"
	"fmt"
	"time"
)

type LocationPacket struct {
	IMEI        string
	GPSAt       time.Time
	Latitude    float64
	Longitude   float64
	SpeedKmh    uint8
	Heading     uint16
	Satellites  uint8
	GPSLocated  bool
	RealtimeGPS bool
	ACC         *bool
	MCC         uint16
	MNC         uint8
	LAC         uint16
	CellID      uint32
	Serial      uint16
}

type ReceivedLocation struct {
	Location   LocationPacket
	ReceivedAt time.Time
}

func ParseLocation(frame Frame, imei string) (LocationPacket, error) {
	if frame.ProtocolNumber != 0x12 {
		return LocationPacket{}, fmt.Errorf("unexpected location protocol 0x%02x", frame.ProtocolNumber)
	}
	if len(frame.Content) != 26 {
		return LocationPacket{}, fmt.Errorf("location content requires 26 bytes, got %d", len(frame.Content))
	}
	content := frame.Content
	gpsAt, err := decodeTimestamp(content[:6])
	if err != nil {
		return LocationPacket{}, err
	}
	if content[6]>>4 != 0x0c {
		return LocationPacket{}, fmt.Errorf("invalid GPS information length %d", content[6]>>4)
	}
	status, err := decodeGPSStatus(content[16], content[17])
	if err != nil {
		return LocationPacket{}, err
	}
	latitudeRaw := binary.BigEndian.Uint32(content[7:11])
	longitudeRaw := binary.BigEndian.Uint32(content[11:15])
	if latitudeRaw > 162_000_000 {
		return LocationPacket{}, fmt.Errorf("latitude raw value out of range: %d", latitudeRaw)
	}
	if longitudeRaw > 324_000_000 {
		return LocationPacket{}, fmt.Errorf("longitude raw value out of range: %d", longitudeRaw)
	}
	acc := status.ACC
	return LocationPacket{
		IMEI:        imei,
		GPSAt:       gpsAt,
		Latitude:    DecodeLatitude(latitudeRaw, status.North),
		Longitude:   DecodeLongitude(longitudeRaw, status.East),
		SpeedKmh:    content[15],
		Heading:     status.Heading,
		Satellites:  content[6] & 0x0f,
		GPSLocated:  status.Located,
		RealtimeGPS: status.Realtime,
		ACC:         &acc,
		MCC:         binary.BigEndian.Uint16(content[18:20]),
		MNC:         content[20],
		LAC:         binary.BigEndian.Uint16(content[21:23]),
		CellID:      uint32(content[23])<<16 | uint32(content[24])<<8 | uint32(content[25]),
		Serial:      frame.Serial,
	}, nil
}
