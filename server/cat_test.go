package main

import (
	"context"
	"io"
	"net"
	"testing"
	"time"

	pb "github.com/accretional/plan92/gen/plan92/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"
)

const bufSize = 1024 * 1024

// setupTestServer creates an in-memory gRPC server for testing
func setupTestServer(t *testing.T) (*grpc.Server, *bufconn.Listener, *MemoryStorage, *SessionManager) {
	lis := bufconn.Listen(bufSize)

	storage := NewMemoryStorage()
	sessions := NewSessionManager()

	server := grpc.NewServer()
	inodeService := NewInodeService(storage, sessions)
	plan92Service := NewPlan92Service(storage, sessions, inodeService)

	pb.RegisterPlan92Server(server, plan92Service)
	pb.RegisterInodeServiceServer(server, inodeService)

	go func() {
		if err := server.Serve(lis); err != nil {
			t.Logf("Server exited with error: %v", err)
		}
	}()

	return server, lis, storage, sessions
}

// createTestClient creates a gRPC client connected to the test server
func createTestClient(ctx context.Context, lis *bufconn.Listener) (pb.Plan92Client, *grpc.ClientConn, error) {
	conn, err := grpc.DialContext(ctx, "bufnet",
		grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) {
			return lis.Dial()
		}),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		return nil, nil, err
	}

	client := pb.NewPlan92Client(conn)
	return client, conn, nil
}

// catFile mimics the behavior of the Unix `cat` command
// It opens a file, reads all contents, and returns them as a string
func catFile(ctx context.Context, client pb.Plan92Client, sessionID, path string) (string, error) {
	// Open file for reading
	openResp, err := client.Open(ctx, &pb.OpenRequest{
		Path:      path,
		Mode:      pb.OpenMode_OPEN_MODE_READ,
		SessionId: sessionID,
	})
	if err != nil {
		return "", err
	}
	fd := openResp.Fd

	// Ensure file is closed when done
	defer func() {
		_, _ = client.Close(ctx, &pb.CloseRequest{Fd: fd})
	}()

	// Read file contents
	readStream, err := client.Read(ctx, &pb.ReadRequest{
		Fd:     fd,
		Offset: -1, // Current position (start)
		Count:  -1, // Read all
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

		// Skip metadata, collect chunks
		if chunk := resp.GetChunk(); chunk != nil {
			content = append(content, chunk...)
		}
	}

	return string(content), nil
}

// writeTestFile is a helper to create test files
func writeTestFile(ctx context.Context, client pb.Plan92Client, sessionID, path, content string) error {
	// Open for writing
	openResp, err := client.Open(ctx, &pb.OpenRequest{
		Path:      path,
		Mode:      pb.OpenMode_OPEN_MODE_WRITE,
		SessionId: sessionID,
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

	// Send metadata
	if err := writeStream.Send(&pb.WriteRequest{
		Data: &pb.WriteRequest_Metadata{
			Metadata: &pb.WriteMetadata{
				Fd:        fd,
				Offset:    -1,
				TotalSize: int64(len(content)),
			},
		},
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
	if _, err := client.Close(ctx, &pb.CloseRequest{Fd: fd}); err != nil {
		return err
	}

	return nil
}

func TestCatCommand_SimpleFile(t *testing.T) {
	server, lis, _, _ := setupTestServer(t)
	defer server.Stop()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	client, conn, err := createTestClient(ctx, lis)
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}
	defer conn.Close()

	// Create session
	sessionResp, err := client.CreateSession(ctx, &pb.CreateSessionRequest{
		User:   "testuser",
		Groups: []string{"testgroup"},
	})
	if err != nil {
		t.Fatalf("Failed to create session: %v", err)
	}
	sessionID := sessionResp.SessionId

	// Write test file
	testContent := "Hello, Plan92 Filesystem!\nThis is a test file.\n"
	if err := writeTestFile(ctx, client, sessionID, "/test.txt", testContent); err != nil {
		t.Fatalf("Failed to write test file: %v", err)
	}

	// Cat the file
	content, err := catFile(ctx, client, sessionID, "/test.txt")
	if err != nil {
		t.Fatalf("Failed to cat file: %v", err)
	}

	// Verify content matches
	if content != testContent {
		t.Errorf("Content mismatch.\nExpected: %q\nGot: %q", testContent, content)
	}

	// Close session
	if _, err := client.CloseSession(ctx, &pb.CloseSessionRequest{SessionId: sessionID}); err != nil {
		t.Fatalf("Failed to close session: %v", err)
	}
}

func TestCatCommand_EmptyFile(t *testing.T) {
	server, lis, _, _ := setupTestServer(t)
	defer server.Stop()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	client, conn, err := createTestClient(ctx, lis)
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}
	defer conn.Close()

	// Create session
	sessionResp, err := client.CreateSession(ctx, &pb.CreateSessionRequest{
		User:   "testuser",
		Groups: []string{"testgroup"},
	})
	if err != nil {
		t.Fatalf("Failed to create session: %v", err)
	}
	sessionID := sessionResp.SessionId

	// Write empty file
	if err := writeTestFile(ctx, client, sessionID, "/empty.txt", ""); err != nil {
		t.Fatalf("Failed to write empty file: %v", err)
	}

	// Cat the empty file
	content, err := catFile(ctx, client, sessionID, "/empty.txt")
	if err != nil {
		t.Fatalf("Failed to cat empty file: %v", err)
	}

	// Verify content is empty
	if content != "" {
		t.Errorf("Expected empty content, got: %q", content)
	}

	// Close session
	if _, err := client.CloseSession(ctx, &pb.CloseSessionRequest{SessionId: sessionID}); err != nil {
		t.Fatalf("Failed to close session: %v", err)
	}
}

func TestCatCommand_LargeFile(t *testing.T) {
	server, lis, _, _ := setupTestServer(t)
	defer server.Stop()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	client, conn, err := createTestClient(ctx, lis)
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}
	defer conn.Close()

	// Create session
	sessionResp, err := client.CreateSession(ctx, &pb.CreateSessionRequest{
		User:   "testuser",
		Groups: []string{"testgroup"},
	})
	if err != nil {
		t.Fatalf("Failed to create session: %v", err)
	}
	sessionID := sessionResp.SessionId

	// Create large file (100KB)
	largeContent := make([]byte, 100*1024)
	for i := range largeContent {
		largeContent[i] = byte('A' + (i % 26))
	}

	if err := writeTestFile(ctx, client, sessionID, "/large.txt", string(largeContent)); err != nil {
		t.Fatalf("Failed to write large file: %v", err)
	}

	// Cat the large file
	content, err := catFile(ctx, client, sessionID, "/large.txt")
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

	// Close session
	if _, err := client.CloseSession(ctx, &pb.CloseSessionRequest{SessionId: sessionID}); err != nil {
		t.Fatalf("Failed to close session: %v", err)
	}
}

func TestCatCommand_MultipleFiles(t *testing.T) {
	server, lis, _, _ := setupTestServer(t)
	defer server.Stop()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	client, conn, err := createTestClient(ctx, lis)
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}
	defer conn.Close()

	// Create session
	sessionResp, err := client.CreateSession(ctx, &pb.CreateSessionRequest{
		User:   "testuser",
		Groups: []string{"testgroup"},
	})
	if err != nil {
		t.Fatalf("Failed to create session: %v", err)
	}
	sessionID := sessionResp.SessionId

	// Write multiple files
	files := map[string]string{
		"/file1.txt": "First file content\n",
		"/file2.txt": "Second file content\n",
		"/file3.txt": "Third file content\n",
	}

	for path, content := range files {
		if err := writeTestFile(ctx, client, sessionID, path, content); err != nil {
			t.Fatalf("Failed to write %s: %v", path, err)
		}
	}

	// Cat each file and concatenate (like `cat file1.txt file2.txt file3.txt`)
	var concatenated string
	for _, path := range []string{"/file1.txt", "/file2.txt", "/file3.txt"} {
		content, err := catFile(ctx, client, sessionID, path)
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

	// Close session
	if _, err := client.CloseSession(ctx, &pb.CloseSessionRequest{SessionId: sessionID}); err != nil {
		t.Fatalf("Failed to close session: %v", err)
	}
}

func TestCatCommand_NonExistentFile(t *testing.T) {
	server, lis, _, _ := setupTestServer(t)
	defer server.Stop()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	client, conn, err := createTestClient(ctx, lis)
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}
	defer conn.Close()

	// Create session
	sessionResp, err := client.CreateSession(ctx, &pb.CreateSessionRequest{
		User:   "testuser",
		Groups: []string{"testgroup"},
	})
	if err != nil {
		t.Fatalf("Failed to create session: %v", err)
	}
	sessionID := sessionResp.SessionId

	// Try to cat non-existent file
	_, err = catFile(ctx, client, sessionID, "/nonexistent.txt")
	if err == nil {
		t.Fatal("Expected error when catting non-existent file, got nil")
	}

	// Verify it's a NotFound error
	st, ok := status.FromError(err)
	if !ok {
		t.Fatalf("Expected gRPC status error, got: %v", err)
	}

	if st.Code() != codes.NotFound && st.Code() != codes.PermissionDenied {
		t.Errorf("Expected NotFound or PermissionDenied error, got: %v", st.Code())
	}

	// Close session
	if _, err := client.CloseSession(ctx, &pb.CloseSessionRequest{SessionId: sessionID}); err != nil {
		t.Fatalf("Failed to close session: %v", err)
	}
}

func TestCatCommand_BinaryContent(t *testing.T) {
	server, lis, _, _ := setupTestServer(t)
	defer server.Stop()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	client, conn, err := createTestClient(ctx, lis)
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}
	defer conn.Close()

	// Create session
	sessionResp, err := client.CreateSession(ctx, &pb.CreateSessionRequest{
		User:   "testuser",
		Groups: []string{"testgroup"},
	})
	if err != nil {
		t.Fatalf("Failed to create session: %v", err)
	}
	sessionID := sessionResp.SessionId

	// Write binary content (all byte values)
	binaryContent := make([]byte, 256)
	for i := range binaryContent {
		binaryContent[i] = byte(i)
	}

	if err := writeTestFile(ctx, client, sessionID, "/binary.dat", string(binaryContent)); err != nil {
		t.Fatalf("Failed to write binary file: %v", err)
	}

	// Cat the binary file
	content, err := catFile(ctx, client, sessionID, "/binary.dat")
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

	// Close session
	if _, err := client.CloseSession(ctx, &pb.CloseSessionRequest{SessionId: sessionID}); err != nil {
		t.Fatalf("Failed to close session: %v", err)
	}
}

func TestCatCommand_UTF8Content(t *testing.T) {
	server, lis, _, _ := setupTestServer(t)
	defer server.Stop()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	client, conn, err := createTestClient(ctx, lis)
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}
	defer conn.Close()

	// Create session
	sessionResp, err := client.CreateSession(ctx, &pb.CreateSessionRequest{
		User:   "testuser",
		Groups: []string{"testgroup"},
	})
	if err != nil {
		t.Fatalf("Failed to create session: %v", err)
	}
	sessionID := sessionResp.SessionId

	// Write UTF-8 content with various characters
	utf8Content := "Hello, 世界! 🌍\nΓεια σου κόσμε\nПривет, мир!\n"

	if err := writeTestFile(ctx, client, sessionID, "/utf8.txt", utf8Content); err != nil {
		t.Fatalf("Failed to write UTF-8 file: %v", err)
	}

	// Cat the UTF-8 file
	content, err := catFile(ctx, client, sessionID, "/utf8.txt")
	if err != nil {
		t.Fatalf("Failed to cat UTF-8 file: %v", err)
	}

	// Verify UTF-8 content matches
	if content != utf8Content {
		t.Errorf("UTF-8 content mismatch.\nExpected: %q\nGot: %q", utf8Content, content)
	}

	// Close session
	if _, err := client.CloseSession(ctx, &pb.CloseSessionRequest{SessionId: sessionID}); err != nil {
		t.Fatalf("Failed to close session: %v", err)
	}
}

// Benchmark cat operation
func BenchmarkCatCommand_1KB(b *testing.B) {
	server, lis, _, _ := setupTestServer(&testing.T{})
	defer server.Stop()

	ctx := context.Background()
	client, conn, err := createTestClient(ctx, lis)
	if err != nil {
		b.Fatalf("Failed to create client: %v", err)
	}
	defer conn.Close()

	// Create session
	sessionResp, _ := client.CreateSession(ctx, &pb.CreateSessionRequest{
		User:   "testuser",
		Groups: []string{"testgroup"},
	})
	sessionID := sessionResp.SessionId

	// Write 1KB file
	content := make([]byte, 1024)
	for i := range content {
		content[i] = 'A'
	}
	writeTestFile(ctx, client, sessionID, "/bench.txt", string(content))

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := catFile(ctx, client, sessionID, "/bench.txt")
		if err != nil {
			b.Fatalf("Failed to cat file: %v", err)
		}
	}
}
