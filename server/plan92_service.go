package main

import (
	"context"
	"io"

	pb "github.com/accretional/plan92/gen/plan92/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/emptypb"
	"google.golang.org/protobuf/types/known/timestamppb"
)

const (
	chunkSize = 32 * 1024 // 32KB chunks for streaming
)

// Plan92ServiceImpl implements the Plan92 gRPC service
type Plan92ServiceImpl struct {
	pb.UnimplementedPlan92Server
	storage      *MemoryStorage
	sessions     *SessionManager
	inodeService *InodeServiceImpl
}

// NewPlan92Service creates a new Plan92 service implementation
func NewPlan92Service(storage *MemoryStorage, sessions *SessionManager, inodeService *InodeServiceImpl) *Plan92ServiceImpl {
	return &Plan92ServiceImpl{
		storage:      storage,
		sessions:     sessions,
		inodeService: inodeService,
	}
}

// ============================================================================
// Session Management
// ============================================================================

// CreateSession creates a new session
func (s *Plan92ServiceImpl) CreateSession(
	ctx context.Context,
	req *pb.CreateSessionRequest,
) (*pb.CreateSessionResponse, error) {
	session, err := s.sessions.Create(req.User, req.Groups)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to create session: %v", err)
	}

	return &pb.CreateSessionResponse{
		SessionId: session.ID,
		CreatedAt: timestamppb.New(session.CreatedAt),
	}, nil
}

// CloseSession closes a session and cleans up resources
func (s *Plan92ServiceImpl) CloseSession(
	ctx context.Context,
	req *pb.CloseSessionRequest,
) (*emptypb.Empty, error) {
	if err := s.sessions.Close(req.SessionId, s.storage); err != nil {
		return nil, status.Errorf(codes.NotFound, "failed to close session: %v", err)
	}

	return &emptypb.Empty{}, nil
}

// ============================================================================
// File Operations
// ============================================================================

// Open opens a file and returns a file descriptor
func (s *Plan92ServiceImpl) Open(
	ctx context.Context,
	req *pb.OpenRequest,
) (*pb.FileStatus, error) {
	// Validate session
	session, err := s.sessions.Get(req.SessionId)
	if err != nil {
		return nil, status.Errorf(codes.Unauthenticated, "invalid session: %v", err)
	}

	// Check permissions using InodeService
	permReq := &pb.CheckPermissionRequest{
		Path:          req.Path,
		SessionId:     req.SessionId,
		RequestedMode: req.Mode,
		Context: &pb.PermissionContext{
			User:   session.User,
			Groups: session.Groups,
		},
	}

	permResp, err := s.inodeService.CheckPermission(ctx, permReq)
	if err != nil {
		return nil, err
	}

	if !permResp.Granted {
		return nil, status.Errorf(codes.PermissionDenied, "permission denied: %s", permResp.Reason)
	}

	// Allocate FD using InodeService
	allocReq := &pb.AllocateFdRequest{
		Path:      req.Path,
		Mode:      req.Mode,
		SessionId: req.SessionId,
		Inode:     permResp.Inode,
	}

	fileStatus, err := s.inodeService.AllocateFd(ctx, allocReq)
	if err != nil {
		return nil, err
	}

	return fileStatus, nil
}

// Read reads data from an open file descriptor (server streaming)
func (s *Plan92ServiceImpl) Read(
	req *pb.ReadRequest,
	stream pb.Plan92_ReadServer,
) error {
	// Get session and validate FD
	handle, err := s.getAndValidateFD(req.Fd)
	if err != nil {
		return err
	}

	// Check if FD is opened for reading
	if handle.Mode != pb.OpenMode_OPEN_MODE_READ &&
		handle.Mode != pb.OpenMode_OPEN_MODE_RDWR {
		return status.Errorf(codes.PermissionDenied, "file not opened for reading")
	}

	// Get file data
	data, err := s.storage.Get(handle.Path)
	if err != nil {
		return status.Errorf(codes.NotFound, "file not found: %v", err)
	}

	// Send metadata first
	metadata := &pb.ReadMetadata{
		Fd:        req.Fd,
		TotalSize: data.Info.Length,
		FileInfo:  data.Info,
	}

	if err := stream.Send(&pb.ReadResponse{
		Data: &pb.ReadResponse_Metadata{Metadata: metadata},
	}); err != nil {
		return status.Errorf(codes.Internal, "failed to send metadata: %v", err)
	}

	// Determine read parameters
	offset := req.Offset
	if offset < 0 {
		offset = handle.Offset
	}

	count := req.Count
	if count <= 0 {
		count = int32(len(data.Content)) - int32(offset)
	}

	// Stream file content in chunks
	content := data.Content[offset:]
	if int32(len(content)) > count {
		content = content[:count]
	}

	for len(content) > 0 {
		end := chunkSize
		if end > len(content) {
			end = len(content)
		}

		chunk := content[:end]
		content = content[end:]

		if err := stream.Send(&pb.ReadResponse{
			Data: &pb.ReadResponse_Chunk{Chunk: chunk},
		}); err != nil {
			return status.Errorf(codes.Internal, "failed to send chunk: %v", err)
		}
	}

	// Update FD offset if using current position
	if req.Offset < 0 {
		newOffset := handle.Offset + int64(count)
		// Note: We're not updating the offset in the FDTable here for simplicity
		// In a production implementation, you'd want to track this
		_ = newOffset
	}

	return nil
}

// Write writes data to an open file descriptor (client streaming)
func (s *Plan92ServiceImpl) Write(
	stream pb.Plan92_WriteServer,
) error {
	var fd int32
	var handle *FileHandle
	var buffer []byte
	var offset int64 = -1

	// Receive all chunks
	for {
		req, err := stream.Recv()
		if err == io.EOF {
			break
		}
		if err != nil {
			return status.Errorf(codes.Internal, "failed to receive chunk: %v", err)
		}

		switch data := req.Data.(type) {
		case *pb.WriteRequest_Metadata:
			// First message should be metadata
			fd = data.Metadata.Fd
			offset = data.Metadata.Offset

			// Validate FD
			h, err := s.getAndValidateFD(fd)
			if err != nil {
				return err
			}
			handle = h

			// Check if FD is opened for writing
			if handle.Mode != pb.OpenMode_OPEN_MODE_WRITE &&
				handle.Mode != pb.OpenMode_OPEN_MODE_RDWR &&
				handle.Mode != pb.OpenMode_OPEN_MODE_TRUNC {
				return status.Errorf(codes.PermissionDenied, "file not opened for writing")
			}

			// Initialize buffer
			buffer = make([]byte, 0, data.Metadata.TotalSize)

		case *pb.WriteRequest_Chunk:
			// Append chunk to buffer
			buffer = append(buffer, data.Chunk...)
		}
	}

	if handle == nil {
		return status.Errorf(codes.InvalidArgument, "no metadata received")
	}

	// Get existing file data
	data, err := s.storage.Get(handle.Path)
	if err != nil {
		return status.Errorf(codes.NotFound, "file not found: %v", err)
	}

	// Write data to storage
	var newContent []byte
	if offset < 0 || handle.Mode == pb.OpenMode_OPEN_MODE_TRUNC {
		// Replace entire file
		newContent = buffer
	} else {
		// Write at specific offset
		existingContent := data.Content
		if int64(len(existingContent)) < offset {
			// Extend file with zeros if needed
			padding := make([]byte, offset-int64(len(existingContent)))
			existingContent = append(existingContent, padding...)
		}

		// Combine: existing up to offset + new buffer + existing after
		newContent = make([]byte, 0, offset+int64(len(buffer)))
		newContent = append(newContent, existingContent[:offset]...)
		newContent = append(newContent, buffer...)

		// Add remaining content if offset + buffer doesn't cover it all
		if int64(len(existingContent)) > offset+int64(len(buffer)) {
			newContent = append(newContent, existingContent[offset+int64(len(buffer)):]...)
		}
	}

	// Update storage
	if err := s.storage.Set(handle.Path, newContent, data.Info); err != nil {
		return status.Errorf(codes.Internal, "failed to write file: %v", err)
	}

	// Send response
	return stream.SendAndClose(&pb.WriteResponse{
		Fd:           fd,
		BytesWritten: int64(len(buffer)),
	})
}

// Close closes an open file descriptor
func (s *Plan92ServiceImpl) Close(
	ctx context.Context,
	req *pb.CloseRequest,
) (*pb.CloseResponse, error) {
	// Get FD handle
	handle, err := s.getAndValidateFD(req.Fd)
	if err != nil {
		return nil, err
	}

	// Decrement reference count in storage
	if err := s.storage.DecRef(handle.Path); err != nil {
		return nil, status.Errorf(codes.Internal, "failed to decrement refcount: %v", err)
	}

	// Release FD from session's FD table
	session, err := s.getSessionForFD(req.Fd)
	if err != nil {
		return nil, err
	}

	if err := session.FDTable.Release(req.Fd); err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "failed to release FD: %v", err)
	}

	return &pb.CloseResponse{
		Success: true,
	}, nil
}

// Stat gets file information without opening
func (s *Plan92ServiceImpl) Stat(
	ctx context.Context,
	req *pb.StatRequest,
) (*pb.StatResponse, error) {
	// Validate session
	_, err := s.sessions.Get(req.SessionId)
	if err != nil {
		return nil, status.Errorf(codes.Unauthenticated, "invalid session: %v", err)
	}

	// Get file info
	data, err := s.storage.Get(req.Path)
	if err != nil {
		return nil, status.Errorf(codes.NotFound, "file not found: %v", err)
	}

	return &pb.StatResponse{
		Info: data.Info,
	}, nil
}

// ============================================================================
// Helper Methods
// ============================================================================

// getAndValidateFD retrieves and validates a file descriptor
func (s *Plan92ServiceImpl) getAndValidateFD(fd int32) (*FileHandle, error) {
	// Find which session owns this FD
	session, err := s.getSessionForFD(fd)
	if err != nil {
		return nil, err
	}

	// Get handle from session's FD table
	handle, err := session.FDTable.Get(fd)
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "invalid file descriptor: %v", err)
	}

	return handle, nil
}

// getSessionForFD finds the session that owns a given FD
func (s *Plan92ServiceImpl) getSessionForFD(fd int32) (*Session, error) {
	// Iterate through all sessions to find the owner
	// In a production system, you'd maintain a reverse mapping for efficiency
	for _, sessionID := range s.sessions.List() {
		session, err := s.sessions.Get(sessionID)
		if err != nil {
			continue
		}

		if _, err := session.FDTable.Get(fd); err == nil {
			return session, nil
		}
	}

	return nil, status.Errorf(codes.InvalidArgument, "file descriptor not found in any session")
}
