package main

import (
	"context"
	"fmt"
	"log"
	"net"
	"os"
	"os/signal"
	"syscall"
	"time"

	pb "github.com/accretional/plan92/gen/plan92/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/reflection"
)

const (
	defaultPort = "9000"
)

func main() {
	// Get port from environment or use default
	port := os.Getenv("PORT")
	if port == "" {
		port = defaultPort
	}

	// Create listener
	lis, err := net.Listen("tcp", fmt.Sprintf(":%s", port))
	if err != nil {
		log.Fatalf("Failed to listen on port %s: %v", port, err)
	}

	log.Printf("Plan92 server starting on port %s...", port)

	// Initialize storage and session manager
	storage := NewMemoryStorage()
	sessions := NewSessionManager()

	// Create gRPC server
	server := grpc.NewServer()

	// Create and register services
	inodeService := NewInodeService(storage, sessions)
	plan92Service := NewPlan92Service(storage, sessions, inodeService)

	pb.RegisterPlan92Server(server, plan92Service)
	pb.RegisterInodeServiceServer(server, inodeService)

	// Register reflection service for debugging
	reflection.Register(server)

	log.Printf("Plan92 server registered services:")
	log.Printf("  - plan92.v1.Plan92")
	log.Printf("  - plan92.v1.InodeService")

	// Setup graceful shutdown
	go func() {
		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, syscall.SIGTERM, syscall.SIGINT)
		sig := <-sigCh

		log.Printf("Received signal %v, shutting down gracefully...", sig)

		// Create context with timeout for shutdown
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		// Attempt graceful stop
		stopped := make(chan struct{})
		go func() {
			server.GracefulStop()
			close(stopped)
		}()

		select {
		case <-stopped:
			log.Printf("Server stopped gracefully")
		case <-ctx.Done():
			log.Printf("Shutdown timeout exceeded, forcing stop")
			server.Stop()
		}

		os.Exit(0)
	}()

	// Start serving
	log.Printf("Plan92 server listening on :%s", port)
	if err := server.Serve(lis); err != nil {
		log.Fatalf("Failed to serve: %v", err)
	}
}
