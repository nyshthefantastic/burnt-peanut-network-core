package transfer

import (
	"fmt"
	"sync"
)

type SessionManager struct {
	sessions map[string]*TransferSession
	mu       sync.Mutex
	maxConc  int
}

func NewSessionManager(maxConcurrent int) *SessionManager {
	if maxConcurrent <= 0 {
		maxConcurrent = 1
	}

	return &SessionManager{
		sessions: make(map[string]*TransferSession),
		maxConc:  maxConcurrent,
	}
}

func (m *SessionManager) Add(s *TransferSession) error {
	if s == nil {
		return fmt.Errorf("session is nil")
	}
	if s.ID == "" {
		return fmt.Errorf("session id is empty")
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	if _, exists := m.sessions[s.ID]; exists {
		return fmt.Errorf("session already exists: %s", s.ID)
	}
	if len(m.sessions) >= m.maxConc {
		return fmt.Errorf("max concurrent sessions reached: %d", m.maxConc)
	}

	m.sessions[s.ID] = s
	return nil
}

func (m *SessionManager) Get(id string) (*TransferSession, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()

	s, ok := m.sessions[id]
	return s, ok
}

func (m *SessionManager) GetByPeer(peerID string) (*TransferSession, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()

	for _, s := range m.sessions {
		if s != nil && s.PeerID == peerID {
			return s, true
		}
	}

	return nil, false
}

func (m *SessionManager) Remove(id string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	delete(m.sessions, id)
}

func (m *SessionManager) ActiveCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()

	return len(m.sessions)
}
