package server

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"time"

	"github.com/juanchopalen/tallerp-telemetry/internal/protocol"
	"github.com/juanchopalen/tallerp-telemetry/internal/protocol/gt02"
	"github.com/juanchopalen/tallerp-telemetry/internal/spool"
	"github.com/juanchopalen/tallerp-telemetry/internal/telemetry"
)

type ConnectionContext struct {
	protocol.SessionContext
	RemoteIP        string
	ConnectedAt     time.Time
	AmbiguousFrames int
}

var errProtocolDetectionFailed = errors.New("protocol detection failed")

func (s *Server) handleConnection(connection net.Conn) {
	s.metrics.ConnectionsActive.Add(1)
	defer s.metrics.ConnectionsActive.Add(-1)
	remote := connection.RemoteAddr().String()
	remoteIP := remote
	if host, _, err := net.SplitHostPort(remote); err == nil {
		remoteIP = host
	}
	context := &ConnectionContext{
		RemoteIP:    remoteIP,
		ConnectedAt: time.Now().UTC(),
	}
	s.logger.Info("tracker connected",
		"event", "tracker_connected",
		"remote", remote,
		"remote_ip", remoteIP,
		"connected_at", context.ConnectedAt.Format(time.RFC3339Nano),
	)

	// Detect JT808 (0x7e-delimited) vs GT06/GT02 (0x78 0x78 / 0x79 0x79
	// headers) once, from the first bytes read, and lock onto that framing
	// for the life of the connection. These byte ranges never overlap, so
	// there is no ambiguity to re-check on later frames.
	if err := connection.SetReadDeadline(time.Now().Add(s.config.ReadTimeout)); err != nil {
		s.logSocketError(context, remote, err)
		s.logDisconnect(context, remote, 0, err)
		_ = connection.Close()
		return
	}
	readBuffer := make([]byte, 4096)
	firstReadBytes, firstErr := connection.Read(readBuffer)
	pending := append([]byte(nil), readBuffer[:firstReadBytes]...)

	if firstReadBytes > 0 && pending[0] == 0x7e {
		s.handleJT808Connection(connection, context, remote, pending, firstErr)
		return
	}
	s.handleGTConnection(connection, context, remote, pending, firstErr)
}

func (s *Server) handleGTConnection(connection net.Conn, context *ConnectionContext, remote string, pending []byte, pendingErr error) {
	var bytesReceived int64
	var disconnectError error
	defer func() {
		_ = connection.Close()
		s.logDisconnect(context, remote, bytesReceived, disconnectError)
	}()

	decoder := protocol.NewDecoder(s.config.MaxFrameSize, s.config.MaxBufferedBytes)
	readBuffer := make([]byte, 4096)
	data, err := pending, pendingErr
	first := true

	for {
		if !first {
			if setErr := connection.SetReadDeadline(time.Now().Add(s.config.ReadTimeout)); setErr != nil {
				disconnectError = setErr
				s.logSocketError(context, remote, setErr)
				return
			}
			var readBytes int
			readBytes, err = connection.Read(readBuffer)
			data = readBuffer[:readBytes]
		}
		first = false

		if len(data) > 0 {
			bytesReceived += int64(len(data))
			receivedAt := time.Now().UTC()
			frames, decodeErrors := decoder.Push(data)
			for _, decodeError := range decodeErrors {
				if errors.Is(decodeError, protocol.ErrInvalidCRC) {
					s.metrics.InvalidCRC.Add(1)
				}
				s.logger.Warn("invalid tracker frame",
					"event", decodeEvent(decodeError),
					"remote", remote,
					"remote_ip", context.RemoteIP,
					"imei", context.IMEI,
					"error", decodeError.Error(),
					"raw_hex", protocol.Hex(decodeError.Raw),
				)
			}
			for _, frame := range frames {
				s.metrics.FramesReceived.Add(1)
				if processErr := s.processFrame(connection, context, remote, frame, receivedAt); processErr != nil {
					disconnectError = processErr
					s.logSocketError(context, remote, processErr)
					return
				}
			}
		}
		if err != nil {
			if errors.Is(err, io.EOF) || (s.isClosing() && errors.Is(err, net.ErrClosed)) {
				return
			}
			disconnectError = err
			s.logSocketError(context, remote, err)
			return
		}
	}
}

func (s *Server) logDisconnect(context *ConnectionContext, remote string, bytesReceived int64, disconnectError error) {
	attributes := []any{
		"event", "tracker_disconnected",
		"remote", remote,
		"remote_ip", context.RemoteIP,
		"imei", context.IMEI,
		"protocol_family", context.Family,
		"connected_at", context.ConnectedAt.Format(time.RFC3339Nano),
		"disconnected_at", time.Now().UTC().Format(time.RFC3339Nano),
		"bytes_received", bytesReceived,
	}
	if disconnectError != nil {
		attributes = append(attributes, "error", disconnectError)
	}
	s.logger.Info("tracker disconnected", attributes...)
}

func (s *Server) processFrame(connection net.Conn, context *ConnectionContext, remote string, frame protocol.Frame, receivedAt time.Time) error {
	rawHex := protocol.Hex(frame.Raw)
	if frame.ProtocolNumber == 0x94 {
		rawHex = "<redacted>"
	}
	common := []any{
		"remote", remote,
		"remote_ip", context.RemoteIP,
		"protocol", fmt.Sprintf("0x%02X", frame.ProtocolNumber),
		"serial", frame.Serial,
		"raw_hex", rawHex,
	}

	if context.Family == protocol.FamilyUnknown && frame.ProtocolNumber == 0x13 {
		ack := gt02.BuildHeartbeatACK(frame.Serial)
		if err := s.writeACK(connection, ack); err != nil {
			return err
		}
		return s.recordAmbiguousFrame(context, append(common, "ack_hex", protocol.Hex(ack))...)
	}

	handler := s.handlerFor(frame, &context.SessionContext)
	if handler == nil {
		return s.recordAmbiguousFrame(context, common...)
	}
	knownFamily := context.Family != protocol.FamilyUnknown
	if knownFamily {
		s.incrementFrameFamily(context.Family)
	}
	result, err := handler.Handle(frame, &context.SessionContext, receivedAt)
	if err != nil {
		if errors.Is(err, protocol.ErrUnsupportedMessage) {
			s.metrics.UnsupportedProtocol.Add(1)
			s.logger.Warn("unsupported tracker protocol", append(common, "event", "unsupported_protocol", "imei", context.IMEI, "protocol_family", context.Family, "error", err)...)
			return nil
		}
		s.logProtocolError(context, common, err)
		return nil
	}
	if context.Family == protocol.FamilyUnknown {
		context.Family = handler.Name()
	}
	context.AmbiguousFrames = 0
	if !knownFamily {
		s.incrementFrameFamily(context.Family)
	}
	if len(result.ACK) > 0 {
		if err := s.writeACK(connection, result.ACK); err != nil {
			return err
		}
		common = append(common, "ack_hex", protocol.Hex(result.ACK))
	}
	for _, event := range result.Events {
		s.recordEvent(context, common, event)
	}
	return nil
}

func (s *Server) handlerFor(frame protocol.Frame, context *protocol.SessionContext) protocol.Handler {
	for _, handler := range s.handlers {
		if handler.CanHandle(frame, context) {
			return handler
		}
	}
	return nil
}

func (s *Server) recordAmbiguousFrame(context *ConnectionContext, attributes ...any) error {
	context.AmbiguousFrames++
	s.metrics.ProtocolDetectionFailure.Add(1)
	s.logger.Warn("tracker protocol detection failed", append(attributes,
		"event", "protocol_detection_failed", "imei", context.IMEI,
		"ambiguous_frames", context.AmbiguousFrames,
	)...)
	if context.AmbiguousFrames > 3 {
		return errProtocolDetectionFailed
	}
	return nil
}

func (s *Server) incrementFrameFamily(family protocol.Family) {
	switch family {
	case protocol.FamilyGT06:
		s.metrics.FramesReceivedGT06.Add(1)
	case protocol.FamilyGT02:
		s.metrics.FramesReceivedGT02.Add(1)
	}
}

func (s *Server) recordEvent(context *ConnectionContext, common []any, event telemetry.TelemetryEvent) {
	attributes := append(common,
		"event", event.EventType, "imei", event.IMEI, "protocol_family", event.ProtocolFamily,
		"received_at", event.ReceivedAt.Format(time.RFC3339Nano),
	)
	if event.GPSAt != nil {
		attributes = append(attributes, "gps_at", event.GPSAt.Format(time.RFC3339Nano))
	}
	if event.Latitude != nil {
		attributes = append(attributes, "lat", *event.Latitude, "lon", *event.Longitude)
	}
	if event.SpeedKmh != nil {
		attributes = append(attributes, "speed_kmh", *event.SpeedKmh)
	}
	if event.AlarmType != nil {
		attributes = append(attributes, "alarm_type", *event.AlarmType)
	}
	if len(event.AlarmTypes) > 0 {
		attributes = append(attributes, "alarm_types", event.AlarmTypes)
	}
	if event.IsRetransmission != nil {
		attributes = append(attributes, "is_retransmission", *event.IsRetransmission)
	}
	if event.GeneralInfo != nil {
		attributes = append(attributes, "general_subtype", fmt.Sprintf("0x%02X", event.GeneralInfo.Subtype))
	}
	if event.EventType == "login" && context.DeviceType != 0 {
		attributes = append(attributes, "device_type", fmt.Sprintf("0x%04X", context.DeviceType))
	}
	if event.EventType == "location" {
		s.metrics.LocationsReceived.Add(1)
		switch event.ProtocolFamily {
		case "gt02":
			s.metrics.LocationsGT02.Add(1)
		case "jt808":
			s.metrics.LocationsJT808.Add(1)
		default:
			s.metrics.LocationsGT06.Add(1)
		}
	}
	if event.EventType == "heartbeat" {
		s.metrics.HeartbeatsReceived.Add(1)
		if event.ProtocolFamily == "jt808" {
			s.metrics.HeartbeatsJT808.Add(1)
		}
	}
	if event.EventType == "alarm" || len(event.AlarmTypes) > 0 {
		s.logger.Warn("tracker alarm", attributes...)
	} else {
		s.logger.Info("tracker telemetry", attributes...)
	}
	if event.IMEI == "" {
		s.logger.Warn("valid packet received before login", append(common, "event", "packet_before_login", "protocol_family", event.ProtocolFamily)...)
		return
	}
	s.persist(event)
}

func (s *Server) persist(event telemetry.TelemetryEvent) {
	if s.sink == nil {
		return
	}
	event.SetID()
	inserted, err := s.sink.Store(context.Background(), event)
	if err != nil {
		level := "ERROR"
		if spool.IsDiskFull(err) {
			level = "CRITICAL"
		}
		s.logger.Error("telemetry persistence failure", "event", "telemetry_persistence_failure", "severity", level, "event_id", event.EventID, "imei", event.IMEI, "protocol", event.Protocol, "error", err)
		return
	}
	s.logger.Debug("telemetry persisted", "event", "telemetry_persisted", "event_id", event.EventID, "inserted", inserted)
}

func (s *Server) writeACK(connection net.Conn, ack []byte) error {
	if err := connection.SetWriteDeadline(time.Now().Add(s.config.WriteTimeout)); err != nil {
		return err
	}
	for len(ack) > 0 {
		written, err := connection.Write(ack)
		if err != nil {
			return err
		}
		if written == 0 {
			return io.ErrShortWrite
		}
		ack = ack[written:]
	}
	return nil
}

func (s *Server) logProtocolError(context *ConnectionContext, common []any, err error) {
	s.logger.Warn("invalid protocol content", append(common,
		"event", "protocol_error",
		"imei", context.IMEI,
		"protocol_family", context.Family,
		"error", err,
	)...)
}

func (s *Server) logSocketError(context *ConnectionContext, remote string, err error) {
	s.logger.Warn("tracker socket error",
		"event", "socket_error",
		"remote", remote,
		"remote_ip", context.RemoteIP,
		"imei", context.IMEI,
		"protocol_family", context.Family,
		"error", err,
	)
}

func decodeEvent(err protocol.DecodeError) string {
	switch {
	case errors.Is(err, protocol.ErrInvalidCRC):
		return "crc_error"
	case errors.Is(err, protocol.ErrInvalidHeader):
		return "header_error"
	case errors.Is(err, protocol.ErrInvalidLength):
		return "length_error"
	case errors.Is(err, protocol.ErrInvalidStop):
		return "stop_bits_error"
	case errors.Is(err, protocol.ErrBufferLimit):
		return "buffer_limit_error"
	default:
		return "frame_error"
	}
}
