package gt02

import (
	"encoding/binary"
	"fmt"

	"github.com/juanchopalen/tallerp-telemetry/internal/protocol"
)

func ParseLocation(frame protocol.Frame, imei string) (LocationPacket, error) {
	if frame.ProtocolNumber != 0x31 {
		return LocationPacket{}, fmt.Errorf("unexpected GT02 location protocol 0x%02x", frame.ProtocolNumber)
	}
	if len(frame.Content) != 31 {
		return LocationPacket{}, fmt.Errorf("GT02 location content requires 31 bytes, got %d", len(frame.Content))
	}
	content := frame.Content
	gpsAt, err := decodeTimestamp(content[:6])
	if err != nil {
		return LocationPacket{}, err
	}
	if content[6]>>4 != 0x0c {
		return LocationPacket{}, fmt.Errorf("invalid GPS information length %d", content[6]>>4)
	}
	status, err := decodeGPSStatus(content[16:18])
	if err != nil {
		return LocationPacket{}, err
	}
	latitude, err := decodeLatitude(binary.BigEndian.Uint32(content[7:11]), status.North)
	if err != nil {
		return LocationPacket{}, err
	}
	longitude, err := decodeLongitude(binary.BigEndian.Uint32(content[11:15]), status.East)
	if err != nil {
		return LocationPacket{}, err
	}
	if content[28] > 1 || content[30] > 1 {
		return LocationPacket{}, fmt.Errorf("invalid ACC or retransmission flag")
	}
	return LocationPacket{
		IMEI: imei, GPSAt: gpsAt, Latitude: latitude, Longitude: longitude, SpeedKmh: content[15],
		Heading: status.Heading, Satellites: content[6] & 0x0f, GPSLocated: status.Located,
		RealtimeGPS: status.Realtime, MCC: binary.BigEndian.Uint16(content[18:20]),
		MNC: binary.BigEndian.Uint16(content[20:22]), LAC: binary.BigEndian.Uint16(content[22:24]),
		CellID: binary.BigEndian.Uint32(content[24:28]), ACC: content[28] == 1,
		DataReportingMode: content[29], IsRetransmission: content[30] == 1, Serial: frame.Serial,
	}, nil
}
