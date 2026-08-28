package config

import "testing"

func TestLoadDefaults(t *testing.T) {
	for _, name := range []string{"TALLERP_TELEMETRY_PORT", "TALLERP_HEALTH_PORT", "TALLERP_SPOOL_PATH", "TALLERP_SPOOL_RETENTION_HOURS", "TALLERP_SPOOL_CRITICAL_PENDING", "TALLERP_API_URL", "TALLERP_TELEMETRY_TOKEN", "TALLERP_DELIVERY_BATCH_SIZE", "TALLERP_DELIVERY_INTERVAL_SECONDS", "TALLERP_HTTP_TIMEOUT_SECONDS"} {
		t.Setenv(name, "")
	}
	configuration, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if configuration.TelemetryPort != 8899 || configuration.HealthPort != 8080 {
		t.Fatalf("unexpected default ports: %+v", configuration)
	}
	if configuration.SpoolPath != "/var/lib/tallerp-telemetry/spool.db" {
		t.Fatalf("unexpected spool path %q", configuration.SpoolPath)
	}
}

func TestLoadRejectsInvalidConfiguration(t *testing.T) {
	t.Setenv("TALLERP_TELEMETRY_PORT", "0")
	if _, err := Load(); err == nil {
		t.Fatal("Load() accepted invalid telemetry port")
	}
	t.Setenv("TALLERP_TELEMETRY_PORT", "")
	t.Setenv("TALLERP_API_URL", "tallerp.example")
	if _, err := Load(); err == nil {
		t.Fatal("Load() accepted relative API URL")
	}
}
