package tui

// updateHit reports whether (x, y) lands on the update badge or the notice.
// The notice is one run of columns starting at noteStart and may wrap.
func updateHit(x, y, width, badgeStart, badgeEnd, noteStart, noteLen int) bool {
	if y == 0 && badgeEnd > badgeStart && x >= badgeStart && x < badgeEnd {
		return true
	}
	if noteLen <= 0 || noteStart < 0 || width <= 0 {
		return false
	}
	pos := noteStart
	row := 0
	left := noteLen
	for left > 0 && row < 6 {
		room := width - pos
		if room <= 0 {
			pos = 0
			row++
			continue
		}
		take := room
		if take > left {
			take = left
		}
		if row == y && x >= pos && x < pos+take {
			return true
		}
		left -= take
		pos = 0
		row++
	}
	return false
}
