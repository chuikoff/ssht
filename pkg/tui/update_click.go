package tui

// updateHit reports whether (x, y) lands on the update badge or the notice.
// Both are columns of the unwrapped header. The terminal wraps that whole
// line, so the click is column y*width+x, including when the notice itself
// starts past the right edge.
func updateHit(x, y, width, badgeStart, badgeEnd, noteStart, noteLen int) bool {
	if width <= 0 || x < 0 || y < 0 {
		return false
	}
	abs := y*width + x
	if badgeEnd > badgeStart && abs >= badgeStart && abs < badgeEnd {
		return true
	}
	if noteLen <= 0 || noteStart < 0 {
		return false
	}
	return abs >= noteStart && abs < noteStart+noteLen
}
