package jt808

import (
	"context"
	"errors"
	"time"

	"github.com/juanchopalen/tallerp-telemetry/internal/protocol"
	"github.com/juanchopalen/tallerp-telemetry/internal/telemetry"
)

// AuthStore is the durable auth-code persistence JT808 registration/auth
// needs. internal/jt808store.Store satisfies this.
type AuthStore interface {
	Register(ctx context.Context, terminalSN, manufacturer, model string, now time.Time) (string, error)
	Authenticate(ctx context.Context, terminalSN, authCode string) (bool, error)
}

// ErrAuthStoreNotConfigured is returned by unconfiguredAuthStore, used
// whenever NewHandler is given a nil store, so a missing store fails every
// registration/auth attempt with a clear error instead of a nil-pointer
// panic.
var ErrAuthStoreNotConfigured = errors.New("jt808 auth store not configured")

type unconfiguredAuthStore struct{}

func (unconfiguredAuthStore) Register(context.Context, string, string, string, time.Time) (string, error) {
	return "", ErrAuthStoreNotConfigured
}

func (unconfiguredAuthStore) Authenticate(context.Context, string, string) (bool, error) {
	return false, ErrAuthStoreNotConfigured
}

// Handler dispatches decoded JT808 frames. Unlike GT06/GT02's
// protocol.Handler, this does not implement that interface directly (JT808
// framing produces its own Frame type, not protocol.Frame) — callers in
// internal/server drive it directly.
type Handler struct {
	AuthStore AuthStore
}

func NewHandler(store AuthStore) *Handler {
	if store == nil {
		store = unconfiguredAuthStore{}
	}
	return &Handler{AuthStore: store}
}

// preAuthAllowed reports whether messageID may be processed before the
// terminal has successfully authenticated. Registration and authentication
// obviously must be allowed. Heartbeat is also allowed pre-auth as a
// deliberate compatibility choice — some firmware in the wild heartbeats
// before completing auth — everything else is rejected (with an ACK, not a
// disconnect) per the PDF's "terminal shall not send other messages before
// auth succeeds."
func preAuthAllowed(messageID uint16) bool {
	switch messageID {
	case MsgTerminalRegistration, MsgTerminalAuth, MsgTerminalHeartbeat:
		return true
	default:
		return false
	}
}

func nextOutSerial(session *protocol.SessionContext) uint16 {
	serial := session.OutSerial
	session.OutSerial++
	return serial
}

// Dispatch handles one decoded frame for a session, returning the ACK bytes
// to write (if any) and telemetry events to persist. It never panics and
// never returns an error that should tear down the connection — malformed
// bodies, unknown message IDs, and pre-auth violations are all logged
// (by the caller, using the returned error) and acked/skipped, not fatal.
func (h *Handler) Dispatch(frame Frame, session *protocol.SessionContext, receivedAt time.Time) (protocol.HandleResult, error) {
	rawHex := protocol.Hex(frame.Raw)

	if !session.Authenticated && !preAuthAllowed(frame.MessageID) {
		ack, err := BuildGeneralResponse(frame.TerminalSN, nextOutSerial(session), frame.MessageSerial, frame.MessageID, ResultFailure)
		if err != nil {
			return protocol.HandleResult{}, err
		}
		return protocol.HandleResult{ACK: ack}, ErrNotAuthenticated
	}

	switch frame.MessageID {
	case MsgTerminalHeartbeat:
		ack, err := BuildGeneralResponse(frame.TerminalSN, nextOutSerial(session), frame.MessageSerial, MsgTerminalHeartbeat, ResultSuccess)
		if err != nil {
			return protocol.HandleResult{}, err
		}
		return protocol.HandleResult{ACK: ack, Events: []telemetry.TelemetryEvent{{
			EventType: "heartbeat", IMEI: frame.TerminalSN, Protocol: "0x0002", ProtocolFamily: string(protocol.FamilyJT808),
			Serial: frame.MessageSerial, ReceivedAt: receivedAt, RawHex: rawHex, TerminalSN: strPtr(frame.TerminalSN),
		}}}, nil

	case MsgTerminalRegistration:
		packet, err := ParseRegistration(frame.Body)
		if err != nil {
			return protocol.HandleResult{}, err
		}
		authCode, err := h.AuthStore.Register(context.Background(), frame.TerminalSN, packet.Manufacturer, packet.Model, receivedAt)
		if err != nil {
			return protocol.HandleResult{}, err
		}
		ack, err := BuildRegistrationACK(frame.TerminalSN, nextOutSerial(session), frame.MessageSerial, RegistrationSuccess, authCode)
		if err != nil {
			return protocol.HandleResult{}, err
		}
		session.TerminalSN = frame.TerminalSN
		return protocol.HandleResult{ACK: ack, Events: []telemetry.TelemetryEvent{{
			EventType: "registration", IMEI: frame.TerminalSN, Protocol: "0x0100", ProtocolFamily: string(protocol.FamilyJT808),
			Serial: frame.MessageSerial, ReceivedAt: receivedAt, RawHex: rawHex, TerminalSN: strPtr(frame.TerminalSN),
		}}}, nil

	case MsgTerminalAuth:
		packet, err := ParseAuth(frame.Body)
		if err != nil {
			return protocol.HandleResult{}, err
		}
		ok, err := h.AuthStore.Authenticate(context.Background(), frame.TerminalSN, packet.AuthCode)
		if err != nil {
			return protocol.HandleResult{}, err
		}
		result := ResultFailure
		if ok {
			result = ResultSuccess
			session.Authenticated = true
			session.TerminalSN = frame.TerminalSN
		}
		ack, err := BuildGeneralResponse(frame.TerminalSN, nextOutSerial(session), frame.MessageSerial, MsgTerminalAuth, result)
		if err != nil {
			return protocol.HandleResult{}, err
		}
		event := telemetry.TelemetryEvent{
			EventType: "auth", Protocol: "0x0102", ProtocolFamily: string(protocol.FamilyJT808),
			Serial: frame.MessageSerial, ReceivedAt: receivedAt, RawHex: rawHex, TerminalSN: strPtr(frame.TerminalSN),
		}
		if ok {
			event.IMEI = frame.TerminalSN
		}
		return protocol.HandleResult{ACK: ack, Events: []telemetry.TelemetryEvent{event}}, nil

	case MsgLocationReport:
		packet, err := ParseLocation(frame.Body)
		if err != nil {
			return protocol.HandleResult{}, err
		}
		ack, err := BuildGeneralResponse(frame.TerminalSN, nextOutSerial(session), frame.MessageSerial, MsgLocationReport, ResultSuccess)
		if err != nil {
			return protocol.HandleResult{}, err
		}
		// JT808 has no dedicated alarm message ID: alarms are flags carried
		// on a location report, and a single report can carry several at
		// once. Keep EventType "location" (recordEvent still logs it at
		// Warn level when AlarmTypes is non-empty) rather than borrowing
		// GT06/GT02's single-alarm "alarm" event type, which would force
		// an artificial one-alarm-per-event choice.
		event := telemetry.TelemetryEvent{
			EventType: "location", IMEI: frame.TerminalSN, Protocol: "0x0200", ProtocolFamily: string(protocol.FamilyJT808),
			Serial: frame.MessageSerial, GPSAt: &packet.GPSAt, ReceivedAt: receivedAt, RawHex: rawHex,
			TerminalSN: strPtr(frame.TerminalSN),
			Latitude:   float64Ptr(packet.Latitude), Longitude: float64Ptr(packet.Longitude),
			SpeedKmh: float64Ptr(packet.SpeedKmh), Heading: uint16Ptr(packet.Heading),
			AltitudeMeters: int32Ptr(packet.AltitudeMeters),
			ACC:            boolPtr(packet.ACC), GPSLocated: boolPtr(packet.GPSLocated), ExternalPower: boolPtr(packet.ExternalPower),
		}
		if len(packet.AlarmTypes) > 0 {
			event.AlarmTypes = packet.AlarmTypes
		}
		applyAdditionalItems(&event, packet.AdditionalItems)
		return protocol.HandleResult{ACK: ack, Events: []telemetry.TelemetryEvent{event}}, nil

	case MsgTerminalGeneralResponse:
		if _, err := ParseGeneralResponse(frame.Body); err != nil {
			return protocol.HandleResult{}, err
		}
		return protocol.HandleResult{}, nil

	case MsgQueryParametersResponse:
		// Parse-only: nothing in this codebase sends 0x8104 automatically
		// yet, so there's nothing to correlate this response against. Kept
		// here (rather than falling into the "unsupported" default) so a
		// terminal answering a manually-issued query doesn't get logged as
		// an unknown message.
		if _, err := ParseQueryParametersResponse(frame.Body); err != nil {
			return protocol.HandleResult{}, err
		}
		return protocol.HandleResult{}, nil

	case MsgTerminalPropertiesResp:
		if _, err := ParsePropertiesResponse(frame.Body); err != nil {
			return protocol.HandleResult{}, err
		}
		return protocol.HandleResult{}, nil

	default:
		ack, err := BuildGeneralResponse(frame.TerminalSN, nextOutSerial(session), frame.MessageSerial, frame.MessageID, ResultNotSupported)
		if err != nil {
			return protocol.HandleResult{}, err
		}
		return protocol.HandleResult{ACK: ack}, protocol.ErrUnsupportedMessage
	}
}
