package jt808

import (
	"context"
	"testing"
	"time"

	"github.com/juanchopalen/tallerp-telemetry/internal/protocol"
)

// memoryAuthStore is a minimal in-memory AuthStore for handler tests, so
// they don't need to spin up SQLite.
type memoryAuthStore struct {
	codes map[string]string
}

func newMemoryAuthStore() *memoryAuthStore { return &memoryAuthStore{codes: map[string]string{}} }

func (m *memoryAuthStore) Register(_ context.Context, terminalSN, _, _ string, _ time.Time) (string, error) {
	if code, ok := m.codes[terminalSN]; ok {
		return code, nil
	}
	code := "code-" + terminalSN
	m.codes[terminalSN] = code
	return code, nil
}

func (m *memoryAuthStore) Authenticate(_ context.Context, terminalSN, authCode string) (bool, error) {
	return m.codes[terminalSN] == authCode, nil
}

func decodeSingleFrame(t *testing.T, raw []byte) Frame {
	t.Helper()
	decoder := NewDecoder(0, 0)
	frames, errs := decoder.Push(raw)
	if len(errs) != 0 {
		t.Fatalf("unexpected decode errors: %v", errs)
	}
	if len(frames) != 1 {
		t.Fatalf("got %d frames, want 1", len(frames))
	}
	return frames[0]
}

func TestDispatchRejectsLocationBeforeAuth(t *testing.T) {
	t.Parallel()
	handler := NewHandler(newMemoryAuthStore())
	session := &protocol.SessionContext{}
	raw, err := EncodeFrame(MsgLocationReport, "013800100034", 1, make([]byte, 28))
	if err != nil {
		t.Fatal(err)
	}
	frame := decodeSingleFrame(t, raw)

	result, dispatchErr := handler.Dispatch(frame, session, time.Now().UTC())
	if dispatchErr == nil {
		t.Fatal("expected an error rejecting location before authentication")
	}
	if len(result.ACK) == 0 {
		t.Fatal("expected an ACK even when rejecting pre-auth")
	}
	if len(result.Events) != 0 {
		t.Fatal("expected no events for a rejected pre-auth message")
	}
}

func TestDispatchAllowsHeartbeatBeforeAuth(t *testing.T) {
	t.Parallel()
	handler := NewHandler(newMemoryAuthStore())
	session := &protocol.SessionContext{}
	raw, err := EncodeFrame(MsgTerminalHeartbeat, "013800100034", 1, nil)
	if err != nil {
		t.Fatal(err)
	}
	frame := decodeSingleFrame(t, raw)

	result, dispatchErr := handler.Dispatch(frame, session, time.Now().UTC())
	if dispatchErr != nil {
		t.Fatalf("unexpected error: %v", dispatchErr)
	}
	if len(result.ACK) == 0 {
		t.Fatal("expected a heartbeat ACK")
	}
	if len(result.Events) != 1 || result.Events[0].EventType != "heartbeat" {
		t.Fatalf("unexpected events: %+v", result.Events)
	}
}

func TestDispatchFullRegistrationAndAuthFlow(t *testing.T) {
	t.Parallel()
	handler := NewHandler(newMemoryAuthStore())
	session := &protocol.SessionContext{}
	terminalSN := "013800100034"

	regBody := buildRegistrationBody(t, "SZRXT", "RXTmt2503d32m", "", 0, "")
	regRaw, err := EncodeFrame(MsgTerminalRegistration, terminalSN, 1, regBody)
	if err != nil {
		t.Fatal(err)
	}
	regResult, err := handler.Dispatch(decodeSingleFrame(t, regRaw), session, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	regAckFrame := decodeSingleFrame(t, regResult.ACK)
	packet, err := parseRegistrationACKBody(regAckFrame.Body)
	if err != nil {
		t.Fatal(err)
	}
	if packet.Result != RegistrationSuccess {
		t.Fatalf("registration result = %d, want success", packet.Result)
	}
	if session.Authenticated {
		t.Fatal("session must not be authenticated yet after registration alone")
	}

	authRaw, err := EncodeFrame(MsgTerminalAuth, terminalSN, 2, []byte(packet.AuthCode))
	if err != nil {
		t.Fatal(err)
	}
	authResult, err := handler.Dispatch(decodeSingleFrame(t, authRaw), session, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	if !session.Authenticated {
		t.Fatal("session should be authenticated after a correct auth code")
	}
	if session.IMEI != "" {
		t.Fatalf("Dispatch should not mutate session.IMEI directly; got %q", session.IMEI)
	}
	if len(authResult.Events) != 1 || authResult.Events[0].IMEI != terminalSN {
		t.Fatalf("expected a successful auth event carrying IMEI=terminalSN, got %+v", authResult.Events)
	}

	// A message class that's rejected pre-auth (general response, standing
	// in for anything not on the pre-auth allow-list) must no longer be
	// rejected purely for lack of authentication once the session is
	// authenticated.
	genRaw, err := EncodeFrame(MsgTerminalGeneralResponse, terminalSN, 3, []byte{0x00, 0x02, 0x01, 0x02, ResultSuccess})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := handler.Dispatch(decodeSingleFrame(t, genRaw), session, time.Now().UTC()); err != nil {
		t.Fatalf("general response should be accepted post-auth, got error: %v", err)
	}
}

func TestDispatchAuthFailureDoesNotAuthenticate(t *testing.T) {
	t.Parallel()
	handler := NewHandler(newMemoryAuthStore())
	session := &protocol.SessionContext{}
	terminalSN := "013800100034"

	authRaw, err := EncodeFrame(MsgTerminalAuth, terminalSN, 1, []byte("wrong-code"))
	if err != nil {
		t.Fatal(err)
	}
	result, err := handler.Dispatch(decodeSingleFrame(t, authRaw), session, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	if session.Authenticated {
		t.Fatal("session must not authenticate with a wrong code")
	}
	if len(result.ACK) == 0 {
		t.Fatal("expected a failure ACK")
	}
}

func TestDispatchUnknownMessageIDIsAckedNotFatal(t *testing.T) {
	t.Parallel()
	handler := NewHandler(newMemoryAuthStore())
	session := &protocol.SessionContext{Authenticated: true, TerminalSN: "013800100034"}
	raw, err := EncodeFrame(0x9999, "013800100034", 1, []byte{0x01, 0x02})
	if err != nil {
		t.Fatal(err)
	}
	result, dispatchErr := handler.Dispatch(decodeSingleFrame(t, raw), session, time.Now().UTC())
	if dispatchErr == nil {
		t.Fatal("expected a sentinel error for an unknown message ID")
	}
	if len(result.ACK) == 0 {
		t.Fatal("expected an ACK for an unknown message ID, never a silent drop")
	}
}

// parseRegistrationACKBody is a tiny local decoder for the 0x8100 body
// shape, kept local to the test since BuildRegistrationACK is the only
// producer and there's no standalone parser (the platform never needs to
// parse its own outgoing registration ACKs in production).
func parseRegistrationACKBody(body []byte) (struct {
	Result   byte
	AuthCode string
}, error) {
	var out struct {
		Result   byte
		AuthCode string
	}
	if len(body) < 3 {
		return out, ErrMalformedBody
	}
	out.Result = body[2]
	if len(body) > 3 {
		out.AuthCode = string(body[3:])
	}
	return out, nil
}
