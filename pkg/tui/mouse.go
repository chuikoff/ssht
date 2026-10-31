package tui

import (
	"time"
)

// ButtonZone defines a clickable region on screen.
type ButtonZone struct {
	StartX int
	EndX   int
	Action string
}

// LastClickState tracks the last click for double-click detection.
type LastClickState struct {
	Tab   TabIndex
	Row   int
	Time  time.Time
}

// IsDoubleClick checks if a click on the same tab and row occurred within 450ms.
func (s *LastClickState) IsDoubleClick(tab TabIndex, row int) bool {
	if s.Tab == tab && s.Row == row && time.Since(s.Time) < 450*time.Millisecond {
		return true
	}
	return false
}

// Record records the click event.
func (s *LastClickState) Record(tab TabIndex, row int) {
	s.Tab = tab
	s.Row = row
	s.Time = time.Now()
}
