package session

import (
	"fmt"
	"strings"

	"sshtunnels/pkg/session/vt"
)

const (
	attrReverse   = 1 << 0
	attrUnderline = 1 << 1
	attrBold      = 1 << 2
	attrItalic    = 1 << 4
)

// SelectionRange represents a highlighted text region inside the terminal.
type SelectionRange struct {
	Active    bool
	Selecting bool
	StartX    int
	StartY    int
	EndX      int
	EndY      int
}

// IsSelected returns true if the cell at (x, y) is within the selection range.
func (s SelectionRange) IsSelected(x, y int) bool {
	if !s.Active {
		return false
	}
	sy, sx := s.StartY, s.StartX
	ey, ex := s.EndY, s.EndX
	if sy > ey || (sy == ey && sx > ex) {
		sy, ey = ey, sy
		sx, ex = ex, sx
	}

	if y < sy || y > ey {
		return false
	}
	if y == sy && y == ey {
		return x >= sx && x <= ex
	}
	if y == sy {
		return x >= sx
	}
	if y == ey {
		return x <= ex
	}
	return true
}

// ExtractSelectedText extracts the plain text from the virtual terminal within the selection range.
func ExtractSelectedText(term vt.Terminal, sel SelectionRange, scrollOffset int) string {
	if !sel.Active || term == nil {
		return ""
	}
	term.Lock()
	defer term.Unlock()

	cols, rows := term.Size()
	if cols <= 0 || rows <= 0 {
		return ""
	}

	sy, sx := sel.StartY, sel.StartX
	ey, ex := sel.EndY, sel.EndX
	if sy > ey || (sy == ey && sx > ex) {
		sy, ey = ey, sy
		sx, ex = ex, sx
	}

	if sy < 0 {
		sy = 0
	}
	if ey >= rows {
		ey = rows - 1
	}
	if sy >= rows {
		return ""
	}

	var lines []string
	for y := sy; y <= ey; y++ {
		startCol := 0
		if y == sy {
			startCol = sx
		}
		if startCol < 0 {
			startCol = 0
		}
		endCol := cols - 1
		if y == ey {
			endCol = ex
		}
		if endCol >= cols {
			endCol = cols - 1
		}
		if startCol > endCol {
			lines = append(lines, "")
			continue
		}

		var lineRunes []rune
		for x := startCol; x <= endCol; x++ {
			g := term.CellWithScroll(x, y, scrollOffset)
			ch := g.Char
			if ch == 0 {
				ch = ' '
			}
			lineRunes = append(lineRunes, ch)
		}
		lines = append(lines, strings.TrimRight(string(lineRunes), " "))
	}

	return strings.Join(lines, "\r\n")
}

// RenderVT converts the current virtual terminal buffer into an ANSI-escaped string.
func RenderVT(term vt.Terminal) string {
	return RenderVTWithSelection(term, SelectionRange{}, 0)
}

// RenderVTWithSelection converts the current virtual terminal buffer into an ANSI-escaped string,
// highlighting cells that fall within the given SelectionRange and offset by scrollOffset lines.
func RenderVTWithSelection(term vt.Terminal, sel SelectionRange, scrollOffset int) string {
	if term == nil {
		return ""
	}

	term.Lock()
	defer term.Unlock()

	cols, rows := term.Size()
	if cols <= 0 || rows <= 0 {
		return ""
	}

	cur := term.Cursor()
	curVis := term.CursorVisible()

	var sb strings.Builder
	var lastFG, lastBG vt.Color = vt.DefaultFG, vt.DefaultBG
	var lastMode int16 = 0
	var lastCursor bool = false
	var lastSel bool = false

	for y := 0; y < rows; y++ {
		// Find last character on line so we don't pad trailing whitespace with redundant color codes
		lastCharIdx := -1
		for x := cols - 1; x >= 0; x-- {
			g := term.CellWithScroll(x, y, scrollOffset)
			isCursor := (scrollOffset == 0) && curVis && cur.Y == y && cur.X == x
			isSel := sel.IsSelected(x, y)
			if (g.Char != 0 && g.Char != ' ') || isCursor || isSel {
				lastCharIdx = x
				break
			}
		}

		limit := lastCharIdx + 1
		for x := 0; x < limit; x++ {
			g := term.CellWithScroll(x, y, scrollOffset)
			isCursor := (scrollOffset == 0) && curVis && cur.Y == y && cur.X == x
			isSel := sel.IsSelected(x, y)
			fg := g.FG
			bg := g.BG
			mode := g.Mode

			if fg != lastFG || bg != lastBG || mode != lastMode || isCursor != lastCursor || isSel != lastSel {
				sb.WriteString("\033[0m")

				if isSel {
					sb.WriteString("\033[7m")
				} else if isCursor || (mode&attrReverse != 0) {
					sb.WriteString("\033[7m")
				}

				if mode&attrBold != 0 {
					sb.WriteString("\033[1m")
				}
				if mode&attrUnderline != 0 {
					sb.WriteString("\033[4m")
				}
				if mode&attrItalic != 0 {
					sb.WriteString("\033[3m")
				}

				if !isSel {
					if fg != vt.DefaultFG {
						if fg < 8 {
							sb.WriteString(fmt.Sprintf("\033[%dm", 30+fg))
						} else if fg < 16 {
							sb.WriteString(fmt.Sprintf("\033[%dm", 90+(fg-8)))
						} else if fg < 256 {
							sb.WriteString(fmt.Sprintf("\033[38;5;%dm", fg))
						}
					}

					if bg != vt.DefaultBG {
						if bg < 8 {
							sb.WriteString(fmt.Sprintf("\033[%dm", 40+bg))
						} else if bg < 16 {
							sb.WriteString(fmt.Sprintf("\033[%dm", 100+(bg-8)))
						} else if bg < 256 {
							sb.WriteString(fmt.Sprintf("\033[48;5;%dm", bg))
						}
					}
				}

				lastFG = fg
				lastBG = bg
				lastMode = mode
				lastCursor = isCursor
				lastSel = isSel
			}

			ch := g.Char
			if ch == 0 {
				ch = ' '
			}
			sb.WriteRune(ch)
		}

		if lastFG != vt.DefaultFG || lastBG != vt.DefaultBG || lastMode != 0 || lastCursor || lastSel {
			sb.WriteString("\033[0m")
			lastFG = vt.DefaultFG
			lastBG = vt.DefaultBG
			lastMode = 0
			lastCursor = false
			lastSel = false
		}

		if y < rows-1 {
			sb.WriteByte('\n')
		}
	}

	return sb.String()
}
