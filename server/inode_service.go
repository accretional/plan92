package main

import (
	"context"

	pb "github.com/accretional/plan92/gen/plan92/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// InodeServiceImpl implements the InodeService gRPC service
type InodeServiceImpl struct {
	pb.UnimplementedInodeServiceServer
	storage     *MemoryStorage
	sessions    *SessionManager
	permChecker *PermissionChecker
}

// NewInodeService creates a new InodeService implementation
func NewInodeService(storage *MemoryStorage, sessions *SessionManager) *InodeServiceImpl {
	return &InodeServiceImpl{
		storage:     storage,
		sessions:    sessions,
		permChecker: NewPermissionChecker(storage),
	}
}

// CheckPermission validates if a user has the requested permissions on a path
func (s *InodeServiceImpl) CheckPermission(
	ctx context.Context,
	req *pb.CheckPermissionRequest,
) (*pb.CheckPermissionResponse, error) {
	// Get session for user context
	session, err := s.sessions.Get(req.SessionId)
	if err != nil {
		return nil, status.Errorf(codes.Unauthenticated, "invalid session: %v", err)
	}

	// Use user from session if not specified in context
	user := req.Context.User
	groups := req.Context.Groups
	if user == "" {
		user = session.User
		groups = session.Groups
	}

	// Check hierarchical permissions
	err = s.permChecker.CheckPathPermissions(req.Path, req.RequestedMode, user, groups)
	if err != nil {
		return &pb.CheckPermissionResponse{
			Granted: false,
			Reason:  err.Error(),
		}, nil
	}

	// Get inode info
	data, err := s.storage.Get(req.Path)
	if err != nil {
		// File doesn't exist - permission checks passed but file needs to be created
		if req.RequestedMode == pb.OpenMode_OPEN_MODE_WRITE ||
			req.RequestedMode == pb.OpenMode_OPEN_MODE_TRUNC {
			return &pb.CheckPermissionResponse{
				Granted: true,
				Reason:  "file will be created",
			}, nil
		}
		return &pb.CheckPermissionResponse{
			Granted: false,
			Reason:  "file not found",
		}, nil
	}

	return &pb.CheckPermissionResponse{
		Granted: true,
		Inode:   data.Info,
	}, nil
}

// AllocateFd allocates a file descriptor for an opened file
func (s *InodeServiceImpl) AllocateFd(
	ctx context.Context,
	req *pb.AllocateFdRequest,
) (*pb.FileStatus, error) {
	// Get session
	session, err := s.sessions.Get(req.SessionId)
	if err != nil {
		return nil, status.Errorf(codes.Unauthenticated, "invalid session: %v", err)
	}

	// Get file data
	data, err := s.storage.Get(req.Path)
	if err != nil {
		// File doesn't exist - create it if opening for write
		if req.Mode == pb.OpenMode_OPEN_MODE_WRITE ||
			req.Mode == pb.OpenMode_OPEN_MODE_TRUNC {
			// Use provided inode info or create default
			info := req.Inode
			if info == nil {
				info = &pb.FileInfo{
					Type:  pb.FileType_FILE_TYPE_REGULAR,
					Mode:  0644, // Default permissions
					Owner: session.User,
					Group: session.Groups[0],
				}
			}

			if err := s.storage.Create(req.Path, info); err != nil {
				return nil, status.Errorf(codes.Internal, "failed to create file: %v", err)
			}

			data, err = s.storage.Get(req.Path)
			if err != nil {
				return nil, status.Errorf(codes.Internal, "failed to get created file: %v", err)
			}
		} else {
			return nil, status.Errorf(codes.NotFound, "file not found: %s", req.Path)
		}
	}

	// Allocate FD in session's FD table
	fd := session.FDTable.Allocate(req.Path, req.Mode, data)

	// Increment reference count
	if err := s.storage.IncRef(req.Path); err != nil {
		session.FDTable.Release(fd) // Clean up on error
		return nil, status.Errorf(codes.Internal, "failed to increment refcount: %v", err)
	}

	return &pb.FileStatus{
		Fd:        fd,
		Path:      req.Path,
		Info:      data.Info,
		Mode:      req.Mode,
		SessionId: req.SessionId,
	}, nil
}

// GetInode retrieves inode information for a path
func (s *InodeServiceImpl) GetInode(
	ctx context.Context,
	req *pb.GetInodeRequest,
) (*pb.FileInfo, error) {
	data, err := s.storage.Get(req.Path)
	if err != nil {
		return nil, status.Errorf(codes.NotFound, "file not found: %s", req.Path)
	}

	return data.Info, nil
}

// CreateInode creates a new inode (file or directory)
func (s *InodeServiceImpl) CreateInode(
	ctx context.Context,
	req *pb.CreateInodeRequest,
) (*pb.FileInfo, error) {
	// Check if file already exists
	if s.storage.Exists(req.Path) {
		return nil, status.Errorf(codes.AlreadyExists, "file already exists: %s", req.Path)
	}

	// Create file info
	info := &pb.FileInfo{
		Type:  req.Type,
		Mode:  req.Mode,
		Owner: req.Owner,
		Group: req.Group,
	}

	// Create the inode
	if err := s.storage.Create(req.Path, info); err != nil {
		return nil, status.Errorf(codes.Internal, "failed to create inode: %v", err)
	}

	// Retrieve and return the created inode
	data, err := s.storage.Get(req.Path)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to get created inode: %v", err)
	}

	return data.Info, nil
}
