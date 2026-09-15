package server

import (
	"errors"
	"fmt"
	"io"
	"net"
	"time"

	"github.com/juanchopalen/tallerp-telemetry/internal/protocol"
	"github.com/juanchopalen/tallerp-telemetry/internal/protocol/jt808"
)

// handleJT808Connection mirrors handleGTConnection's read/deadline/EOF loop
// shape, but drives a jt808.Decoder and jt808.Handler instead of the
// GT06/GT02 protocol.Decoder — JT808 framing (0x7e-delimited, byte-stuffed)
// is fundamentally different and cannot share that decoder. This is the
// only place JT808 support touches connection handling; the GT06/GT02 path
// in handleGTConnection is untouched.
func (s *Server) handleJT808Connection(connection net.Conn, context *ConnectionContext, remote string, pending []byte, pendingErr error) {
	context.Family = protocol.FamilyJT808

	var bytesReceived int64
	var disconnectError error
	defer func() {
		_ = connection.Close()
		s.logDisconnect(context, remote, bytesReceived, disconnectError)
	}()

	decoder := jt808.NewDecoder(s.config.MaxFrameSize, s.config.MaxBufferedBytes)
	reassembler := jt808.NewReassembler(jt808.DefaultReassemblyTTL)
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
				if errors.Is(decodeError, jt808.ErrInvalidChecksum) {
					s.metrics.ChecksumErrorsJT808.Add(1)
				}
				s.logger.Warn("invalid jt808 frame",
					"event", jt808DecodeEvent(decodeError),
					"remote", remote,
					"remote_ip", context.RemoteIP,
					"imei", context.IMEI,
					"error", decodeError.Error(),
					"raw_hex", protocol.Hex(decodeError.Raw),
				)
			}
			for _, frame := range frames {
				s.metrics.FramesReceived.Add(1)
				s.metrics.FramesReceivedJT808.Add(1)
				reassembled, complete := reassembler.Push(frame, receivedAt)
				if !complete {
					continue
				}
				if processErr := s.processJT808Frame(connection, context, remote, reassembled, receivedAt); processErr != nil {
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

func (s *Server) processJT808Frame(connection net.Conn, context *ConnectionContext, remote string, frame jt808.Frame, receivedAt time.Time) error {
	rawHex := protocol.Hex(frame.Raw)
	common := []any{
		"remote", remote,
		"remote_ip", context.RemoteIP,
		"protocol", fmt.Sprintf("0x%04X", frame.MessageID),
		"serial", frame.MessageSerial,
		"terminal_sn", frame.TerminalSN,
		"raw_hex", rawHex,
	}
	if s.config.JT808Debug {
		s.logger.Debug("jt808 frame debug", append(append([]any{}, common...),
			"event", "jt808_debug_frame",
			"unescaped_hex", protocol.Hex(frame.Body),
			"body_length", len(frame.Body),
		)...)
	}

	wasAuthenticated := context.Authenticated
	result, err := s.jt808Handler.Dispatch(frame, &context.SessionContext, receivedAt)
	if context.TerminalSN != "" {
		context.IMEI = context.TerminalSN
	}

	if err != nil {
		switch {
		case errors.Is(err, protocol.ErrUnsupportedMessage):
			s.metrics.UnsupportedProtocol.Add(1)
			s.metrics.UnknownMessageIDsJT808.Add(1)
			s.logger.Warn("unsupported jt808 message", append(common, "event", "jt808_unsupported_message", "imei", context.IMEI)...)
		case errors.Is(err, jt808.ErrNotAuthenticated):
			s.logger.Warn("jt808 message received before authentication", append(common, "event", "jt808_not_authenticated", "imei", context.IMEI)...)
		default:
			s.metrics.ParseErrorsJT808.Add(1)
			s.logProtocolError(context, common, err)
		}
		if len(result.ACK) > 0 {
			if writeErr := s.writeACK(connection, result.ACK); writeErr != nil {
				return writeErr
			}
		}
		return nil
	}

	switch frame.MessageID {
	case jt808.MsgTerminalRegistration:
		s.metrics.RegistrationJT808.Add(1)
	case jt808.MsgTerminalAuth:
		if !wasAuthenticated && context.Authenticated {
			s.metrics.AuthSuccessJT808.Add(1)
		} else if !context.Authenticated {
			s.metrics.AuthFailureJT808.Add(1)
		}
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

func jt808DecodeEvent(err jt808.DecodeError) string {
	switch {
	case errors.Is(err, jt808.ErrInvalidChecksum):
		return "jt808_checksum_error"
	case errors.Is(err, jt808.ErrInvalidEscape):
		return "jt808_escape_error"
	case errors.Is(err, jt808.ErrTruncatedFrame):
		return "jt808_truncated_frame"
	case errors.Is(err, jt808.ErrBufferLimit):
		return "jt808_buffer_limit_error"
	default:
		return "jt808_frame_error"
	}
}
