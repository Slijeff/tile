package server

import (
	tea "charm.land/bubbletea/v2"
)

// renamer is the rename prompt's open state: a single-line text buffer the
// user edits, seeded with the active pane's border title, the active
// window's tab name, or the session's own name. forWindow/forSession say
// which one Enter commits to; neither set means the active pane.
type renamer struct {
	text       string
	forWindow  bool
	forSession bool
}

// openRenamer seeds the prompt with the active pane's current border
// title — its manual rename or auto-tracked command name if it has one,
// otherwise its shell title.
func (s *server) openRenamer() {
	name := ""
	if p := s.activePane(); p != nil {
		name = p.borderTitle()
	}
	s.renamer = &renamer{text: name}
}

// openWindowRenamer seeds the prompt with the active window's current tab
// name — its manual rename if it has one, otherwise whatever the active
// pane's title is showing there.
func (s *server) openWindowRenamer() {
	s.renamer = &renamer{text: s.win().displayName(), forWindow: true}
}

// openSessionRenamer seeds the prompt with the session's current name.
// Unlike a pane or window, a session has no auto-tracked name to fall back
// to, so renameSession — unlike pane.rename/window.rename — treats a blank
// commit as a no-op rather than a reset.
func (s *server) openSessionRenamer() {
	s.renamer = &renamer{text: s.sessionName(), forSession: true}
}

// renamerKey handles one keystroke while the rename prompt is open. Enter
// commits the typed name to the active pane's border or the active
// window's tab, whichever openRenamer/openWindowRenamer targeted; leaving
// it blank reverts to that target's own shell/auto-tracked name instead of
// pinning an empty label. Esc discards the edit.
func (s *server) renamerKey(k tea.Key) {
	s.dirty = true
	switch {
	case k.Code == tea.KeyEnter:
		switch {
		case s.renamer.forWindow:
			s.win().rename(s.renamer.text)
		case s.renamer.forSession:
			s.renameSession(s.renamer.text)
		default:
			if p := s.activePane(); p != nil {
				p.rename(s.renamer.text)
			}
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
func renameBox(text string, forWindow, forSession bool, th theme) []string {
	title := "rename pane  (enter confirm · esc cancel)"
	switch {
	case forWindow:
		title = "rename window  (enter confirm · esc cancel)"
	case forSession:
		title = "rename session  (enter confirm · esc cancel)"
	}
	input := fg(th.Text) + text + "▏\x1b[m"
	return panel(title, []string{input}, 24, th)
}
