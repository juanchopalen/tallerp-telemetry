package protocol

import (
	"encoding/hex"
	"fmt"
	"time"
)

func Hex(data []byte) string { return hex.EncodeToString(data) }

func DecodeLatitude(raw uint32, north bool) float64 {
	value := float64(raw) / 1_800_000
	if !north {
		value = -value
	}
	return value
}

func DecodeLongitude(raw uint32, east bool) float64 {
	value := float64(raw) / 1_800_000
	if !east {
		value = -value
	}
	return value
}

func decodeTimestamp(data []byte) (time.Time, error) {
	if len(data) != 6 {
		return time.Time{}, fmt.Errorf("timestamp requires 6 bytes, got %d", len(data))
	}
	year := 2000 + int(data[0])
	month, day := int(data[1]), int(data[2])
	hour, minute, second := int(data[3]), int(data[4]), int(data[5])
	if month < 1 || month > 12 || day < 1 || day > 31 || hour > 23 || minute > 59 || second > 59 {
		return time.Time{}, fmt.Errorf("invalid GPS timestamp %04d-%02d-%02dT%02d:%02d:%02dZ", year, month, day, hour, minute, second)
	}
	value := time.Date(year, time.Month(month), day, hour, minute, second, 0, time.UTC)
	if value.Year() != year || int(value.Month()) != month || value.Day() != day {
		return time.Time{}, fmt.Errorf("invalid GPS calendar date %04d-%02d-%02d", year, month, day)
	}
	return value, nil
}

type gpsStatus struct {
	Heading  uint16
	ACC      bool
	Realtime bool
	Located  bool
	East     bool
	North    bool
}

func decodeGPSStatus(first, second byte) (gpsStatus, error) {
	word := uint16(first)<<8 | uint16(second)
	heading := word & 0x03ff
	if heading > 360 {
		return gpsStatus{}, fmt.Errorf("invalid GPS heading %d", heading)
	}
	return gpsStatus{
		Heading:  heading,
		ACC:      first&(1<<6) != 0,
		Realtime: first&(1<<5) == 0,
		Located:  first&(1<<4) != 0,
		East:     first&(1<<3) == 0,
		North:    first&(1<<2) != 0,
	}, nil
}
