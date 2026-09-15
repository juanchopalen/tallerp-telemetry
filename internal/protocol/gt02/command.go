package gt02

import (
	"encoding/binary"
	"fmt"
	"unicode/utf16"
	"unicode/utf8"

	"github.com/juanchopalen/tallerp-telemetry/internal/protocol"
)

func ParseCommandResponse(frame protocol.Frame, imei string) (CommandResponsePacket, error) {
	if frame.ProtocolNumber != 0x21 {
		return CommandResponsePacket{}, fmt.Errorf("unexpected GT02 command response protocol 0x%02x", frame.ProtocolNumber)
	}
	if len(frame.Header) != 2 || frame.Header[0] != 0x79 || len(frame.Content) < 5 {
		return CommandResponsePacket{}, fmt.Errorf("invalid GT02 command response framing or content")
	}
	serverFlag := binary.BigEndian.Uint32(frame.Content[:4])
	encoding := frame.Content[4]
	data := frame.Content[5:]
	var content, encodingName string
	switch encoding {
	case 0x01:
		for _, value := range data {
			if value > 0x7f {
				return CommandResponsePacket{}, fmt.Errorf("invalid ASCII command response")
			}
		}
		content, encodingName = string(data), "ascii"
	case 0x02:
		if len(data)%2 != 0 {
			return CommandResponsePacket{}, fmt.Errorf("UTF-16BE response has odd byte length")
		}
		words := make([]uint16, len(data)/2)
		for index := range words {
			words[index] = binary.BigEndian.Uint16(data[index*2 : index*2+2])
		}
		content, encodingName = string(utf16.Decode(words)), "utf16-be"
		if !utf8.ValidString(content) {
			return CommandResponsePacket{}, fmt.Errorf("invalid UTF-16BE command response")
		}
	default:
		return CommandResponsePacket{}, fmt.Errorf("unsupported command response encoding 0x%02x", encoding)
	}
	return CommandResponsePacket{
		IMEI: imei, ServerFlag: serverFlag, Encoding: encodingName, Content: content, Serial: frame.Serial,
	}, nil
}
