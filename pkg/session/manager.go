package session

import (
	"fmt"
	"strings"
	"sync"
	"time"

	"sshtunnels/pkg/session/vt"
)

// SessionManager manages embedded SSH and shell sessions.
type SessionManager struct {
	sessions    []*Session
	activeIdx   int
	mu          sync.RWMutex
	notifyCh    chan struct{}
	closedCh    chan string
	OnConnected func(target, alias string)
}

// NewSessionManager creates a new session manager instance.
func NewSessionManager() *SessionManager {
	return &SessionManager{
		sessions:  make([]*Session, 0),
		activeIdx: 0,
		notifyCh:  make(chan struct{}, 1),
		closedCh:  make(chan string, 16),
	}
}

// NotifyChan returns the channel signaling when terminal state has been updated.
func (sm *SessionManager) NotifyChan() <-chan struct{} {
	return sm.notifyCh
}

// ClosedChan returns the channel signaling when a session has exited.
func (sm *SessionManager) ClosedChan() <-chan string {
	return sm.closedCh
}

// Notify triggers an update signal without blocking.
func (sm *SessionManager) Notify() {
	select {
	case sm.notifyCh <- struct{}{}:
	default:
	}
}

// StartSession creates, starts, and focuses a new SSH session.
func (sm *SessionManager) StartSession(alias, target string, cols, rows int) (*Session, error) {
	if cols < 20 {
		cols = 80
	}
	if rows < 5 {
		rows = 24
	}

	ptyDev, err := StartPTY("ssh", []string{target}, cols, rows)
	if err != nil {
		return nil, fmt.Errorf("ошибка запуска SSH PTY: %w", err)
	}

	vt := vt.New(vt.WithSize(cols, rows))

	id := fmt.Sprintf("sess_%d", time.Now().UnixNano())
	sess := &Session{
		ID:        id,
		Alias:     alias,
		Target:    target,
		PTY:       ptyDev,
		VT:        vt,
		StartedAt: time.Now(),
		State:     SessionStateConnecting,
	}

	sm.mu.Lock()
	sm.sessions = append(sm.sessions, sess)
	sm.activeIdx = len(sm.sessions) - 1
	sm.mu.Unlock()

	// Goroutine reading from PTY and parsing into Virtual Terminal emulator
	go func() {
		buf := make([]byte, 4096)
		var leftover []byte
		var connectTimer *time.Timer
		var onceConnected sync.Once

		for {
			n, err := sess.PTY.Read(buf)
			if err != nil {
				break
			}
			if n > 0 {
				chunk := buf[:n]
				if len(leftover) > 0 {
					chunk = append(leftover, chunk...)
					leftover = nil
				}

				validBytes, tail := splitIncompleteUTF8(chunk)
				if len(validBytes) > 0 {
					sess.VT.Write(validBytes)
					sm.Notify()
				}
				if len(tail) > 0 {
					leftover = append(leftover, tail...)
				}

				// Connection detection: if still connecting, check if output confirms failure or success
				if sess.GetState() == SessionStateConnecting {
					text := string(chunk)
					isFailure := strings.Contains(text, "Connection timed out") ||
						strings.Contains(text, "Connection refused") ||
						strings.Contains(text, "No route to host") ||
						strings.Contains(text, "Could not resolve hostname") ||
						strings.Contains(text, "Network is unreachable") ||
						strings.Contains(text, "Host key verification failed") ||
						strings.Contains(text, "Permission denied")

					if !isFailure && connectTimer == nil {
						// Delay slightly (300ms) to ensure process doesn't exit immediately on error
						connectTimer = time.AfterFunc(300*time.Millisecond, func() {
							if !sess.IsClosed() {
								onceConnected.Do(func() {
									if sess.SetConnected() {
										sm.Notify()
										if sm.OnConnected != nil {
											sm.OnConnected(sess.Target, sess.Alias)
										}
									}
								})
							}
						})
					}
				}
			}
		}

		if connectTimer != nil {
			connectTimer.Stop()
		}
		_ = sess.Close()
		sm.handleSessionExit(sess.ID)
	}()

	sm.Notify()
	return sess, nil
}

func (sm *SessionManager) handleSessionExit(id string) {
	select {
	case sm.closedCh <- id:
	default:
	}
	sm.Notify()
}

// Count returns the number of open sessions.
func (sm *SessionManager) Count() int {
	sm.mu.RLock()
	defer sm.mu.RUnlock()
	return len(sm.sessions)
}

// List returns a copy of the slice of all current sessions.
func (sm *SessionManager) List() []*Session {
	sm.mu.RLock()
	defer sm.mu.RUnlock()
	res := make([]*Session, len(sm.sessions))
	copy(res, sm.sessions)
	return res
}

// ActiveIndex returns the index of the currently active session.
func (sm *SessionManager) ActiveIndex() int {
	sm.mu.RLock()
	defer sm.mu.RUnlock()
	return sm.activeIdx
}

// GetActive returns the currently active session, or nil if none.
func (sm *SessionManager) GetActive() *Session {
	sm.mu.RLock()
	defer sm.mu.RUnlock()
	if len(sm.sessions) == 0 {
		return nil
	}
	if sm.activeIdx >= len(sm.sessions) {
		sm.activeIdx = len(sm.sessions) - 1
	}
	if sm.activeIdx < 0 {
		sm.activeIdx = 0
	}
	return sm.sessions[sm.activeIdx]
}

// SetActive sets the active session by index.
func (sm *SessionManager) SetActive(idx int) {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	if idx >= 0 && idx < len(sm.sessions) {
		sm.activeIdx = idx
	}
}

// NextTab switches to the next tab.
func (sm *SessionManager) NextTab() {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	if len(sm.sessions) > 1 {
		sm.activeIdx = (sm.activeIdx + 1) % len(sm.sessions)
	}
}

// PrevTab switches to the previous tab.
func (sm *SessionManager) PrevTab() {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	if len(sm.sessions) > 1 {
		sm.activeIdx = (sm.activeIdx - 1 + len(sm.sessions)) % len(sm.sessions)
	}
}

// FindByTarget returns an existing session with the given target or alias if open.
func (sm *SessionManager) FindByTarget(target string) (int, *Session) {
	sm.mu.RLock()
	defer sm.mu.RUnlock()
	for i, s := range sm.sessions {
		if s.Target == target || s.Alias == target {
			return i, s
		}
	}
	return -1, nil
}

// FindByID returns the session with the specified ID or nil if not found.
func (sm *SessionManager) FindByID(id string) *Session {
	sm.mu.RLock()
	defer sm.mu.RUnlock()
	for _, s := range sm.sessions {
		if s.ID == id {
			return s
		}
	}
	return nil
}

// WriteActive sends bytes to the active session's PTY.
func (sm *SessionManager) WriteActive(data []byte) error {
	s := sm.GetActive()
	if s == nil || s.PTY == nil || s.IsClosed() {
		return nil
	}
	// If session is still connecting, typing input indicates interactive session -> mark connected
	if s.GetState() == SessionStateConnecting {
		if s.SetConnected() {
			sm.Notify()
			if sm.OnConnected != nil {
				sm.OnConnected(s.Target, s.Alias)
			}
		}
	}
	_, err := s.PTY.Write(data)
	return err
}

// splitIncompleteUTF8 splits data into complete UTF-8 bytes and any trailing incomplete multibyte UTF-8 bytes.
func splitIncompleteUTF8(b []byte) ([]byte, []byte) {
	n := len(b)
	if n == 0 {
		return b, nil
	}
	// Look back up to 3 bytes from the end for a multibyte start byte (>= 0xC0)
	for i := 1; i <= 3 && i <= n; i++ {
		lead := b[n-i]
		if lead >= 0xC0 {
			var expectedLen int
			if lead >= 0xF0 {
				expectedLen = 4
			} else if lead >= 0xE0 {
				expectedLen = 3
			} else {
				expectedLen = 2
			}
			if i < expectedLen {
				// Incomplete UTF-8 sequence at end of buffer
				return b[:n-i], b[n-i:]
			}
			break
		}
	}
	return b, nil
}

// ResizeAll resizes all sessions' PTY and VT emulators.
func (sm *SessionManager) ResizeAll(cols, rows int) {
	if cols < 20 {
		cols = 20
	}
	if rows < 5 {
		rows = 5
	}
	sm.mu.RLock()
	defer sm.mu.RUnlock()
	for _, s := range sm.sessions {
		if s.VT != nil {
			s.VT.Resize(cols, rows)
		}
		if s.PTY != nil && !s.IsClosed() {
			_ = s.PTY.Resize(cols, rows)
		}
	}
}

// Close removes and terminates the session with the specified ID.
func (sm *SessionManager) Close(id string) {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	idx := -1
	for i, s := range sm.sessions {
		if s.ID == id {
			idx = i
			_ = s.Close()
			break
		}
	}

	if idx != -1 {
		sm.sessions = append(sm.sessions[:idx], sm.sessions[idx+1:]...)
		if sm.activeIdx >= len(sm.sessions) {
			sm.activeIdx = len(sm.sessions) - 1
		}
		if sm.activeIdx < 0 {
			sm.activeIdx = 0
		}
	}
	sm.Notify()
}

// CloseActive terminates the currently active session.
func (sm *SessionManager) CloseActive() {
	s := sm.GetActive()
	if s != nil {
		sm.Close(s.ID)
	}
}

// CloseAll terminates all active sessions.
func (sm *SessionManager) CloseAll() {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	for _, s := range sm.sessions {
		_ = s.Close()
	}
	sm.sessions = sm.sessions[:0]
	sm.activeIdx = 0
	sm.Notify()
}

// InjectMockSessionForTest adds a dummy session with virtual terminal for tests.
func (sm *SessionManager) InjectMockSessionForTest(id, alias, target string) *Session {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	s := &Session{
		ID:     id,
		Alias:  alias,
		Target: target,
		State:  SessionStateConnected,
		VT:     vt.New(vt.WithSize(80, 24)),
	}
	sm.sessions = append(sm.sessions, s)
	return s
}
