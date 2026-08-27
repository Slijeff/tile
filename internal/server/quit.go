package server

import (
	tea "charm.land/bubbletea/v2"
)

// Quit is the one binding that doesn't just change what's on screen: it
// takes the daemon down, and with it every shell in every window, attached
// or not. So it asks first, the same modal-overlay way the pickers and the
// rename prompt do.

// confirmQuit raises the quit confirmation instead of shutting down on the
// spot.
func (s *server) confirmQuit() {
	s.quitting = true
	s.dirty = true
}

// quitKey answers the quit confirmation: y goes through with it, anything
// else backs out. Deliberately not "esc cancels, other keys are ignored" —
// a stray keystroke landing on this dialog should never be able to end the
// session, and should never be able to leave the user stuck in front of it
// either.
func (s *server) quitKey(k tea.Key) {
	s.quitting = false
	if k.Text == "y" || k.Text == "Y" {
		s.shutdown()
	}
}

// quitBox renders the confirmation as a bordered floating panel, styled like
// the which-key tooltip and the other overlays.
func quitBox(th theme) []string {
	return entriesBox("quit tile?", []helpEntry{
		{"y", "stop the daemon and every shell in it"},
		{"esc", "cancel (or any other key)"},
	}, th)
}
