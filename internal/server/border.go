package server

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/x/ansi"
)

// contentRect insets r by the 1-cell border borderPane draws on every edge.
// Too small to hold a border (borderPane's own fallback), it returns r
// unchanged, since that's the rect borderPane fills with content instead.
func contentRect(r rect) rect {
	if r.w < 3 || r.h < 3 {
		return r
	}
	return rect{r.x + 1, r.y + 1, r.w - 2, r.h - 2}
}

// borderPane draws a titled box around a pane's view, one cell of border on
// every edge, with the pane's name in the top edge. A focused pane's border
// is drawn in the theme's accent color, every other pane's in its dim
// surface color — the clearest cue, for a stacked pane, that it's the one
// currently on top (see collapsedHeader for the layers behind it, drawn
// brighter so they stay legible while collapsed).
//
// Too small to fit a border (either edge under 3 cells), it falls back to
// the bare, unbordered view, matching contentRect's own fallback.
func borderPane(p *pane, r rect, focused bool, th theme) string {
	if r.w < 3 || r.h < 3 {
		return fit(p.view(), r.w, r.h)
	}
	color := fg(th.Surface)
	if focused {
		color = fg(th.Accent)
	}
	const reset = "\x1b[m"

	rows := make([]string, r.h)
	rows[0] = color + edgeLine(r.w, p, '┌', '┐') + reset
	content := strings.Split(fit(p.view(), r.w-2, r.h-2), "\n")
	for i, line := range content {
		rows[i+1] = color + "│" + reset + line + color + "│" + reset
	}
	rows[r.h-1] = color + "└" + strings.Repeat("─", r.w-2) + "┘" + reset
	return strings.Join(rows, "\n")
}

// edgeLine lays out a titled border row: "┌─ title ────┐" and, when the
// pane has scrollback to show (not an alt-screen program, and it has
// produced more output than fits on screen), a right-aligned
// "SCROLL: pos/total", zellij style. Either is dropped, in that order, if
// there isn't room for it. left/right pick the corner glyphs, so a
// collapsed stack header below the active layer can point its corners up
// (└…┘) instead of down (┌…┐), toward the layer it would grow into.
func edgeLine(w int, p *pane, left, right rune) string {
	if w < 3 {
		return strings.Repeat("─", max(w, 0))
	}
	avail := w - 3 // interior width, minus the corners and one leading "─"
	label := " " + p.borderTitle() + " "
	var suffix string
	if !p.emu.IsAltScreen() {
		if total := p.emu.ScrollbackLen(); total > 0 {
			suffix = fmt.Sprintf(" SCROLL: %d/%d ", p.scroll, total)
		}
	}
	lw, sw := ansi.StringWidth(label), ansi.StringWidth(suffix)
	if lw > avail {
		label, suffix = ansi.Truncate(label, avail, ""), ""
		lw, sw = ansi.StringWidth(label), 0
	} else if lw+sw > avail {
		suffix, sw = "", 0
	}
	fillLen := avail - lw - sw
	return string(left) + "─" + label + strings.Repeat("─", fillLen) + suffix + string(right)
}

// collapsedHeader draws a background stack layer's one-row title bar — the
// border's top edge, in the theme's subtext color, with nothing below it.
// It's the only trace a layer leaves on screen until a click or cycleLayer
// brings it to the front, zellij's compact stacked-panes look — brighter
// than an ordinary unfocused border's dim surface color so a collapsed
// layer stays legible instead of blending into the background. below marks
// a layer stacked after the active one, whose corners point up (toward the
// active pane above it) rather than down.
func collapsedHeader(n *node, w int, th theme, below bool) string {
	left, right := '┌', '┐'
	if below {
		left, right = '└', '┘'
	}
	return fg(th.Subtext) + edgeLine(w, firstLeaf(n).pane, left, right) + "\x1b[m"
}
