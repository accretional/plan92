# Plan92 Filesystem RPC Service

A Plan9-like filesystem service providing simple file I/O operations over gRPC with global file descriptor management.

## Overview

Plan92 implements a filesystem abstraction over gRPC that enables:
- **Simple file operations** - Open, Read, Write, Close without session management
- **Streaming I/O** for efficient large file transfers
- **Global file descriptors** that can be passed between operations
- **In-memory storage** for fast access

This service is designed to support building pipeline DAGs where file operations can be chained and composed.

## Architecture

### Service

**Plan92 Service** (`plan92.proto`):
- `Open` - Open a file at a path and return a file descriptor
- `Read` - Stream file contents from an open FD
- `Write` - Stream data to write to an open FD
- `Close` - Close a file descriptor
- `Stat` - Get file metadata without opening

### Components

- **Storage Backend** (`storage.go`) - In-memory file storage with reference counting
- **FD Table** (`fdtable.go`) - Global file descriptor allocation and management
- **Service Implementation** (`plan92_service.go`) - gRPC service handlers

## Building

```bash
# Generate proto code
cd /home/workspace/plan92
protoc --go_out=gen --go_opt=module=github.com/accretional/plan92/gen \
       --go-grpc_out=gen --go-grpc_opt=module=github.com/accretional/plan92/gen \
       plan92.proto

# Build server
cd server
go build -o plan92-server

# Build client
cd ../client
go build -o plan92-client
```

## Running

### Start the Server

```bash
cd /home/workspace/plan92/server
./plan92-server
```

The server will start on port 9000 by default. You can override this with the `PORT` environment variable:

```bash
PORT=8080 ./plan92-server
```

### Run the Example Client

```bash
cd /home/workspace/plan92/client
./plan92-client
```

The example client demonstrates:
1. Writing a file
2. Getting file statistics
3. Reading the file back
4. Writing another file

## Usage Example

```go
// Connect to server
conn, err := grpc.Dial("localhost:9000",
    grpc.WithTransportCredentials(insecure.NewCredentials()),
)
client := pb.NewPlan92Client(conn)

// Open file for writing
fd, _ := client.Open(ctx, &pb.OpenRequest{
    Path: "/test.txt",
    Mode: pb.OpenMode_OPEN_MODE_WRITE,
})

// Stream write
writeStream, _ := client.Write(ctx)
writeStream.Send(&pb.WriteRequest{
    Data: &pb.WriteRequest_Metadata{
        Metadata: &pb.WriteMetadata{Fd: fd.Fd},
    },
})
writeStream.Send(&pb.WriteRequest{
    Data: &pb.WriteRequest_Chunk{Chunk: []byte("Hello, Plan92!")},
})
writeStream.CloseAndRecv()

// Close file
client.Close(ctx, &pb.CloseRequest{Fd: fd.Fd})

// Open file for reading
readFd, _ := client.Open(ctx, &pb.OpenRequest{
    Path: "/test.txt",
    Mode: pb.OpenMode_OPEN_MODE_READ,
})

// Stream read
readStream, _ := client.Read(ctx, &pb.ReadRequest{
    Fd:     readFd.Fd,
    Offset: -1, // Current position (start)
    Count:  -1, // Read all
})

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

// Close file
client.Close(ctx, &pb.CloseRequest{Fd: readFd.Fd})
```

## Testing with grpcurl

```bash
# List services
grpcurl -plaintext localhost:9000 list

# Open a file (returns fd)
grpcurl -plaintext -d '{"path":"/test.txt","mode":"OPEN_MODE_WRITE"}' \
    localhost:9000 plan92.v1.Plan92/Open

# Get file stats
grpcurl -plaintext -d '{"path":"/test.txt"}' \
    localhost:9000 plan92.v1.Plan92/Stat
```

## Design Decisions

### Global File Descriptors

All file descriptors are managed in a single global table. This provides:
- **Simplicity** - No session management overhead
- **Composability** - FDs can be passed between different operations
- **Pipeline support** - Natural fit for building file processing pipelines

### In-Memory Storage

The current implementation uses in-memory storage for simplicity and speed:
- Fast operations with no disk I/O
- Easy testing and development
- Reference counting prevents premature deletion
- Can be extended with disk-backed storage in the future

### Streaming Pattern

Streaming operations use `oneof` for efficient data transfer:
- **Metadata-first**: First message contains metadata (FD, size, etc.)
- **Chunk streaming**: Subsequent messages contain data chunks
- **Efficient**: 32KB chunks for optimal network utilization

### Open Modes

Files can be opened with different modes:
- `OPEN_MODE_READ` - Open for reading
- `OPEN_MODE_WRITE` - Open for writing (creates if doesn't exist)
- `OPEN_MODE_RDWR` - Open for both reading and writing
- `OPEN_MODE_TRUNC` - Truncate file on open
- `OPEN_MODE_EXEC` - Open for execution
- `OPEN_MODE_RCLOSE` - Remove file on close

## Testing

The service includes comprehensive tests mimicking Unix `cat` command behavior:

```bash
cd /home/workspace/plan92/server
go test -v -run TestCatCommand

# All tests:
# ✓ TestCatCommand_SimpleFile
# ✓ TestCatCommand_EmptyFile
# ✓ TestCatCommand_LargeFile (100KB)
# ✓ TestCatCommand_MultipleFiles
# ✓ TestCatCommand_NonExistentFile
# ✓ TestCatCommand_BinaryContent
# ✓ TestCatCommand_UTF8Content
```

See `cat_test_README.md` for detailed test documentation.

## Future Enhancements

- **Full Plan9 Walk/Fid semantics** with QID tracking
- **Pipeline orchestration service** for DAG construction
- **Disk-backed storage** with persistence
- **Named pipes and Unix sockets** for inter-process communication
- **File locking** (flock, fcntl)
- **Directory operations** (readdir, mkdir, rmdir)
- **Permission system** (optional user/group access control)

## Dependencies

- `google.golang.org/grpc` - gRPC framework
- `google.golang.org/protobuf` - Protocol Buffers

## License

See parent project license.
