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

	// Goroutine waiting for underlying process to exit
	go func() {
		if sess.PTY != nil {
			exitCode, _ := sess.PTY.Wait()
			sess.mu.Lock()
			sess.ExitCode = exitCode
			sess.mu.Unlock()
		}
		_ = sess.Close()
		sm.handleSessionExit(sess.ID)
	}()

	// Goroutine reading from PTY and parsing into Virtual Terminal emulator
	go func() {
		buf := make([]byte, 4096)
		var leftover []byte
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

				// Connection detection: only transition to Connected when VT has visible non-error content
				if sess.GetState() == SessionStateConnecting {
					if hasSuccessfulConnection(sess.VT) {
						onceConnected.Do(func() {
							if sess.SetConnected() {
								sm.Notify()
								if sm.OnConnected != nil {
									sm.OnConnected(sess.Target, sess.Alias)
								}
							}
						})
					}
				}
			}
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

// hasSuccessfulConnection returns true if the virtual terminal contains visible text
// that is NOT an SSH connection error message.
func hasSuccessfulConnection(term vt.Terminal) bool {
	if term == nil {
		return false
	}
	term.Lock()
	defer term.Unlock()

	cols, rows := term.Size()
	var sb strings.Builder
	hasNonSpace := false

	for y := 0; y < rows; y++ {
		lineHasChar := false
		for x := 0; x < cols; x++ {
			ch := term.Cell(x, y).Char
			if ch != 0 && ch != ' ' {
				hasNonSpace = true
				lineHasChar = true
			}
			if lineHasChar {
				if ch == 0 {
					sb.WriteRune(' ')
				} else {
					sb.WriteRune(ch)
				}
			}
		}
		if lineHasChar {
			sb.WriteRune('\n')
		}
	}

	if !hasNonSpace {
		return false
	}

	text := strings.ToLower(sb.String())
	if strings.Contains(text, "connection timed out") ||
		strings.Contains(text, "connection refused") ||
		strings.Contains(text, "no route to host") ||
		strings.Contains(text, "could not resolve hostname") ||
		strings.Contains(text, "network is unreachable") ||
		strings.Contains(text, "host key verification failed") ||
		strings.Contains(text, "permission denied") {
		return false
	}

	return true
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
