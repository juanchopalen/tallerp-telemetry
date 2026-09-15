package gt02

import (
	"time"

	"github.com/juanchopalen/tallerp-telemetry/internal/telemetry"
)

type LoginPacket struct {
	IMEI                  string
	DeviceType            uint16
	TimezoneOffsetMinutes int
	Language              uint8
	Serial                uint16
}

type LocationPacket struct {
	IMEI              string
	GPSAt             time.Time
	Latitude          float64
	Longitude         float64
	SpeedKmh          uint8
	Heading           uint16
	Satellites        uint8
	GPSLocated        bool
	RealtimeGPS       bool
	ACC               bool
	MCC               uint16
	MNC               uint16
	LAC               uint16
	CellID            uint32
	DataReportingMode uint8
	IsRetransmission  bool
	Serial            uint16
}

type PowerStatus struct {
	VoltageLevel    *uint8
	BatteryPercent  *uint8
	ExternalVoltage *float64
}

type HeartbeatPacket struct {
	IMEI                string
	TerminalInformation uint8
	OilElectricCut      bool
	GPSLocated          bool
	AlarmState          uint8
	ExternalPower       bool
	ACC                 bool
	Armed               bool
	Power               PowerStatus
	GSMSignal           uint8
	Language            uint8
	Serial              uint16
}

type AlarmPacket struct {
	Location            LocationPacket
	TerminalInformation uint8
	ExternalPower       bool
	Power               PowerStatus
	GSMSignal           uint8
	AlarmCode           uint8
	AlarmType           string
	Language            uint8
}

type LBSPacket struct {
	IMEI          string
	GPSAt         time.Time
	TimingAdvance uint8
	MCC           uint16
	MNC           uint16
	Cells         []telemetry.CellObservation
	Serial        uint16
}

type WiFiPacket struct {
	IMEI          string
	GPSAt         time.Time
	MCC           uint16
	MNC           uint16
	TimingAdvance uint8
	Cells         []telemetry.CellObservation
	AccessPoints  []telemetry.WiFiAccessPoint
	Serial        uint16
}

type GeneralPacket struct {
	IMEI       string
	Subtype    uint8
	DeviceIMEI string
	IMSI       string
	ICCID      string
	DataHex    string
	Serial     uint16
}

type CommandResponsePacket struct {
	IMEI       string
	ServerFlag uint32
	Encoding   string
	Content    string
	Serial     uint16
}
