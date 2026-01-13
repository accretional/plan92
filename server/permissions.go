package main

import (
	"fmt"
	"path"
	"strings"

	pb "github.com/accretional/plan92/gen/plan92/v1"
)

// PermissionChecker handles hierarchical permission validation
type PermissionChecker struct {
	storage *MemoryStorage
}

// NewPermissionChecker creates a new permission checker
func NewPermissionChecker(storage *MemoryStorage) *PermissionChecker {
	return &PermissionChecker{
		storage: storage,
	}
}

// CheckPathPermissions validates permissions for each component in the path
// Returns error if any component denies access
func (pc *PermissionChecker) CheckPathPermissions(
	filePath string,
	mode pb.OpenMode,
	user string,
	groups []string,
) error {
	// Clean and normalize path
	filePath = path.Clean(filePath)

	// Split path into components
	components := splitPath(filePath)

	// Check permissions for each component
	currentPath := ""
	for i, component := range components {
		if currentPath == "" {
			currentPath = "/" + component
		} else {
			currentPath = path.Join(currentPath, component)
		}

		// Check if this is the final component
		isFinal := (i == len(components)-1)

		// Get file info from storage
		data, err := pc.storage.Get(currentPath)
		if err != nil {
			// If file doesn't exist and this is the final component,
			// check parent directory write permission for create
			if isFinal && mode == pb.OpenMode_OPEN_MODE_WRITE {
				// For now, allow creation
				return nil
			}
			return fmt.Errorf("no such file or directory: %s", currentPath)
		}

		// Check permissions based on mode and position in path
		if isFinal {
			// Final component - check read/write/exec permissions
			if err := pc.checkFilePermission(data.Info, mode, user, groups); err != nil {
				return fmt.Errorf("permission denied for %s: %v", currentPath, err)
			}
		} else {
			// Intermediate component - must be a directory and have execute permission
			if data.Info.Type != pb.FileType_FILE_TYPE_DIRECTORY {
				return fmt.Errorf("not a directory: %s", currentPath)
			}

			// Need execute permission to traverse directories
			if !pc.hasExecutePermission(data.Info, user, groups) {
				return fmt.Errorf("permission denied (no execute) for directory: %s", currentPath)
			}
		}
	}

	return nil
}

// checkFilePermission checks if the user has the requested permission on the file
func (pc *PermissionChecker) checkFilePermission(
	info *pb.FileInfo,
	mode pb.OpenMode,
	user string,
	groups []string,
) error {
	switch mode {
	case pb.OpenMode_OPEN_MODE_READ:
		if !pc.hasReadPermission(info, user, groups) {
			return fmt.Errorf("no read permission")
		}
	case pb.OpenMode_OPEN_MODE_WRITE, pb.OpenMode_OPEN_MODE_TRUNC:
		if !pc.hasWritePermission(info, user, groups) {
			return fmt.Errorf("no write permission")
		}
	case pb.OpenMode_OPEN_MODE_RDWR:
		if !pc.hasReadPermission(info, user, groups) {
			return fmt.Errorf("no read permission")
		}
		if !pc.hasWritePermission(info, user, groups) {
			return fmt.Errorf("no write permission")
		}
	case pb.OpenMode_OPEN_MODE_EXEC:
		if !pc.hasExecutePermission(info, user, groups) {
			return fmt.Errorf("no execute permission")
		}
	}

	return nil
}

// hasReadPermission checks if user has read permission
func (pc *PermissionChecker) hasReadPermission(
	info *pb.FileInfo,
	user string,
	groups []string,
) bool {
	// Owner permissions (bits 8-6)
	if info.Owner == user {
		return (info.Mode & 0400) != 0 // Owner read bit
	}

	// Group permissions (bits 5-3)
	for _, group := range groups {
		if info.Group == group {
			return (info.Mode & 0040) != 0 // Group read bit
		}
	}

	// Other permissions (bits 2-0)
	return (info.Mode & 0004) != 0 // Other read bit
}

// hasWritePermission checks if user has write permission
func (pc *PermissionChecker) hasWritePermission(
	info *pb.FileInfo,
	user string,
	groups []string,
) bool {
	// Owner permissions
	if info.Owner == user {
		return (info.Mode & 0200) != 0 // Owner write bit
	}

	// Group permissions
	for _, group := range groups {
		if info.Group == group {
			return (info.Mode & 0020) != 0 // Group write bit
		}
	}

	// Other permissions
	return (info.Mode & 0002) != 0 // Other write bit
}

// hasExecutePermission checks if user has execute permission
func (pc *PermissionChecker) hasExecutePermission(
	info *pb.FileInfo,
	user string,
	groups []string,
) bool {
	// Owner permissions
	if info.Owner == user {
		return (info.Mode & 0100) != 0 // Owner execute bit
	}

	// Group permissions
	for _, group := range groups {
		if info.Group == group {
			return (info.Mode & 0010) != 0 // Group execute bit
		}
	}

	// Other permissions
	return (info.Mode & 0001) != 0 // Other execute bit
}

// splitPath splits a path into components, handling both absolute and relative paths
func splitPath(p string) []string {
	p = path.Clean(p)

	// Remove leading slash
	p = strings.TrimPrefix(p, "/")

	// Split by separator
	if p == "" {
		return []string{}
	}

	return strings.Split(p, "/")
}
