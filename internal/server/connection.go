package server

import (
	"errors"
	"fmt"
	"io"
	"net"
	"time"

	"github.com/juanchopalen/tallerp-telemetry/internal/protocol"
)

type ConnectionContext struct {
	IMEI        string
	RemoteIP    string
	ConnectedAt time.Time
}

func (s *Server) handleConnection(connection net.Conn) {
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

	var bytesReceived int64
	var disconnectError error
	defer func() {
		_ = connection.Close()
		attributes := []any{
			"event", "tracker_disconnected",
			"remote", remote,
			"remote_ip", remoteIP,
			"imei", context.IMEI,
			"connected_at", context.ConnectedAt.Format(time.RFC3339Nano),
			"disconnected_at", time.Now().UTC().Format(time.RFC3339Nano),
			"bytes_received", bytesReceived,
		}
		if disconnectError != nil {
			attributes = append(attributes, "error", disconnectError)
		}
		s.logger.Info("tracker disconnected", attributes...)
	}()

	decoder := protocol.NewDecoder(s.config.MaxFrameSize, s.config.MaxBufferedBytes)
	readBuffer := make([]byte, 4096)
	for {
		if err := connection.SetReadDeadline(time.Now().Add(s.config.ReadTimeout)); err != nil {
			disconnectError = err
			s.logSocketError(context, remote, err)
			return
		}
		readBytes, err := connection.Read(readBuffer)
		if readBytes > 0 {
			bytesReceived += int64(readBytes)
			receivedAt := time.Now().UTC()
			frames, decodeErrors := decoder.Push(readBuffer[:readBytes])
			for _, decodeError := range decodeErrors {
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

func (s *Server) processFrame(connection net.Conn, context *ConnectionContext, remote string, frame protocol.Frame, receivedAt time.Time) error {
	common := []any{
		"remote", remote,
		"remote_ip", context.RemoteIP,
		"protocol", fmt.Sprintf("0x%02X", frame.ProtocolNumber),
		"serial", frame.Serial,
		"raw_hex", protocol.Hex(frame.Raw),
	}
	if len(frame.Header) == 2 && frame.Header[0] == 0x79 {
		s.logger.Warn("unsupported tracker protocol", append(common,
			"event", "unsupported_protocol",
			"imei", context.IMEI,
			"header", "0x7979",
		)...)
		return nil
	}
	if frame.ProtocolNumber != 0x01 && context.IMEI == "" {
		s.logger.Warn("packet received before login", append(common, "event", "packet_before_login")...)
	}

	switch frame.ProtocolNumber {
	case 0x01:
		packet, err := protocol.ParseLogin(frame)
		if err != nil {
			s.logProtocolError(context, common, err)
			return nil
		}
		context.IMEI = packet.IMEI
		ack, err := protocol.BuildLoginACK(packet.Serial)
		if err != nil {
			return err
		}
		if err := s.writeACK(connection, ack); err != nil {
			return err
		}
		s.logger.Info("tracker login", append(common,
			"event", "tracker_login",
			"imei", packet.IMEI,
			"ack_hex", protocol.Hex(ack),
		)...)
	case 0x12:
		packet, err := protocol.ParseLocation(frame, context.IMEI)
		if err != nil {
			s.logProtocolError(context, common, err)
			return nil
		}
		received := protocol.ReceivedLocation{Location: packet, ReceivedAt: receivedAt}
		s.logger.Info("tracker location", append(common,
			"event", "location",
			"imei", packet.IMEI,
			"gps_at", received.Location.GPSAt.Format(time.RFC3339Nano),
			"received_at", received.ReceivedAt.Format(time.RFC3339Nano),
			"lat", packet.Latitude,
			"lon", packet.Longitude,
			"speed_kmh", packet.SpeedKmh,
			"heading", packet.Heading,
			"satellites", packet.Satellites,
			"gps_located", packet.GPSLocated,
			"realtime_gps", packet.RealtimeGPS,
			"acc", *packet.ACC,
			"mcc", packet.MCC,
			"mnc", packet.MNC,
			"lac", packet.LAC,
			"cell_id", packet.CellID,
		)...)
	case 0x13:
		packet, err := protocol.ParseHeartbeat(frame, context.IMEI)
		if err != nil {
			s.logProtocolError(context, common, err)
			return nil
		}
		ack, err := protocol.BuildHeartbeatACK(packet.Serial)
		if err != nil {
			return err
		}
		if err := s.writeACK(connection, ack); err != nil {
			return err
		}
		s.logger.Info("tracker heartbeat", append(common,
			"event", "heartbeat",
			"imei", packet.IMEI,
			"acc", packet.ACC,
			"gps_located", packet.GPSLocated,
			"external_power", packet.ExternalPower,
			"voltage_level", packet.VoltageLevel,
			"gsm_signal", packet.GSMSignal,
			"external_voltage", packet.ExternalVoltage,
			"language", packet.Language,
			"ack_hex", protocol.Hex(ack),
		)...)
	case 0x16:
		packet, err := protocol.ParseAlarm(frame, context.IMEI)
		if err != nil {
			s.logProtocolError(context, common, err)
			return nil
		}
		ack, err := protocol.BuildAlarmACK(packet.Serial)
		if err != nil {
			return err
		}
		if err := s.writeACK(connection, ack); err != nil {
			return err
		}
		s.logger.Warn("tracker alarm", append(common,
			"event", "alarm",
			"imei", packet.IMEI,
			"gps_at", packet.GPSAt.Format(time.RFC3339Nano),
			"received_at", receivedAt.Format(time.RFC3339Nano),
			"lat", packet.Latitude,
			"lon", packet.Longitude,
			"speed_kmh", packet.SpeedKmh,
			"alarm_type", fmt.Sprintf("0x%02X", packet.AlarmType),
			"alarm_name", protocol.AlarmTypeName(packet.AlarmType),
			"acc", packet.ACC,
			"gps_located", packet.GPSLocated,
			"voltage_level", packet.VoltageLevel,
			"gsm_signal", packet.GSMSignal,
			"ack_hex", protocol.Hex(ack),
		)...)
	default:
		s.logger.Warn("unsupported tracker protocol", append(common,
			"event", "unsupported_protocol",
			"imei", context.IMEI,
		)...)
	}
	return nil
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
		"error", err,
	)...)
}

func (s *Server) logSocketError(context *ConnectionContext, remote string, err error) {
	s.logger.Warn("tracker socket error",
		"event", "socket_error",
		"remote", remote,
		"remote_ip", context.RemoteIP,
		"imei", context.IMEI,
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
