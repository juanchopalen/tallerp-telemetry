package gt02

import (
	"encoding/binary"
	"fmt"
	"strings"

	"github.com/juanchopalen/tallerp-telemetry/internal/protocol"
)

func BuildLoginACK(serial uint16) []byte     { return buildACK(0x01, serial) }
func BuildHeartbeatACK(serial uint16) []byte { return buildACK(0x13, serial) }
func BuildAlarmACK(serial uint16) []byte     { return buildACK(0x26, serial) }

func buildACK(protocolNumber byte, serial uint16) []byte {
	frame := []byte{0x78, 0x78, 0x05, protocolNumber, 0, 0, 0, 0, 0x0d, 0x0a}
	binary.BigEndian.PutUint16(frame[4:6], serial)
	binary.BigEndian.PutUint16(frame[6:8], protocol.CalculateCRC(frame[2:6]))
	return frame
}

func BuildGT02Command(serverFlag uint32, command string, language uint16, serial uint16) ([]byte, error) {
	if command == "" || !strings.HasSuffix(command, "#") {
		return nil, fmt.Errorf("GT02 command must be non-empty and end with #")
	}
	for _, value := range []byte(command) {
		if value < 0x20 || value > 0x7e {
			return nil, fmt.Errorf("GT02 command must contain printable ASCII only")
		}
	}
	instructionLength := 4 + len(command)
	declaredLength := 1 + 1 + instructionLength + 2 + 2 + 2
	if instructionLength > 255 || declaredLength > 255 {
		return nil, fmt.Errorf("GT02 command is too long")
	}
	frame := []byte{0x78, 0x78, byte(declaredLength), 0x80, byte(instructionLength)}
	frame = binary.BigEndian.AppendUint32(frame, serverFlag)
	frame = append(frame, command...)
	frame = binary.BigEndian.AppendUint16(frame, language)
	frame = binary.BigEndian.AppendUint16(frame, serial)
	frame = binary.BigEndian.AppendUint16(frame, protocol.CalculateCRC(frame[2:]))
	return append(frame, 0x0d, 0x0a), nil
}
