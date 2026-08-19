package server

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/x/ansi"
)

// tabHit is a clickable column range in the tab bar; clicking within
// [lo, hi) on row 0 switches to window win.
type tabHit struct{ lo, hi, win int }

// tabLine renders the window-switcher row, styled like an editor's buffer
// tabs, and returns the column ranges clickable to reach each window.
func tabLine(windows []*window, cur, w int, th theme) (string, []tabHit) {
	var b strings.Builder
	hits := make([]tabHit, 0, len(windows))
	col := 0
	for i, win := range windows {
		name := win.name
		if !win.customName && win.active != nil && win.active.pane != nil {
			name = win.active.pane.title
		}
		label := fmt.Sprintf(" %d:%s ", i, name)
		style := fg(th.Subtext) + bg(th.Mantle)
		if i == cur {
			style = "\x1b[1m" + fg(th.Base) + bg(th.Accent)
		}
		b.WriteString(style + label + "\x1b[m")
		lw := ansi.StringWidth(label)
		hits = append(hits, tabHit{col, col + lw, i})
		col += lw
		if i < len(windows)-1 {
			b.WriteString(fg(th.Surface) + bg(th.Mantle) + "│\x1b[m")
			col++
		}
	}
	txt := b.String()
	if d := w - col; d > 0 {
		txt += bg(th.Mantle) + strings.Repeat(" ", d) + "\x1b[m"
	} else {
		txt = ansi.Truncate(txt, w, "")
	}
	return txt, hits
}
