package jt808

import (
	"encoding/binary"
	"fmt"
	"sort"
)

// Known terminal parameter IDs (Table 12, subset relevant to this
// hardware).
const (
	ParamMainServerAddress     uint32 = 0x00000013 // STRING
	ParamServerTCPPort         uint32 = 0x00000018 // DWORD
	ParamSleepReportInterval   uint32 = 0x00000027 // DWORD, seconds
	ParamWorkingReportInterval uint32 = 0x00000029 // DWORD, seconds
	ParamMaxSpeedKmh           uint32 = 0x00000055 // DWORD
	ParamVehicleMileage        uint32 = 0x00000080 // DWORD
)

func EncodeDWORDParam(value uint32) []byte {
	buf := make([]byte, 4)
	binary.BigEndian.PutUint32(buf, value)
	return buf
}

func EncodeStringParam(value string) []byte { return []byte(value) }

// BuildSetParameters builds a 0x8103 message setting terminal parameters.
// params maps a parameter ID to its already-encoded value bytes (see
// EncodeDWORDParam/EncodeStringParam). This is a builder only — nothing in
// this codebase sends it automatically; the platform never pushes
// configuration to a terminal without an explicit operator action.
func BuildSetParameters(terminalSN string, serial uint16, params map[uint32][]byte) ([]byte, error) {
	if len(params) > 255 {
		return nil, fmt.Errorf("jt808: too many parameters: %d", len(params))
	}
	ids := make([]uint32, 0, len(params))
	for id := range params {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })

	body := make([]byte, 0, 1+len(params)*8)
	body = append(body, byte(len(params)))
	for _, id := range ids {
		value := params[id]
		if len(value) > 255 {
			return nil, fmt.Errorf("jt808: parameter 0x%08x value too long: %d bytes", id, len(value))
		}
		body = binary.BigEndian.AppendUint32(body, id)
		body = append(body, byte(len(value)))
		body = append(body, value...)
	}
	return EncodeFrame(MsgSetTerminalParameters, terminalSN, serial, body)
}

// BuildQueryParameters builds an empty-body 0x8104 message.
func BuildQueryParameters(terminalSN string, serial uint16) ([]byte, error) {
	return EncodeFrame(MsgQueryTerminalParameters, terminalSN, serial, nil)
}

type ParamItem struct {
	ID    uint32
	Value []byte
}

type ParamResponse struct {
	ResponseSerial uint16
	Params         []ParamItem
}

// ParseQueryParametersResponse decodes a 0x0104 response.
func ParseQueryParametersResponse(body []byte) (ParamResponse, error) {
	if len(body) < 3 {
		return ParamResponse{}, fmt.Errorf("%w: parameter response body requires at least 3 bytes, got %d", ErrMalformedBody, len(body))
	}
	response := ParamResponse{ResponseSerial: binary.BigEndian.Uint16(body[0:2])}
	count := int(body[2])
	offset := 3
	for i := 0; i < count; i++ {
		if offset+5 > len(body) {
			return ParamResponse{}, fmt.Errorf("%w: truncated parameter item at index %d", ErrMalformedBody, i)
		}
		id := binary.BigEndian.Uint32(body[offset : offset+4])
		length := int(body[offset+4])
		start := offset + 5
		end := start + length
		if end > len(body) {
			return ParamResponse{}, fmt.Errorf("%w: parameter 0x%08x declares length %d beyond body", ErrMalformedBody, id, length)
		}
		response.Params = append(response.Params, ParamItem{ID: id, Value: append([]byte(nil), body[start:end]...)})
		offset = end
	}
	return response, nil
}
