package server

import (
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

// renamer is the window-rename prompt's open state: a single-line text
// buffer the user edits, seeded with the window's current label.
type renamer struct {
	text string
}

// openRenamer seeds the prompt with whatever name is currently showing —
// the custom name if one was set, otherwise the active pane's title.
func (s *server) openRenamer() {
	w := s.win()
	name := w.name
	if !w.customName && w.active != nil && w.active.pane != nil {
		name = w.active.pane.title
	}
	s.renamer = &renamer{text: name}
}

// renamerKey handles one keystroke while the rename prompt is open. Enter
// commits the typed name; leaving it blank reverts to following the active
// pane's title instead of pinning an empty label. Esc discards the edit.
func (s *server) renamerKey(k tea.Key) {
	s.dirty = true
	switch {
	case k.Code == tea.KeyEnter:
		w := s.win()
		if name := strings.TrimSpace(s.renamer.text); name == "" {
			w.customName = false
		} else {
			w.name = name
			w.customName = true
		}
		s.renamer = nil
	case k.Code == tea.KeyEscape:
		s.renamer = nil
	case k.Code == tea.KeyBackspace:
		if r := []rune(s.renamer.text); len(r) > 0 {
			s.renamer.text = string(r[:len(r)-1])
		}
	case k.Text != "" && k.Mod&(tea.ModCtrl|tea.ModAlt) == 0:
		s.renamer.text += k.Text
	}
}

// renameBox renders the rename prompt as a bordered floating panel, styled
// like the picker and help overlays: a title row and one editable line with
// a trailing cursor.
func renameBox(text string, th theme) []string {
	const minWidth = 24
	title := "rename window  (enter confirm · esc cancel)"
	input := text + "▏"

	textWidth := ansi.StringWidth(title)
	if w := ansi.StringWidth(input); w > textWidth {
		textWidth = w
	}
	if textWidth < minWidth {
		textWidth = minWidth
	}
	pad := func(s string) string {
		if d := textWidth - ansi.StringWidth(s); d > 0 {
			return s + strings.Repeat(" ", d)
		}
		return s
	}

	b := fg(th.Surface)
	box := make([]string, 0, 5)
	box = append(box, b+"┌"+strings.Repeat("─", textWidth+2)+"┐\x1b[m")
	box = append(box, b+"│ \x1b[m"+pad("\x1b[1m"+fg(th.Text)+title+"\x1b[22m\x1b[m")+b+" │\x1b[m")
	box = append(box, b+"│ "+pad(strings.Repeat("─", textWidth))+" │\x1b[m")
	box = append(box, b+"│ \x1b[m"+pad(fg(th.Text)+input+"\x1b[m")+b+" │\x1b[m")
	box = append(box, b+"└"+strings.Repeat("─", textWidth+2)+"┘\x1b[m")
	return box
}
