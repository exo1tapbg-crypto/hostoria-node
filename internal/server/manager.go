package server

import (
	"fmt"
	"sync"
)

// Manager maintains the in-memory registry of all servers on this node.
type Manager struct {
	mu      sync.RWMutex
	servers map[string]*Server
}

// NewManager creates an empty Manager.
func NewManager() *Manager {
	return &Manager{servers: make(map[string]*Server)}
}

// Add registers a server. Returns an error if a server with the same UUID already exists.
func (m *Manager) Add(s *Server) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, exists := m.servers[s.UUID]; exists {
		return fmt.Errorf("server %s already exists", s.UUID)
	}
	m.servers[s.UUID] = s
	return nil
}

// Get returns the server with the given UUID, or nil if not found.
func (m *Manager) Get(uuid string) *Server {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.servers[uuid]
}

// Remove stops and removes a server from the registry.
func (m *Manager) Remove(uuid string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if s, ok := m.servers[uuid]; ok {
		s.Cancel()
		delete(m.servers, uuid)
	}
}

// All returns a copy of all registered servers.
func (m *Manager) All() []*Server {
	m.mu.RLock()
	defer m.mu.RUnlock()
	list := make([]*Server, 0, len(m.servers))
	for _, s := range m.servers {
		list = append(list, s)
	}
	return list
}

// Len returns the number of registered servers.
func (m *Manager) Len() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.servers)
}
