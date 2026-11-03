package vt

import (
	"testing"
)

func TestScrollback(t *testing.T) {
	term := New(WithSize(20, 3))

	// Write 5 lines (more than 3 rows)
	term.Write([]byte("Line 1\r\nLine 2\r\nLine 3\r\nLine 4\r\nLine 5"))

	sbLen := term.ScrollbackLen()
	if sbLen != 2 {
		t.Fatalf("expected 2 lines in scrollback, got %d", sbLen)
	}

	// Live view (scrollOffset = 0): line 0 is "Line 3", line 1 is "Line 4", line 2 is "Line 5"
	cell0 := term.CellWithScroll(0, 0, 0)
	if cell0.Char != 'L' {
		t.Errorf("expected 'L' at (0, 0) live view, got %c", cell0.Char)
	}
	cellLine5 := term.CellWithScroll(5, 2, 0)
	if cellLine5.Char != '5' {
		t.Errorf("expected '5' at (5, 2) live view, got %c", cellLine5.Char)
	}

	// Scrolled up by 2 lines (scrollOffset = 2): line 0 is "Line 1", line 1 is "Line 2", line 2 is "Line 3"
	cellScrolled1 := term.CellWithScroll(5, 0, 2)
	if cellScrolled1.Char != '1' {
		t.Errorf("expected '1' at (5, 0) with scrollOffset=2, got %c", cellScrolled1.Char)
	}
	cellScrolled2 := term.CellWithScroll(5, 1, 2)
	if cellScrolled2.Char != '2' {
		t.Errorf("expected '2' at (5, 1) with scrollOffset=2, got %c", cellScrolled2.Char)
	}
}
