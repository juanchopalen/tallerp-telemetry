package jt808

import "testing"

func TestParseAuth(t *testing.T) {
	t.Parallel()
	packet, err := ParseAuth([]byte("abc123"))
	if err != nil {
		t.Fatal(err)
	}
	if packet.AuthCode != "abc123" {
		t.Fatalf("AuthCode = %q, want abc123", packet.AuthCode)
	}
}

func TestParseAuthRejectsEmptyBody(t *testing.T) {
	t.Parallel()
	if _, err := ParseAuth(nil); err == nil {
		t.Fatal("expected an error for an empty auth code")
	}
}
