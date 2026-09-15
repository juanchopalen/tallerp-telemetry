package jt808

import (
	"encoding/hex"
	"testing"
)

func TestCalculateChecksumExamples(t *testing.T) {
	t.Parallel()
	tests := map[string]byte{
		"01":         0x01,
		"0102":       0x03,
		"000000":     0x00,
		"ff00ff":     0x00,
		"0102030405": 0x01,
		"307e087d55": 0x30 ^ 0x7e ^ 0x08 ^ 0x7d ^ 0x55,
	}
	for raw, expected := range tests {
		raw, expected := raw, expected
		t.Run(raw, func(t *testing.T) {
			t.Parallel()
			data, err := hex.DecodeString(raw)
			if err != nil {
				t.Fatal(err)
			}
			if got := CalculateChecksum(data); got != expected {
				t.Fatalf("CalculateChecksum() = %02x, want %02x", got, expected)
			}
		})
	}
}
