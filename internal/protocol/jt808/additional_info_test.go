package jt808

import (
	"encoding/binary"
	"testing"
)

func tlv(id byte, value []byte) []byte {
	return append([]byte{id, byte(len(value))}, value...)
}

func TestParseAdditionalItemsKnownIDs(t *testing.T) {
	t.Parallel()

	mileage := make([]byte, 4)
	binary.BigEndian.PutUint32(mileage, 1234) // 123.4 km
	fuel := make([]byte, 2)
	binary.BigEndian.PutUint16(fuel, 500) // 50.0 L
	voltage := make([]byte, 2)
	binary.BigEndian.PutUint16(voltage, 800) // 80.0 V per the manual's own 0x320 example... using round numbers here

	data := append([]byte{}, tlv(infoMileage, mileage)...)
	data = append(data, tlv(infoFuelVolume, fuel)...)
	data = append(data, tlv(infoBatteryPercent, []byte{77})...)
	data = append(data, tlv(infoGSMSignal, []byte{28})...)
	data = append(data, tlv(infoSatellites, []byte{9})...)
	data = append(data, tlv(infoVoltage, voltage)...)

	items, err := ParseAdditionalItems(data)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 6 {
		t.Fatalf("got %d items, want 6", len(items))
	}
	if *items[0].MileageKm != 123.4 {
		t.Errorf("MileageKm = %v, want 123.4", *items[0].MileageKm)
	}
	if *items[1].FuelVolumeL != 50.0 {
		t.Errorf("FuelVolumeL = %v, want 50.0", *items[1].FuelVolumeL)
	}
	if *items[2].BatteryPercent != 77 {
		t.Errorf("BatteryPercent = %v, want 77", *items[2].BatteryPercent)
	}
	if *items[3].GSMSignal != 28 {
		t.Errorf("GSMSignal = %v, want 28", *items[3].GSMSignal)
	}
	if *items[4].Satellites != 9 {
		t.Errorf("Satellites = %v, want 9", *items[4].Satellites)
	}
	if *items[5].VoltageVolts != 80.0 {
		t.Errorf("VoltageVolts = %v, want 80.0", *items[5].VoltageVolts)
	}
}

func TestParseAdditionalItemsUnknownIDIsSkippedNotFatal(t *testing.T) {
	t.Parallel()
	data := append([]byte{}, tlv(0xAB, []byte{0x01, 0x02, 0x03})...)
	data = append(data, tlv(infoSatellites, []byte{7})...)

	items, err := ParseAdditionalItems(data)
	if err != nil {
		t.Fatalf("unknown TLV ID must not be a parse error: %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("got %d items, want 2", len(items))
	}
	if items[0].ID != 0xAB || items[0].Satellites != nil {
		t.Fatalf("expected unknown item preserved with Raw only, got %+v", items[0])
	}
	if *items[1].Satellites != 7 {
		t.Fatalf("expected known item after an unknown one to still parse, got %+v", items[1])
	}
}

func TestParseAdditionalItemsRejectsTruncatedHeader(t *testing.T) {
	t.Parallel()
	if _, err := ParseAdditionalItems([]byte{0x01}); err == nil {
		t.Fatal("expected an error for a truncated TLV header")
	}
}

func TestParseAdditionalItemsRejectsLengthBeyondBody(t *testing.T) {
	t.Parallel()
	if _, err := ParseAdditionalItems([]byte{0x01, 0xff, 0x00}); err == nil {
		t.Fatal("expected an error for a declared length exceeding the remaining bytes")
	}
}

func TestDecode2GCells(t *testing.T) {
	t.Parallel()
	record := make([]byte, 8)
	binary.BigEndian.PutUint16(record[0:2], 460)  // mcc
	record[2] = 0                                 // mnc
	binary.BigEndian.PutUint16(record[3:5], 1234) // lac
	binary.BigEndian.PutUint16(record[5:7], 5678) // cellid
	record[7] = 40                                // signal

	value := append([]byte{1}, record...)
	cells := decode2GCells(value)
	if len(cells) != 1 {
		t.Fatalf("got %d cells, want 1", len(cells))
	}
	if cells[0].LAC != 1234 || cells[0].CellID != 5678 || cells[0].RSSI != 40 {
		t.Fatalf("unexpected cell: %+v", cells[0])
	}
}

func TestDecodeWiFi(t *testing.T) {
	t.Parallel()
	record := append([]byte{0xaa, 0xbb, 0xcc, 0xdd, 0xee, 0xff}, 55)
	value := append([]byte{1}, record...)
	aps := decodeWiFi(value)
	if len(aps) != 1 {
		t.Fatalf("got %d access points, want 1", len(aps))
	}
	if aps[0].MAC != "aa:bb:cc:dd:ee:ff" || aps[0].SignalStrength != 55 {
		t.Fatalf("unexpected access point: %+v", aps[0])
	}
}

func TestMileageNeverOverwritesOtherFields(t *testing.T) {
	t.Parallel()
	mileage := make([]byte, 4)
	binary.BigEndian.PutUint32(mileage, 100)
	items, err := ParseAdditionalItems(tlv(infoMileage, mileage))
	if err != nil {
		t.Fatal(err)
	}
	if items[0].FuelVolumeL != nil || items[0].BatteryPercent != nil {
		t.Fatalf("mileage item must not populate unrelated fields: %+v", items[0])
	}
}
