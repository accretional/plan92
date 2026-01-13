package main

import (
	"context"
	"io"
	"net"
	"testing"
	"time"

	pb "github.com/accretional/plan92/gen/plan92/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/test/bufconn"
)

const bufSize = 1024 * 1024

// setupTestServer creates an in-memory gRPC server for testing
func setupTestServer(t *testing.T) (*grpc.Server, *bufconn.Listener, *MemoryStorage) {
	lis := bufconn.Listen(bufSize)

	storage := NewMemoryStorage()
	fdTable := NewFDTable()

	server := grpc.NewServer()
	plan92Service := NewPlan92Service(storage, fdTable)

	pb.RegisterPlan92FSServer(server, plan92Service)

	go func() {
		if err := server.Serve(lis); err != nil {
			t.Logf("Server exited with error: %v", err)
		}
	}()

	return server, lis, storage
}

// createTestClient creates a gRPC client connected to the test server
func createTestClient(ctx context.Context, lis *bufconn.Listener) (pb.Plan92FSClient, *grpc.ClientConn, error) {
	conn, err := grpc.DialContext(ctx, "bufnet",
		grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) {
			return lis.Dial()
		}),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		return nil, nil, err
	}

	client := pb.NewPlan92FSClient(conn)
	return client, conn, nil
}

// catFile mimics the behavior of the Unix `cat` command
// It opens a file, reads all contents, and returns them as a string
func catFile(ctx context.Context, client pb.Plan92FSClient, path string) (string, error) {
	// Open file
	openResp, err := client.Open(ctx, &pb.URI{
		Path: path,
	})
	if err != nil {
		return "", err
	}
	fd := openResp.Fd

	// Ensure file is closed when done
	defer func() {
		_, _ = client.Close(ctx, &pb.OpenFileDescriptor{Fd: fd})
	}()

	// Read file contents
	readStream, err := client.Read(ctx, &pb.OpenFileDescriptor{
		Fd: fd,
	})
	if err != nil {
		return "", err
	}

	// Accumulate all chunks
	var content []byte
	for {
		resp, err := readStream.Recv()
		if err == io.EOF {
			break
		}
		if err != nil {
			return "", err
		}

		// Collect bytes
		content = append(content, resp.Data...)
	}

	return string(content), nil
}

// writeTestFile is a helper to create test files
func writeTestFile(ctx context.Context, client pb.Plan92FSClient, path, content string) error {
	// Open for writing
	openResp, err := client.Open(ctx, &pb.URI{
		Path: path,
	})
	if err != nil {
		return err
	}
	fd := openResp.Fd

	// Write content
	writeStream, err := client.Write(ctx)
	if err != nil {
		return err
	}

	// Send fd first
	if err := writeStream.Send(&pb.WriteRequest{
		Data: &pb.WriteRequest_Fd{Fd: fd},
	}); err != nil {
		return err
	}

	// Send content
	if err := writeStream.Send(&pb.WriteRequest{
		Data: &pb.WriteRequest_Chunk{Chunk: []byte(content)},
	}); err != nil {
		return err
	}

	// Close write stream
	if _, err := writeStream.CloseAndRecv(); err != nil {
		return err
	}

	// Close file
	if _, err := client.Close(ctx, &pb.OpenFileDescriptor{Fd: fd}); err != nil {
		return err
	}

	return nil
}

func TestCatCommand_SimpleFile(t *testing.T) {
	server, lis, _ := setupTestServer(t)
	defer server.Stop()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	client, conn, err := createTestClient(ctx, lis)
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}
	defer conn.Close()

	// Write test file
	testContent := "Hello, Plan92 Filesystem!\nThis is a test file.\n"
	if err := writeTestFile(ctx, client, "/test.txt", testContent); err != nil {
		t.Fatalf("Failed to write test file: %v", err)
	}

	// Cat the file
	content, err := catFile(ctx, client, "/test.txt")
	if err != nil {
		t.Fatalf("Failed to cat file: %v", err)
	}

	// Verify content matches
	if content != testContent {
		t.Errorf("Content mismatch.\nExpected: %q\nGot: %q", testContent, content)
	}
}

func TestCatCommand_EmptyFile(t *testing.T) {
	server, lis, _ := setupTestServer(t)
	defer server.Stop()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	client, conn, err := createTestClient(ctx, lis)
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}
	defer conn.Close()

	// Write empty file
	if err := writeTestFile(ctx, client, "/empty.txt", ""); err != nil {
		t.Fatalf("Failed to write empty file: %v", err)
	}

	// Cat the empty file
	content, err := catFile(ctx, client, "/empty.txt")
	if err != nil {
		t.Fatalf("Failed to cat empty file: %v", err)
	}

	// Verify content is empty
	if content != "" {
		t.Errorf("Expected empty content, got: %q", content)
	}
}

func TestCatCommand_LargeFile(t *testing.T) {
	server, lis, _ := setupTestServer(t)
	defer server.Stop()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	client, conn, err := createTestClient(ctx, lis)
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}
	defer conn.Close()

	// Create large file (100KB)
	largeContent := make([]byte, 100*1024)
	for i := range largeContent {
		largeContent[i] = byte('A' + (i % 26))
	}

	if err := writeTestFile(ctx, client, "/large.txt", string(largeContent)); err != nil {
		t.Fatalf("Failed to write large file: %v", err)
	}

	// Cat the large file
	content, err := catFile(ctx, client, "/large.txt")
	if err != nil {
		t.Fatalf("Failed to cat large file: %v", err)
	}

	// Verify size and content
	if len(content) != len(largeContent) {
		t.Errorf("Size mismatch. Expected: %d, Got: %d", len(largeContent), len(content))
	}

	if content != string(largeContent) {
		t.Errorf("Content mismatch for large file")
	}
}

func TestCatCommand_MultipleFiles(t *testing.T) {
	server, lis, _ := setupTestServer(t)
	defer server.Stop()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	client, conn, err := createTestClient(ctx, lis)
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}
	defer conn.Close()

	// Write multiple files
	files := map[string]string{
		"/file1.txt": "First file content\n",
		"/file2.txt": "Second file content\n",
		"/file3.txt": "Third file content\n",
	}

	for path, content := range files {
		if err := writeTestFile(ctx, client, path, content); err != nil {
			t.Fatalf("Failed to write %s: %v", path, err)
		}
	}

	// Cat each file and concatenate (like `cat file1.txt file2.txt file3.txt`)
	var concatenated string
	for _, path := range []string{"/file1.txt", "/file2.txt", "/file3.txt"} {
		content, err := catFile(ctx, client, path)
		if err != nil {
			t.Fatalf("Failed to cat %s: %v", path, err)
		}
		concatenated += content
	}

	// Verify concatenated content
	expected := "First file content\nSecond file content\nThird file content\n"
	if concatenated != expected {
		t.Errorf("Concatenated content mismatch.\nExpected: %q\nGot: %q", expected, concatenated)
	}
}

func TestCatCommand_NonExistentFile(t *testing.T) {
	server, lis, _ := setupTestServer(t)
	defer server.Stop()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	client, conn, err := createTestClient(ctx, lis)
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}
	defer conn.Close()

	// Try to cat non-existent file (it will be created on open, then we can check it's empty)
	content, err := catFile(ctx, client, "/nonexistent.txt")
	if err != nil {
		t.Fatalf("Failed to cat non-existent file: %v", err)
	}

	// Since Open creates files, the file should exist but be empty
	if content != "" {
		t.Errorf("Expected empty content for newly created file, got: %q", content)
	}
}

func TestCatCommand_BinaryContent(t *testing.T) {
	server, lis, _ := setupTestServer(t)
	defer server.Stop()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	client, conn, err := createTestClient(ctx, lis)
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}
	defer conn.Close()

	// Write binary content (all byte values)
	binaryContent := make([]byte, 256)
	for i := range binaryContent {
		binaryContent[i] = byte(i)
	}

	if err := writeTestFile(ctx, client, "/binary.dat", string(binaryContent)); err != nil {
		t.Fatalf("Failed to write binary file: %v", err)
	}

	// Cat the binary file
	content, err := catFile(ctx, client, "/binary.dat")
	if err != nil {
		t.Fatalf("Failed to cat binary file: %v", err)
	}

	// Verify binary content matches
	if content != string(binaryContent) {
		t.Errorf("Binary content mismatch")
	}

	// Verify each byte
	for i := 0; i < 256; i++ {
		if content[i] != byte(i) {
			t.Errorf("Byte mismatch at position %d. Expected: %d, Got: %d", i, i, content[i])
		}
	}
}

func TestCatCommand_UTF8Content(t *testing.T) {
	server, lis, _ := setupTestServer(t)
	defer server.Stop()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	client, conn, err := createTestClient(ctx, lis)
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}
	defer conn.Close()

	// Write UTF-8 content with various characters
	utf8Content := "Hello, 世界! 🌍\nΓεια σου κόσμε\nПривет, мир!\n"

	if err := writeTestFile(ctx, client, "/utf8.txt", utf8Content); err != nil {
		t.Fatalf("Failed to write UTF-8 file: %v", err)
	}

	// Cat the UTF-8 file
	content, err := catFile(ctx, client, "/utf8.txt")
	if err != nil {
		t.Fatalf("Failed to cat UTF-8 file: %v", err)
	}

	// Verify UTF-8 content matches
	if content != utf8Content {
		t.Errorf("UTF-8 content mismatch.\nExpected: %q\nGot: %q", utf8Content, content)
	}
}

// Benchmark cat operation
func BenchmarkCatCommand_1KB(b *testing.B) {
	server, lis, _ := setupTestServer(&testing.T{})
	defer server.Stop()

	ctx := context.Background()
	client, conn, err := createTestClient(ctx, lis)
	if err != nil {
		b.Fatalf("Failed to create client: %v", err)
	}
	defer conn.Close()

	// Write 1KB file
	content := make([]byte, 1024)
	for i := range content {
		content[i] = 'A'
	}
	writeTestFile(ctx, client, "/bench.txt", string(content))

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := catFile(ctx, client, "/bench.txt")
		if err != nil {
			b.Fatalf("Failed to cat file: %v", err)
		}
	}
}
