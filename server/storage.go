package main

import (
	"fmt"
	"sync"
)

// FileData represents the content of a file in storage
type FileData struct {
	Content  []byte
	RefCount int32 // Number of open file descriptors
}

// MemoryStorage provides an in-memory storage backend for files
type MemoryStorage struct {
	mu    sync.RWMutex
	files map[string]*FileData
}

// NewMemoryStorage creates a new in-memory storage backend
func NewMemoryStorage() *MemoryStorage {
	return &MemoryStorage{
		files: make(map[string]*FileData),
	}
}

// Get retrieves file data for the given path
func (s *MemoryStorage) Get(path string) (*FileData, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	data, exists := s.files[path]
	if !exists {
		return nil, fmt.Errorf("file not found: %s", path)
	}

	return data, nil
}

// Set stores file data at the given path
func (s *MemoryStorage) Set(path string, content []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	data, exists := s.files[path]
	if exists {
		// Update existing file
		data.Content = content
	} else {
		// Create new file
		s.files[path] = &FileData{
			Content:  content,
			RefCount: 0,
		}
	}

	return nil
}

// Create creates a new empty file
func (s *MemoryStorage) Create(path string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, exists := s.files[path]; exists {
		return fmt.Errorf("file already exists: %s", path)
	}

	s.files[path] = &FileData{
		Content:  []byte{},
		RefCount: 0,
	}

	return nil
}

// Delete removes file data at the given path
func (s *MemoryStorage) Delete(path string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	data, exists := s.files[path]
	if !exists {
		return fmt.Errorf("file not found: %s", path)
	}

	if data.RefCount > 0 {
		return fmt.Errorf("file is still open (refcount: %d)", data.RefCount)
	}

	delete(s.files, path)
	return nil
}

// Exists checks if a file exists at the given path
func (s *MemoryStorage) Exists(path string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()

	_, exists := s.files[path]
	return exists
}

// List returns all file paths in storage
func (s *MemoryStorage) List() []string {
	s.mu.RLock()
	defer s.mu.RUnlock()

	paths := make([]string, 0, len(s.files))
	for path := range s.files {
		paths = append(paths, path)
	}

	return paths
}

// IncRef increments the reference count for a file
func (s *MemoryStorage) IncRef(path string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	data, exists := s.files[path]
	if !exists {
		return fmt.Errorf("file not found: %s", path)
	}

	data.RefCount++
	return nil
}

// DecRef decrements the reference count for a file
func (s *MemoryStorage) DecRef(path string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	data, exists := s.files[path]
	if !exists {
		return fmt.Errorf("file not found: %s", path)
	}

	if data.RefCount > 0 {
		data.RefCount--
	}

	return nil
}

// GetRefCount returns the current reference count for a file
func (s *MemoryStorage) GetRefCount(path string) (int32, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	data, exists := s.files[path]
	if !exists {
		return 0, fmt.Errorf("file not found: %s", path)
	}

	return data.RefCount, nil
}

// CreateSimple creates a new empty file with default metadata
func (s *MemoryStorage) CreateSimple(path string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, exists := s.files[path]; exists {
		return fmt.Errorf("file already exists: %s", path)
	}

	s.files[path] = &FileData{
		Content:  []byte{},
		RefCount: 0,
	}

	return nil
}

// Write writes data to an existing file, creating it if necessary
func (s *MemoryStorage) Write(path string, content []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	data, exists := s.files[path]
	if exists {
		// Update existing file
		data.Content = content
	} else {
		// Create new file
		s.files[path] = &FileData{
			Content:  content,
			RefCount: 0,
		}
	}

	return nil
}
