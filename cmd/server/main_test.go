package main

import "testing"

func TestConfiguredPort(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		raw       string
		expected  int
		wantError bool
	}{
		{name: "default", expected: 8899},
		{name: "configured", raw: "12345", expected: 12345},
		{name: "zero", raw: "0", wantError: true},
		{name: "too large", raw: "65536", wantError: true},
		{name: "not a number", raw: "tcp", wantError: true},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			port, err := configuredPort(test.raw)
			if (err != nil) != test.wantError {
				t.Fatalf("configuredPort(%q) error=%v", test.raw, err)
			}
			if port != test.expected {
				t.Fatalf("configuredPort(%q)=%d, want %d", test.raw, port, test.expected)
			}
		})
	}
}
