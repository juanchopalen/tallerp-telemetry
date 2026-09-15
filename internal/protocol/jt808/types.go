package jt808

import "errors"

// Message IDs implemented by this package.
const (
	MsgTerminalGeneralResponse uint16 = 0x0001
	MsgPlatformGeneralResponse uint16 = 0x8001
	MsgTerminalHeartbeat       uint16 = 0x0002
	MsgTerminalRegistration    uint16 = 0x0100
	MsgRegistrationResponse    uint16 = 0x8100
	MsgTerminalAuth            uint16 = 0x0102
	MsgSetTerminalParameters   uint16 = 0x8103
	MsgQueryTerminalParameters uint16 = 0x8104
	MsgQueryParametersResponse uint16 = 0x0104
	MsgTerminalControl         uint16 = 0x8105
	MsgQueryTerminalProperties uint16 = 0x8107
	MsgTerminalPropertiesResp  uint16 = 0x0107
	MsgLocationReport          uint16 = 0x0200
)

// General/platform response results (Table 5/6).
const (
	ResultSuccess        byte = 0
	ResultFailure        byte = 1
	ResultMessageError   byte = 2
	ResultNotSupported   byte = 3
	ResultAlarmConfirmed byte = 4 // platform general response only
)

// Registration results (Table 8).
const (
	RegistrationSuccess               byte = 0
	RegistrationVehicleAlreadyExists  byte = 1
	RegistrationNoSuchVehicle         byte = 2
	RegistrationTerminalAlreadyExists byte = 3
	RegistrationNoSuchTerminal        byte = 4
)

// ErrMalformedBody signals a message body that is structurally invalid for
// its message ID (too short, or a required field cannot be parsed).
var ErrMalformedBody = errors.New("malformed jt808 message body")

// ErrNotAuthenticated signals a message that arrived before the terminal
// completed authentication and isn't on the pre-auth allow-list. The
// caller still gets an ACK to send (a failure response), this error is
// only a signal for logging/metrics.
var ErrNotAuthenticated = errors.New("jt808 message received before authentication")

type RegistrationPacket struct {
	ProvinceID   uint16
	CityCountyID uint16
	Manufacturer string
	Model        string
	TerminalID   string
	PlateColor   byte
	VehicleID    string
}

type AuthPacket struct {
	AuthCode string
}

type GeneralResponsePacket struct {
	ResponseSerial uint16
	ReplyID        uint16
	Result         byte
}

func trimTrailingNulls(data []byte) string {
	end := len(data)
	for end > 0 && data[end-1] == 0x00 {
		end--
	}
	return string(data[:end])
}
