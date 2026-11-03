package session

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"sshtunnels/pkg/session/vt"
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
	term := vt.New(vt.WithSize(40, 5))
	term.Write([]byte("\033[31mRed Alert\033[0m\r\nLine 2"))

	rendered := RenderVT(term)
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

func TestSelectionAndExtraction(t *testing.T) {
	term := vt.New(vt.WithSize(40, 5))
	term.Write([]byte("Line 1: Hello World from SSH\r\nLine 2: Server running on Ubuntu"))

	sel := SelectionRange{
		Active: true,
		StartX: 8,
		StartY: 0,
		EndX:   18,
		EndY:   0,
	}

	if !sel.IsSelected(8, 0) || !sel.IsSelected(15, 0) || !sel.IsSelected(18, 0) {
		t.Errorf("expected cells to be selected")
	}
	if sel.IsSelected(7, 0) || sel.IsSelected(19, 0) || sel.IsSelected(10, 1) {
		t.Errorf("cells outside range should not be selected")
	}

	text := ExtractSelectedText(term, sel, 0)
	if text != "Hello World" {
		t.Errorf("ExtractSelectedText = %q, want %q", text, "Hello World")
	}

	// Reverse drag (drag from right to left)
	revSel := SelectionRange{
		Active: true,
		StartX: 18,
		StartY: 0,
		EndX:   8,
		EndY:   0,
	}
	if !revSel.IsSelected(10, 0) {
		t.Errorf("expected cell (10, 0) to be selected in reverse selection")
	}
	revText := ExtractSelectedText(term, revSel, 0)
	if revText != "Hello World" {
		t.Errorf("reverse ExtractSelectedText = %q, want %q", revText, "Hello World")
	}

	// Render with selection contains reverse video code \033[7m
	rendered := RenderVTWithSelection(term, sel, 0)
	if !strings.Contains(rendered, "\033[7m") {
		t.Errorf("RenderVTWithSelection should contain reverse ANSI code for selection highlight")
	}
}
