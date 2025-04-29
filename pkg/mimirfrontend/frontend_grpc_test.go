package mimirfrontend

import (
	"context"
	"log/slog"
	"net"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/grafana/dskit/httpgrpc"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

func TestFrontendGRPC_Query(t *testing.T) {
	// Start a server on a random port
	lis, err := net.Listen("tcp", "localhost:0")
	if err != nil {
		t.Fatalf("Failed to listen: %v", err)
	}

	// Create a new logger for testing
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))

	// Create and start the server
	srv := grpc.NewServer()
	frontend := NewFrontendGRPC(logger)
	frontend.RegisterGRPC(srv)

	// Use a channel to capture server errors
	errCh := make(chan error, 1)
	go func() {
		errCh <- srv.Serve(lis)
	}()
	defer func() {
		srv.Stop()
		// Check for server errors after stopping
		select {
		case err := <-errCh:
			if err != nil {
				// When stopping a server gracefully, we might get an error about the listener being closed
				if !strings.Contains(err.Error(), "use of closed network connection") {
					t.Errorf("Server error: %v", err)
				}
			}
		default:
			// No error
		}
	}()

	// Setup client connection
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	conn, err := grpc.DialContext(ctx, lis.Addr().String(),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithBlock())
	if err != nil {
		t.Fatalf("Failed to dial server: %v", err)
	}
	defer conn.Close()

	// Create the client
	client := NewFrontendClient(conn)

	// Prepare the request
	req := &QueryResultRequest{
		QueryID: 12345,
		HttpResponse: &httpgrpc.HTTPResponse{
			Code: 200,
			Body: []byte(`{"status":"success","data":{"resultType":"vector","result":[]}}`),
		},
		// Stats can be nil for this test
	}

	// Make the request
	response, err := client.Query(ctx, req)
	if err != nil {
		t.Fatalf("Query failed: %v", err)
	}

	// Validate response (which is currently an empty struct)
	if response == nil {
		t.Fatal("Expected non-nil response")
	}
}

// Helper function to create a new Frontend client
func NewFrontendClient(conn *grpc.ClientConn) FrontendClient {
	return &frontendClient{conn}
}

// frontendClient is a client implementation of the FrontendClient interface
type frontendClient struct {
	conn *grpc.ClientConn
}

// Query implements the Query method for the Frontend client
func (c *frontendClient) Query(ctx context.Context, in *QueryResultRequest, opts ...grpc.CallOption) (*QueryResultResponse, error) {
	out := new(QueryResultResponse)
	err := c.conn.Invoke(ctx, "/mimirfrontend.Frontend/Query", in, out, opts...)
	if err != nil {
		return nil, err
	}
	return out, nil
}

// FrontendClient is the client API for Frontend service.
type FrontendClient interface {
	// Query processes a query result.
	Query(ctx context.Context, in *QueryResultRequest, opts ...grpc.CallOption) (*QueryResultResponse, error)
}