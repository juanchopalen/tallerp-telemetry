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
	EventID          string              `json:"event_id"`
	EventType        string              `json:"event_type"`
	IMEI             string              `json:"imei"`
	Protocol         string              `json:"protocol"`
	ProtocolFamily   string              `json:"protocol_family,omitempty"`
	Serial           uint16              `json:"serial"`
	GPSAt            *time.Time          `json:"gps_at,omitempty"`
	ReceivedAt       time.Time           `json:"received_at"`
	Latitude         *float64            `json:"latitude,omitempty"`
	Longitude        *float64            `json:"longitude,omitempty"`
	SpeedKmh         *float64            `json:"speed_kmh,omitempty"`
	Heading          *uint16             `json:"heading,omitempty"`
	Satellites       *uint8              `json:"satellites,omitempty"`
	GPSLocated       *bool               `json:"gps_located,omitempty"`
	RealtimeGPS      *bool               `json:"realtime_gps,omitempty"`
	ACC              *bool               `json:"acc,omitempty"`
	ExternalPower    *bool               `json:"external_power,omitempty"`
	GSMSignal        *uint8              `json:"gsm_signal,omitempty"`
	VoltageLevel     *uint8              `json:"voltage_level,omitempty"`
	BatteryPercent   *uint8              `json:"battery_percent,omitempty"`
	ExternalVoltage  *float64            `json:"external_voltage,omitempty"`
	IsRetransmission *bool               `json:"is_retransmission,omitempty"`
	AlarmType        *string             `json:"alarm_type,omitempty"`
	MCC              *uint16             `json:"mcc,omitempty"`
	MNC              *uint16             `json:"mnc,omitempty"`
	TimingAdvance    *uint8              `json:"timing_advance,omitempty"`
	Cells            []CellObservation   `json:"cells,omitempty"`
	WiFiAccessPoints []WiFiAccessPoint   `json:"wifi_access_points,omitempty"`
	GeneralInfo      *GeneralInformation `json:"general_info,omitempty"`
	CommandResponse  *CommandResponse    `json:"command_response,omitempty"`
	RawHex           string              `json:"raw_hex"`

	// JT808-only fields. All additive/optional; GT06 and GT02 never set
	// them.
	TerminalSN *string  `json:"terminal_sn,omitempty"`
	AlarmTypes []string `json:"alarm_types,omitempty"`
	// DeviceMileageKm is the tracker's own odometer estimate (JT808
	// additional-info 0x01). It is device-calculated, not derived from the
	// vehicle ECU, and must never overwrite or feed TallERP's own
	// GPS-estimated mileage.
	DeviceMileageKm *float64 `json:"device_mileage_km,omitempty"`
	FuelVolumeL     *float64 `json:"fuel_volume_l,omitempty"`
	OilPercent      *float64 `json:"oil_percent,omitempty"`
	TemperatureC    *float64 `json:"temperature_c,omitempty"`
	HumidityPercent *float64 `json:"humidity_percent,omitempty"`
	AltitudeMeters  *int32   `json:"altitude_meters,omitempty"`
}

type CellObservation struct {
	LAC    uint16 `json:"lac"`
	CellID uint32 `json:"cell_id"`
	RSSI   uint8  `json:"rssi"`
}

type WiFiAccessPoint struct {
	MAC            string `json:"mac"`
	SignalStrength uint8  `json:"signal_strength"`
}

type GeneralInformation struct {
	Subtype uint8  `json:"subtype"`
	IMEI    string `json:"imei,omitempty"`
	IMSI    string `json:"imsi,omitempty"`
	ICCID   string `json:"iccid,omitempty"`
	DataHex string `json:"data_hex,omitempty"`
}

type CommandResponse struct {
	ServerFlag uint32 `json:"server_flag"`
	Encoding   string `json:"encoding"`
	Content    string `json:"content"`
}

func (event *TelemetryEvent) SetID() {
	gpsAt := ""
	if event.GPSAt != nil {
		gpsAt = event.GPSAt.UTC().Format(time.RFC3339Nano)
	}
	parts := []string{event.IMEI, event.Protocol, strconv.FormatUint(uint64(event.Serial), 10), gpsAt, strings.ToLower(event.RawHex)}
	switch {
	case strings.EqualFold(event.ProtocolFamily, "gt02"):
		parts = append(parts, "gt02")
	case strings.EqualFold(event.ProtocolFamily, "jt808"):
		parts = append(parts, "jt808")
	}
	sum := sha256.Sum256([]byte(strings.Join(parts, "\x00")))
	event.EventID = hex.EncodeToString(sum[:])
}

type Sink interface {
	Store(context.Context, TelemetryEvent) (bool, error)
}
