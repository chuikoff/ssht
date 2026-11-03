package session

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/hinshun/vt10x"
)

func TestKeyToBytes(t *testing.T) {
	tests := []struct {
		name     string
		msg      tea.KeyMsg
		expected []byte
	}{
		{
			name:     "Enter key",
			msg:      tea.KeyMsg{Type: tea.KeyEnter},
			expected: []byte{'\r'},
		},
		{
			name:     "Ctrl+C",
			msg:      tea.KeyMsg{Type: tea.KeyCtrlC},
			expected: []byte{0x03},
		},
		{
			name:     "Rune key",
			msg:      tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'a'}},
			expected: []byte{'a'},
		},
		{
			name:     "Alt+Rune key",
			msg:      tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'x'}, Alt: true},
			expected: []byte{0x1b, 'x'},
		},
		{
			name:     "Up arrow",
			msg:      tea.KeyMsg{Type: tea.KeyUp},
			expected: []byte("\x1b[A"),
		},
		{
			name:     "Down arrow",
			msg:      tea.KeyMsg{Type: tea.KeyDown},
			expected: []byte("\x1b[B"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := KeyToBytes(tt.msg)
			if string(got) != string(tt.expected) {
				t.Errorf("KeyToBytes() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestRenderVT(t *testing.T) {
	vt := vt10x.New(vt10x.WithSize(40, 5))
	vt.Write([]byte("\033[31mRed Alert\033[0m\r\nLine 2"))

	rendered := RenderVT(vt)
	if !strings.Contains(rendered, "Red Alert") {
		t.Errorf("RenderVT missing 'Red Alert', got: %s", rendered)
	}
	if !strings.Contains(rendered, "Line 2") {
		t.Errorf("RenderVT missing 'Line 2', got: %s", rendered)
	}
}

func TestSessionManagerBasic(t *testing.T) {
	sm := NewSessionManager()
	if sm.Count() != 0 {
		t.Fatalf("expected 0 sessions, got %d", sm.Count())
	}
	if sm.GetActive() != nil {
		t.Fatalf("expected nil active session")
	}

	// Manually inject mock sessions
	s1 := &Session{ID: "1", Alias: "Host1", Target: "h1"}
	s2 := &Session{ID: "2", Alias: "Host2", Target: "h2"}

	sm.sessions = append(sm.sessions, s1, s2)
	sm.activeIdx = 0

	if sm.Count() != 2 {
		t.Fatalf("expected 2 sessions, got %d", sm.Count())
	}
	if sm.GetActive().Alias != "Host1" {
		t.Fatalf("expected Host1 active, got %s", sm.GetActive().Alias)
	}

	sm.NextTab()
	if sm.GetActive().Alias != "Host2" {
		t.Fatalf("expected Host2 active after NextTab, got %s", sm.GetActive().Alias)
	}

	sm.PrevTab()
	if sm.GetActive().Alias != "Host1" {
		t.Fatalf("expected Host1 active after PrevTab, got %s", sm.GetActive().Alias)
	}

	sm.Close("1")
	if sm.Count() != 1 {
		t.Fatalf("expected 1 session after close, got %d", sm.Count())
	}
	if sm.GetActive().Alias != "Host2" {
		t.Fatalf("expected Host2 active after Host1 closed")
	}
}
