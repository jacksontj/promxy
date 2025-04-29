package mimirfrontend

import (
	"context"
	"log/slog"

	"google.golang.org/grpc"
)

// FrontendGRPC implements the Frontend service.
type FrontendGRPC struct {
	UnimplementedFrontendServer
	logger *slog.Logger
}

// NewFrontendGRPC creates a new FrontendGRPC.
func NewFrontendGRPC(logger *slog.Logger) *FrontendGRPC {
	return &FrontendGRPC{
		UnimplementedFrontendServer: UnimplementedFrontendServer{},
		logger: logger,
	}
}

// Query implements the Frontend service Query method.
// It returns an empty QueryResultResponse as required.
func (f *FrontendGRPC) Query(ctx context.Context, req *QueryResultRequest) (*QueryResultResponse, error) {
	f.logger.Debug("received query request", "queryID", req.GetQueryID())

	// Return an empty QueryResultResponse
	return &QueryResultResponse{}, nil
}

// RegisterGRPC registers the Frontend service with the given GRPC server.
func (f *FrontendGRPC) RegisterGRPC(s *grpc.Server) {
	RegisterFrontendServer(s, f)
	f.logger.Debug("gRPC Frontend service registered in Promxy")
}
