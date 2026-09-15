package jt808

import (
	"encoding/binary"
	"fmt"
	"time"
)

const locationFixedBodyLength = 28

// Alarm flag bits (Table 19).
const (
	alarmBitEmergency             = 0
	alarmBitOverspeed             = 1
	alarmBitMainPowerUndervoltage = 7
	alarmBitMainPowerOff          = 8
	alarmBitVibration             = 15
	alarmBitLowBattery            = 16
	alarmBitDismantle             = 17
	alarmBitAreaEnterExit         = 20
	alarmBitIllegalMovement       = 28
	alarmBitCollision             = 29
	alarmBitRollover              = 30
)

var alarmBitNames = map[uint]string{
	alarmBitEmergency:             "emergency",
	alarmBitOverspeed:             "overspeed",
	alarmBitMainPowerUndervoltage: "main_power_undervoltage",
	alarmBitMainPowerOff:          "main_power_off",
	alarmBitVibration:             "vibration",
	alarmBitLowBattery:            "low_battery",
	alarmBitDismantle:             "dismantle",
	alarmBitAreaEnterExit:         "area_enter_exit",
	alarmBitIllegalMovement:       "illegal_movement",
	alarmBitCollision:             "collision",
	alarmBitRollover:              "rollover",
}

// Terminal status bits (Table 20).
const (
	statusBitACC               = 0
	statusBitPositioned        = 1
	statusBitLatitudeSouth     = 2
	statusBitLongitudeWest     = 3
	statusBitFortify           = 6
	statusBitOilCircuit        = 10
	statusBitVehicleCircuit    = 11
	statusBitGPSInUse          = 18
	statusBitBeidouInUse       = 19
	statusBitGLONASSInUse      = 20
	statusBitGalileoInUse      = 21
	statusBitExternalPower     = 22
	statusBitWirelessDeviceOff = 23
)

type LocationPacket struct {
	AlarmFlags      uint32
	AlarmTypes      []string
	TerminalStatus  uint32
	Latitude        float64
	Longitude       float64
	AltitudeMeters  int32
	SpeedKmh        float64
	Heading         uint16
	GPSAt           time.Time
	ACC             bool
	GPSLocated      bool
	ExternalPower   bool
	AdditionalItems []AdditionalItem
}

func statusBit(status uint32, bit uint) bool { return status&(1<<bit) != 0 }

func decodeAlarmTypes(alarmFlags uint32) []string {
	var alarms []string
	for bit := uint(0); bit < 32; bit++ {
		if alarmFlags&(1<<bit) == 0 {
			continue
		}
		if name, ok := alarmBitNames[bit]; ok {
			alarms = append(alarms, name)
		}
	}
	return alarms
}

// decodeBCDTime parses the BCD[6] YY-MM-DD-hh-mm-ss time field. The
// manufacturer's PDF documents GMT+8 domestic / GMT overseas as the
// firmware default unless configured otherwise by SMS; for these
// Venezuela-bound devices we deliberately do NOT assume GMT+8 and instead
// interpret the field as UTC, pending validation against real hardware
// (see docs/jt808.md).
func decodeBCDTime(data []byte) (time.Time, error) {
	if len(data) != 6 {
		return time.Time{}, fmt.Errorf("%w: BCD time requires 6 bytes, got %d", ErrMalformedBody, len(data))
	}
	digits, err := decodeBCD(data)
	if err != nil {
		return time.Time{}, fmt.Errorf("location time: %w", err)
	}
	year := 2000 + int(digits[0]-'0')*10 + int(digits[1]-'0')
	month := int(digits[2]-'0')*10 + int(digits[3]-'0')
	day := int(digits[4]-'0')*10 + int(digits[5]-'0')
	hour := int(digits[6]-'0')*10 + int(digits[7]-'0')
	minute := int(digits[8]-'0')*10 + int(digits[9]-'0')
	second := int(digits[10]-'0')*10 + int(digits[11]-'0')
	return time.Date(year, time.Month(month), day, hour, minute, second, 0, time.UTC), nil
}

// ParseLocation decodes a 0x0200 location report: the 28-byte fixed part
// plus a variable list of additional-info TLV items. An unknown TLV ID is
// always safely skippable and never causes a parse failure.
func ParseLocation(body []byte) (LocationPacket, error) {
	if len(body) < locationFixedBodyLength {
		return LocationPacket{}, fmt.Errorf("%w: location body requires at least %d bytes, got %d", ErrMalformedBody, locationFixedBodyLength, len(body))
	}
	alarmFlags := binary.BigEndian.Uint32(body[0:4])
	status := binary.BigEndian.Uint32(body[4:8])
	rawLat := binary.BigEndian.Uint32(body[8:12])
	rawLon := binary.BigEndian.Uint32(body[12:16])
	altitude := int32(binary.BigEndian.Uint16(body[16:18]))
	rawSpeed := binary.BigEndian.Uint16(body[18:20])
	heading := binary.BigEndian.Uint16(body[20:22])
	gpsAt, err := decodeBCDTime(body[22:28])
	if err != nil {
		return LocationPacket{}, err
	}

	latitude := float64(rawLat) / 1_000_000
	if statusBit(status, statusBitLatitudeSouth) {
		latitude = -latitude
	}
	longitude := float64(rawLon) / 1_000_000
	if statusBit(status, statusBitLongitudeWest) {
		longitude = -longitude
	}

	items, err := ParseAdditionalItems(body[locationFixedBodyLength:])
	if err != nil {
		return LocationPacket{}, err
	}

	return LocationPacket{
		AlarmFlags:      alarmFlags,
		AlarmTypes:      decodeAlarmTypes(alarmFlags),
		TerminalStatus:  status,
		Latitude:        latitude,
		Longitude:       longitude,
		AltitudeMeters:  altitude,
		SpeedKmh:        float64(rawSpeed) / 10.0,
		Heading:         heading,
		GPSAt:           gpsAt,
		ACC:             statusBit(status, statusBitACC),
		GPSLocated:      statusBit(status, statusBitPositioned),
		ExternalPower:   statusBit(status, statusBitExternalPower),
		AdditionalItems: items,
	}, nil
}
