package server

import (
	"context"
	"encoding/json"
	"sync"
	"time"
)

// State represents the power/lifecycle state of a game server.
type State string

const (
	StateOffline       State = "offline"
	StateInstalling    State = "installing"
	StateInstallFailed State = "install_failed"
	StateStarting      State = "starting"
	StateRunning       State = "running"
	StateStopping      State = "stopping"
	StateCrashed       State = "crashed"
)

// CreateRequest is sent by the billing panel when provisioning a new server.
type CreateRequest struct {
	UUID           string            `json:"uuid"`
	Name           string            `json:"name"`
	DockerImage    string            `json:"docker_image"`
	StartupCommand string            `json:"startup_command"`
	Environment    map[string]string `json:"environment"`
	Limits         Limits            `json:"limits"`
	Ports          []PortMapping     `json:"ports"`
	Install        *InstallConfig    `json:"install,omitempty"`
}

// Limits defines the resource constraints for a server.
type Limits struct {
	MemoryMB   int `json:"memory_mb"`
	DiskMB     int `json:"disk_mb"`
	CPUPercent int `json:"cpu_percent"`
	SwapMB     int `json:"swap_mb"`
}

// PortMapping describes a port exposed on the host.
type PortMapping struct {
	IP       string `json:"ip"`
	Port     int    `json:"port"`
	Protocol string `json:"protocol"` // "tcp" or "udp"
}

// InstallConfig describes the installer container to run once before the server starts.
type InstallConfig struct {
	Script      string `json:"script"`
	DockerImage string `json:"docker_image"`
	Entrypoint  string `json:"entrypoint"`
}

// Resources holds real-time resource usage read from Docker stats.
type Resources struct {
	MemoryBytes    uint64  `json:"memory_bytes"`
	MemoryLimit    uint64  `json:"memory_limit"`
	CPUAbsolute    float64 `json:"cpu_absolute"`
	DiskBytes      uint64  `json:"disk_bytes"`
	NetworkRxBytes uint64  `json:"network_rx_bytes"`
	NetworkTxBytes uint64  `json:"network_tx_bytes"`
	Uptime         int64   `json:"uptime"`
}

// Server represents a single managed game server.
type Server struct {
	mu sync.RWMutex

	UUID           string
	Name           string
	DockerImage    string
	StartupCommand string
	Environment    map[string]string
	Limits         Limits
	Ports          []PortMapping
	Install        *InstallConfig

	containerID string
	state       State
	resources   Resources
	startedAt   *time.Time

	// Console line broadcast
	consoleMu        sync.Mutex
	consoleListeners []chan string

	// Background goroutine lifecycle
	ctx    context.Context
	cancel context.CancelFunc
}

// New creates a new Server instance from a CreateRequest.
func New(req CreateRequest) *Server {
	ctx, cancel := context.WithCancel(context.Background())
	return &Server{
		UUID:           req.UUID,
		Name:           req.Name,
		DockerImage:    req.DockerImage,
		StartupCommand: req.StartupCommand,
		Environment:    req.Environment,
		Limits:         req.Limits,
		Ports:          req.Ports,
		Install:        req.Install,
		state:          StateOffline,
		ctx:            ctx,
		cancel:         cancel,
	}
}

func (s *Server) Context() context.Context { return s.ctx }
func (s *Server) Cancel()                  { s.cancel() }

func (s *Server) GetState() State {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.state
}

func (s *Server) SetState(state State) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.state = state
}

func (s *Server) GetResources() Resources {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.resources
}

func (s *Server) SetResources(r Resources) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.resources = r
}

func (s *Server) GetContainerID() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.containerID
}

func (s *Server) SetContainerID(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.containerID = id
}

// AddConsoleListener returns a buffered channel that receives console output lines.
func (s *Server) AddConsoleListener() chan string {
	ch := make(chan string, 256)
	s.consoleMu.Lock()
	s.consoleListeners = append(s.consoleListeners, ch)
	s.consoleMu.Unlock()
	return ch
}

// RemoveConsoleListener removes and closes a console listener channel.
func (s *Server) RemoveConsoleListener(ch chan string) {
	s.consoleMu.Lock()
	defer s.consoleMu.Unlock()
	for i, l := range s.consoleListeners {
		if l == ch {
			s.consoleListeners = append(s.consoleListeners[:i], s.consoleListeners[i+1:]...)
			close(ch)
			return
		}
	}
}

// BroadcastConsole sends a line to all active console listeners (non-blocking).
func (s *Server) BroadcastConsole(line string) {
	s.consoleMu.Lock()
	defer s.consoleMu.Unlock()
	for _, ch := range s.consoleListeners {
		select {
		case ch <- line:
		default:
		}
	}
}

// ToMap returns a map suitable for JSON serialisation in API responses.
func (s *Server) ToMap() map[string]interface{} {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return map[string]interface{}{
		"uuid":            s.UUID,
		"name":            s.Name,
		"status":          string(s.state),
		"docker_image":    s.DockerImage,
		"startup_command": s.StartupCommand,
		"limits":          s.Limits,
		"ports":           s.Ports,
	}
}

func (s *Server) MarshalJSON() ([]byte, error) {
	return json.Marshal(s.ToMap())
}
