package jt808

import (
	"bytes"
	"encoding/hex"
	"testing"
)

func decodeHex(t *testing.T, raw string) []byte {
	t.Helper()
	data, err := hex.DecodeString(raw)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

// TestEscapeManualExample uses the exact worked example from the JT808 V1.0
// spec: sending 0x30 0x7e 0x08 0x7d 0x55 is encapsulated as
// 0x7e 0x30 0x7d 0x02 0x08 0x7d 0x01 0x55 0x7e.
func TestEscapeManualExample(t *testing.T) {
	t.Parallel()
	raw := decodeHex(t, "307e087d55")
	want := decodeHex(t, "307d02087d0155")
	got := Escape(raw)
	if !bytes.Equal(got, want) {
		t.Fatalf("Escape() = %x, want %x", got, want)
	}
}

func TestUnescapeManualExample(t *testing.T) {
	t.Parallel()
	escaped := decodeHex(t, "307d02087d0155")
	want := decodeHex(t, "307e087d55")
	got, err := Unescape(escaped)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("Unescape() = %x, want %x", got, want)
	}
}

func TestEscapeUnescapeRoundTrip(t *testing.T) {
	t.Parallel()
	cases := []string{
		"",
		"00",
		"7e",
		"7d",
		"7e7d",
		"7d7e",
		"0102030405",
		"7e7e7e7d7d7d",
		"0001027e7dfffe",
	}
	for _, raw := range cases {
		raw := raw
		t.Run(raw, func(t *testing.T) {
			t.Parallel()
			data := decodeHex(t, raw)
			escaped := Escape(data)
			for _, value := range escaped {
				if value == delimiter {
					t.Fatalf("Escape(%x) left a bare delimiter in %x", data, escaped)
				}
			}
			got, err := Unescape(escaped)
			if err != nil {
				t.Fatalf("Unescape(Escape(%x)) failed: %v", data, err)
			}
			if !bytes.Equal(got, data) {
				t.Fatalf("Unescape(Escape(%x)) = %x, want %x", data, got, data)
			}
		})
	}
}

func TestUnescapeRejectsBareDelimiter(t *testing.T) {
	t.Parallel()
	if _, err := Unescape(decodeHex(t, "01027e0304")); err == nil {
		t.Fatal("expected error for bare delimiter inside escaped region")
	}
}

func TestUnescapeRejectsTrailingEscapeByte(t *testing.T) {
	t.Parallel()
	if _, err := Unescape(decodeHex(t, "01027d")); err == nil {
		t.Fatal("expected error for trailing escape byte with no follower")
	}
}

func TestUnescapeRejectsInvalidEscapeSequence(t *testing.T) {
	t.Parallel()
	if _, err := Unescape(decodeHex(t, "01027d0304")); err == nil {
		t.Fatal("expected error for 0x7d followed by neither 0x01 nor 0x02")
	}
}
