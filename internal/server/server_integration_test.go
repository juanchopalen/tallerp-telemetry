package server

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/juanchopalen/tallerp-telemetry/internal/delivery"
	"github.com/juanchopalen/tallerp-telemetry/internal/protocol"
	"github.com/juanchopalen/tallerp-telemetry/internal/spool"
	"github.com/juanchopalen/tallerp-telemetry/internal/telemetry"
)

const (
	loginFrame     = "78780d01012345678901234500018cdd0d0a"
	heartbeatFrame = "78780a134406030b02001fb4e20d0a"
	locationFrame  = "78781f12160611073835cf026c6cf70c3715200b147601cc002633000e81001db3a80d0a"
)

func TestTCPTrackerSession(t *testing.T) {
	listener, server, serveDone := startTestServer(t)
	connection := dialTestServer(t, listener)
	defer connection.Close()

	login := decodeHex(t, loginFrame)
	invalidLogin := append([]byte(nil), login...)
	invalidLogin[len(invalidLogin)-4] ^= 0xff
	writeAll(t, connection, append(invalidLogin, login...))
	assertRead(t, connection, decodeHex(t, "787805010001d9dc0d0a"))

	unknown := buildFrame(0x15, []byte{0x01, 0x02}, 7)
	writeAll(t, connection, unknown)
	writeAll(t, connection, decodeHex(t, heartbeatFrame))
	heartbeatACK, err := protocol.BuildHeartbeatACK(0x001f)
	if err != nil {
		t.Fatal(err)
	}
	assertRead(t, connection, heartbeatACK)

	writeAll(t, connection, decodeHex(t, locationFrame))
	if err := connection.SetReadDeadline(time.Now().Add(150 * time.Millisecond)); err != nil {
		t.Fatal(err)
	}
	oneByte := make([]byte, 1)
	if _, err := connection.Read(oneByte); err == nil {
		t.Fatal("GPS packet unexpectedly produced an ACK")
	} else {
		var networkError net.Error
		if !errors.As(err, &networkError) || !networkError.Timeout() {
			t.Fatalf("GPS ACK check returned %v, want timeout", err)
		}
	}

	alarmContent := decodeHex(t, "13081d110c10cb026b3f3e0c371c9210154c0901cc00287d001fb84e04640002")
	writeAll(t, connection, buildFrame(0x16, alarmContent, 3))
	alarmACK, err := protocol.BuildAlarmACK(3)
	if err != nil {
		t.Fatal(err)
	}
	assertRead(t, connection, alarmACK)

	if err := connection.Close(); err != nil {
		t.Fatal(err)
	}
	stopTestServer(t, server, serveDone)
}

func TestHeartbeatBeforeLoginIsAcknowledged(t *testing.T) {
	listener, server, serveDone := startTestServer(t)
	connection := dialTestServer(t, listener)
	defer connection.Close()

	writeAll(t, connection, decodeHex(t, heartbeatFrame))
	ack, err := protocol.BuildHeartbeatACK(0x001f)
	if err != nil {
		t.Fatal(err)
	}
	assertRead(t, connection, ack)
	stopTestServer(t, server, serveDone)
}

func TestServerAcceptsConcurrentConnections(t *testing.T) {
	listener, server, serveDone := startTestServer(t)
	const connectionCount = 6
	errChannel := make(chan error, connectionCount)
	var waitGroup sync.WaitGroup
	for range connectionCount {
		waitGroup.Add(1)
		go func() {
			defer waitGroup.Done()
			connection, err := net.DialTimeout("tcp", listener.Addr().String(), time.Second)
			if err != nil {
				errChannel <- err
				return
			}
			defer connection.Close()
			_ = connection.SetDeadline(time.Now().Add(time.Second))
			if _, err := connection.Write(decodeHexNoTest(loginFrame)); err != nil {
				errChannel <- err
				return
			}
			response := make([]byte, 10)
			if _, err := io.ReadFull(connection, response); err != nil {
				errChannel <- err
				return
			}
			if !bytes.Equal(response, decodeHexNoTest("787805010001d9dc0d0a")) {
				errChannel <- errors.New("unexpected login ACK")
			}
		}()
	}
	waitGroup.Wait()
	close(errChannel)
	for err := range errChannel {
		if err != nil {
			t.Fatal(err)
		}
	}
	stopTestServer(t, server, serveDone)
}

func TestGT06AndGT02ShareSpoolAndDeliveryBatch(t *testing.T) {
	queue, err := spool.Open(filepath.Join(t.TempDir(), "multiprotocol.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer queue.Close()

	config := DefaultConfig()
	config.ReadTimeout = 2 * time.Second
	config.WriteTimeout = time.Second
	config.EventSink = queue
	listener, server, serveDone := startTestServerWithConfig(t, config)

	gt06Connection := dialTestServer(t, listener)
	writeAll(t, gt06Connection, decodeHex(t, loginFrame))
	assertRead(t, gt06Connection, decodeHex(t, "787805010001d9dc0d0a"))
	writeAll(t, gt06Connection, decodeHex(t, locationFrame))

	gt02Connection := dialTestServer(t, listener)
	writeAll(t, gt02Connection, decodeHex(t, "78781101086654505250169628013201000169040d0a"))
	assertRead(t, gt02Connection, decodeHex(t, "787805010001d9dc0d0a"))
	writeAll(t, gt02Connection, decodeHex(t, "78782431140813082d04cb026c6f6c0c3713a600140001cc000125fc06146402000000000bcd210d0a"))

	var pending []spool.Item
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		pending, err = queue.Pending(context.Background(), 10, time.Now().Add(time.Second))
		if err != nil {
			t.Fatal(err)
		}
		if len(pending) == 4 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if len(pending) != 4 {
		t.Fatalf("pending events=%d, want 4", len(pending))
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

	worker := delivery.New(delivery.Config{APIURL: api.URL, BatchSize: 10, Interval: 10 * time.Millisecond}, queue, api.Client(), slog.New(slog.NewJSONHandler(io.Discard, nil)), &telemetry.Metrics{})
	worker.Start(context.Background())
	defer worker.Shutdown(context.Background())

	select {
	case batch := <-batchChannel:
		if len(batch) != 4 {
			t.Fatalf("delivered batch=%d, want 4", len(batch))
		}
		families := map[string]int{}
		for _, event := range batch {
			families[event.ProtocolFamily]++
		}
		if families["gt06"] != 2 || families["gt02"] != 2 {
			t.Fatalf("unexpected protocol families: %v", families)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("multiprotocol batch was not delivered")
	}

	_ = gt06Connection.Close()
	_ = gt02Connection.Close()
	stopTestServer(t, server, serveDone)
}

func TestProtocolDetectionClosesAfterFourthAmbiguousFrame(t *testing.T) {
	listener, server, serveDone := startTestServer(t)
	connection := dialTestServer(t, listener)
	heartbeat := decodeHex(t, heartbeatFrame)
	for index := 0; index < 4; index++ {
		writeAll(t, connection, heartbeat)
		ack, err := protocol.BuildHeartbeatACK(0x001f)
		if err != nil {
			t.Fatal(err)
		}
		assertRead(t, connection, ack)
	}
	if err := connection.SetReadDeadline(time.Now().Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	if _, err := connection.Read(make([]byte, 1)); !errors.Is(err, io.EOF) {
		t.Fatalf("read after ambiguity limit=%v, want EOF", err)
	}
	stopTestServer(t, server, serveDone)
}

func startTestServer(t *testing.T) (net.Listener, *Server, <-chan error) {
	t.Helper()
	config := DefaultConfig()
	config.ReadTimeout = 2 * time.Second
	config.WriteTimeout = time.Second
	return startTestServerWithConfig(t, config)
}

func startTestServerWithConfig(t *testing.T, config Config) (net.Listener, *Server, <-chan error) {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
	server := New(config, logger)
	serveDone := make(chan error, 1)
	go func() { serveDone <- server.Serve(listener) }()
	return listener, server, serveDone
}

func stopTestServer(t *testing.T, server *Server, serveDone <-chan error) {
	t.Helper()
	if err := server.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-serveDone:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("server did not stop")
	}
}

func dialTestServer(t *testing.T, listener net.Listener) net.Conn {
	t.Helper()
	connection, err := net.DialTimeout("tcp", listener.Addr().String(), time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if err := connection.SetDeadline(time.Now().Add(2 * time.Second)); err != nil {
		t.Fatal(err)
	}
	return connection
}

func assertRead(t *testing.T, connection net.Conn, expected []byte) {
	t.Helper()
	if err := connection.SetReadDeadline(time.Now().Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	response := make([]byte, len(expected))
	if _, err := io.ReadFull(connection, response); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(response, expected) {
		t.Fatalf("response=%x, want %x", response, expected)
	}
}

func writeAll(t *testing.T, connection net.Conn, data []byte) {
	t.Helper()
	for len(data) > 0 {
		written, err := connection.Write(data)
		if err != nil {
			t.Fatal(err)
		}
		data = data[written:]
	}
}

func buildFrame(protocolNumber byte, content []byte, serial uint16) []byte {
	length := 1 + len(content) + 2 + 2
	frame := []byte{0x78, 0x78, byte(length), protocolNumber}
	frame = append(frame, content...)
	frame = binary.BigEndian.AppendUint16(frame, serial)
	frame = binary.BigEndian.AppendUint16(frame, protocol.CalculateCRC(frame[2:]))
	return append(frame, 0x0d, 0x0a)
}

func decodeHex(t *testing.T, value string) []byte {
	t.Helper()
	data, err := hex.DecodeString(value)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func decodeHexNoTest(value string) []byte {
	data, _ := hex.DecodeString(value)
	return data
}
