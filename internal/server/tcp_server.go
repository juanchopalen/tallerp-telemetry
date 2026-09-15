package server

import (
	"errors"
	"log/slog"
	"net"
	"sync"
	"time"

	"github.com/juanchopalen/tallerp-telemetry/internal/protocol"
	"github.com/juanchopalen/tallerp-telemetry/internal/protocol/gt02"
	"github.com/juanchopalen/tallerp-telemetry/internal/protocol/gt06"
	"github.com/juanchopalen/tallerp-telemetry/internal/protocol/jt808"
	"github.com/juanchopalen/tallerp-telemetry/internal/telemetry"
)

type Config struct {
	ReadTimeout      time.Duration
	WriteTimeout     time.Duration
	MaxFrameSize     int
	MaxBufferedBytes int
	EventSink        telemetry.Sink
	Metrics          *telemetry.Metrics

	// JT808AuthStore persists JT808 terminal auth codes. If nil, JT808
	// registration/auth fails closed (the handler is still constructed,
	// but every terminal store lookup errors) rather than silently
	// disabling the protocol.
	JT808AuthStore jt808.AuthStore
	// JT808Debug enables verbose per-frame JT808 diagnostic logging.
	JT808Debug bool
}

func DefaultConfig() Config {
	return Config{
		ReadTimeout:      10 * time.Minute,
		WriteTimeout:     5 * time.Second,
		MaxFrameSize:     protocol.DefaultMaxFrameSize,
		MaxBufferedBytes: protocol.DefaultMaxBufferedBytes,
	}
}

type Server struct {
	config       Config
	logger       *slog.Logger
	sink         telemetry.Sink
	metrics      *telemetry.Metrics
	handlers     []protocol.Handler
	jt808Handler *jt808.Handler

	mu          sync.Mutex
	listener    net.Listener
	connections map[net.Conn]struct{}
	closing     bool
	closeOnce   sync.Once
	waitGroup   sync.WaitGroup
}

func New(config Config, logger *slog.Logger) *Server {
	defaults := DefaultConfig()
	if config.ReadTimeout <= 0 {
		config.ReadTimeout = defaults.ReadTimeout
	}
	if config.WriteTimeout <= 0 {
		config.WriteTimeout = defaults.WriteTimeout
	}
	if config.MaxFrameSize <= 0 {
		config.MaxFrameSize = defaults.MaxFrameSize
	}
	if config.MaxBufferedBytes < config.MaxFrameSize {
		config.MaxBufferedBytes = defaults.MaxBufferedBytes
		if config.MaxBufferedBytes < config.MaxFrameSize {
			config.MaxBufferedBytes = config.MaxFrameSize
		}
	}
	if logger == nil {
		logger = slog.Default()
	}
	if config.Metrics == nil {
		config.Metrics = &telemetry.Metrics{}
	}
	return &Server{
		config:       config,
		logger:       logger,
		sink:         config.EventSink,
		metrics:      config.Metrics,
		handlers:     []protocol.Handler{gt06.NewHandler(), gt02.NewHandler()},
		jt808Handler: jt808.NewHandler(config.JT808AuthStore),
		connections:  make(map[net.Conn]struct{}),
	}
}

func (s *Server) Ready() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.listener != nil && !s.closing
}

func (s *Server) Serve(listener net.Listener) error {
	if listener == nil {
		return errors.New("listener is required")
	}
	s.mu.Lock()
	if s.closing {
		s.mu.Unlock()
		return net.ErrClosed
	}
	if s.listener != nil {
		s.mu.Unlock()
		return errors.New("server is already serving")
	}
	s.listener = listener
	s.mu.Unlock()

	for {
		connection, err := listener.Accept()
		if err != nil {
			s.mu.Lock()
			closing := s.closing
			s.mu.Unlock()
			if closing || errors.Is(err, net.ErrClosed) {
				return nil
			}
			if temporary, ok := err.(interface{ Temporary() bool }); ok && temporary.Temporary() {
				s.logger.Warn("temporary accept failure", "event", "accept_error", "error", err)
				continue
			}
			return err
		}

		s.mu.Lock()
		if s.closing {
			s.mu.Unlock()
			_ = connection.Close()
			continue
		}
		s.connections[connection] = struct{}{}
		s.waitGroup.Add(1)
		s.mu.Unlock()
		go func() {
			defer s.waitGroup.Done()
			defer s.removeConnection(connection)
			s.handleConnection(connection)
		}()
	}
}

func (s *Server) Close() error {
	var closeErr error
	s.closeOnce.Do(func() {
		s.mu.Lock()
		s.closing = true
		listener := s.listener
		connections := make([]net.Conn, 0, len(s.connections))
		for connection := range s.connections {
			connections = append(connections, connection)
		}
		s.mu.Unlock()

		if listener != nil {
			closeErr = listener.Close()
			if errors.Is(closeErr, net.ErrClosed) {
				closeErr = nil
			}
		}
		for _, connection := range connections {
			_ = connection.Close()
		}
		s.waitGroup.Wait()
	})
	return closeErr
}

func (s *Server) removeConnection(connection net.Conn) {
	s.mu.Lock()
	delete(s.connections, connection)
	s.mu.Unlock()
}

func (s *Server) isClosing() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.closing
}
