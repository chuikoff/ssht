package session

import (
	"io"
	"sync"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"sshtunnels/pkg/session/vt"
)

// PTYDevice defines the interface for an OS pseudoconsole device.
type PTYDevice interface {
	io.ReadWriteCloser
	Resize(cols, rows int) error
	Wait() (int, error)
}

// SessionState represents the connection lifecycle state of an embedded SSH session.
type SessionState int

const (
	SessionStateConnecting SessionState = iota // 🟡 Подключение...
	SessionStateConnected                      // 🟢 В сети
	SessionStateClosed                         // 🔴 Отключен
)

// Session represents a single embedded SSH or shell session.
type Session struct {
	ID           string
	Alias        string
	Target       string
	PTY          PTYDevice
	VT           vt.Terminal
	ScrollOffset int
	StartedAt    time.Time
	ConnectedAt  time.Time
	ExitCode     int
	State             SessionState
	Closed            bool
	connectedReported bool
	mu                sync.Mutex
}

// Close terminates the session and its underlying PTY device.
func (s *Session) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.Closed {
		return nil
	}
	s.Closed = true
	s.State = SessionStateClosed
	if s.PTY != nil {
		return s.PTY.Close()
	}
	return nil
}

// IsClosed checks if session is closed.
func (s *Session) IsClosed() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.Closed || s.State == SessionStateClosed
}

// IsConnected checks if session is alive and connected.
func (s *Session) IsConnected() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return !s.Closed && s.State == SessionStateConnected
}

// GetState returns the current SessionState thread-safely.
func (s *Session) GetState() SessionState {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.State
}

// SetConnected marks the session as successfully established. Returns true if state changed.
func (s *Session) SetConnected() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.Closed || s.State == SessionStateConnected {
		return false
	}
	s.State = SessionStateConnected
	s.ConnectedAt = time.Now()
	return true
}

// ReportConnected returns true the first time it is called after the session enters Connected state.
func (s *Session) ReportConnected() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.State == SessionStateConnected && !s.connectedReported {
		s.connectedReported = true
		return true
	}
	return false
}

// StatusIcon returns a visual status badge icon ("🟡", "🟢", "🔴").
func (s *Session) StatusIcon() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	switch s.State {
	case SessionStateConnected:
		return "🟢"
	case SessionStateClosed:
		return "🔴"
	default:
		return "🟡"
	}
}

// StatusText returns a human-readable status description.
func (s *Session) StatusText() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	switch s.State {
	case SessionStateConnected:
		return "🟢 В сети"
	case SessionStateClosed:
		return "🔴 Отключен"
	default:
		return "🟡 Подключение..."
	}
}

// KeyToBytes converts a Bubble Tea KeyMsg to raw bytes suitable for PTY input.
func KeyToBytes(k tea.KeyMsg) []byte {
	// Standard ASCII control keys (0..31 and 127)
	if k.Type >= tea.KeyNull && k.Type <= tea.KeyCtrlQuestionMark {
		b := byte(k.Type)
		if k.Type == tea.KeyEnter {
			b = '\r'
		}
		if k.Alt {
			return []byte{0x1b, b}
		}
		return []byte{b}
	}

	switch k.Type {
	case tea.KeyRunes:
		b := []byte(string(k.Runes))
		if k.Alt {
			return append([]byte{0x1b}, b...)
		}
		return b
	case tea.KeySpace:
		if k.Alt {
			return []byte{0x1b, ' '}
		}
		return []byte{' '}
	case tea.KeyUp:
		return []byte("\x1b[A")
	case tea.KeyDown:
		return []byte("\x1b[B")
	case tea.KeyRight:
		return []byte("\x1b[C")
	case tea.KeyLeft:
		return []byte("\x1b[D")
	case tea.KeyHome:
		return []byte("\x1b[H")
	case tea.KeyEnd:
		return []byte("\x1b[F")
	case tea.KeyPgUp:
		return []byte("\x1b[5~")
	case tea.KeyPgDown:
		return []byte("\x1b[6~")
	case tea.KeyDelete:
		return []byte("\x1b[3~")
	case tea.KeyInsert:
		return []byte("\x1b[2~")
	case tea.KeyShiftTab:
		return []byte("\x1b[Z")
	case tea.KeyF1:
		return []byte("\x1bOP")
	case tea.KeyF2:
		return []byte("\x1bOQ")
	case tea.KeyF3:
		return []byte("\x1bOR")
	case tea.KeyF4:
		return []byte("\x1bOS")
	case tea.KeyF5:
		return []byte("\x1b[15~")
	case tea.KeyF6:
		return []byte("\x1b[17~")
	case tea.KeyF7:
		return []byte("\x1b[18~")
	case tea.KeyF8:
		return []byte("\x1b[19~")
	case tea.KeyF9:
		return []byte("\x1b[20~")
	case tea.KeyF10:
		return []byte("\x1b[21~")
	case tea.KeyF11:
		return []byte("\x1b[23~")
	case tea.KeyF12:
		return []byte("\x1b[24~")
	default:
		if len(k.Runes) > 0 {
			b := []byte(string(k.Runes))
			if k.Alt {
				return append([]byte{0x1b}, b...)
			}
			return b
		}
		return nil
	}
}
