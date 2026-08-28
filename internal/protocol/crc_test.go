package protocol

import (
	"encoding/hex"
	"testing"
)

func TestCalculateCRCExamples(t *testing.T) {
	t.Parallel()
	tests := map[string]uint16{
		"05010001":                 0xd9dc,
		"05130001":                 0xe9f1,
		"05160005":                 0x9668,
		"0d0101234567890123450001": 0x8cdd,
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
