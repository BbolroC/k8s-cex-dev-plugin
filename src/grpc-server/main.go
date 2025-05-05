// File: cmd/zcrypt-server/main.go (or similar structure)
package main

import (
	"context"
	"log"
	"net"

	pb "cex-plugin/zcryptpb" // Import the generated code

	"google.golang.org/grpc"
	"google.golang.org/grpc/reflection" // Optional: for debugging/discovery
)

const (
	grpcPort = ":50051" // Port the gRPC server will listen on
)

// server implements the ZCryptManagerServer interface
type zcryptServer struct {
	pb.UnimplementedZCryptManagerServer // Embed for forward compatibility
}

// CreateSimpleNode is the RPC handler
func (s *zcryptServer) CreateSimpleNode(ctx context.Context, req *pb.CreateSimpleNodeRequest) (*pb.CreateSimpleNodeResponse, error) {
	log.Printf("Received CreateSimpleNode request: DeviceID=%s, ContainerPath=%d, HostDevicePath=%d",
		req.Nodename, req.Adapter, req.Domain)

	err := zcryptCreateSimpleNode(req.Nodename, int(req.Adapter), int(req.Domain))
	if err != nil {
		log.Printf("Error in performNodeCreation for device %s: %v", req.Nodename, err)
		return &pb.CreateSimpleNodeResponse{
			Success:      false,
			ErrorMessage: err.Error(),
		}, nil // Return error details in the response, not as gRPC error
	}

	log.Printf("Successfully processed CreateSimpleNode for device %s", req.Nodename)
	return &pb.CreateSimpleNodeResponse{
		Success:  true,
		NodePath: req.Nodename,
	}, nil
}

func main() {
	log.Printf("Starting zcrypt gRPC server on port %s", grpcPort)
	lis, err := net.Listen("tcp", grpcPort)
	if err != nil {
		log.Fatalf("Failed to listen: %v", err)
	}

	s := grpc.NewServer(
	// Add gRPC server options here if needed (e.g., interceptors, TLS)
	)

	// Register the implementation
	pb.RegisterZCryptManagerServer(s, &zcryptServer{})

	// Optional: Register reflection service on gRPC server.
	reflection.Register(s)

	log.Printf("Server listening at %v", lis.Addr())
	if err := s.Serve(lis); err != nil {
		log.Fatalf("Failed to serve: %v", err)
	}
}
