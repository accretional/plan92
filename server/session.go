package main

import (
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"
)

// Session represents a user session with its own FD table and permissions
type Session struct {
	ID        string
	User      string
	Groups    []string
	FDTable   *FDTable
	CreatedAt time.Time
}

// SessionManager manages active sessions
type SessionManager struct {
	mu       sync.RWMutex
	sessions map[string]*Session
}

// NewSessionManager creates a new session manager
func NewSessionManager() *SessionManager {
	return &SessionManager{
		sessions: make(map[string]*Session),
	}
}

// Create creates a new session
func (sm *SessionManager) Create(user string, groups []string) (*Session, error) {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	sessionID := uuid.New().String()

	session := &Session{
		ID:        sessionID,
		User:      user,
		Groups:    groups,
		FDTable:   NewFDTable(),
		CreatedAt: time.Now(),
	}

	sm.sessions[sessionID] = session

	return session, nil
}

// Get retrieves a session by ID
func (sm *SessionManager) Get(sessionID string) (*Session, error) {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	session, exists := sm.sessions[sessionID]
	if !exists {
		return nil, fmt.Errorf("invalid or expired session: %s", sessionID)
	}

	return session, nil
}

// Close closes a session and cleans up all its resources
func (sm *SessionManager) Close(sessionID string, storage *MemoryStorage) error {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	session, exists := sm.sessions[sessionID]
	if !exists {
		return fmt.Errorf("invalid or expired session: %s", sessionID)
	}

	// Close all open file descriptors and decrement refcounts
	openFDs := session.FDTable.CloseAll()
	for _, fd := range openFDs {
		// Note: We ignore errors here during cleanup
		// Get handle to find path (handle is already removed from table)
		handles := session.FDTable.List()
		for _, handle := range handles {
			if handle.FD == fd {
				_ = storage.DecRef(handle.Path)
				break
			}
		}
	}

	// Remove session from map
	delete(sm.sessions, sessionID)

	return nil
}

// List returns all active session IDs
func (sm *SessionManager) List() []string {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	ids := make([]string, 0, len(sm.sessions))
	for id := range sm.sessions {
		ids = append(ids, id)
	}

	return ids
}

// Count returns the number of active sessions
func (sm *SessionManager) Count() int {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	return len(sm.sessions)
}
