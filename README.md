# Plan92 Filesystem RPC Service

A Plan9-like filesystem service providing file I/O operations over gRPC with hierarchical permission checking and session-based file descriptor management.

## Overview

Plan92 implements a filesystem abstraction over gRPC that enables:
- **Session-based file access** with isolated file descriptor tables
- **Hierarchical permission checking** through path component validation
- **Streaming I/O** for efficient large file transfers
- **Unix-style permissions** (user/group/other with rwx bits)

This service is designed to support building pipeline DAGs where file operations can be chained and composed.

## Architecture

### Services

**Plan92 Service** (`plan92.proto`):
- `CreateSession` - Initialize a new session with user context
- `CloseSession` - Clean up session and all open file descriptors
- `Open` - Open a file and return a file descriptor
- `Read` - Stream file contents from an open FD
- `Write` - Stream data to write to an open FD
- `Close` - Close a file descriptor
- `Stat` - Get file metadata without opening

**InodeService** (`inode.proto`):
- `CheckPermission` - Validate permissions for a path
- `AllocateFd` - Allocate a file descriptor for an opened file
- `GetInode` - Retrieve inode information
- `CreateInode` - Create a new file or directory

### Components

- **Storage Backend** (`storage.go`) - In-memory file storage with reference counting
- **FD Table** (`fdtable.go`) - File descriptor allocation and management
- **Session Manager** (`session.go`) - Session lifecycle and cleanup
- **Permission Checker** (`permissions.go`) - Hierarchical path permission validation
- **Service Implementations** (`plan92_service.go`, `inode_service.go`) - gRPC service handlers

## Building

```bash
# Generate proto code
protoc --go_out=gen --go_opt=module=github.com/accretional/plan92/gen \
       --go-grpc_out=gen --go-grpc_opt=module=github.com/accretional/plan92/gen \
       plan92.proto inode.proto

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
cd server
./plan92-server
```

The server will start on port 9000 by default. You can override this with the `PORT` environment variable:

```bash
PORT=8080 ./plan92-server
```

### Run the Example Client

```bash
cd client
./plan92-client
```

The example client demonstrates:
1. Creating a session
2. Writing a file
3. Reading the file back
4. Getting file statistics
5. Closing the session

## Usage Example

```go
// Connect to server
conn, err := grpc.Dial("localhost:9000",
    grpc.WithTransportCredentials(insecure.NewCredentials()),
)
client := pb.NewPlan92Client(conn)

// Create session
session, _ := client.CreateSession(ctx, &pb.CreateSessionRequest{
    User:   "alice",
    Groups: []string{"users"},
})

// Open file for writing
fd, _ := client.Open(ctx, &pb.OpenRequest{
    Path:      "/test.txt",
    Mode:      pb.OpenMode_OPEN_MODE_WRITE,
    SessionId: session.SessionId,
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

// Close session
client.CloseSession(ctx, &pb.CloseSessionRequest{
    SessionId: session.SessionId,
})
```

## Testing with grpcurl

```bash
# List services
grpcurl -plaintext localhost:9000 list

# Create a session
grpcurl -plaintext -d '{"user":"alice","groups":["users"]}' \
    localhost:9000 plan92.v1.Plan92/CreateSession
```

## Design Decisions

### Hierarchical Permission Checking

When opening a file, Plan92 validates permissions at each level of the path:
1. Split path into components (e.g., `/a/b/file.txt` → `["a", "b", "file.txt"]`)
2. For each component, check execute permission on parent directory
3. For the final component, check the requested access mode (read/write/exec)

### Session-Based Isolation

Each session maintains its own file descriptor table. This provides:
- **Isolation** - Sessions cannot access each other's file descriptors
- **Automatic cleanup** - Closing a session releases all associated FDs
- **Multi-tenant support** - Different users can safely use the same service

### In-Memory Storage

The current implementation uses in-memory storage for simplicity and speed:
- Fast operations with no disk I/O
- Easy testing and development
- Reference counting prevents premature deletion
- Can be extended with disk-backed storage in the future

### Streaming Pattern

- **Metadata-first**: First message contains metadata (FD, size, etc.)
- **Chunk streaming**: Subsequent messages contain data chunks
- **Efficient**: 32KB chunks for optimal network utilization

## Future Enhancements

- **Full Plan9 Walk/Fid semantics** with QID tracking
- **Pipeline orchestration service** for DAG construction
- **Disk-backed storage** with persistence
- **Named pipes and Unix sockets** for inter-process communication
- **File locking** (flock, fcntl)
- **Directory operations** (readdir, mkdir, rmdir)
- **ACLs** beyond basic Unix permissions
