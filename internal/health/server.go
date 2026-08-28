package health

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"time"
)

type Checker interface{ Ready(context.Context) error }

type Server struct {
	server   *http.Server
	listener net.Listener
}

func New(address string, tcpReady func() bool, spool Checker) (*Server, error) {
	listener, err := net.Listen("tcp", address)
	if err != nil {
		return nil, err
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", func(writer http.ResponseWriter, _ *http.Request) { write(writer, http.StatusOK, "ok") })
	mux.HandleFunc("GET /ready", func(writer http.ResponseWriter, request *http.Request) {
		if !tcpReady() {
			write(writer, http.StatusServiceUnavailable, "tcp listener unavailable")
			return
		}
		ctx, cancel := context.WithTimeout(request.Context(), 2*time.Second)
		defer cancel()
		if err := spool.Ready(ctx); err != nil {
			write(writer, http.StatusServiceUnavailable, "spool unavailable")
			return
		}
		write(writer, http.StatusOK, "ready")
	})
	return &Server{server: &http.Server{Handler: mux, ReadHeaderTimeout: 5 * time.Second}, listener: listener}, nil
}

func (s *Server) Serve() error                       { return s.server.Serve(s.listener) }
func (s *Server) Shutdown(ctx context.Context) error { return s.server.Shutdown(ctx) }
func write(writer http.ResponseWriter, status int, value string) {
	writer.Header().Set("Content-Type", "application/json")
	writer.WriteHeader(status)
	_ = json.NewEncoder(writer).Encode(map[string]string{"status": value})
}
