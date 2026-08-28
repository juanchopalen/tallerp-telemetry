package telemetry

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"strconv"
	"strings"
	"time"
)

type TelemetryEvent struct {
	EventID      string     `json:"event_id"`
	EventType    string     `json:"event_type"`
	IMEI         string     `json:"imei"`
	Protocol     string     `json:"protocol"`
	Serial       uint16     `json:"serial"`
	GPSAt        *time.Time `json:"gps_at,omitempty"`
	ReceivedAt   time.Time  `json:"received_at"`
	Latitude     *float64   `json:"latitude,omitempty"`
	Longitude    *float64   `json:"longitude,omitempty"`
	SpeedKmh     *float64   `json:"speed_kmh,omitempty"`
	Heading      *uint16    `json:"heading,omitempty"`
	Satellites   *uint8     `json:"satellites,omitempty"`
	GPSLocated   *bool      `json:"gps_located,omitempty"`
	RealtimeGPS  *bool      `json:"realtime_gps,omitempty"`
	ACC          *bool      `json:"acc,omitempty"`
	GSMSignal    *uint8     `json:"gsm_signal,omitempty"`
	VoltageLevel *uint8     `json:"voltage_level,omitempty"`
	RawHex       string     `json:"raw_hex"`
}

func (event *TelemetryEvent) SetID() {
	gpsAt := ""
	if event.GPSAt != nil {
		gpsAt = event.GPSAt.UTC().Format(time.RFC3339Nano)
	}
	parts := []string{event.IMEI, event.Protocol, strconv.FormatUint(uint64(event.Serial), 10), gpsAt, strings.ToLower(event.RawHex)}
	sum := sha256.Sum256([]byte(strings.Join(parts, "\x00")))
	event.EventID = hex.EncodeToString(sum[:])
}

type Sink interface {
	Store(context.Context, TelemetryEvent) (bool, error)
}
