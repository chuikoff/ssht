package tunnel

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// TunnelType defines the forwarding mode.
type TunnelType string

const (
	TypeLocal   TunnelType = "local"   // -L [bind:]local_port:remote_host:remote_port
	TypeRemote  TunnelType = "remote"  // -R [bind:]remote_port:local_host:local_port
	TypeDynamic TunnelType = "dynamic" // -D [bind:]local_port (SOCKS5)
)

// TunnelStatus represents current state of a tunnel.
type TunnelStatus string

const (
	StatusInactive TunnelStatus = "INACTIVE"
	StatusStarting TunnelStatus = "STARTING"
	StatusActive   TunnelStatus = "ACTIVE"
	StatusFailed   TunnelStatus = "FAILED"
)

// Tunnel represents an SSH tunnel configuration and state.
type Tunnel struct {
	ID          string     `json:"id"`
	Name        string     `json:"name"`
	Type        TunnelType `json:"type"`        // "local", "remote", "dynamic"
	Host        string     `json:"host"`        // SSH host alias from ~/.ssh/config or user@ip
	BindAddress string     `json:"bind_address"`// Optional bind address, default 127.0.0.1
	LocalPort   int        `json:"local_port"`
	RemoteHost  string     `json:"remote_host"` // Destination host for local forward (e.g. localhost)
	RemotePort  int        `json:"remote_port"` // Destination port
	AutoStart   bool       `json:"auto_start"`  // Start automatically when manager opens
	Description string     `json:"description"`

	// Runtime fields (not persisted)
	Status    TunnelStatus `json:"-"`
	PID       int          `json:"-"`
	LastError string       `json:"-"`
	StartedAt time.Time    `json:"-"`
}

// GenerateID produces a short random identifier.
func GenerateID() string {
	b := make([]byte, 4)
	rand.Read(b)
	return hex.EncodeToString(b)
}

// Summary returns a human-readable representation of what the tunnel forwards.
func (t *Tunnel) Summary() string {
	bind := t.BindAddress
	if bind == "" {
		bind = "127.0.0.1"
	}
	switch t.Type {
	case TypeLocal:
		remote := t.RemoteHost
		if remote == "" {
			remote = "127.0.0.1"
		}
		return fmt.Sprintf("%s:%d -> %s:%d via %s", bind, t.LocalPort, remote, t.RemotePort, t.Host)
	case TypeRemote:
		remote := t.RemoteHost
		if remote == "" {
			remote = "127.0.0.1"
		}
		return fmt.Sprintf("Remote:%d -> %s:%d via %s", t.RemotePort, remote, t.LocalPort, t.Host)
	case TypeDynamic:
		return fmt.Sprintf("SOCKS5 %s:%d via %s", bind, t.LocalPort, t.Host)
	default:
		return fmt.Sprintf("%s:%d", bind, t.LocalPort)
	}
}

// ForwardFlag returns the ssh argument flag (e.g. "-L", "-R", "-D").
func (t *Tunnel) ForwardFlag() string {
	switch t.Type {
	case TypeRemote:
		return "-R"
	case TypeDynamic:
		return "-D"
	default:
		return "-L"
	}
}

// ForwardSpec returns the parameter for the forward flag.
func (t *Tunnel) ForwardSpec() string {
	bind := t.BindAddress
	if bind == "" {
		bind = "127.0.0.1"
	}
	switch t.Type {
	case TypeLocal:
		remote := t.RemoteHost
		if remote == "" {
			remote = "127.0.0.1"
		}
		return fmt.Sprintf("%s:%d:%s:%d", bind, t.LocalPort, remote, t.RemotePort)
	case TypeRemote:
		local := t.RemoteHost
		if local == "" {
			local = "127.0.0.1"
		}
		return fmt.Sprintf("%d:%s:%d", t.RemotePort, local, t.LocalPort)
	case TypeDynamic:
		return fmt.Sprintf("%s:%d", bind, t.LocalPort)
	default:
		return fmt.Sprintf("%s:%d", bind, t.LocalPort)
	}
}

// IsPortListening checks if the local port is accepting connections.
func (t *Tunnel) IsPortListening() bool {
	if t.LocalPort <= 0 {
		return false
	}
	bind := t.BindAddress
	if bind == "" {
		bind = "127.0.0.1"
	}
	addr := fmt.Sprintf("%s:%d", bind, t.LocalPort)
	conn, err := net.DialTimeout("tcp", addr, 200*time.Millisecond)
	if err != nil {
		return false
	}
	conn.Close()
	return true
}

// TunnelStore handles persistence of tunnel configurations.
type TunnelStore struct {
	mu       sync.RWMutex
	filePath string
}

func NewTunnelStore() (*TunnelStore, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	sshDir := filepath.Join(home, ".ssh")
	if err := os.MkdirAll(sshDir, 0700); err != nil {
		return nil, err
	}
	path := filepath.Join(sshDir, "ssht_tunnels.json")
	return &TunnelStore{filePath: path}, nil
}

// NewTunnelStoreWithPath creates a store with a specified file path.
func NewTunnelStoreWithPath(path string) *TunnelStore {
	return &TunnelStore{filePath: path}
}

func (s *TunnelStore) Load() ([]*Tunnel, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if _, err := os.Stat(s.filePath); os.IsNotExist(err) {
		return []*Tunnel{}, nil
	}

	data, err := os.ReadFile(s.filePath)
	if err != nil {
		return nil, err
	}

	var tunnels []*Tunnel
	if err := json.Unmarshal(data, &tunnels); err != nil {
		return nil, err
	}

	var unique []*Tunnel
	seen := make(map[string]bool)
	hasDuplicates := false

	for _, t := range tunnels {
		if t.ID != "" && seen[t.ID] {
			hasDuplicates = true
			continue
		}
		if t.ID != "" {
			seen[t.ID] = true
		}
		t.Status = StatusInactive
		unique = append(unique, t)
	}

	if hasDuplicates {
		go func(clean []*Tunnel) {
			_ = s.Save(clean)
		}(unique)
	}

	return unique, nil
}

func (s *TunnelStore) Save(tunnels []*Tunnel) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	data, err := json.MarshalIndent(tunnels, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(s.filePath, data, 0600)
}
