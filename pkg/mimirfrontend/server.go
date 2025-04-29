package mimirfrontend

import (
	"fmt"
	"log/slog"
	"net"

	"google.golang.org/grpc"
)

// GRPCServer represents a gRPC server for Mimir Frontend service.
type GRPCServer struct {
	server  *grpc.Server
	logger  *slog.Logger
	address string
}

// NewGRPCServer creates a new GRPCServer.
func NewGRPCServer(logger *slog.Logger, address string) *GRPCServer {
	server := grpc.NewServer()
	return &GRPCServer{
		server:  server,
		logger:  logger,
		address: address,
	}
}

// Start starts the gRPC server.
func (s *GRPCServer) Start() error {
	lis, err := net.Listen("tcp", s.address)
	if err != nil {
		return fmt.Errorf("failed to listen on %s: %w", s.address, err)
	}

	// Register the Frontend service
	frontend := NewFrontendGRPC(s.logger)
	frontend.RegisterGRPC(s.server)

	s.logger.Info("starting gRPC server", "address", s.address)
	return s.server.Serve(lis)
}

// Stop stops the gRPC server gracefully.
func (s *GRPCServer) Stop() {
	if s.server != nil {
		s.logger.Info("Stopping gRPC server")
		s.server.GracefulStop()
	}
}
