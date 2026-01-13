package main

import (
	"fmt"
	"sync"
	"sync/atomic"
)

// FileHandle represents an open file descriptor
type FileHandle struct {
	FD     int32
	Path   string
	Offset int64
	Data   *FileData
}

// FDTable manages file descriptor allocation and mapping
type FDTable struct {
	mu      sync.RWMutex
	handles map[int32]*FileHandle
	nextFD  atomic.Int32
}

// NewFDTable creates a new file descriptor table
func NewFDTable() *FDTable {
	table := &FDTable{
		handles: make(map[int32]*FileHandle),
	}
	// Start FD allocation at 3 (0, 1, 2 are stdin, stdout, stderr)
	table.nextFD.Store(3)
	return table
}

// Allocate allocates a new file descriptor
func (t *FDTable) Allocate(path string, data *FileData) int32 {
	t.mu.Lock()
	defer t.mu.Unlock()

	fd := t.nextFD.Add(1)
	t.handles[fd] = &FileHandle{
		FD:     fd,
		Path:   path,
		Offset: 0,
		Data:   data,
	}

	return fd
}

// AllocateSimple is an alias for Allocate (for clarity in new code)
func (t *FDTable) AllocateSimple(path string, data *FileData) int32 {
	return t.Allocate(path, data)
}

// Get retrieves a file handle by FD
func (t *FDTable) Get(fd int32) (*FileHandle, error) {
	t.mu.RLock()
	defer t.mu.RUnlock()

	handle, exists := t.handles[fd]
	if !exists {
		return nil, fmt.Errorf("invalid file descriptor: %d", fd)
	}

	return handle, nil
}

// Release releases a file descriptor
func (t *FDTable) Release(fd int32) error {
	t.mu.Lock()
	defer t.mu.Unlock()

	if _, exists := t.handles[fd]; !exists {
		return fmt.Errorf("invalid file descriptor: %d", fd)
	}

	delete(t.handles, fd)
	return nil
}

// List returns all file handles in the table
func (t *FDTable) List() []*FileHandle {
	t.mu.RLock()
	defer t.mu.RUnlock()

	handles := make([]*FileHandle, 0, len(t.handles))
	for _, handle := range t.handles {
		handles = append(handles, handle)
	}

	return handles
}

// Count returns the number of open file descriptors
func (t *FDTable) Count() int {
	t.mu.RLock()
	defer t.mu.RUnlock()

	return len(t.handles)
}

// CloseAll closes all file descriptors in the table
func (t *FDTable) CloseAll() []int32 {
	t.mu.Lock()
	defer t.mu.Unlock()

	fds := make([]int32, 0, len(t.handles))
	for fd := range t.handles {
		fds = append(fds, fd)
	}

	// Clear all handles
	t.handles = make(map[int32]*FileHandle)

	return fds
}

// UpdateOffset updates the offset for a file handle
func (t *FDTable) UpdateOffset(fd int32, newOffset int64) error {
	t.mu.Lock()
	defer t.mu.Unlock()

	handle, exists := t.handles[fd]
	if !exists {
		return fmt.Errorf("invalid file descriptor: %d", fd)
	}

	handle.Offset = newOffset
	return nil
}

// GetOffset returns the current offset for a file handle
func (t *FDTable) GetOffset(fd int32) (int64, error) {
	t.mu.RLock()
	defer t.mu.RUnlock()

	handle, exists := t.handles[fd]
	if !exists {
		return 0, fmt.Errorf("invalid file descriptor: %d", fd)
	}

	return handle.Offset, nil
}
