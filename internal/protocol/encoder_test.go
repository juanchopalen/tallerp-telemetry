package protocol

import (
	"encoding/hex"
	"testing"
)

func TestACKExamples(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		build    func(uint16) ([]byte, error)
		serial   uint16
		expected string
	}{
		{name: "login", build: BuildLoginACK, serial: 1, expected: "787805010001d9dc0d0a"},
		{name: "heartbeat", build: BuildHeartbeatACK, serial: 1, expected: "787805130001e9f10d0a"},
		{name: "alarm", build: BuildAlarmACK, serial: 5, expected: "78780516000596680d0a"},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			ack, err := test.build(test.serial)
			if err != nil {
				t.Fatal(err)
			}
			if got := hex.EncodeToString(ack); got != test.expected {
				t.Fatalf("ACK=%s, want %s", got, test.expected)
			}
		})
	}
}
