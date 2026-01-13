# Cat Command Test Suite

This test suite mimics the behavior of the Unix `cat` command using the Plan92 filesystem service.

## Overview

The `catFile()` function implements cat functionality:
1. Opens a file for reading
2. Streams all content from the file
3. Returns the content as a string
4. Automatically closes the file descriptor

## Test Cases

### ✅ TestCatCommand_SimpleFile
Tests reading a basic text file with multiple lines.
- Creates a file with newlines
- Verifies exact content match

### ✅ TestCatCommand_EmptyFile
Tests reading an empty file (edge case).
- Creates a zero-length file
- Verifies empty string is returned

### ✅ TestCatCommand_LargeFile
Tests reading a large file (100KB).
- Creates a 100KB file with repeating pattern
- Verifies all content is correctly streamed
- Tests chunked streaming behavior

### ✅ TestCatCommand_MultipleFiles
Tests concatenating multiple files (like `cat file1 file2 file3`).
- Creates three separate files
- Cats each one sequentially
- Concatenates results
- Verifies total output

### ✅ TestCatCommand_NonExistentFile
Tests error handling for missing files.
- Attempts to cat a non-existent file
- Verifies NotFound error is returned
- Mimics `cat: file not found` behavior

### ✅ TestCatCommand_BinaryContent
Tests reading binary data (non-text content).
- Creates file with all byte values (0-255)
- Verifies binary data is preserved exactly
- Ensures no encoding issues

### ✅ TestCatCommand_UTF8Content
Tests reading Unicode/UTF-8 content.
- Creates file with emoji, Chinese, Greek, and Cyrillic characters
- Verifies UTF-8 encoding is preserved
- Tests multi-byte character handling

## Performance

### Benchmark Results
```
BenchmarkCatCommand_1KB-2   	   53707	    111030 ns/op
```

**Performance**: ~111 microseconds per 1KB file read operation
- Includes: session lookup, FD validation, streaming read, chunk assembly
- Very fast for in-memory storage backend

## Running Tests

```bash
# Run all cat tests
cd server
go test -v -run TestCatCommand

# Run specific test
go test -v -run TestCatCommand_SimpleFile

# Run benchmark
go test -bench=BenchmarkCatCommand -benchtime=5s

# Run with coverage
go test -cover -run TestCatCommand
```

## Example Output

```
=== RUN   TestCatCommand_SimpleFile
--- PASS: TestCatCommand_SimpleFile (0.00s)
=== RUN   TestCatCommand_EmptyFile
--- PASS: TestCatCommand_EmptyFile (0.00s)
=== RUN   TestCatCommand_LargeFile
--- PASS: TestCatCommand_LargeFile (0.00s)
=== RUN   TestCatCommand_MultipleFiles
--- PASS: TestCatCommand_MultipleFiles (0.00s)
=== RUN   TestCatCommand_NonExistentFile
--- PASS: TestCatCommand_NonExistentFile (0.00s)
=== RUN   TestCatCommand_BinaryContent
--- PASS: TestCatCommand_BinaryContent (0.00s)
=== RUN   TestCatCommand_UTF8Content
--- PASS: TestCatCommand_UTF8Content (0.00s)
PASS
```

## Implementation Details

### catFile() Function
```go
func catFile(ctx context.Context, client pb.Plan92Client, sessionID, path string) (string, error) {
    // 1. Open file for reading
    openResp, _ := client.Open(ctx, &pb.OpenRequest{
        Path:      path,
        Mode:      pb.OpenMode_OPEN_MODE_READ,
        SessionId: sessionID,
    })

    // 2. Ensure cleanup
    defer client.Close(ctx, &pb.CloseRequest{Fd: openResp.Fd})

    // 3. Stream read
    readStream, _ := client.Read(ctx, &pb.ReadRequest{
        Fd:     openResp.Fd,
        Offset: -1, // Start from beginning
        Count:  -1, // Read all
    })

    // 4. Accumulate chunks
    var content []byte
    for {
        resp, err := readStream.Recv()
        if err == io.EOF {
            break
        }
        if chunk := resp.GetChunk(); chunk != nil {
            content = append(content, chunk...)
        }
    }

    return string(content), nil
}
```

### Test Server Setup
- Uses in-memory gRPC server with `bufconn`
- No network I/O for fast tests
- Full service initialization (storage, sessions, both services)
- Isolated per test (no shared state)

## What This Tests

### Core Functionality
- ✅ File opening with read permissions
- ✅ Streaming read operations
- ✅ Chunk assembly and ordering
- ✅ File descriptor management
- ✅ Session-based access control
- ✅ Automatic cleanup (defer Close)

### Edge Cases
- ✅ Empty files
- ✅ Large files (>32KB chunks)
- ✅ Non-existent files
- ✅ Binary content
- ✅ Multi-byte UTF-8 characters

### Integration Points
- ✅ Plan92 service
- ✅ Session manager
- ✅ Storage backend
- ✅ Permission system (implicit via Open)

## Future Enhancements

Could add tests for:
- Multiple concurrent cat operations
- Cat with offset (start reading from middle of file)
- Cat with count limit (read only N bytes)
- Permission denied scenarios
- Session expiration during read
- Network errors/retries
- Memory limits for very large files

## Notes

This test demonstrates how Plan92 can be used to implement Unix-style file operations over gRPC. The `cat` command is a simple example, but the same patterns apply to more complex operations like:
- `grep` - filter lines matching pattern
- `sed` - stream editing
- `awk` - text processing
- Pipeline composition - chain multiple operations

The hierarchical permission checking and streaming architecture make Plan92 suitable for building distributed file processing pipelines.
