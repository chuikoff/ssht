package tunnel

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"
)

// ActiveProcess holds the running command and captured logs.
type ActiveProcess struct {
	Cmd       *exec.Cmd
	StderrBuf *bytes.Buffer
	LogLines  []string
	mu        sync.Mutex
}

// Manager orchestrates tunnel lifecycle and persistence.
type Manager struct {
	mu        sync.RWMutex
	store     *TunnelStore
	tunnels   []*Tunnel
	processes map[string]*ActiveProcess // key: tunnel.ID
}

// NewManager creates a manager and loads existing tunnel configs.
func NewManager() (*Manager, error) {
	store, err := NewTunnelStore()
	if err != nil {
		return nil, err
	}
	return NewManagerWithStore(store)
}

// NewManagerWithStore creates a manager using an injected TunnelStore.
func NewManagerWithStore(store *TunnelStore) (*Manager, error) {
	tunnels, err := store.Load()
	if err != nil {
		return nil, err
	}

	m := &Manager{
		store:     store,
		tunnels:   tunnels,
		processes: make(map[string]*ActiveProcess),
	}

	return m, nil
}

// GetTunnels returns the list of all tunnels.
func (m *Manager) GetTunnels() []*Tunnel {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.tunnels
}

// SetTunnels replaces all tunnels and persists them.
func (m *Manager) SetTunnels(tunnels []*Tunnel) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	// De-duplicate just in case
	var unique []*Tunnel
	seen := make(map[string]bool)
	for _, t := range tunnels {
		if t.ID != "" && seen[t.ID] {
			continue
		}
		if t.ID != "" {
			seen[t.ID] = true
		}
		t.Status = StatusInactive
		unique = append(unique, t)
	}

	m.tunnels = unique
	return m.store.Save(m.tunnels)
}

// FindTunnel finds a tunnel by ID.
func (m *Manager) FindTunnel(id string) *Tunnel {
	m.mu.RLock()
	defer m.mu.RUnlock()
	for _, t := range m.tunnels {
		if t.ID == id {
			return t
		}
	}
	return nil
}

// AddTunnel adds a new tunnel (or updates if ID already exists) and persists it.
func (m *Manager) AddTunnel(t *Tunnel) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if t.ID == "" {
		t.ID = GenerateID()
	}
	t.Status = StatusInactive

	for i, existing := range m.tunnels {
		if existing.ID == t.ID {
			m.tunnels[i] = t
			return m.store.Save(m.tunnels)
		}
	}

	m.tunnels = append(m.tunnels, t)
	return m.store.Save(m.tunnels)
}

// UpdateTunnel updates an existing tunnel.
func (m *Manager) UpdateTunnel(t *Tunnel) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	for i, existing := range m.tunnels {
		if existing.ID == t.ID {
			// If active, stop it before updating
			if proc, ok := m.processes[t.ID]; ok && proc.Cmd != nil && proc.Cmd.Process != nil {
				_ = proc.Cmd.Process.Kill()
				delete(m.processes, t.ID)
			}
			t.Status = StatusInactive
			m.tunnels[i] = t
			return m.store.Save(m.tunnels)
		}
	}
	return fmt.Errorf("tunnel not found: %s", t.ID)
}

// DeleteTunnel removes a tunnel and stops it if running.
func (m *Manager) DeleteTunnel(id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if proc, ok := m.processes[id]; ok && proc.Cmd != nil && proc.Cmd.Process != nil {
		_ = proc.Cmd.Process.Kill()
		delete(m.processes, id)
	}

	var filtered []*Tunnel
	for _, t := range m.tunnels {
		if t.ID != id {
			filtered = append(filtered, t)
		}
	}
	m.tunnels = filtered
	return m.store.Save(m.tunnels)
}

// StartTunnel launches the SSH tunnel process.
func (m *Manager) StartTunnel(id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	var t *Tunnel
	for _, item := range m.tunnels {
		if item.ID == id {
			t = item
			break
		}
	}
	if t == nil {
		return fmt.Errorf("tunnel not found: %s", id)
	}

	if t.Status == StatusActive {
		return nil // already running
	}

	// Check if local port is already in use by another program
	if t.Type != TypeRemote && t.IsPortListening() {
		t.Status = StatusFailed
		t.LastError = fmt.Sprintf("Port %d is already in use by another application", t.LocalPort)
		return errors.New(t.LastError)
	}

	t.Status = StatusStarting
	t.LastError = ""

	args := []string{
		"-N", // Do not execute remote command
		"-o", "ExitOnForwardFailure=yes",
		"-o", "ServerAliveInterval=30",
		"-o", "ServerAliveCountMax=3",
		"-o", "BatchMode=yes", // Prevent hanging on interactive password prompt in background
		t.ForwardFlag(), t.ForwardSpec(),
		t.Host,
	}

	cmd := exec.Command("ssh", args...)
	var stderrBuf bytes.Buffer
	cmd.Stderr = io.MultiWriter(&stderrBuf)

	if err := cmd.Start(); err != nil {
		t.Status = StatusFailed
		t.LastError = fmt.Sprintf("Failed to spawn ssh: %v", err)
		return err
	}

	t.PID = cmd.Process.Pid
	t.StartedAt = time.Now()

	act := &ActiveProcess{
		Cmd:       cmd,
		StderrBuf: &stderrBuf,
	}
	m.processes[t.ID] = act

	// Monitor background exit
	go func(tun *Tunnel, proc *ActiveProcess) {
		err := proc.Cmd.Wait()
		m.mu.Lock()
		defer m.mu.Unlock()

		if m.processes[tun.ID] == proc {
			delete(m.processes, tun.ID)
			tun.PID = 0
			if err != nil {
				tun.Status = StatusFailed
				msg := strings.TrimSpace(proc.StderrBuf.String())
				if msg == "" {
					msg = err.Error()
				}
				tun.LastError = msg
			} else {
				tun.Status = StatusInactive
			}
		}
	}(t, act)

	// Wait briefly (350ms) to detect fast fail (e.g. auth failed, port bind error)
	time.Sleep(350 * time.Millisecond)
	if t.Status == StatusFailed {
		return errors.New(t.LastError)
	}

	// If local/dynamic, verify port opens up within 1.5 seconds
	if t.Type != TypeRemote {
		for i := 0; i < 6; i++ {
			if t.IsPortListening() {
				t.Status = StatusActive
				t.LastError = ""
				return nil
			}
			time.Sleep(200 * time.Millisecond)
		}
	}

	// If remote or process is running fine:
	t.Status = StatusActive
	return nil
}

// StopTunnel stops a running tunnel process.
func (m *Manager) StopTunnel(id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	var t *Tunnel
	for _, item := range m.tunnels {
		if item.ID == id {
			t = item
			break
		}
	}

	proc, ok := m.processes[id]
	if ok && proc.Cmd != nil && proc.Cmd.Process != nil {
		killProcess(proc.Cmd.Process.Pid)
		delete(m.processes, id)
	}

	if t != nil {
		t.Status = StatusInactive
		t.PID = 0
		t.LastError = ""
	}

	return nil
}

// StopAll stops all active tunnels.
func (m *Manager) StopAll() {
	m.mu.Lock()
	defer m.mu.Unlock()

	for id, proc := range m.processes {
		if proc.Cmd != nil && proc.Cmd.Process != nil {
			killProcess(proc.Cmd.Process.Pid)
		}
		delete(m.processes, id)
	}

	for _, t := range m.tunnels {
		t.Status = StatusInactive
		t.PID = 0
	}
}

// ToggleTunnel switches tunnel between active and inactive.
func (m *Manager) ToggleTunnel(id string) error {
	t := m.FindTunnel(id)
	if t == nil {
		return fmt.Errorf("tunnel not found: %s", id)
	}
	if t.Status == StatusActive || t.Status == StatusStarting {
		return m.StopTunnel(id)
	}
	return m.StartTunnel(id)
}

// GetLog returns recent stderr output for a tunnel.
func (m *Manager) GetLog(id string) string {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if proc, ok := m.processes[id]; ok && proc.StderrBuf != nil {
		return proc.StderrBuf.String()
	}
	t := m.FindTunnel(id)
	if t != nil && t.LastError != "" {
		return t.LastError
	}
	return "No logs available."
}

// ActiveCount returns count of running tunnels.
func (m *Manager) ActiveCount() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	count := 0
	for _, t := range m.tunnels {
		if t.Status == StatusActive {
			count++
		}
	}
	return count
}

func killProcess(pid int) {
	if pid <= 0 {
		return
	}
	if runtime.GOOS == "windows" {
		_ = exec.Command("taskkill", "/F", "/T", "/PID", strconv.Itoa(pid)).Run()
	} else {
		proc, err := os.FindProcess(pid)
		if err == nil {
			_ = proc.Kill()
		}
	}
}
