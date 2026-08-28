package protocol

import (
	"math"
	"testing"
	"time"
)

const (
	locationExample  = "78781f12160611073835cf026c6cf70c3715200b147601cc002633000e81001db3a80d0a"
	heartbeatExample = "78780a134406030b02001fb4e20d0a"
)

func TestParseLocationExample(t *testing.T) {
	t.Parallel()
	frame := decodeOne(t, locationExample)
	packet, err := ParseLocation(frame, "123456789012345")
	if err != nil {
		t.Fatal(err)
	}
	expectedTime := time.Date(2022, 6, 17, 7, 56, 53, 0, time.UTC)
	if !packet.GPSAt.Equal(expectedTime) || packet.GPSAt.Location() != time.UTC {
		t.Fatalf("GPSAt=%v", packet.GPSAt)
	}
	if !closeEnough(packet.Latitude, float64(0x026c6cf7)/1_800_000) || !closeEnough(packet.Longitude, float64(0x0c371520)/1_800_000) {
		t.Fatalf("coordinates=(%f,%f)", packet.Latitude, packet.Longitude)
	}
	if packet.SpeedKmh != 11 || packet.Heading != 118 || packet.Satellites != 15 {
		t.Fatalf("unexpected GPS values: %+v", packet)
	}
	if !packet.GPSLocated || !packet.RealtimeGPS || packet.ACC == nil || *packet.ACC {
		t.Fatalf("unexpected GPS flags: %+v", packet)
	}
	if packet.MCC != 460 || packet.MNC != 0 || packet.LAC != 0x2633 || packet.CellID != 0x000e81 || packet.Serial != 0x001d {
		t.Fatalf("unexpected LBS values: %+v", packet)
	}
}

func TestCoordinateSigns(t *testing.T) {
	t.Parallel()
	if got := DecodeLatitude(1_800_000, true); got != 1 {
		t.Fatalf("north latitude=%f", got)
	}
	if got := DecodeLatitude(1_800_000, false); got != -1 {
		t.Fatalf("south latitude=%f", got)
	}
	if got := DecodeLongitude(3_600_000, true); got != 2 {
		t.Fatalf("east longitude=%f", got)
	}
	if got := DecodeLongitude(3_600_000, false); got != -2 {
		t.Fatalf("west longitude=%f", got)
	}
}

func TestParseLocationValidation(t *testing.T) {
	t.Parallel()
	frame := decodeOne(t, locationExample)
	tests := []struct {
		name  string
		at    int
		value byte
	}{
		{name: "calendar date", at: 1, value: 2},
		{name: "GPS information length", at: 6, value: 0xbb},
		{name: "heading", at: 16, value: 0x17},
	}
	frame.Content[2] = 29
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			copyFrame := frame
			copyFrame.Content = append([]byte(nil), frame.Content...)
			copyFrame.Content[test.at] = test.value
			if test.name == "heading" {
				copyFrame.Content[17] = 0xff
			}
			if _, err := ParseLocation(copyFrame, ""); err == nil {
				t.Fatal("expected validation error")
			}
		})
	}
}

func TestParseHeartbeatExample(t *testing.T) {
	t.Parallel()
	packet, err := ParseHeartbeat(decodeOne(t, heartbeatExample), "123456789012345")
	if err != nil {
		t.Fatal(err)
	}
	if packet.TerminalInformation != 0x44 || packet.ACC || !packet.GPSLocated || !packet.ExternalPower {
		t.Fatalf("unexpected terminal flags: %+v", packet)
	}
	if packet.VoltageLevel != 6 || packet.GSMSignal != 3 || packet.ExternalVoltage != 11 || packet.Language != 2 || packet.Serial != 0x001f {
		t.Fatalf("unexpected heartbeat: %+v", packet)
	}
}

func TestParseAlarmExampleAndTypes(t *testing.T) {
	t.Parallel()
	frame := alarmFrameExample(t)
	packet, err := ParseAlarm(frame, "123456789012345")
	if err != nil {
		t.Fatal(err)
	}
	if packet.AlarmType != AlarmNormal || packet.Heading != 332 || packet.Satellites != 11 || !packet.ACC || !packet.GPSLocated {
		t.Fatalf("unexpected alarm: %+v", packet)
	}
	if packet.MCC != 460 || packet.CellID != 0x001fb8 || packet.VoltageLevel != 4 || packet.GSMSignal != 100 || packet.Language != 2 {
		t.Fatalf("unexpected alarm status: %+v", packet)
	}

	types := map[byte]string{
		AlarmNormal:       "normal",
		AlarmSOS:          "sos",
		AlarmPowerFailure: "power_failure",
		AlarmVibration:    "vibration",
		AlarmEnterFence:   "enter_geofence",
		AlarmExitFence:    "exit_geofence",
		AlarmOverspeed:    "overspeed",
		AlarmDisplacement: "displacement",
		AlarmLowBattery:   "low_battery",
		AlarmACCFlameout:  "acc_flameout",
		AlarmACCIgnition:  "acc_ignition",
	}
	for alarmType, expectedName := range types {
		copyFrame := frame
		copyFrame.Content = append([]byte(nil), frame.Content...)
		copyFrame.Content[30] = alarmType
		parsed, err := ParseAlarm(copyFrame, "")
		if err != nil || parsed.AlarmType != alarmType || AlarmTypeName(alarmType) != expectedName {
			t.Fatalf("type 0x%02x parsed=%+v name=%s err=%v", alarmType, parsed, AlarmTypeName(alarmType), err)
		}
	}
	if AlarmTypeName(0x77) != "unknown" {
		t.Fatal("unknown alarm type was not preserved")
	}
}

func TestParseAlarmValidation(t *testing.T) {
	t.Parallel()
	frame := alarmFrameExample(t)
	frame.Content = append([]byte(nil), frame.Content...)
	frame.Content[18] = 8
	if _, err := ParseAlarm(frame, ""); err == nil {
		t.Fatal("expected invalid LBS length error")
	}
}

func decodeOne(t *testing.T, raw string) Frame {
	t.Helper()
	frames, decodeErrors := NewDecoder(0, 0).Push(mustDecodeHex(t, raw))
	if len(decodeErrors) != 0 || len(frames) != 1 {
		t.Fatalf("frames=%d errors=%v", len(frames), decodeErrors)
	}
	return frames[0]
}

func alarmFrameExample(t *testing.T) Frame {
	t.Helper()
	content := mustDecodeHex(t, "13081d110c10cb026b3f3e0c371c9210154c0901cc00287d001fb84e04640002")
	raw := buildTestFrame(t, []byte{0x78, 0x78}, 0x16, content, 3)
	frames, decodeErrors := NewDecoder(0, 0).Push(raw)
	if len(decodeErrors) != 0 || len(frames) != 1 {
		t.Fatalf("frames=%d errors=%v", len(frames), decodeErrors)
	}
	return frames[0]
}

func closeEnough(left, right float64) bool { return math.Abs(left-right) < 0.0000001 }
