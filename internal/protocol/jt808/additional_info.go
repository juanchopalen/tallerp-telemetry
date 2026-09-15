package jt808

import (
	"encoding/binary"
	"fmt"

	"github.com/juanchopalen/tallerp-telemetry/internal/telemetry"
)

// Additional-info TLV IDs (Table 22).
const (
	infoMileage         byte = 0x01
	infoFuelVolume      byte = 0x02
	infoBatteryPercent  byte = 0x10
	infoGSMSignal       byte = 0x30
	infoSatellites      byte = 0x31
	infoTemperature     byte = 0x51
	infoHumidity        byte = 0x52
	infoCells2G         byte = 0x53
	infoWiFi            byte = 0x54
	infoCells4G         byte = 0x55
	infoPowerExtension  byte = 0x56
	infoStatusExtension byte = 0x57
	infoVoltage         byte = 0xfe
	infoOilPercent      byte = 0xe6
)

// AdditionalItem is one TLV item from a location report's additional-info
// list. Only the fields relevant to its ID are populated; everything else
// (including recognized-but-not-yet-domain-mapped IDs like power/status
// extension, and any genuinely unknown ID) is available via Raw so it can
// still be logged, never dropped silently and never a parse failure.
type AdditionalItem struct {
	ID  byte
	Raw []byte

	// MileageKm is the tracker's OWN odometer estimate (0x01), 1/10 km
	// resolution. It is device-calculated, not the vehicle ECU's odometer,
	// and must never be conflated with TallERP's GPS-estimated mileage.
	MileageKm        *float64
	FuelVolumeL      *float64 // 0x02
	BatteryPercent   *uint8   // 0x10
	GSMSignal        *uint8   // 0x30, raw signal value, not normalized to a percentage
	Satellites       *uint8   // 0x31
	TemperatureC     *float64 // 0x51
	HumidityPercent  *float64 // 0x52
	VoltageVolts     *float64 // 0xFE: external supply voltage (wired) or battery voltage (wireless)
	OilPercent       *float64 // 0xE6
	Cells            []telemetry.CellObservation
	WiFiAccessPoints []telemetry.WiFiAccessPoint
}

// ParseAdditionalItems parses the variable-length list of TLV
// (ID BYTE, length BYTE, value) items trailing a 0x0200 location body. An
// item with an ID this package doesn't map to a domain field is still
// returned (with Raw populated) rather than treated as an error — per the
// protocol's own robustness requirement, unknown TLV IDs must never break
// parsing of the rest of the message.
func ParseAdditionalItems(data []byte) ([]AdditionalItem, error) {
	var items []AdditionalItem
	for offset := 0; offset < len(data); {
		if offset+2 > len(data) {
			return nil, fmt.Errorf("%w: truncated additional-info item header at offset %d", ErrMalformedBody, offset)
		}
		id := data[offset]
		length := int(data[offset+1])
		start := offset + 2
		end := start + length
		if end > len(data) {
			return nil, fmt.Errorf("%w: additional-info item 0x%02x declares length %d beyond body", ErrMalformedBody, id, length)
		}
		value := data[start:end]
		item := AdditionalItem{ID: id, Raw: append([]byte(nil), value...)}
		decodeAdditionalItem(&item, value)
		items = append(items, item)
		offset = end
	}
	return items, nil
}

func decodeAdditionalItem(item *AdditionalItem, value []byte) {
	switch item.ID {
	case infoMileage:
		if len(value) == 4 {
			item.MileageKm = float64Ptr(float64(binary.BigEndian.Uint32(value)) / 10.0)
		}
	case infoFuelVolume:
		if len(value) == 2 {
			item.FuelVolumeL = float64Ptr(float64(binary.BigEndian.Uint16(value)) / 10.0)
		}
	case infoBatteryPercent:
		if len(value) == 1 {
			item.BatteryPercent = uint8Ptr(value[0])
		}
	case infoGSMSignal:
		if len(value) == 1 {
			item.GSMSignal = uint8Ptr(value[0])
		}
	case infoSatellites:
		if len(value) == 1 {
			item.Satellites = uint8Ptr(value[0])
		}
	case infoTemperature:
		if len(value) == 2 {
			item.TemperatureC = float64Ptr(float64(int16(binary.BigEndian.Uint16(value))) / 10.0)
		}
	case infoHumidity:
		if len(value) == 2 {
			item.HumidityPercent = float64Ptr(float64(binary.BigEndian.Uint16(value)) / 10.0)
		}
	case infoVoltage:
		if len(value) == 2 {
			item.VoltageVolts = float64Ptr(float64(binary.BigEndian.Uint16(value)) / 10.0)
		}
	case infoOilPercent:
		if len(value) == 2 {
			item.OilPercent = float64Ptr(float64(binary.BigEndian.Uint16(value)))
		}
	case infoCells2G:
		item.Cells = decode2GCells(value)
	case infoCells4G:
		item.Cells = decode4GCells(value)
	case infoWiFi:
		item.WiFiAccessPoints = decodeWiFi(value)
	}
}

// decode2GCells parses Table 23: BYTE count, then n * {mcc WORD, mnc BYTE,
// lac WORD, cellID WORD, signal BYTE} (8 bytes/record). MCC/MNC are parsed
// but not surfaced (telemetry.CellObservation, shared with GT02, has no
// MCC/MNC-per-cell fields).
func decode2GCells(value []byte) []telemetry.CellObservation {
	const recordSize = 8
	if len(value) < 1 {
		return nil
	}
	count := int(value[0])
	cells := make([]telemetry.CellObservation, 0, count)
	offset := 1
	for i := 0; i < count && offset+recordSize <= len(value); i++ {
		lac := binary.BigEndian.Uint16(value[offset+3 : offset+5])
		cellID := uint32(binary.BigEndian.Uint16(value[offset+5 : offset+7]))
		rssi := value[offset+7]
		cells = append(cells, telemetry.CellObservation{LAC: lac, CellID: cellID, RSSI: rssi})
		offset += recordSize
	}
	return cells
}

// decode4GCells parses Table 24: BYTE count, then n * {mcc WORD, mnc BYTE,
// lac WORD, cellID DWORD, signal BYTE} (10 bytes/record).
func decode4GCells(value []byte) []telemetry.CellObservation {
	const recordSize = 10
	if len(value) < 1 {
		return nil
	}
	count := int(value[0])
	cells := make([]telemetry.CellObservation, 0, count)
	offset := 1
	for i := 0; i < count && offset+recordSize <= len(value); i++ {
		lac := binary.BigEndian.Uint16(value[offset+3 : offset+5])
		cellID := binary.BigEndian.Uint32(value[offset+5 : offset+9])
		rssi := value[offset+9]
		cells = append(cells, telemetry.CellObservation{LAC: lac, CellID: cellID, RSSI: rssi})
		offset += recordSize
	}
	return cells
}

// decodeWiFi parses 0x54: BYTE count, then n * {MAC[6], signal BYTE}
// (7 bytes/record).
func decodeWiFi(value []byte) []telemetry.WiFiAccessPoint {
	const recordSize = 7
	if len(value) < 1 {
		return nil
	}
	count := int(value[0])
	aps := make([]telemetry.WiFiAccessPoint, 0, count)
	offset := 1
	for i := 0; i < count && offset+recordSize <= len(value); i++ {
		mac := value[offset : offset+6]
		signal := value[offset+6]
		aps = append(aps, telemetry.WiFiAccessPoint{MAC: formatMAC(mac), SignalStrength: signal})
		offset += recordSize
	}
	return aps
}

func formatMAC(mac []byte) string {
	return fmt.Sprintf("%02x:%02x:%02x:%02x:%02x:%02x", mac[0], mac[1], mac[2], mac[3], mac[4], mac[5])
}

// applyAdditionalItems folds a location report's parsed TLV items into a
// TelemetryEvent. Only the last occurrence of a repeated ID wins for
// scalar fields (real traffic sends each ID at most once); cell/WiFi lists
// from whichever of 0x53/0x55/0x54 is present are assigned directly.
func applyAdditionalItems(event *telemetry.TelemetryEvent, items []AdditionalItem) {
	for _, item := range items {
		switch {
		case item.MileageKm != nil:
			event.DeviceMileageKm = item.MileageKm
		case item.FuelVolumeL != nil:
			event.FuelVolumeL = item.FuelVolumeL
		case item.BatteryPercent != nil:
			event.BatteryPercent = item.BatteryPercent
		case item.GSMSignal != nil:
			event.GSMSignal = item.GSMSignal
		case item.Satellites != nil:
			event.Satellites = item.Satellites
		case item.TemperatureC != nil:
			event.TemperatureC = item.TemperatureC
		case item.HumidityPercent != nil:
			event.HumidityPercent = item.HumidityPercent
		case item.VoltageVolts != nil:
			event.ExternalVoltage = item.VoltageVolts
		case item.OilPercent != nil:
			event.OilPercent = item.OilPercent
		}
		if len(item.Cells) > 0 {
			event.Cells = item.Cells
		}
		if len(item.WiFiAccessPoints) > 0 {
			event.WiFiAccessPoints = item.WiFiAccessPoints
		}
	}
}
