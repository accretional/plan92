package main

import (
	"context"
	"io"
	"log"
	"time"

	pb "github.com/accretional/plan92/gen/plan92/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

func main() {
	// Connect to server
	conn, err := grpc.Dial("localhost:9000",
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		log.Fatalf("Failed to connect: %v", err)
	}
	defer conn.Close()

	client := pb.NewPlan92FSClient(conn)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	log.Println("========================================")
	log.Println("Plan92 Filesystem Client Example")
	log.Println("========================================")

	// ========================================================================
	// Demo 1: Streamed Write and Read
	// ========================================================================
	log.Println("\n[Demo 1] Streamed Write and Read")
	log.Println("----------------------------------")

	// Open file
	log.Println("Opening /test.txt...")
	fdResp, err := client.Open(ctx, &pb.URI{
		Path: "/test.txt",
	})
	if err != nil {
		log.Fatalf("Failed to open file: %v", err)
	}
	fd := fdResp.Fd
	log.Printf("✓ Opened: fd=%d", fd)

	// Stream write
	log.Println("Writing data via stream...")
	writeStream, err := client.Write(ctx)
	if err != nil {
		log.Fatalf("Failed to create write stream: %v", err)
	}

	// First message: FD
	err = writeStream.Send(&pb.WriteRequest{
		Data: &pb.WriteRequest_Fd{Fd: fd},
	})
	if err != nil {
		log.Fatalf("Failed to send fd: %v", err)
	}

	// Subsequent messages: Data chunks
	content := []byte("Hello, Plan92 Filesystem!")
	err = writeStream.Send(&pb.WriteRequest{
		Data: &pb.WriteRequest_Chunk{Chunk: content},
	})
	if err != nil {
		log.Fatalf("Failed to send chunk: %v", err)
	}

	// Close write stream and get response
	writeResp, err := writeStream.CloseAndRecv()
	if err != nil {
		log.Fatalf("Failed to close write stream: %v", err)
	}
	log.Printf("✓ Wrote %d bytes", writeResp.Count)
	if writeResp.Errno != nil {
		log.Printf("  Warning: errno=%d", *writeResp.Errno)
	}

	// Close file
	closeResp, err := client.Close(ctx, &pb.OpenFileDescriptor{Fd: fd})
	if err != nil {
		log.Fatalf("Failed to close: %v", err)
	}
	if closeResp.Errno != nil {
		log.Printf("Warning: close errno=%d", *closeResp.Errno)
	}
	log.Printf("✓ Closed fd=%d", fd)

	// Open for reading
	log.Println("Opening /test.txt for reading...")
	fdResp, err = client.Open(ctx, &pb.URI{
		Path: "/test.txt",
	})
	if err != nil {
		log.Fatalf("Failed to open file: %v", err)
	}
	readFD := fdResp.Fd
	log.Printf("✓ Opened for reading: fd=%d", readFD)

	// Stream read
	log.Println("Reading data via stream...")
	readStream, err := client.Read(ctx, &pb.OpenFileDescriptor{
		Fd: readFD,
	})
	if err != nil {
		log.Fatalf("Failed to create read stream: %v", err)
	}

	var readContent []byte
	for {
		resp, err := readStream.Recv()
		if err == io.EOF {
			break
		}
		if err != nil {
			log.Fatalf("Failed to receive read response: %v", err)
		}
		readContent = append(readContent, resp.Data...)
	}

	log.Printf("✓ Read %d bytes: %q", len(readContent), string(readContent))

	// Close read FD
	closeResp, err = client.Close(ctx, &pb.OpenFileDescriptor{Fd: readFD})
	if err != nil {
		log.Fatalf("Failed to close: %v", err)
	}
	if closeResp.Errno != nil {
		log.Printf("Warning: close errno=%d", *closeResp.Errno)
	}
	log.Printf("✓ Closed fd=%d", readFD)

	// ========================================================================
	// Demo 2: Non-Streamed Write and Read (WriteAll/ReadAll)
	// ========================================================================
	log.Println("\n[Demo 2] Non-Streamed Write and Read")
	log.Println("--------------------------------------")

	// Open file
	log.Println("Opening /small.txt...")
	fdResp, err = client.Open(ctx, &pb.URI{
		Path: "/small.txt",
	})
	if err != nil {
		log.Fatalf("Failed to open file: %v", err)
	}
	fd2 := fdResp.Fd
	log.Printf("✓ Opened: fd=%d", fd2)

	// Write all at once
	log.Println("Writing data with WriteAll...")
	smallContent := []byte("Quick write!")
	writeResp2, err := client.WriteAll(ctx, &pb.WriteAllRequest{
		Fd:   fd2,
		Data: smallContent,
	})
	if err != nil {
		log.Fatalf("Failed to write all: %v", err)
	}
	log.Printf("✓ Wrote %d bytes", writeResp2.Count)
	if writeResp2.Errno != nil {
		log.Printf("  Warning: errno=%d", *writeResp2.Errno)
	}

	// Read all at once
	log.Println("Reading data with ReadAll...")
	readResp, err := client.ReadAll(ctx, &pb.OpenFileDescriptor{Fd: fd2})
	if err != nil {
		log.Fatalf("Failed to read all: %v", err)
	}
	log.Printf("✓ Read %d bytes: %q", len(readResp.Data), string(readResp.Data))

	// Close file
	closeResp, err = client.Close(ctx, &pb.OpenFileDescriptor{Fd: fd2})
	if err != nil {
		log.Fatalf("Failed to close: %v", err)
	}
	if closeResp.Errno != nil {
		log.Printf("Warning: close errno=%d", *closeResp.Errno)
	}
	log.Printf("✓ Closed fd=%d", fd2)

	// ========================================================================
	// Demo 3: Direct URI-Based Operations
	// ========================================================================
	log.Println("\n[Demo 3] Direct URI-Based Operations")
	log.Println("--------------------------------------")

	// Write directly by path
	log.Println("Writing to /direct.txt...")
	directContent := []byte("Direct write without open/close!")
	directWriteResp, err := client.WriteDirect(ctx, &pb.WriteDirectRequest{
		Path: "/direct.txt",
		Data: directContent,
	})
	if err != nil {
		log.Fatalf("Failed to write direct: %v", err)
	}
	log.Printf("✓ Wrote %d bytes directly", directWriteResp.Count)
	if directWriteResp.Errno != nil {
		log.Printf("  Warning: errno=%d", *directWriteResp.Errno)
	}

	// Read directly by path
	log.Println("Reading from /direct.txt...")
	directReadResp, err := client.ReadDirect(ctx, &pb.URI{
		Path: "/direct.txt",
	})
	if err != nil {
		log.Fatalf("Failed to read direct: %v", err)
	}
	log.Printf("✓ Read %d bytes directly: %q", len(directReadResp.Data), string(directReadResp.Data))

	log.Println("\n========================================")
	log.Println("✓ All operations completed successfully!")
	log.Println("========================================")
}
