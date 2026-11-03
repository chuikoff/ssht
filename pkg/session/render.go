package session

import (
	"fmt"
	"strings"

	"github.com/hinshun/vt10x"
)

const (
	attrReverse   = 1 << 0
	attrUnderline = 1 << 1
	attrBold      = 1 << 2
	attrItalic    = 1 << 4
)

// RenderVT converts the current virtual terminal buffer into an ANSI-escaped string.
func RenderVT(vt vt10x.Terminal) string {
	if vt == nil {
		return ""
	}

	vt.Lock()
	defer vt.Unlock()

	cols, rows := vt.Size()
	if cols <= 0 || rows <= 0 {
		return ""
	}

	cur := vt.Cursor()
	curVis := vt.CursorVisible()

	var sb strings.Builder
	var lastFG, lastBG vt10x.Color = vt10x.DefaultFG, vt10x.DefaultBG
	var lastMode int16 = 0
	var lastCursor bool = false

	for y := 0; y < rows; y++ {
		// Find last character on line so we don't pad trailing whitespace with redundant color codes
		lastCharIdx := -1
		for x := cols - 1; x >= 0; x-- {
			g := vt.Cell(x, y)
			if (g.Char != 0 && g.Char != ' ') || (curVis && cur.Y == y && cur.X == x) {
				lastCharIdx = x
				break
			}
		}

		limit := lastCharIdx + 1
		for x := 0; x < limit; x++ {
			g := vt.Cell(x, y)
			isCursor := curVis && cur.Y == y && cur.X == x
			fg := g.FG
			bg := g.BG
			mode := g.Mode

			if fg != lastFG || bg != lastBG || mode != lastMode || isCursor != lastCursor {
				sb.WriteString("\033[0m")

				if isCursor || (mode&attrReverse != 0) {
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

				if fg != vt10x.DefaultFG {
					if fg < 8 {
						sb.WriteString(fmt.Sprintf("\033[%dm", 30+fg))
					} else if fg < 16 {
						sb.WriteString(fmt.Sprintf("\033[%dm", 90+(fg-8)))
					} else if fg < 256 {
						sb.WriteString(fmt.Sprintf("\033[38;5;%dm", fg))
					}
				}

				if bg != vt10x.DefaultBG {
					if bg < 8 {
						sb.WriteString(fmt.Sprintf("\033[%dm", 40+bg))
					} else if bg < 16 {
						sb.WriteString(fmt.Sprintf("\033[%dm", 100+(bg-8)))
					} else if bg < 256 {
						sb.WriteString(fmt.Sprintf("\033[48;5;%dm", bg))
					}
				}

				lastFG = fg
				lastBG = bg
				lastMode = mode
				lastCursor = isCursor
			}

			ch := g.Char
			if ch == 0 {
				ch = ' '
			}
			sb.WriteRune(ch)
		}

		if lastFG != vt10x.DefaultFG || lastBG != vt10x.DefaultBG || lastMode != 0 || lastCursor {
			sb.WriteString("\033[0m")
			lastFG = vt10x.DefaultFG
			lastBG = vt10x.DefaultBG
			lastMode = 0
			lastCursor = false
		}

		if y < rows-1 {
			sb.WriteByte('\n')
		}
	}

	return sb.String()
}
