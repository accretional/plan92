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

	client := pb.NewPlan92Client(conn)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	log.Println("========================================")
	log.Println("Plan92 Filesystem Client Example")
	log.Println("========================================")

	// Step 1: Create Session
	log.Println("\n[1] Creating session...")
	sessionResp, err := client.CreateSession(ctx, &pb.CreateSessionRequest{
		User:   "alice",
		Groups: []string{"users", "developers"},
	})
	if err != nil {
		log.Fatalf("Failed to create session: %v", err)
	}
	sessionID := sessionResp.SessionId
	log.Printf("✓ Session created: %s", sessionID)
	log.Printf("  Created at: %s", sessionResp.CreatedAt.AsTime().Format(time.RFC3339))

	// Step 2: Write File
	log.Println("\n[2] Writing file /test.txt...")

	// Open for writing
	openResp, err := client.Open(ctx, &pb.OpenRequest{
		Path:      "/test.txt",
		Mode:      pb.OpenMode_OPEN_MODE_WRITE,
		SessionId: sessionID,
	})
	if err != nil {
		log.Fatalf("Failed to open file for writing: %v", err)
	}
	writeFD := openResp.Fd
	log.Printf("✓ Opened for writing: fd=%d", writeFD)

	// Stream write
	writeStream, err := client.Write(ctx)
	if err != nil {
		log.Fatalf("Failed to create write stream: %v", err)
	}

	// Send metadata
	err = writeStream.Send(&pb.WriteRequest{
		Data: &pb.WriteRequest_Metadata{
			Metadata: &pb.WriteMetadata{
				Fd:        writeFD,
				Offset:    -1, // Append
				TotalSize: int64(len("Hello, Plan92 Filesystem!")),
			},
		},
	})
	if err != nil {
		log.Fatalf("Failed to send write metadata: %v", err)
	}

	// Send data chunk
	content := []byte("Hello, Plan92 Filesystem!")
	err = writeStream.Send(&pb.WriteRequest{
		Data: &pb.WriteRequest_Chunk{Chunk: content},
	})
	if err != nil {
		log.Fatalf("Failed to send write chunk: %v", err)
	}

	// Close write stream and get response
	writeResp, err := writeStream.CloseAndRecv()
	if err != nil {
		log.Fatalf("Failed to close write stream: %v", err)
	}
	log.Printf("✓ Wrote %d bytes", writeResp.BytesWritten)

	// Close write FD
	_, err = client.Close(ctx, &pb.CloseRequest{Fd: writeFD})
	if err != nil {
		log.Fatalf("Failed to close write FD: %v", err)
	}
	log.Printf("✓ Closed fd=%d", writeFD)

	// Step 3: Stat File
	log.Println("\n[3] Getting file stats...")
	statResp, err := client.Stat(ctx, &pb.StatRequest{
		Path:      "/test.txt",
		SessionId: sessionID,
	})
	if err != nil {
		log.Fatalf("Failed to stat file: %v", err)
	}
	log.Printf("✓ File stats:")
	log.Printf("  Type: %s", statResp.Info.Type)
	log.Printf("  Mode: %o", statResp.Info.Mode)
	log.Printf("  Size: %d bytes", statResp.Info.Length)
	log.Printf("  Owner: %s", statResp.Info.Owner)
	log.Printf("  Group: %s", statResp.Info.Group)
	log.Printf("  Modified: %s", statResp.Info.Mtime.AsTime().Format(time.RFC3339))

	// Step 4: Read File
	log.Println("\n[4] Reading file /test.txt...")

	// Open for reading
	openResp, err = client.Open(ctx, &pb.OpenRequest{
		Path:      "/test.txt",
		Mode:      pb.OpenMode_OPEN_MODE_READ,
		SessionId: sessionID,
	})
	if err != nil {
		log.Fatalf("Failed to open file for reading: %v", err)
	}
	readFD := openResp.Fd
	log.Printf("✓ Opened for reading: fd=%d", readFD)

	// Stream read
	readStream, err := client.Read(ctx, &pb.ReadRequest{
		Fd:     readFD,
		Offset: -1, // Current position (start)
		Count:  -1, // Read all
	})
	if err != nil {
		log.Fatalf("Failed to create read stream: %v", err)
	}

	var readContent []byte
	var readMetadata *pb.ReadMetadata

	for {
		resp, err := readStream.Recv()
		if err == io.EOF {
			break
		}
		if err != nil {
			log.Fatalf("Failed to receive read response: %v", err)
		}

		switch data := resp.Data.(type) {
		case *pb.ReadResponse_Metadata:
			readMetadata = data.Metadata
			log.Printf("✓ Read metadata received:")
			log.Printf("  Total size: %d bytes", readMetadata.TotalSize)
		case *pb.ReadResponse_Chunk:
			readContent = append(readContent, data.Chunk...)
		}
	}

	log.Printf("✓ Read %d bytes: %q", len(readContent), string(readContent))

	// Close read FD
	_, err = client.Close(ctx, &pb.CloseRequest{Fd: readFD})
	if err != nil {
		log.Fatalf("Failed to close read FD: %v", err)
	}
	log.Printf("✓ Closed fd=%d", readFD)

	// Step 5: Write Another File
	log.Println("\n[5] Writing file /data/output.txt...")

	openResp, err = client.Open(ctx, &pb.OpenRequest{
		Path:      "/data/output.txt",
		Mode:      pb.OpenMode_OPEN_MODE_WRITE,
		SessionId: sessionID,
	})
	if err != nil {
		log.Fatalf("Failed to open file for writing: %v", err)
	}
	fd2 := openResp.Fd
	log.Printf("✓ Opened for writing: fd=%d", fd2)

	writeStream2, err := client.Write(ctx)
	if err != nil {
		log.Fatalf("Failed to create write stream: %v", err)
	}

	content2 := []byte("This demonstrates multiple file operations in a single session.")
	err = writeStream2.Send(&pb.WriteRequest{
		Data: &pb.WriteRequest_Metadata{
			Metadata: &pb.WriteMetadata{
				Fd:        fd2,
				Offset:    -1,
				TotalSize: int64(len(content2)),
			},
		},
	})
	if err != nil {
		log.Fatalf("Failed to send write metadata: %v", err)
	}

	err = writeStream2.Send(&pb.WriteRequest{
		Data: &pb.WriteRequest_Chunk{Chunk: content2},
	})
	if err != nil {
		log.Fatalf("Failed to send write chunk: %v", err)
	}

	writeResp2, err := writeStream2.CloseAndRecv()
	if err != nil {
		log.Fatalf("Failed to close write stream: %v", err)
	}
	log.Printf("✓ Wrote %d bytes", writeResp2.BytesWritten)

	_, err = client.Close(ctx, &pb.CloseRequest{Fd: fd2})
	if err != nil {
		log.Fatalf("Failed to close FD: %v", err)
	}
	log.Printf("✓ Closed fd=%d", fd2)

	// Step 6: Close Session
	log.Println("\n[6] Closing session...")
	_, err = client.CloseSession(ctx, &pb.CloseSessionRequest{
		SessionId: sessionID,
	})
	if err != nil {
		log.Fatalf("Failed to close session: %v", err)
	}
	log.Printf("✓ Session closed: %s", sessionID)

	log.Println("\n========================================")
	log.Println("✓ All operations completed successfully!")
	log.Println("========================================")
}
