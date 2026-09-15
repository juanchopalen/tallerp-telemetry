package gt02

import (
	"encoding/binary"
	"encoding/hex"
	"math"
	"testing"
	"time"

	"github.com/juanchopalen/tallerp-telemetry/internal/protocol"
)

const (
	manualLoginFrame     = "78781101086654505250169628013201000169040d0a"
	manualHeartbeatFrame = "78780a130005040001000e0e4a0d0a"
	manualLocationFrame  = "78782431140813082d04cb026c6f6c0c3713a600140001cc000125fc06146402000000000bcd210d0a"
	manualAlarmFrame     = "78782732" + "140813082d34" + "c0" + "00000000" + "00000000" + "00" + "0800" +
		"08" + "01cc" + "0001" + "25fc" + "06146402" + "10" + "05" + "03" + "02" + "01" + "000d" + "4aef" + "0d0a"
	manualLBSFrame = "78783734" + "140815061714" + "00" + "01cc" + "0001" + "01" +
		"2542" + "06174403" + "43" +
		"00000000000000" + "00000000000000" + "00000000000000" + "00000000000000" +
		"000000" + "000c" + "9a70" + "0d0a"
	manualGeneralFrame = "79790020940a086654505250169646004594130269008986043910189067269900061b250d0a"
)

func TestManualLoginAndACK(t *testing.T) {
	frame := decodeFrame(t, manualLoginFrame)
	packet, err := ParseLogin(frame)
	if err != nil {
		t.Fatal(err)
	}
	if packet.IMEI != "866545052501696" || packet.DeviceType != 0x2801 || packet.TimezoneOffsetMinutes != 480 || packet.Language != 1 || packet.Serial != 1 {
		t.Fatalf("unexpected login: %+v", packet)
	}
	if got := hex.EncodeToString(BuildLoginACK(packet.Serial)); got != "787805010001d9dc0d0a" {
		t.Fatalf("login ACK=%s", got)
	}
}

func TestManualLocation(t *testing.T) {
	packet, err := ParseLocation(decodeFrame(t, manualLocationFrame), "866545052501696")
	if err != nil {
		t.Fatal(err)
	}
	wantTime := time.Date(2020, 8, 19, 8, 45, 4, 0, time.UTC)
	if !packet.GPSAt.Equal(wantTime) || packet.Satellites != 11 || packet.SpeedKmh != 0 || packet.Heading != 0 || !packet.GPSLocated || !packet.RealtimeGPS {
		t.Fatalf("unexpected GPS state: %+v", packet)
	}
	if math.Abs(packet.Latitude-22.58935777777778) > 1e-9 || math.Abs(packet.Longitude-113.85339) > 1e-9 {
		t.Fatalf("coordinates=(%.12f, %.12f)", packet.Latitude, packet.Longitude)
	}
	if packet.MCC != 460 || packet.MNC != 1 || packet.LAC != 0x25fc || packet.CellID != 0x06146402 || packet.ACC || packet.IsRetransmission || packet.Serial != 0x000b {
		t.Fatalf("unexpected network/status: %+v", packet)
	}
}

func TestLocationSignsACCAndRetransmission(t *testing.T) {
	frame := decodeFrame(t, manualLocationFrame)
	content := append([]byte(nil), frame.Content...)
	content[16], content[17] = 0x18, 0x01 // located, west, south, heading 1
	content[28], content[30] = 1, 1
	packet, err := ParseLocation(buildFrame(t, []byte{0x78, 0x78}, 0x31, content, frame.Serial), "1")
	if err != nil {
		t.Fatal(err)
	}
	if packet.Latitude >= 0 || packet.Longitude >= 0 || !packet.ACC || !packet.IsRetransmission || packet.Heading != 1 {
		t.Fatalf("unexpected signed/retransmitted location: %+v", packet)
	}
}

func TestManualHeartbeatAndPowerFormats(t *testing.T) {
	packet, err := ParseHeartbeat(decodeFrame(t, manualHeartbeatFrame), "866545052501696")
	if err != nil {
		t.Fatal(err)
	}
	if packet.Power.VoltageLevel == nil || *packet.Power.VoltageLevel != 5 || packet.GSMSignal != 4 || packet.Serial != 0x000e {
		t.Fatalf("unexpected heartbeat: %+v", packet)
	}
	percentage, err := decodePower(0xd3, 0)
	if err != nil || percentage.BatteryPercent == nil || *percentage.BatteryPercent != 83 {
		t.Fatalf("percentage=%+v err=%v", percentage, err)
	}
	external, err := decodePower(0xf3, 0x20)
	if err != nil || external.ExternalVoltage == nil || *external.ExternalVoltage != 80 {
		t.Fatalf("external=%+v err=%v", external, err)
	}
	if got := hex.EncodeToString(BuildHeartbeatACK(packet.Serial)); got != "78780513000e11060d0a" {
		t.Fatalf("heartbeat ACK=%s", got)
	}
	statusContent := []byte{0xc7, 0x05, 0x04, 0x00, 0x01}
	status, err := ParseHeartbeat(buildFrame(t, []byte{0x78, 0x78}, 0x13, statusContent, 1), "1")
	if err != nil || !status.OilElectricCut || !status.GPSLocated || !status.ExternalPower || !status.ACC || !status.Armed {
		t.Fatalf("status bits=%+v err=%v", status, err)
	}
}

func TestManualAlarmAndACK(t *testing.T) {
	packet, err := ParseAlarm(decodeFrame(t, manualAlarmFrame), "866545052501696")
	if err != nil {
		t.Fatal(err)
	}
	if packet.AlarmType != "power_failure" || packet.Location.Serial != 0x000d || packet.Location.ACC || packet.GSMSignal != 3 {
		t.Fatalf("unexpected alarm: %+v", packet)
	}
	ack := BuildAlarmACK(packet.Location.Serial)
	frame := decodeFrame(t, hex.EncodeToString(ack))
	if frame.ProtocolNumber != 0x26 || frame.Serial != 0x000d {
		t.Fatalf("unexpected alarm ACK: %x", ack)
	}
	if AlarmTypeName(0xaa) != "unknown_0xaa" {
		t.Fatal("unknown alarm code was not preserved")
	}
}

func TestManualLBSAndGeneralInformation(t *testing.T) {
	lbs, err := ParseLBS(decodeFrame(t, manualLBSFrame), "866545052501696")
	if err != nil {
		t.Fatal(err)
	}
	if lbs.MCC != 460 || lbs.MNC != 1 || len(lbs.Cells) != 1 || lbs.Cells[0].CellID != 0x06174403 {
		t.Fatalf("unexpected LBS: %+v", lbs)
	}
	general, err := ParseGeneral(decodeFrame(t, manualGeneralFrame), "866545052501696")
	if err != nil {
		t.Fatal(err)
	}
	if general.Subtype != 0x0a || general.DeviceIMEI != "866545052501696" || general.IMSI != "4600459413026900" || general.ICCID != "89860439101890672699" {
		t.Fatalf("unexpected general information: %+v", general)
	}
}

func TestWiFiPacket(t *testing.T) {
	content := make([]byte, 54+7)
	copy(content[:6], []byte{0x14, 0x08, 0x15, 0x08, 0x06, 0x38})
	binary.BigEndian.PutUint16(content[6:8], 460)
	binary.BigEndian.PutUint16(content[8:10], 1)
	binary.BigEndian.PutUint16(content[10:12], 0x2542)
	binary.BigEndian.PutUint32(content[12:16], 0x06174408)
	content[16] = 0x48
	content[52], content[53] = 5, 1
	copy(content[54:60], []byte{0x1c, 0x78, 0x39, 0x01, 0x39, 0x92})
	content[60] = 0xce
	packet, err := ParseWiFi(buildFrame(t, []byte{0x78, 0x78}, 0x33, content, 14), "866545052501696")
	if err != nil {
		t.Fatal(err)
	}
	if len(packet.AccessPoints) != 1 || packet.AccessPoints[0].MAC != "1c:78:39:01:39:92" || packet.TimingAdvance != 5 {
		t.Fatalf("unexpected WiFi: %+v", packet)
	}
}

func TestCommandResponseEncodingsAndCommandBuilder(t *testing.T) {
	asciiContent := binary.BigEndian.AppendUint32(nil, 0x01020304)
	asciiContent = append(asciiContent, 0x01)
	asciiContent = append(asciiContent, "STATUS_OK"...)
	ascii, err := ParseCommandResponse(buildFrame(t, []byte{0x79, 0x79}, 0x21, asciiContent, 7), "1")
	if err != nil || ascii.Content != "STATUS_OK" || ascii.Encoding != "ascii" || ascii.ServerFlag != 0x01020304 {
		t.Fatalf("ASCII=%+v err=%v", ascii, err)
	}
	utfContent := binary.BigEndian.AppendUint32(nil, 9)
	utfContent = append(utfContent, 0x02, 0x00, 0x4f, 0x00, 0x4b)
	utf, err := ParseCommandResponse(buildFrame(t, []byte{0x79, 0x79}, 0x21, utfContent, 8), "1")
	if err != nil || utf.Content != "OK" || utf.Encoding != "utf16-be" {
		t.Fatalf("UTF=%+v err=%v", utf, err)
	}
	command, err := BuildGT02Command(0x01020304, "STATUS#", 2, 9)
	if err != nil {
		t.Fatal(err)
	}
	decoded := decodeFrame(t, hex.EncodeToString(command))
	if decoded.ProtocolNumber != 0x80 || decoded.Serial != 9 || string(decoded.Content[5:12]) != "STATUS#" {
		t.Fatalf("unexpected command frame: %x", command)
	}
	for _, invalid := range []string{"", "STATUS", "STÁTUS#"} {
		if _, err := BuildGT02Command(1, invalid, 2, 1); err == nil {
			t.Fatalf("invalid command %q was accepted", invalid)
		}
	}
}

func TestGeneralInformationDoesNotProduceACK(t *testing.T) {
	handler := NewHandler()
	context := &protocol.SessionContext{IMEI: "866545052501696", Family: protocol.FamilyGT02}
	result, err := handler.Handle(decodeFrame(t, manualGeneralFrame), context, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	if len(result.ACK) != 0 || len(result.Events) != 1 || result.Events[0].EventType != "general_info" {
		t.Fatalf("unexpected general result: %+v", result)
	}
}

func decodeFrame(t *testing.T, value string) protocol.Frame {
	t.Helper()
	raw, err := hex.DecodeString(value)
	if err != nil {
		t.Fatal(err)
	}
	frames, decodeErrors := protocol.NewDecoder(0, 0).Push(raw)
	if len(decodeErrors) != 0 || len(frames) != 1 {
		t.Fatalf("frames=%d errors=%v raw=%s", len(frames), decodeErrors, value)
	}
	return frames[0]
}

func buildFrame(t *testing.T, header []byte, protocolNumber byte, content []byte, serial uint16) protocol.Frame {
	t.Helper()
	length := 1 + len(content) + 2 + 2
	raw := append([]byte(nil), header...)
	if header[0] == 0x79 {
		raw = binary.BigEndian.AppendUint16(raw, uint16(length))
	} else {
		raw = append(raw, byte(length))
	}
	raw = append(raw, protocolNumber)
	raw = append(raw, content...)
	raw = binary.BigEndian.AppendUint16(raw, serial)
	raw = binary.BigEndian.AppendUint16(raw, protocol.CalculateCRC(raw[2:]))
	raw = append(raw, 0x0d, 0x0a)
	return decodeFrame(t, hex.EncodeToString(raw))
}
