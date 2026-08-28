package telemetry

import (
	"io"
	"log/slog"
	"time"
)

func NewLogger(output io.Writer, level slog.Leveler) *slog.Logger {
	if level == nil {
		level = slog.LevelInfo
	}
	handler := slog.NewJSONHandler(output, &slog.HandlerOptions{
		Level: level,
		ReplaceAttr: func(_ []string, attribute slog.Attr) slog.Attr {
			if attribute.Key == slog.TimeKey {
				if value, ok := attribute.Value.Any().(time.Time); ok {
					attribute.Value = slog.StringValue(value.UTC().Format(time.RFC3339Nano))
				}
			}
			return attribute
		},
	})
	return slog.New(handler)
}
