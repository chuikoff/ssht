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
	mu                sync.RWMutex
	store             *TunnelStore
	running           *RunningTracker
	tunnels           []*Tunnel
	processes         map[string]*ActiveProcess // key: tunnel.ID
	userStopped       map[string]bool           // key: tunnel.ID
	reconnectAttempts map[string]int            // key: tunnel.ID
	stopWatchdog      chan struct{}
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

	running := NewRunningTracker()

	m := &Manager{
		store:             store,
		running:           running,
		tunnels:           tunnels,
		processes:         make(map[string]*ActiveProcess),
		userStopped:       make(map[string]bool),
		reconnectAttempts: make(map[string]int),
		stopWatchdog:      make(chan struct{}),
	}

	// Initial sync with OS running processes
	m.syncRunningStateLocked()

	// Start post-sleep watchdog timer
	go m.runWatchdog()

	return m, nil
}

// syncRunningStateLocked synchronizes in-memory tunnel statuses with OS process table.
func (m *Manager) syncRunningStateLocked() {
	records := m.running.Load()
	for _, t := range m.tunnels {
		if rec, ok := records[t.ID]; ok && isProcessAlive(rec.PID) {
			t.Status = StatusActive
			t.PID = rec.PID
			t.StartedAt = rec.StartedAt
		} else {
			if _, localRunning := m.processes[t.ID]; !localRunning {
				if t.Status == StatusActive {
					t.Status = StatusInactive
					t.PID = 0
				}
			}
		}
	}
}

// Close stops the manager's background watchdog.
func (m *Manager) Close() {
	select {
	case <-m.stopWatchdog:
	default:
		close(m.stopWatchdog)
	}
}

// runWatchdog detects system sleep/resume and monitors tunnel socket health.
func (m *Manager) runWatchdog() {
	ticker := time.NewTicker(4 * time.Second)
	defer ticker.Stop()
	lastTick := time.Now()

	for {
		select {
		case <-m.stopWatchdog:
			return
		case now := <-ticker.C:
			// If tick interval was significantly delayed (> 12 seconds), system was asleep
			if now.Sub(lastTick) > 12*time.Second {
				m.mu.Lock()
				m.syncRunningStateLocked()
				for _, t := range m.tunnels {
					if t.Status == StatusActive && t.Type != TypeRemote {
						if !t.IsPortListening() {
							id := t.ID
							go func(tunID string) {
								_ = m.StopTunnel(tunID)
								time.Sleep(500 * time.Millisecond)
								_ = m.StartTunnel(tunID)
							}(id)
						}
					}
				}
				m.mu.Unlock()
			}
			lastTick = now
		}
	}
}

// GetTunnels returns the list of all tunnels with updated runtime statuses.
func (m *Manager) GetTunnels() []*Tunnel {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.syncRunningStateLocked()

	// Return cloned copies to prevent race conditions during rendering
	copies := make([]*Tunnel, len(m.tunnels))
	for i, t := range m.tunnels {
		cp := *t
		copies[i] = &cp
	}
	return copies
}

// SetTunnels replaces all tunnels and persists them.
func (m *Manager) SetTunnels(tunnels []*Tunnel) error {
	m.mu.Lock()
	defer m.mu.Unlock()

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
			if proc, ok := m.processes[t.ID]; ok && proc.Cmd != nil && proc.Cmd.Process != nil {
				killProcess(proc.Cmd.Process.Pid)
				delete(m.processes, t.ID)
			}
			m.running.Remove(t.ID)
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
		killProcess(proc.Cmd.Process.Pid)
		delete(m.processes, id)
	}
	m.running.Remove(id)

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

	var t *Tunnel
	for _, item := range m.tunnels {
		if item.ID == id {
			t = item
			break
		}
	}
	if t == nil {
		m.mu.Unlock()
		return fmt.Errorf("tunnel not found: %s", id)
	}

	// Check if already active and alive
	if t.Status == StatusActive && t.PID > 0 && isProcessAlive(t.PID) {
		m.mu.Unlock()
		return nil
	}

	// Check if local port is already in use
	if t.Type != TypeRemote && t.IsPortListening() {
		t.Status = StatusFailed
		t.LastError = fmt.Sprintf("Port %d is already in use by another application", t.LocalPort)
		m.mu.Unlock()
		return errors.New(t.LastError)
	}

	t.Status = StatusStarting
	t.LastError = ""

	args := []string{
		"-N",
		"-o", "ExitOnForwardFailure=yes",
		"-o", "ServerAliveInterval=30",
		"-o", "ServerAliveCountMax=3",
		"-o", "BatchMode=yes",
		t.ForwardFlag(), t.ForwardSpec(),
		"--",
		t.Host,
	}

	cmd := exec.Command("ssh", args...)
	var stderrBuf bytes.Buffer
	cmd.Stderr = io.MultiWriter(&stderrBuf)

	if err := cmd.Start(); err != nil {
		t.Status = StatusFailed
		t.LastError = fmt.Sprintf("Failed to spawn ssh: %v", err)
		m.mu.Unlock()
		return err
	}

	pid := cmd.Process.Pid
	t.PID = pid
	t.StartedAt = time.Now()
	m.userStopped[t.ID] = false

	act := &ActiveProcess{
		Cmd:       cmd,
		StderrBuf: &stderrBuf,
	}
	m.processes[t.ID] = act
	m.running.SetRunning(t.ID, pid, t.StartedAt)

	// Goroutine monitoring process exit
	go func(tunID string, proc *ActiveProcess) {
		err := proc.Cmd.Wait()
		m.mu.Lock()
		defer m.mu.Unlock()

		m.running.Remove(tunID)

		var cur *Tunnel
		for _, item := range m.tunnels {
			if item.ID == tunID {
				cur = item
				break
			}
		}

		if m.processes[tunID] == proc {
			delete(m.processes, tunID)
			if cur != nil {
				cur.PID = 0
				stoppedByUser := m.userStopped[tunID]

				if !stoppedByUser && cur.AutoReconnect {
					attempt := m.reconnectAttempts[tunID]
					if attempt >= 10 {
						cur.Status = StatusFailed
						cur.LastError = "Превышен лимит попыток переподключения (10)"
						return
					}

					delaySec := 2 << attempt
					if delaySec > 60 {
						delaySec = 60
					}
					delay := time.Duration(delaySec) * time.Second
					m.reconnectAttempts[tunID]++

					cur.Status = StatusReconnecting
					cur.LastError = fmt.Sprintf("Переподключение #%d через %v...", attempt+1, delay)

					go func(reconnectID string, d time.Duration) {
						time.Sleep(d)
						m.mu.RLock()
						stopped := m.userStopped[reconnectID]
						m.mu.RUnlock()
						if !stopped {
							_ = m.StartTunnel(reconnectID)
						}
					}(tunID, delay)
					return
				}

				if err != nil {
					cur.Status = StatusFailed
					msg := strings.TrimSpace(proc.StderrBuf.String())
					if msg == "" {
						msg = err.Error()
					}
					cur.LastError = msg
				} else {
					cur.Status = StatusInactive
				}
			}
		}
	}(t.ID, act)

	// Release mutex before waiting to avoid deadlock & UI freeze
	m.mu.Unlock()

	// Wait briefly to verify whether process fast-fails
	failed := false
	for i := 0; i < 6; i++ {
		time.Sleep(250 * time.Millisecond)
		if !isProcessAlive(pid) {
			failed = true
			break
		}
		if t.Type != TypeRemote && t.IsPortListening() {
			break
		}
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	if failed || !isProcessAlive(pid) {
		t.Status = StatusFailed
		m.running.Remove(t.ID)
		msg := strings.TrimSpace(stderrBuf.String())
		if msg == "" {
			msg = "SSH процесс завершился с ошибкой (проверьте настройки хоста и ключи)"
		}
		t.LastError = msg
		return errors.New(msg)
	}

	m.reconnectAttempts[t.ID] = 0
	t.Status = StatusActive
	t.LastError = ""
	return nil
}

// StopTunnel stops a running tunnel process.
func (m *Manager) StopTunnel(id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.userStopped[id] = true
	m.reconnectAttempts[id] = 0
	m.running.Remove(id)

	var t *Tunnel
	for _, item := range m.tunnels {
		if item.ID == id {
			t = item
			break
		}
	}

	if proc, ok := m.processes[id]; ok && proc.Cmd != nil && proc.Cmd.Process != nil {
		killProcess(proc.Cmd.Process.Pid)
		delete(m.processes, id)
	}

	if t != nil {
		if t.PID > 0 {
			killProcess(t.PID)
		}
		t.Status = StatusInactive
		t.PID = 0
		t.LastError = ""
	}

	return nil
}

// StopAll stops all active tunnels across this and external processes.
func (m *Manager) StopAll() {
	m.mu.Lock()
	defer m.mu.Unlock()

	for id, proc := range m.processes {
		m.userStopped[id] = true
		m.reconnectAttempts[id] = 0
		if proc.Cmd != nil && proc.Cmd.Process != nil {
			killProcess(proc.Cmd.Process.Pid)
		}
		delete(m.processes, id)
	}

	records := m.running.Load()
	for _, rec := range records {
		if rec.PID > 0 {
			killProcess(rec.PID)
		}
	}
	m.running.Clear()

	for _, t := range m.tunnels {
		if t.PID > 0 {
			killProcess(t.PID)
		}
		t.Status = StatusInactive
		t.PID = 0
		t.LastError = ""
	}
}

// StartAll starts all configured tunnels.
func (m *Manager) StartAll() []error {
	var errs []error
	for _, t := range m.GetTunnels() {
		if err := m.StartTunnel(t.ID); err != nil {
			errs = append(errs, err)
		}
	}
	return errs
}

// StartAutoStart starts tunnels configured with AutoStart.
func (m *Manager) StartAutoStart() []error {
	var errs []error
	for _, t := range m.GetTunnels() {
		if t.AutoStart {
			if err := m.StartTunnel(t.ID); err != nil {
				errs = append(errs, err)
			}
		}
	}
	return errs
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

// GetLog returns recent stderr output for a tunnel without recursive lock deadlock.
func (m *Manager) GetLog(id string) string {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if proc, ok := m.processes[id]; ok && proc.StderrBuf != nil {
		return proc.StderrBuf.String()
	}
	for _, t := range m.tunnels {
		if t.ID == id && t.LastError != "" {
			return t.LastError
		}
	}
	return "No logs available."
}

// ActiveCount returns count of running tunnels.
func (m *Manager) ActiveCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.syncRunningStateLocked()

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
