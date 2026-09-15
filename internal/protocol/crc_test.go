package protocol

import (
	"encoding/hex"
	"testing"
)

func TestCalculateCRCExamples(t *testing.T) {
	t.Parallel()
	tests := map[string]uint16{
		"05010001":                         0xd9dc,
		"05130001":                         0xe9f1,
		"05160005":                         0x9668,
		"0d0101234567890123450001":         0x8cdd,
		"11010866545052501696280132010001": 0x6904,
		"0a130005040001000e":               0x0e4a,
		"2431140813082d04cb026c6f6c0c3713a600140001cc000125fc06146402000000000b": 0xcd21,
	}
	for raw, expected := range tests {
		raw, expected := raw, expected
		t.Run(raw, func(t *testing.T) {
			t.Parallel()
			data, err := hex.DecodeString(raw)
			if err != nil {
				t.Fatal(err)
			}
			if got := CalculateCRC(data); got != expected {
				t.Fatalf("CalculateCRC() = %04x, want %04x", got, expected)
			}
		})
	}
}
