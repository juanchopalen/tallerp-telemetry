package jt808

import (
	"encoding/binary"
	"fmt"
)

const minRegistrationBodyLength = 37 // up to and including plate color; vehicle ID string is optional/variable

// ParseRegistration decodes a 0x0100 terminal registration body. Per the
// manufacturer's PDF, province/city/terminal-ID/plate-color/vehicle-ID are
// mostly ignorable defaults for this hardware — the header's TerminalSN is
// the identifier that matters — but they're still parsed for logging and
// completeness.
func ParseRegistration(body []byte) (RegistrationPacket, error) {
	if len(body) < minRegistrationBodyLength {
		return RegistrationPacket{}, fmt.Errorf("%w: registration body requires at least %d bytes, got %d", ErrMalformedBody, minRegistrationBodyLength, len(body))
	}
	packet := RegistrationPacket{
		ProvinceID:   binary.BigEndian.Uint16(body[0:2]),
		CityCountyID: binary.BigEndian.Uint16(body[2:4]),
		Manufacturer: trimTrailingNulls(body[4:9]),
		Model:        trimTrailingNulls(body[9:29]),
		TerminalID:   trimTrailingNulls(body[29:36]),
		PlateColor:   body[36],
	}
	if len(body) > minRegistrationBodyLength {
		packet.VehicleID = trimTrailingNulls(body[minRegistrationBodyLength:])
	}
	return packet, nil
}

// BuildRegistrationACK builds a 0x8100 terminal registration response.
// authCode is only included in the encoded body when result is
// RegistrationSuccess, per the manufacturer's PDF.
func BuildRegistrationACK(terminalSN string, serial uint16, responseSerial uint16, result byte, authCode string) ([]byte, error) {
	body := make([]byte, 0, 3+len(authCode))
	body = binary.BigEndian.AppendUint16(body, responseSerial)
	body = append(body, result)
	if result == RegistrationSuccess {
		body = append(body, authCode...)
	}
	return EncodeFrame(MsgRegistrationResponse, terminalSN, serial, body)
}
