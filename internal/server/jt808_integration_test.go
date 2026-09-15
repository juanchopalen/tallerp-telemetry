package server

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/juanchopalen/tallerp-telemetry/internal/delivery"
	"github.com/juanchopalen/tallerp-telemetry/internal/jt808store"
	"github.com/juanchopalen/tallerp-telemetry/internal/protocol/jt808"
	"github.com/juanchopalen/tallerp-telemetry/internal/spool"
	"github.com/juanchopalen/tallerp-telemetry/internal/telemetry"
)

func readJT808Frame(t *testing.T, connection net.Conn) jt808.Frame {
	t.Helper()
	decoder := jt808.NewDecoder(0, 0)
	buffer := make([]byte, 512)
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if err := connection.SetReadDeadline(time.Now().Add(2 * time.Second)); err != nil {
			t.Fatal(err)
		}
		n, err := connection.Read(buffer)
		if n > 0 {
			frames, decodeErrors := decoder.Push(buffer[:n])
			if len(decodeErrors) != 0 {
				t.Fatalf("unexpected jt808 decode errors: %v", decodeErrors)
			}
			if len(frames) > 0 {
				return frames[0]
			}
		}
		if err != nil {
			t.Fatalf("read jt808 frame: %v", err)
		}
	}
	t.Fatal("timed out waiting for a jt808 frame")
	return jt808.Frame{}
}

func encodeBCDTime(t *testing.T, when time.Time) []byte {
	t.Helper()
	digits := []int{
		when.Year() % 100, int(when.Month()), when.Day(),
		when.Hour(), when.Minute(), when.Second(),
	}
	out := make([]byte, 0, 6)
	for _, d := range digits {
		out = append(out, byte((d/10)<<4|(d%10)))
	}
	return out
}

func buildJT808LocationBody(t *testing.T, when time.Time) []byte {
	t.Helper()
	body := make([]byte, 0, 28)
	body = binary.BigEndian.AppendUint32(body, 0)         // no alarms
	body = binary.BigEndian.AppendUint32(body, 1<<0|1<<1) // ACC on, positioned
	body = binary.BigEndian.AppendUint32(body, 10_500_000)
	body = binary.BigEndian.AppendUint32(body, 66_900_000)
	body = binary.BigEndian.AppendUint16(body, 100)
	body = binary.BigEndian.AppendUint16(body, 300)
	body = binary.BigEndian.AppendUint16(body, 45)
	body = append(body, encodeBCDTime(t, when)...)
	return body
}

// TestGT06GT02AndJT808ShareSpoolAndDeliveryBatch extends
// TestGT06AndGT02ShareSpoolAndDeliveryBatch with a third, JT808 connection
// that registers, authenticates, and sends a location report, proving all
// three protocol families land in the same durable spool and the same
// delivery batch without cross-contamination.
func TestGT06GT02AndJT808ShareSpoolAndDeliveryBatch(t *testing.T) {
	queue, err := spool.Open(filepath.Join(t.TempDir(), "multiprotocol-jt808.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer queue.Close()

	authStore, err := jt808store.Open(queue.DB())
	if err != nil {
		t.Fatal(err)
	}

	config := DefaultConfig()
	config.ReadTimeout = 2 * time.Second
	config.WriteTimeout = time.Second
	config.EventSink = queue
	config.JT808AuthStore = authStore
	listener, server, serveDone := startTestServerWithConfig(t, config)

	gt06Connection := dialTestServer(t, listener)
	writeAll(t, gt06Connection, decodeHex(t, loginFrame))
	assertRead(t, gt06Connection, decodeHex(t, "787805010001d9dc0d0a"))
	writeAll(t, gt06Connection, decodeHex(t, locationFrame))

	gt02Connection := dialTestServer(t, listener)
	writeAll(t, gt02Connection, decodeHex(t, "78781101086654505250169628013201000169040d0a"))
	assertRead(t, gt02Connection, decodeHex(t, "787805010001d9dc0d0a"))
	writeAll(t, gt02Connection, decodeHex(t, "78782431140813082d04cb026c6f6c0c3713a600140001cc000125fc06146402000000000bcd210d0a"))

	jt808Connection := dialTestServer(t, listener)
	terminalSN := "013800100034"

	regBody := make([]byte, 0, 37)
	regBody = append(regBody, 0x00, 0x00, 0x00, 0x00)
	manufacturer := make([]byte, 5)
	copy(manufacturer, "SZRXT")
	regBody = append(regBody, manufacturer...)
	model := make([]byte, 20)
	copy(model, "RXTmt2503d32m")
	regBody = append(regBody, model...)
	regBody = append(regBody, make([]byte, 7)...) // terminal ID
	regBody = append(regBody, 0x00)               // plate color

	regFrame, err := jt808.EncodeFrame(jt808.MsgTerminalRegistration, terminalSN, 1, regBody)
	if err != nil {
		t.Fatal(err)
	}
	writeAll(t, jt808Connection, regFrame)
	regACK := readJT808Frame(t, jt808Connection)
	if regACK.MessageID != jt808.MsgRegistrationResponse {
		t.Fatalf("registration ACK MessageID = 0x%04x, want 0x%04x", regACK.MessageID, jt808.MsgRegistrationResponse)
	}
	if len(regACK.Body) < 4 || regACK.Body[2] != jt808.RegistrationSuccess {
		t.Fatalf("unexpected registration ACK body: %x", regACK.Body)
	}
	authCode := string(regACK.Body[3:])

	authFrame, err := jt808.EncodeFrame(jt808.MsgTerminalAuth, terminalSN, 2, []byte(authCode))
	if err != nil {
		t.Fatal(err)
	}
	writeAll(t, jt808Connection, authFrame)
	authACK := readJT808Frame(t, jt808Connection)
	if authACK.MessageID != jt808.MsgPlatformGeneralResponse || len(authACK.Body) < 5 || authACK.Body[4] != jt808.ResultSuccess {
		t.Fatalf("unexpected auth ACK: id=0x%04x body=%x", authACK.MessageID, authACK.Body)
	}

	locationBody := buildJT808LocationBody(t, time.Now().UTC())
	locationFrameBytes, err := jt808.EncodeFrame(jt808.MsgLocationReport, terminalSN, 3, locationBody)
	if err != nil {
		t.Fatal(err)
	}
	writeAll(t, jt808Connection, locationFrameBytes)
	locationACK := readJT808Frame(t, jt808Connection)
	if locationACK.MessageID != jt808.MsgPlatformGeneralResponse {
		t.Fatalf("unexpected location ACK MessageID = 0x%04x", locationACK.MessageID)
	}

	const wantPending = 7 // gt06: login+location, gt02: login+lbs, jt808: registration+auth+location
	var pending []spool.Item
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		pending, err = queue.Pending(context.Background(), 20, time.Now().Add(time.Second))
		if err != nil {
			t.Fatal(err)
		}
		if len(pending) == wantPending {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if len(pending) != wantPending {
		t.Fatalf("pending events=%d, want %d", len(pending), wantPending)
	}

	batchChannel := make(chan []telemetry.TelemetryEvent, 1)
	api := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		var body struct {
			Events []telemetry.TelemetryEvent `json:"events"`
		}
		if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
			http.Error(writer, err.Error(), http.StatusBadRequest)
			return
		}
		batchChannel <- body.Events
		accepted := make([]string, len(body.Events))
		for index, event := range body.Events {
			accepted[index] = event.EventID
		}
		_ = json.NewEncoder(writer).Encode(map[string]any{"accepted": accepted, "duplicates": []string{}, "rejected": []string{}})
	}))
	defer api.Close()

	worker := delivery.New(delivery.Config{APIURL: api.URL, BatchSize: 20, Interval: 10 * time.Millisecond}, queue, api.Client(), slog.New(slog.NewJSONHandler(io.Discard, nil)), &telemetry.Metrics{})
	worker.Start(context.Background())
	defer worker.Shutdown(context.Background())

	select {
	case batch := <-batchChannel:
		if len(batch) != wantPending {
			t.Fatalf("delivered batch=%d, want %d", len(batch), wantPending)
		}
		families := map[string]int{}
		for _, event := range batch {
			families[event.ProtocolFamily]++
		}
		if families["gt06"] != 2 || families["gt02"] != 2 || families["jt808"] != 3 {
			t.Fatalf("unexpected protocol families: %v", families)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("multiprotocol batch was not delivered")
	}

	_ = gt06Connection.Close()
	_ = gt02Connection.Close()
	_ = jt808Connection.Close()
	stopTestServer(t, server, serveDone)
}
