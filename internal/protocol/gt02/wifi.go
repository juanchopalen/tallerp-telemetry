package gt02

import (
	"encoding/binary"
	"fmt"
	"net"

	"github.com/juanchopalen/tallerp-telemetry/internal/protocol"
	"github.com/juanchopalen/tallerp-telemetry/internal/telemetry"
)

func ParseWiFi(frame protocol.Frame, imei string) (WiFiPacket, error) {
	if frame.ProtocolNumber != 0x33 {
		return WiFiPacket{}, fmt.Errorf("unexpected GT02 WiFi protocol 0x%02x", frame.ProtocolNumber)
	}
	if len(frame.Content) < 54 {
		return WiFiPacket{}, fmt.Errorf("GT02 WiFi content requires at least 54 bytes, got %d", len(frame.Content))
	}
	content := frame.Content
	gpsAt, err := decodeTimestamp(content[:6])
	if err != nil {
		return WiFiPacket{}, err
	}
	wifiCount := int(content[53])
	if wifiCount > 5 || len(content) != 54+wifiCount*7 {
		return WiFiPacket{}, fmt.Errorf("invalid WiFi count or content length: count=%d length=%d", wifiCount, len(content))
	}
	cells := []telemetry.CellObservation{{
		LAC: binary.BigEndian.Uint16(content[10:12]), CellID: binary.BigEndian.Uint32(content[12:16]), RSSI: content[16],
	}}
	for index := 0; index < 5; index++ {
		offset := 17 + index*7
		lac := binary.BigEndian.Uint16(content[offset : offset+2])
		cellID := binary.BigEndian.Uint32(content[offset+2 : offset+6])
		rssi := content[offset+6]
		if lac != 0 || cellID != 0 || rssi != 0 {
			cells = append(cells, telemetry.CellObservation{LAC: lac, CellID: cellID, RSSI: rssi})
		}
	}
	accessPoints := make([]telemetry.WiFiAccessPoint, 0, wifiCount)
	for index := 0; index < wifiCount; index++ {
		offset := 54 + index*7
		accessPoints = append(accessPoints, telemetry.WiFiAccessPoint{
			MAC: net.HardwareAddr(content[offset : offset+6]).String(), SignalStrength: content[offset+6],
		})
	}
	return WiFiPacket{
		IMEI: imei, GPSAt: gpsAt, MCC: binary.BigEndian.Uint16(content[6:8]), MNC: binary.BigEndian.Uint16(content[8:10]),
		TimingAdvance: content[52], Cells: cells, AccessPoints: accessPoints, Serial: frame.Serial,
	}, nil
}
