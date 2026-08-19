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
		{km.CyclePane, "cycle panes"},
		{"←↑↓→", "move focus (cycles layer at a dead end)"},
		{km.Windows.Key, "windows…"},
		{km.Panes.Key, "panes…"},
		{km.Theme, "colorscheme picker"},
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

// entriesBox is the shared renderer behind helpBox and chordBox: a bordered
// box with a title row and one "key → desc" row per entry, every line the
// same visible width.
func entriesBox(title string, entries []helpEntry, th theme) []string {
	keyWidth, textWidth := 0, ansi.StringWidth(title)
	for _, e := range entries {
		if w := len([]rune(e.key)); w > keyWidth {
			keyWidth = w
		}
	}
	for _, e := range entries {
		if w := keyWidth + 4 + ansi.StringWidth(e.desc); w > textWidth {
			textWidth = w
		}
	}

	pad := func(s string) string {
		if d := textWidth - ansi.StringWidth(s); d > 0 {
			return s + strings.Repeat(" ", d)
		}
		return s
	}

	b := fg(th.Surface)
	box := make([]string, 0, len(entries)+3)
	box = append(box, b+"┌"+strings.Repeat("─", textWidth+2)+"┐\x1b[m")
	box = append(box, b+"│ \x1b[m"+pad("\x1b[1m"+fg(th.Text)+title+"\x1b[22m\x1b[m")+b+" │\x1b[m")
	box = append(box, b+"│ "+pad(strings.Repeat("─", textWidth))+" │\x1b[m")
	for _, e := range entries {
		row := fmt.Sprintf("%s%-*s\x1b[m → %s", fg(th.Accent), keyWidth, e.key, e.desc)
		box = append(box, b+"│ \x1b[m"+pad(row)+b+" │\x1b[m")
	}
	box = append(box, b+"└"+strings.Repeat("─", textWidth+2)+"┘\x1b[m")
	return box
}

// overlay stamps box onto the bottom-right corner of a w-by-h grid of
// already-fit lines (see fit), the way render composes pane content.
func overlay(base string, w, h int, box []string) string {
	if len(box) == 0 || len(box) > h {
		return base
	}
	bw := 0
	for _, l := range box {
		if lw := ansi.StringWidth(l); lw > bw {
			bw = lw
		}
	}
	if bw >= w {
		return base
	}
	lines := strings.Split(base, "\n")
	top := len(lines) - len(box)
	for i, bl := range box {
		y := top + i
		if y < 0 || y >= len(lines) {
			continue
		}
		left := ansi.Truncate(lines[y], w-bw, "")
		if d := (w - bw) - ansi.StringWidth(left); d > 0 {
			left += strings.Repeat(" ", d)
		}
		lines[y] = left + "\x1b[m" + bl
	}
	return strings.Join(lines, "\n")
}
