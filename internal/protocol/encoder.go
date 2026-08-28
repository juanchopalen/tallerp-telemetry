package protocol

import "encoding/binary"

func BuildLoginACK(serial uint16) ([]byte, error) {
	return buildACK(0x01, serial), nil
}

func BuildHeartbeatACK(serial uint16) ([]byte, error) {
	return buildACK(0x13, serial), nil
}

func BuildAlarmACK(serial uint16) ([]byte, error) {
	return buildACK(0x16, serial), nil
}

func buildACK(protocolNumber byte, serial uint16) []byte {
	ack := []byte{0x78, 0x78, 0x05, protocolNumber, 0, 0, 0, 0, 0x0d, 0x0a}
	binary.BigEndian.PutUint16(ack[4:6], serial)
	binary.BigEndian.PutUint16(ack[6:8], CalculateCRC(ack[2:6]))
	return ack
}
