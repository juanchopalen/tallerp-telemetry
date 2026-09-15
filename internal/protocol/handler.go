package protocol

import (
	"errors"
	"time"

	"github.com/juanchopalen/tallerp-telemetry/internal/telemetry"
)

type Family string

const (
	FamilyUnknown Family = ""
	FamilyGT06    Family = "gt06"
	FamilyGT02    Family = "gt02"
	FamilyJT808   Family = "jt808"
)

var ErrUnsupportedMessage = errors.New("unsupported protocol message")

type SessionContext struct {
	IMEI       string
	Family     Family
	DeviceType uint16

	// TerminalSN and Authenticated are JT808-only. TerminalSN is the
	// device identity from the JT808 header (BCD[6], decoded to a digit
	// string) and, once known, is also mirrored into IMEI so the rest of
	// the pipeline (event persistence, Laravel ingestion) keeps working
	// off a single identifier slot. Authenticated gates which messages a
	// terminal may send before a successful 0x0102 authentication.
	TerminalSN    string
	Authenticated bool
	// OutSerial is the platform's own outgoing message serial counter for
	// this JT808 session, cyclically accumulating from 0 per the spec.
	OutSerial uint16
}

type HandleResult struct {
	Events []telemetry.TelemetryEvent
	ACK    []byte
}

type Handler interface {
	Name() Family
	CanHandle(Frame, *SessionContext) bool
	Handle(Frame, *SessionContext, time.Time) (HandleResult, error)
}
