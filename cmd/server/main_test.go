package main

import (
	"testing"

	appconfig "github.com/juanchopalen/tallerp-telemetry/internal/config"
)

func TestConfiguredPort(t *testing.T) {
	for _, name := range []string{"TALLERP_HEALTH_PORT", "TALLERP_SPOOL_RETENTION_HOURS", "TALLERP_SPOOL_CRITICAL_PENDING", "TALLERP_API_URL", "TALLERP_DELIVERY_BATCH_SIZE", "TALLERP_DELIVERY_INTERVAL_SECONDS", "TALLERP_HTTP_TIMEOUT_SECONDS"} {
		t.Setenv(name, "")
	}
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
		t.Run(test.name, func(t *testing.T) {
			t.Setenv("TALLERP_TELEMETRY_PORT", test.raw)
			configuration, err := appconfig.Load()
			if (err != nil) != test.wantError {
				t.Fatalf("Load() error=%v", err)
			}
			if err == nil && configuration.TelemetryPort != test.expected {
				t.Fatalf("TelemetryPort=%d, want %d", configuration.TelemetryPort, test.expected)
			}
		})
	}
}
