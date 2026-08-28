package config

import (
	"fmt"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	TelemetryPort     int
	HealthPort        int
	SpoolPath         string
	SpoolRetention    time.Duration
	APIURL            string
	TelemetryToken    string
	DeliveryBatchSize int
	DeliveryInterval  time.Duration
	HTTPTimeout       time.Duration
	CriticalPending   int64
}

func Load() (Config, error) {
	telemetryPort, err := integer("TALLERP_TELEMETRY_PORT", 8899, 1, 65535)
	if err != nil {
		return Config{}, err
	}
	healthPort, err := integer("TALLERP_HEALTH_PORT", 8080, 1, 65535)
	if err != nil {
		return Config{}, err
	}
	batchSize, err := integer("TALLERP_DELIVERY_BATCH_SIZE", 100, 1, 10000)
	if err != nil {
		return Config{}, err
	}
	intervalSeconds, err := integer("TALLERP_DELIVERY_INTERVAL_SECONDS", 5, 1, 86400)
	if err != nil {
		return Config{}, err
	}
	timeoutSeconds, err := integer("TALLERP_HTTP_TIMEOUT_SECONDS", 10, 1, 3600)
	if err != nil {
		return Config{}, err
	}
	retentionHours, err := integer("TALLERP_SPOOL_RETENTION_HOURS", 24, 1, 24*365)
	if err != nil {
		return Config{}, err
	}
	criticalPending, err := integer("TALLERP_SPOOL_CRITICAL_PENDING", 100000, 10001, int(^uint(0)>>1))
	if err != nil {
		return Config{}, err
	}

	apiURL := strings.TrimRight(strings.TrimSpace(os.Getenv("TALLERP_API_URL")), "/")
	if apiURL == "" {
		apiURL = "https://tallerp.com"
	}
	parsed, err := url.Parse(apiURL)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return Config{}, fmt.Errorf("TALLERP_API_URL must be an absolute HTTP(S) URL")
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return Config{}, fmt.Errorf("TALLERP_API_URL must use http or https")
	}
	spoolPath := strings.TrimSpace(os.Getenv("TALLERP_SPOOL_PATH"))
	if spoolPath == "" {
		spoolPath = "/var/lib/tallerp-telemetry/spool.db"
	}

	return Config{
		TelemetryPort: telemetryPort, HealthPort: healthPort, SpoolPath: spoolPath,
		SpoolRetention: time.Duration(retentionHours) * time.Hour, APIURL: apiURL,
		TelemetryToken:    strings.TrimSpace(os.Getenv("TALLERP_TELEMETRY_TOKEN")),
		DeliveryBatchSize: batchSize, DeliveryInterval: time.Duration(intervalSeconds) * time.Second,
		HTTPTimeout: time.Duration(timeoutSeconds) * time.Second, CriticalPending: int64(criticalPending),
	}, nil
}

func integer(name string, fallback, minimum, maximum int) (int, error) {
	raw := strings.TrimSpace(os.Getenv(name))
	if raw == "" {
		return fallback, nil
	}
	value, err := strconv.Atoi(raw)
	if err != nil || value < minimum || value > maximum {
		return 0, fmt.Errorf("%s must be an integer from %d to %d", name, minimum, maximum)
	}
	return value, nil
}
