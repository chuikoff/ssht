package tui

import "testing"

func TestUpdateHit(t *testing.T) {
	// Badge on the header, notice starts at column 50 and wraps on a 60-wide screen.
	const width, badgeStart, badgeEnd, noteStart, noteLen = 60, 20, 32, 50, 25
	if !updateHit(21, 0, width, badgeStart, badgeEnd, noteStart, noteLen) {
		t.Fatal("badge click missed")
	}
	if updateHit(10, 0, width, badgeStart, badgeEnd, noteStart, noteLen) {
		t.Fatal("empty header counted as the badge")
	}
	if !updateHit(50, 0, width, badgeStart, badgeEnd, noteStart, noteLen) {
		t.Fatal("notice on the first row missed")
	}
	if !updateHit(5, 1, width, badgeStart, badgeEnd, noteStart, noteLen) {
		t.Fatal("wrapped notice missed")
	}
	if updateHit(20, 1, width, badgeStart, badgeEnd, noteStart, noteLen) {
		t.Fatal("click past the wrapped notice started an update")
	}
	if updateHit(21, 0, width, 0, 0, -1, 0) {
		t.Fatal("no update target should ignore the click")
	}

	// Notice starts past the edge. On an 80-column screen column 101 is
	// row 1, column 21. The wrapped tabs to its left are not the notice.
	const narrow, past, length = 80, 101, 60
	if updateHit(0, 1, narrow, 0, 0, past, length) {
		t.Fatal("wrapped tab counted as the notice")
	}
	if !updateHit(21, 1, narrow, 0, 0, past, length) {
		t.Fatal("notice past the edge missed")
	}
	if !updateHit(0, 2, narrow, 0, 0, past, length) {
		t.Fatal("notice tail missed")
	}
	if updateHit(1, 2, narrow, 0, 0, past, length) {
		t.Fatal("click past the notice started an update")
	}
}
