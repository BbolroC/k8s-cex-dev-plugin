package main

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	pb "cex-plugin/zcryptpb"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

var (
	grpcClient     pb.ZCryptManagerClient
	grpcConn       *grpc.ClientConn
	grpcClientOnce sync.Once
	grpcClientErr  error
	grpcMutex      sync.Mutex // Mutex to protect access to gRPC client
)

// InitGrpcClient initializes the shared gRPC client and connection
func InitGrpcClient(ctx context.Context, address string) error {
	grpcClientOnce.Do(func() {
		connTimeout := 5 * time.Second
		connCtx, cancel := context.WithTimeout(ctx, connTimeout)
		defer cancel()

		conn, err := grpc.DialContext(
			connCtx,
			address,
			grpc.WithTransportCredentials(insecure.NewCredentials()),
			grpc.WithBlock(),
		)
		if err != nil {
			grpcClientErr = fmt.Errorf("failed to connect to gRPC server at %s: %w", address, err)
			return
		}

		grpcConn = conn
		grpcClient = pb.NewZCryptManagerClient(conn)
		log.Printf("gRPC connection established to %s", address)
	})

	return grpcClientErr
}

// ExecuteGrpcCall executes a gRPC call with proper synchronization
func ExecuteGrpcCall[T any](ctx context.Context, call func(pb.ZCryptManagerClient) (T, error)) (T, error) {
	grpcMutex.Lock()
	defer grpcMutex.Unlock()

	if grpcClient == nil {
		var zero T
		return zero, fmt.Errorf("gRPC client not initialized")
	}

	return call(grpcClient)
}

// CloseGrpcConn closes the shared gRPC connection
func CloseGrpcConn() {
	grpcMutex.Lock()
	defer grpcMutex.Unlock()

	if grpcConn != nil {
		log.Println("Closing gRPC connection to zcrypt server...")
		grpcConn.Close()
		grpcConn = nil
		grpcClient = nil
	}
}

// WithGrpcCall is a wrapper function that handles the common gRPC call pattern
func WithGrpcCall[T any](ctx context.Context, timeoutSeconds int, operation string, call func(pb.ZCryptManagerClient) (T, error)) (T, error) {
	callTimeout := time.Duration(timeoutSeconds) * time.Second
	rpcCtx, rpcCancel := context.WithTimeout(ctx, callTimeout)
	defer rpcCancel()

	log.Printf("Calling %s via gRPC", operation)
	return ExecuteGrpcCall(rpcCtx, call)
}
