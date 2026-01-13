package main

import (
	"context"
	"io"

	pb "github.com/accretional/plan92/gen/plan92/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const (
	chunkSize = 32 * 1024 // 32KB chunks for streaming
)

// Plan92ServiceImpl implements the Plan92FS gRPC service
type Plan92ServiceImpl struct {
	pb.UnimplementedPlan92FSServer
	storage *MemoryStorage
	fdTable *FDTable
}

// NewPlan92Service creates a new Plan92FS service implementation
func NewPlan92Service(storage *MemoryStorage, fdTable *FDTable) *Plan92ServiceImpl {
	return &Plan92ServiceImpl{
		storage: storage,
		fdTable: fdTable,
	}
}

// ============================================================================
// Streamed File Operations
// ============================================================================

// Open opens a file and returns a file descriptor.
// Files are always opened with read/write access.
func (s *Plan92ServiceImpl) Open(
	ctx context.Context,
	req *pb.URI,
) (*pb.OpenFileDescriptor, error) {
	// Check if file exists
	data, err := s.storage.Get(req.Path)
	if err != nil {
		// File doesn't exist - create it
		if err := s.storage.CreateSimple(req.Path); err != nil {
			return nil, status.Errorf(codes.Internal, "failed to create file: %v", err)
		}
		// Get the newly created file
		data, err = s.storage.Get(req.Path)
		if err != nil {
			return nil, status.Errorf(codes.Internal, "failed to get created file: %v", err)
		}
	}

	// Allocate FD and increment refcount (always RDWR internally)
	s.storage.IncRef(req.Path)
	fd := s.fdTable.AllocateSimple(req.Path, data)

	return &pb.OpenFileDescriptor{
		Fd: fd,
	}, nil
}

// Read reads data from an open file descriptor (server streaming)
func (s *Plan92ServiceImpl) Read(
	req *pb.OpenFileDescriptor,
	stream pb.Plan92FS_ReadServer,
) error {
	// Get file handle
	handle, err := s.fdTable.Get(req.Fd)
	if err != nil {
		return status.Errorf(codes.InvalidArgument, "invalid file descriptor: %d", req.Fd)
	}

	// Get file data
	data, err := s.storage.Get(handle.Path)
	if err != nil {
		return status.Errorf(codes.NotFound, "file not found: %v", err)
	}

	// Stream file content in chunks
	content := data.Content
	for len(content) > 0 {
		end := chunkSize
		if end > len(content) {
			end = len(content)
		}

		chunk := content[:end]
		content = content[end:]

		if err := stream.Send(&pb.BytesValue{
			Data: chunk,
		}); err != nil {
			return status.Errorf(codes.Internal, "failed to send chunk: %v", err)
		}
	}

	return nil
}

// Write writes data to an open file descriptor (client streaming)
func (s *Plan92ServiceImpl) Write(
	stream pb.Plan92FS_WriteServer,
) error {
	var fd int32
	var handle *FileHandle
	var buffer []byte
	var firstMessage = true

	// Receive all chunks
	for {
		req, err := stream.Recv()
		if err == io.EOF {
			break
		}
		if err != nil {
			return status.Errorf(codes.Internal, "failed to receive chunk: %v", err)
		}

		if firstMessage {
			// First message should contain FD
			fd = req.GetFd()
			if fd == 0 {
				return status.Errorf(codes.InvalidArgument, "first message must contain fd")
			}

			// Validate FD
			h, err := s.fdTable.Get(fd)
			if err != nil {
				return status.Errorf(codes.InvalidArgument, "invalid file descriptor: %d", fd)
			}
			handle = h
			firstMessage = false
		} else {
			// Subsequent messages contain data chunks
			chunk := req.GetChunk()
			if chunk != nil {
				buffer = append(buffer, chunk...)
			}
		}
	}

	if handle == nil {
		return status.Errorf(codes.InvalidArgument, "no fd received")
	}

	// Write data to storage (replace entire file)
	if err := s.storage.Write(handle.Path, buffer); err != nil {
		return stream.SendAndClose(&pb.WriteResponse{
			Count: 0,
			Errno: toErrnoPtr(pb.Errno_ERRNO_EIO),
		})
	}

	// Send response
	return stream.SendAndClose(&pb.WriteResponse{
		Count: int32(len(buffer)),
	})
}

// Close closes an open file descriptor
func (s *Plan92ServiceImpl) Close(
	ctx context.Context,
	req *pb.OpenFileDescriptor,
) (*pb.CloseResponse, error) {
	// Get FD handle to find path
	handle, err := s.fdTable.Get(req.Fd)
	if err != nil {
		return &pb.CloseResponse{
			Errno: toErrnoPtr(pb.Errno_ERRNO_EBADF),
		}, nil
	}

	// Release FD from table
	if err := s.fdTable.Release(req.Fd); err != nil {
		return &pb.CloseResponse{
			Errno: toErrnoPtr(pb.Errno_ERRNO_EIO),
		}, nil
	}

	// Decrement reference count in storage
	if err := s.storage.DecRef(handle.Path); err != nil {
		// Log but don't fail - FD is already released
		_ = err
	}

	return &pb.CloseResponse{}, nil
}

// ============================================================================
// Non-Streamed Operations
// ============================================================================

// ReadAll reads entire file content from an open file descriptor
func (s *Plan92ServiceImpl) ReadAll(
	ctx context.Context,
	req *pb.OpenFileDescriptor,
) (*pb.BytesValue, error) {
	// Get file handle
	handle, err := s.fdTable.Get(req.Fd)
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "invalid file descriptor: %d", req.Fd)
	}

	// Get file data
	data, err := s.storage.Get(handle.Path)
	if err != nil {
		return nil, status.Errorf(codes.NotFound, "file not found: %v", err)
	}

	return &pb.BytesValue{
		Data: data.Content,
	}, nil
}

// WriteAll writes entire content to an open file descriptor
func (s *Plan92ServiceImpl) WriteAll(
	ctx context.Context,
	req *pb.WriteAllRequest,
) (*pb.WriteResponse, error) {
	// Get file handle
	handle, err := s.fdTable.Get(req.Fd)
	if err != nil {
		return &pb.WriteResponse{
			Count: 0,
			Errno: toErrnoPtr(pb.Errno_ERRNO_EBADF),
		}, nil
	}

	// Write data to storage
	if err := s.storage.Write(handle.Path, req.Data); err != nil {
		return &pb.WriteResponse{
			Count: 0,
			Errno: toErrnoPtr(pb.Errno_ERRNO_EIO),
		}, nil
	}

	return &pb.WriteResponse{
		Count: int32(len(req.Data)),
	}, nil
}

// ============================================================================
// Direct URI-Based Operations
// ============================================================================

// ReadDirect reads a file directly by path (open, read, close in one call)
func (s *Plan92ServiceImpl) ReadDirect(
	ctx context.Context,
	req *pb.URI,
) (*pb.BytesValue, error) {
	// Get file data directly from storage
	data, err := s.storage.Get(req.Path)
	if err != nil {
		return nil, status.Errorf(codes.NotFound, "file not found: %s", req.Path)
	}

	return &pb.BytesValue{
		Data: data.Content,
	}, nil
}

// WriteDirect writes to a file directly by path (open, write, close in one call)
func (s *Plan92ServiceImpl) WriteDirect(
	ctx context.Context,
	req *pb.WriteDirectRequest,
) (*pb.WriteResponse, error) {
	// Check if file exists, create if not
	_, err := s.storage.Get(req.Path)
	if err != nil {
		if err := s.storage.CreateSimple(req.Path); err != nil {
			return &pb.WriteResponse{
				Count: 0,
				Errno: toErrnoPtr(pb.Errno_ERRNO_EIO),
			}, nil
		}
	}

	// Write data to storage
	if err := s.storage.Write(req.Path, req.Data); err != nil {
		return &pb.WriteResponse{
			Count: 0,
			Errno: toErrnoPtr(pb.Errno_ERRNO_EIO),
		}, nil
	}

	return &pb.WriteResponse{
		Count: int32(len(req.Data)),
	}, nil
}

// ============================================================================
// Helper Functions
// ============================================================================

// toErrnoPtr converts an Errno to a pointer for optional field
func toErrnoPtr(e pb.Errno) *int32 {
	val := int32(e)
	return &val
}
