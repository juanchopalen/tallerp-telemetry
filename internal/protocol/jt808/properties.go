package jt808

import (
	"encoding/binary"
	"fmt"
)

// BuildQueryProperties builds an empty-body 0x8107 message.
func BuildQueryProperties(terminalSN string, serial uint16) ([]byte, error) {
	return EncodeFrame(MsgQueryTerminalProperties, terminalSN, serial, nil)
}

type PropertiesResponse struct {
	TerminalType    uint16
	Manufacturer    string
	Model           string
	TerminalID      string
	ICCID           string
	HardwareVersion string
	FirmwareVersion string
	GNSSCapability  byte
	CommsCapability byte
}

// ParsePropertiesResponse decodes a 0x0107 terminal properties response
// (Table 17): fixed fields up to ICCID, then length-prefixed hardware and
// firmware version strings, then two capability bitmask bytes.
func ParsePropertiesResponse(body []byte) (PropertiesResponse, error) {
	const fixedLen = 2 + 5 + 20 + 7 + 10 // type + manufacturer + model + terminal ID + ICCID(BCD[10])
	if len(body) < fixedLen+1 {
		return PropertiesResponse{}, fmt.Errorf("%w: terminal properties body requires at least %d bytes, got %d", ErrMalformedBody, fixedLen+1, len(body))
	}
	resp := PropertiesResponse{
		TerminalType: binary.BigEndian.Uint16(body[0:2]),
		Manufacturer: trimTrailingNulls(body[2:7]),
		Model:        trimTrailingNulls(body[7:27]),
		TerminalID:   trimTrailingNulls(body[27:34]),
	}
	iccid, err := decodeBCD(body[34:44])
	if err != nil {
		return PropertiesResponse{}, fmt.Errorf("ICCID: %w", err)
	}
	resp.ICCID = iccid

	offset := fixedLen
	hwLen := int(body[offset])
	offset++
	if offset+hwLen > len(body) {
		return PropertiesResponse{}, fmt.Errorf("%w: truncated hardware version", ErrMalformedBody)
	}
	resp.HardwareVersion = string(body[offset : offset+hwLen])
	offset += hwLen

	if offset >= len(body) {
		return PropertiesResponse{}, fmt.Errorf("%w: truncated firmware version length", ErrMalformedBody)
	}
	fwLen := int(body[offset])
	offset++
	if offset+fwLen > len(body) {
		return PropertiesResponse{}, fmt.Errorf("%w: truncated firmware version", ErrMalformedBody)
	}
	resp.FirmwareVersion = string(body[offset : offset+fwLen])
	offset += fwLen

	if offset < len(body) {
		resp.GNSSCapability = body[offset]
		offset++
	}
	if offset < len(body) {
		resp.CommsCapability = body[offset]
		offset++
	}
	return resp, nil
}
