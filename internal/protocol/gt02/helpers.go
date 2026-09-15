package gt02

import (
	"encoding/binary"
	"fmt"
	"time"
)

func decodeBCD(data []byte) (string, error) {
	digits := make([]byte, 0, len(data)*2)
	for _, value := range data {
		high, low := value>>4, value&0x0f
		if high > 9 || low > 9 {
			return "", fmt.Errorf("invalid BCD value 0x%02x", value)
		}
		digits = append(digits, '0'+high, '0'+low)
	}
	return string(digits), nil
}

func decodeIMEI(data []byte) (string, error) {
	digits, err := decodeBCD(data)
	if err != nil {
		return "", err
	}
	if len(digits) != 16 || digits[0] != '0' {
		return "", fmt.Errorf("15-digit IMEI must use a leading BCD zero")
	}
	return digits[1:], nil
}

func decodeTimestamp(data []byte) (time.Time, error) {
	if len(data) != 6 {
		return time.Time{}, fmt.Errorf("timestamp requires 6 bytes, got %d", len(data))
	}
	year := 2000 + int(data[0])
	month, day := int(data[1]), int(data[2])
	hour, minute, second := int(data[3]), int(data[4]), int(data[5])
	if month < 1 || month > 12 || day < 1 || day > 31 || hour > 23 || minute > 59 || second > 59 {
		return time.Time{}, fmt.Errorf("invalid UTC timestamp")
	}
	value := time.Date(year, time.Month(month), day, hour, minute, second, 0, time.UTC)
	if value.Year() != year || int(value.Month()) != month || value.Day() != day {
		return time.Time{}, fmt.Errorf("invalid UTC calendar date")
	}
	return value, nil
}

type gpsStatus struct {
	Heading  uint16
	Realtime bool
	Located  bool
	East     bool
	North    bool
}

func decodeGPSStatus(data []byte) (gpsStatus, error) {
	if len(data) != 2 {
		return gpsStatus{}, fmt.Errorf("GPS status requires 2 bytes")
	}
	word := binary.BigEndian.Uint16(data)
	heading := word & 0x03ff
	if heading > 360 {
		return gpsStatus{}, fmt.Errorf("invalid GPS heading %d", heading)
	}
	return gpsStatus{
		Heading: heading, Realtime: data[0]&(1<<5) == 0, Located: data[0]&(1<<4) != 0,
		East: data[0]&(1<<3) == 0, North: data[0]&(1<<2) != 0,
	}, nil
}

func decodeLatitude(raw uint32, north bool) (float64, error) {
	if raw > 162_000_000 {
		return 0, fmt.Errorf("latitude raw value out of range: %d", raw)
	}
	value := float64(raw) / 1_800_000
	if !north {
		value = -value
	}
	return value, nil
}

func decodeLongitude(raw uint32, east bool) (float64, error) {
	if raw > 324_000_000 {
		return 0, fmt.Errorf("longitude raw value out of range: %d", raw)
	}
	value := float64(raw) / 1_800_000
	if !east {
		value = -value
	}
	return value, nil
}

func decodePower(voltage, extension byte) (PowerStatus, error) {
	if voltage>>4 == 0x0f {
		raw := uint16(voltage&0x0f)<<8 | uint16(extension)
		value := float64(raw) / 10
		return PowerStatus{ExternalVoltage: &value}, nil
	}
	if voltage&0x80 != 0 {
		value := voltage & 0x7f
		if value > 100 {
			return PowerStatus{}, fmt.Errorf("invalid battery percentage %d", value)
		}
		return PowerStatus{BatteryPercent: &value}, nil
	}
	if voltage > 6 {
		return PowerStatus{}, fmt.Errorf("invalid voltage level %d", voltage)
	}
	value := voltage
	return PowerStatus{VoltageLevel: &value}, nil
}
