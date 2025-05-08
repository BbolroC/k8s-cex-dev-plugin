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
type server struct {
	pb.UnimplementedZCryptManagerServer // Embed for forward compatibility
}

// CreateSimpleNode is the RPC handler
func (s *server) CreateSimpleNode(ctx context.Context, req *pb.CreateSimpleNodeRequest) (*pb.CreateSimpleNodeResponse, error) {
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

// CreateMdevNode is the RPC handler for creating mediated devices
func (s *server) CreateMdevNode(ctx context.Context, req *pb.CreateMdevNodeRequest) (*pb.CreateMdevNodeResponse, error) {
	log.Printf("Received CreateMdevNode request: APQN=%s", req.Apqn)

	devicePath, err := zcryptCreateMDevNode(req.Apqn)
	if err != nil {
		log.Printf("Error creating mediated device for APQN %s: %v", req.Apqn, err)
		return &pb.CreateMdevNodeResponse{
			Success:      false,
			ErrorMessage: err.Error(),
		}, nil
	}

	log.Printf("Successfully created mediated device for APQN %s at %s", req.Apqn, devicePath)
	return &pb.CreateMdevNodeResponse{
		Success:    true,
		DevicePath: devicePath,
	}, nil
}

func (s *server) FetchActiveNodes(ctx context.Context, req *pb.FetchActiveNodesRequest) (*pb.FetchActiveNodesResponse, error) {
	nodes, err := zcryptFetchActiveNodes()
	if err != nil {
		return &pb.FetchActiveNodesResponse{
			ErrorMessage: err.Error(),
		}, nil
	}

	return &pb.FetchActiveNodesResponse{
		Nodes: nodes,
	}, nil
}

func (s *server) DestroyNode(ctx context.Context, req *pb.DestroyNodeRequest) (*pb.DestroyNodeResponse, error) {
	err := zcryptDestroyNode(req.Nodename)
	if err != nil {
		return &pb.DestroyNodeResponse{
			Success:      false,
			ErrorMessage: err.Error(),
		}, nil
	}

	return &pb.DestroyNodeResponse{
		Success: true,
	}, nil
}

func (s *server) NodeExists(ctx context.Context, req *pb.NodeExistsRequest) (*pb.NodeExistsResponse, error) {
	exists := zcryptNodeExists(req.Nodename)
	return &pb.NodeExistsResponse{
		Exists: exists,
	}, nil
}

func (s *server) HasNodesSupport(ctx context.Context, req *pb.HasNodesSupportRequest) (*pb.HasNodesSupportResponse, error) {
	hasSupport := zcryptHasNodesSupport()
	return &pb.HasNodesSupportResponse{
		HasSupport: hasSupport,
	}, nil
}

func (s *server) MakeShadowApSysfs(ctx context.Context, req *pb.MakeShadowApSysfsRequest) (*pb.MakeShadowApSysfsResponse, error) {
	shadowPath, shadowName, err := makeShadowApSysfs(req.Id, int(req.Adapter), int(req.Domain))
	if err != nil {
		return &pb.MakeShadowApSysfsResponse{
			ShadowPath:   "",
			ShadowName:   "",
			ErrorMessage: err.Error(),
		}, nil
	}
	return &pb.MakeShadowApSysfsResponse{
		ShadowPath:   shadowPath,
		ShadowName:   shadowName,
		ErrorMessage: "",
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
	pb.RegisterZCryptManagerServer(s, &server{})

	// Optional: Register reflection service on gRPC server.
	reflection.Register(s)

	log.Printf("Server listening at %v", lis.Addr())
	if err := s.Serve(lis); err != nil {
		log.Fatalf("Failed to serve: %v", err)
	}
}
