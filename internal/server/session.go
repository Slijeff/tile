package server

import (
	"encoding/json"
	"fmt"
	"net"
	"os"
	"strings"

	tea "charm.land/bubbletea/v2"

	"tile/internal/proto"
)

// A session is what "tile ls" lists and "-t name" targets: a wholly
// separate daemon with its own socket, windows and panes. The sessions
// layer reaches the things worth doing to one from inside the TUI, without
// dropping back to a shell prompt for "tile kill-server", "tile ls" or
// "tile -t <name>".

// sessionName returns the session's display name: its own if it was given
// one, or "default" for the unnamed session every "-t"-less command
// reaches — the same fallback the status bar and "tile ls" use.
func (s *server) sessionName() string {
	if s.name == "" {
		return "default"
	}
	return s.name
}

// confirmDeleteSession raises a confirmation instead of tearing the session
// down on the spot: deleting the current session kills the whole daemon
// and every shell in it, exactly what Quit does — it's just reached from
// the sessions layer here instead of the top-level binding.
func (s *server) confirmDeleteSession() {
	s.sessionDeleting = true
	s.dirty = true
}

// sessionDeleteKey answers the delete-session confirmation, the same way
// quitKey answers quit's: y goes through with it, anything else backs out.
// Deliberately not "esc cancels, other keys are ignored" — a stray
// keystroke landing on this dialog should never be able to end the
// session, and should never be able to leave the user stuck in front of it
// either.
func (s *server) sessionDeleteKey(k tea.Key) {
	s.sessionDeleting = false
	if k.Text == "y" || k.Text == "Y" {
		s.shutdown()
	}
}

// sessionDeleteBox renders the confirmation as a bordered floating panel,
// styled like the quit dialog it mirrors.
func sessionDeleteBox(name string, th theme) []string {
	return entriesBox(fmt.Sprintf("delete session %q?", name), []helpEntry{
		{"y", "stop the daemon and every shell in it"},
		{"esc", "cancel (or any other key)"},
	}, th)
}

// renameSession moves the session's socket to match a new name, so every
// future "tile -t <name>" — including "tile ls" and another session's
// "close others" — finds it under the new one. Renaming the underlying
// unix socket file this way is safe while the listener stays up: connect()
// resolves the path at dial time, not at bind time, so a client dialing the
// new path still reaches the same listener the old path did. A blank name,
// one equal to the current name, or one already claimed by another running
// session is refused — the last silently, same as any other action that
// can't complete, rather than surfacing an error with nowhere to put it.
func (s *server) renameSession(name string) {
	name = strings.TrimSpace(name)
	if name == "" || name == s.sessionName() {
		return
	}
	sock, err := proto.SocketPath(name)
	if err != nil {
		return
	}
	if _, err := os.Stat(sock); err == nil {
		return
	}
	if err := os.Rename(s.sock, sock); err != nil {
		return
	}
	s.sock, s.name = sock, name
	s.dirty = true
}

// switchSession detaches the attached client the same way detach() does,
// but names which session to attach to next instead of just dropping back
// to a shell prompt. The client (cmd/tile) reads that name off the same
// MsgDetach a plain detach sends and reconnects there itself — the server
// has no say in what runs on the other end of the socket, so it can only
// ask.
func (s *server) switchSession(name string) {
	if s.cli != nil {
		s.cli.send(proto.ServerMsg{Type: proto.MsgDetach, Content: name})
		s.cli = nil
	}
}

// newSession spawns a fresh session under an automatically chosen name and
// switches the attached client to it. Creation itself happens implicitly:
// switchSession's MsgDetach names a session nothing is listening on yet,
// and the CLI's attach loop (attachOnce, in cmd/tile) already starts a
// daemon for exactly that case — the same one dialing a session that was
// never started falls into — so there is nothing session-spawning to do
// here beyond picking the name.
func (s *server) newSession() {
	s.switchSession(unusedSessionName())
}

// unusedSessionName returns a session name no currently running session has
// claimed: "session-1", "session-2", …, the first one free. A session
// started this way keeps that name until manually renamed with s+r.
func unusedSessionName() string {
	names, _ := proto.Sessions()
	taken := make(map[string]bool, len(names))
	for _, n := range names {
		taken[n] = true
	}
	for i := 1; ; i++ {
		name := fmt.Sprintf("session-%d", i)
		if !taken[name] {
			return name
		}
	}
}

// sessionPicker is the session picker's open state: every running
// session's name, and the row index of the current highlight.
type sessionPicker struct {
	names []string
	sel   int
}

// openSessionPicker opens a picker listing every currently running
// session, current one included so its entry marks where "you are here" —
// and starts the highlight there. A no-op if proto.Sessions can't be read,
// the same "do nothing" contract openPresetList uses for a picker with
// nothing to show.
func (s *server) openSessionPicker() {
	names, err := proto.Sessions()
	if err != nil || len(names) == 0 {
		return
	}
	sel := 0
	for i, n := range names {
		if n == s.sessionName() {
			sel = i
			break
		}
	}
	s.sessions = &sessionPicker{names: names, sel: sel}
}

// sessionPickerKey handles one keystroke while the session picker is open.
// Enter switches to the highlighted session — a no-op, rather than a
// pointless detach/reattach round trip, when that's the one already
// attached; Esc/q cancels.
func (s *server) sessionPickerKey(k tea.Key) {
	s.dirty = true
	switch {
	case k.Code == tea.KeyUp || k.Text == "k":
		n := len(s.sessions.names)
		s.sessions.sel = (s.sessions.sel - 1 + n) % n
	case k.Code == tea.KeyDown || k.Text == "j":
		s.sessions.sel = (s.sessions.sel + 1) % len(s.sessions.names)
	case k.Code == tea.KeyEnter:
		name := s.sessions.names[s.sessions.sel]
		s.sessions = nil
		if name != s.sessionName() {
			s.switchSession(name)
		}
	case k.Code == tea.KeyEscape || k.Text == "q":
		s.sessions = nil
	}
}

// sessionPickerBox renders the session picker as a bordered panel, one row
// per running session, the highlighted one picked out in the theme's
// accent color — styled like presetListBox. The current session's row is
// marked so it's clear which one switching would be a no-op on.
func sessionPickerBox(sp *sessionPicker, current string, th theme) []string {
	rows := make([]string, len(sp.names))
	for i, n := range sp.names {
		label := n
		if n == current {
			label += "  (current)"
		}
		row, style := "  "+label, fg(th.Text)
		if i == sp.sel {
			row, style = "› "+label, "\x1b[1m"+fg(th.Base)+bg(th.Accent)
		}
		rows[i] = style + row + "\x1b[m"
	}
	return panel("sessions  (↑↓/jk move · enter switch · esc cancel)", rows, 0, th)
}

// closeOtherSessions kills every other running session's daemon and every
// shell in it, leaving this one untouched. It doesn't ask first, unlike
// deleting the current session: unlike that binding, this one can never
// take down the session the user is looking at.
func (s *server) closeOtherSessions() {
	names, err := proto.Sessions()
	if err != nil {
		return
	}
	for _, n := range names {
		if n == s.sessionName() {
			continue
		}
		killSession(n)
	}
}

// killSession dials another session's socket and asks its daemon to shut
// down — the same MsgKill "tile kill-server" sends over the CLI. Errors are
// dropped: a session that's already gone, or never was there, needs no
// report from a binding with no prompt to show one on.
func killSession(name string) {
	sock, err := proto.SocketPath(name)
	if err != nil {
		return
	}
	conn, err := net.Dial("unix", sock)
	if err != nil {
		return
	}
	defer conn.Close()
	_ = json.NewEncoder(conn).Encode(proto.ClientMsg{Type: proto.MsgKill})
}
