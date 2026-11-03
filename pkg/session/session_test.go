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

func TestRussianCyrillicUTF8(t *testing.T) {
	// 1. KeyToBytes Cyrillic typing
	cyrillicRune := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'П'}}
	bytesGot := KeyToBytes(cyrillicRune)
	if string(bytesGot) != "П" {
		t.Fatalf("expected 'П', got %v", bytesGot)
	}

	// 2. Virtual terminal Cyrillic rendering
	term := vt.New(vt.WithSize(40, 5))
	term.Write([]byte("Привет, мир! Тест UTF-8 кодировки\r\n"))
	rendered := RenderVT(term)

	if !strings.Contains(rendered, "Привет, мир!") {
		t.Fatalf("RenderVT does not contain expected Russian text 'Привет, мир!', got: %s", rendered)
	}
	if !strings.Contains(rendered, "Тест UTF-8") {
		t.Fatalf("RenderVT does not contain expected Russian text 'Тест UTF-8', got: %s", rendered)
	}
}

func TestSplitIncompleteUTF8(t *testing.T) {
	// ASCII complete
	ascii := []byte("Hello world 123")
	comp, left := splitIncompleteUTF8(ascii)
	if string(comp) != "Hello world 123" || len(left) != 0 {
		t.Fatalf("ASCII split failed: comp=%q, left=%v", comp, left)
	}

	// Complete Cyrillic UTF-8 ("Привет")
	cyrillic := []byte("Привет")
	comp, left = splitIncompleteUTF8(cyrillic)
	if string(comp) != "Привет" || len(left) != 0 {
		t.Fatalf("Complete Cyrillic failed: comp=%q, left=%v", comp, left)
	}

	// Incomplete Cyrillic: "Привет" truncated by 1 byte (the second byte of 'т' missing)
	incomplete := cyrillic[:len(cyrillic)-1]
	comp, left = splitIncompleteUTF8(incomplete)
	if string(comp) != "Приве" {
		t.Fatalf("expected comp='Приве', got %q", comp)
	}
	if len(left) != 1 {
		t.Fatalf("expected 1 leftover byte, got %d", len(left))
	}

	// Re-assembling with the missing byte gives back full "т"
	reassembled := append(left, cyrillic[len(cyrillic)-1])
	if string(reassembled) != "т" {
		t.Fatalf("expected reassembled 'т', got %q", reassembled)
	}
}

func TestSessionStateTransitions(t *testing.T) {
	s := &Session{
		ID:     "test-sess",
		Alias:  "Server-Offline",
		Target: "192.168.88.91",
		State:  SessionStateConnecting,
	}

	if s.GetState() != SessionStateConnecting {
		t.Fatalf("expected initial state SessionStateConnecting")
	}
	if s.StatusIcon() != "🟡" {
		t.Fatalf("expected status icon 🟡 for connecting, got %s", s.StatusIcon())
	}
	if s.StatusText() != "🟡 Подключение..." {
		t.Fatalf("expected status text '🟡 Подключение...', got %s", s.StatusText())
	}
	if s.IsConnected() {
		t.Fatalf("IsConnected should be false while connecting")
	}

	// Transition to connected
	if !s.SetConnected() {
		t.Fatalf("expected SetConnected() to return true on transition")
	}
	if s.GetState() != SessionStateConnected {
		t.Fatalf("expected state SessionStateConnected")
	}
	if s.StatusIcon() != "🟢" {
		t.Fatalf("expected status icon 🟢, got %s", s.StatusIcon())
	}
	if !s.IsConnected() {
		t.Fatalf("IsConnected should be true after SetConnected()")
	}

	// Close session
	_ = s.Close()
	if s.GetState() != SessionStateClosed {
		t.Fatalf("expected state SessionStateClosed after Close()")
	}
	if s.StatusIcon() != "🔴" {
		t.Fatalf("expected status icon 🔴, got %s", s.StatusIcon())
	}
	if !s.IsClosed() {
		t.Fatalf("IsClosed should be true after Close()")
	}
	if s.IsConnected() {
		t.Fatalf("IsConnected should be false after Close()")
	}
}

func TestHasSuccessfulConnection(t *testing.T) {
	// 1. Empty terminal
	termEmpty := vt.New(vt.WithSize(80, 24))
	if hasSuccessfulConnection(termEmpty) {
		t.Fatalf("expected hasSuccessfulConnection == false for empty terminal")
	}

	// 2. Terminal with only ANSI cursor/mode sequences (what ConPTY sends at start)
	termEscapeOnly := vt.New(vt.WithSize(80, 24))
	termEscapeOnly.Write([]byte("\x1b[?9001h\x1b[?1004h\x1b[?25h\x1b[2J"))
	if hasSuccessfulConnection(termEscapeOnly) {
		t.Fatalf("expected hasSuccessfulConnection == false for terminal with only invisible control codes")
	}

	// 3. Terminal with SSH fatal error
	termError := vt.New(vt.WithSize(80, 24))
	termError.Write([]byte("ssh: connect to host 192.168.88.91 port 22: Connection timed out\r\n"))
	if hasSuccessfulConnection(termError) {
		t.Fatalf("expected hasSuccessfulConnection == false for fatal connection error output")
	}

	// 4. Terminal with real remote prompt
	termConnected := vt.New(vt.WithSize(80, 24))
	termConnected.Write([]byte("Welcome to Ubuntu 26.04\r\nroot@server:~# "))
	if !hasSuccessfulConnection(termConnected) {
		t.Fatalf("expected hasSuccessfulConnection == true for valid shell output")
	}

	// 5. Terminal with password prompt
	termPass := vt.New(vt.WithSize(80, 24))
	termPass.Write([]byte("root@192.168.88.91's password: "))
	if !hasSuccessfulConnection(termPass) {
		t.Fatalf("expected hasSuccessfulConnection == true for password prompt")
	}
}
