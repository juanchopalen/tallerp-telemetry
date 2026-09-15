package gt02

import (
	"encoding/binary"
	"fmt"

	"github.com/juanchopalen/tallerp-telemetry/internal/protocol"
	"github.com/juanchopalen/tallerp-telemetry/internal/telemetry"
)

func ParseLBS(frame protocol.Frame, imei string) (LBSPacket, error) {
	if frame.ProtocolNumber != 0x34 {
		return LBSPacket{}, fmt.Errorf("unexpected GT02 LBS protocol 0x%02x", frame.ProtocolNumber)
	}
	if len(frame.Content) != 50 {
		return LBSPacket{}, fmt.Errorf("GT02 LBS content requires 50 bytes, got %d", len(frame.Content))
	}
	content := frame.Content
	gpsAt, err := decodeTimestamp(content[:6])
	if err != nil {
		return LBSPacket{}, err
	}
	cellCount := int(content[11])
	if cellCount > 5 {
		return LBSPacket{}, fmt.Errorf("invalid LBS cell count %d", cellCount)
	}
	cells := make([]telemetry.CellObservation, 0, cellCount)
	for index := 0; index < cellCount; index++ {
		offset := 12 + index*7
		cells = append(cells, telemetry.CellObservation{
			LAC:    binary.BigEndian.Uint16(content[offset : offset+2]),
			CellID: binary.BigEndian.Uint32(content[offset+2 : offset+6]), RSSI: content[offset+6],
		})
	}
	return LBSPacket{
		IMEI: imei, GPSAt: gpsAt, TimingAdvance: content[6], MCC: binary.BigEndian.Uint16(content[7:9]),
		MNC: binary.BigEndian.Uint16(content[9:11]), Cells: cells, Serial: frame.Serial,
	}, nil
}
