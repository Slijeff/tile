package server

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/x/ansi"
)

// helpEntries is the full prefix tooltip: one row per remappable action,
// next/prev window folded into a single row, plus the structural bindings
// (digit select, arrows) that always work but aren't configurable.
func helpEntries(km keymap) []helpEntry {
	return []helpEntry{
		{km.NextWindow + "/" + km.PrevWindow, "next / prev window"},
		{"0-9", "select window"},
		{km.NewPane, "new pane (auto direction)"},
		{km.SplitHoriz, "split side-by-side"},
		{km.SplitVert, "split top-to-bottom"},
		{km.Stack, "stack a pane"},
		{km.Zoom, "zoom the active pane"},
		{km.Float, "toggle floating terminal"},
		{km.Swap, "swap two panes (click + drag)"},
		{km.CyclePane, "cycle panes"},
		{"←↑↓→", "move focus (cycles layer at a dead end)"},
		{km.Windows.Key, "windows…"},
		{km.Panes.Key, "panes…"},
		{km.Preset, "save preset"},
		{km.LoadPreset, "load preset"},
		{km.Theme, "colorscheme picker"},
		{km.Reload, "reload config"},
		{km.Detach, "detach"},
		{km.Quit, "quit"},
	}
}

// chordEntries lists the remaining suffix of every binding reachable from
// chord — e.g. chord "p" turns a "pr" binding into a "r" entry paired with
// its label — for the which-key box shown while a chord is in progress.
// Works for a chord of any depth: typing more keys just narrows this list
// further, the same way it narrows command()'s dispatch.
func chordEntries(km keymap, chord string) []helpEntry {
	all := actionEntries(km)
	entries := make([]helpEntry, 0, len(all))
	for _, e := range all {
		if len(e.key) > len(chord) && strings.HasPrefix(e.key, chord) {
			entries = append(entries, helpEntry{e.key[len(chord):], e.desc})
		}
	}
	return entries
}

// helpBox renders the prefix tooltip as a bordered box, one line per
// []string entry, every line the same visible width.
func helpBox(km keymap, th theme) []string {
	return entriesBox("» "+km.Prefix, helpEntries(km), th)
}

// chordBox renders the which-key box for a chord in progress: the title
// shows the full sequence typed so far, and each row the key (or further
// leader) that continues it.
func chordBox(chord string, km keymap, th theme) []string {
	return entriesBox("» "+km.Prefix+" "+chord, chordEntries(km, chord), th)
}

// swapPromptBox is the corner tooltip shown for as long as swap mode is
// armed, so a user who just pressed the keybind knows what to do with the
// mouse before making a move.
func swapPromptBox(km keymap, th theme) []string {
	return panel("» swap", []string{
		"click a pane, drag onto another to trade them",
		km.Swap + " cancels",
	}, 0, th)
}

// entriesBox is the shared renderer behind helpBox and chordBox: one
// "key → desc" row per entry, the keys padded into a column.
func entriesBox(title string, entries []helpEntry, th theme) []string {
	keyWidth := 0
	for _, e := range entries {
		if w := len([]rune(e.key)); w > keyWidth {
			keyWidth = w
		}
	}
	rows := make([]string, len(entries))
	for i, e := range entries {
		rows[i] = fmt.Sprintf("%s%-*s\x1b[m → %s", fg(th.Accent), keyWidth, e.key, e.desc)
	}
	return panel(title, rows, 0, th)
}

// panel frames pre-styled rows as one of yatm's overlay boxes: a border, a
// bold title, a rule, then one line per row, each padded — or truncated — to
// the same visible width, at least minWidth wide. Uniform width is load
// bearing: a row wider than the rest desyncs the box from what overlay and
// overlayCenter measure, and they drop the whole thing rather than place it.
func panel(title string, rows []string, minWidth int, th theme) []string {
	w := max(ansi.StringWidth(title), minWidth)
	for _, r := range rows {
		w = max(w, ansi.StringWidth(r))
	}

	b := fg(th.Surface)
	box := make([]string, 0, len(rows)+4)
	box = append(box, b+"┌"+strings.Repeat("─", w+2)+"┐\x1b[m")
	box = append(box, b+"│ \x1b[m"+padTo("\x1b[1m"+fg(th.Text)+title+"\x1b[22m\x1b[m", w)+b+" │\x1b[m")
	box = append(box, b+"│ "+strings.Repeat("─", w)+" │\x1b[m")
	for _, r := range rows {
		box = append(box, b+"│ \x1b[m"+padTo(r, w)+b+" │\x1b[m")
	}
	box = append(box, b+"└"+strings.Repeat("─", w+2)+"┘\x1b[m")
	return box
}

// padTo pads s out with spaces, or truncates it, to exactly w visible cells.
// Rows have to measure the same or the box they sit in loses its shape.
func padTo(s string, w int) string {
	switch d := w - ansi.StringWidth(s); {
	case d > 0:
		return s + strings.Repeat(" ", d)
	case d < 0:
		return ansi.Truncate(s, w, "") + "\x1b[m"
	}
	return s
}

// overlay stamps box onto the bottom-right corner of a w-by-h grid of
// already-fit lines (see fit), the way render composes pane content.
func overlay(base string, w, h int, box []string) string {
	bw := boxWidth(box)
	if len(box) == 0 || len(box) > h || bw >= w {
		return base
	}
	return overlayAt(base, w, w-bw, h-len(box), box)
}

// overlayAt stamps box onto a w-cell-wide grid of already-fit lines with its
// top-left corner at (x, y), padding the row up to x and back out to w so
// the line keeps its exact width. Rows past the bottom of the grid are
// dropped; a box too wide to fit at that column is refused outright. Shared
// by overlay's corner tooltips, overlayCenter's modal panels and the
// floating terminal, which places itself at an explicit rect.
func overlayAt(base string, w, x, y int, box []string) string {
	bw := boxWidth(box)
	if bw == 0 || x < 0 || x+bw > w {
		return base
	}
	lines := strings.Split(base, "\n")
	for i, bl := range box {
		row := y + i
		if row < 0 || row >= len(lines) {
			continue
		}
		pre := ansi.Truncate(lines[row], x, "")
		if d := x - ansi.StringWidth(pre); d > 0 {
			pre += strings.Repeat(" ", d)
		}
		lines[row] = pre + "\x1b[m" + bl + "\x1b[m" + strings.Repeat(" ", w-x-bw)
	}
	return strings.Join(lines, "\n")
}

// boxWidth is the widest visible line in box.
func boxWidth(box []string) int {
	w := 0
	for _, l := range box {
		if lw := ansi.StringWidth(l); lw > w {
			w = lw
		}
	}
	return w
}
