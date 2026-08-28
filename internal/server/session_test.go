package server

import (
	"encoding/json"
	"net"
	"os"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"tile/internal/proto"
)

func newSessionTestServer(win *window) *server {
	return &server{
		windows:    []*window{win},
		w:          40,
		h:          10,
		km:         defaultKeymap,
		prefixSpec: parseKeySpec(defaultKeymap.Prefix),
		lockSpec:   parseKeySpec(defaultKeymap.Lock),
	}
}

// Stack moved from the top level to the panes layer, freeing "s" for the
// sessions layer: plain "s" must no longer stack a pane by itself, and
// p+s must.
func TestStackMovedUnderPanesLayer(t *testing.T) {
	win := &window{name: "0", root: &node{weight: 1, pane: &pane{}}, active: &node{weight: 1, pane: &pane{}}}
	s := newSessionTestServer(win)

	s.command(tea.Key{Text: "s"})
	if s.chord != "s" {
		t.Fatalf("chord = %q after plain \"s\", want %q (the sessions leader)", s.chord, "s")
	}

	events := make(chan event, 256)
	p, err := newPane(0, 20, 6, events)
	if err != nil {
		t.Fatal(err)
	}
	defer p.close()
	root := &node{pane: p, weight: 1}
	win2 := &window{name: "0", root: root, active: root}
	s2 := newSessionTestServer(win2)

	s2.command(tea.Key{Text: "p"})
	s2.command(tea.Key{Text: "s"})
	if got := leaves(s2.win().root); len(got) != 2 {
		t.Fatalf("p+s did not stack a pane: %d leaves, want 2", len(got))
	}
}

// s+x raises a confirmation instead of shutting down on the spot, exactly
// like Quit's — mirrors TestQuitAsksBeforeShuttingDown.
func TestSessionDeleteAsksBeforeShuttingDown(t *testing.T) {
	events := make(chan event, 256)
	p, err := newPane(0, 20, 6, events)
	if err != nil {
		t.Fatal(err)
	}
	root := &node{pane: p, weight: 1}
	s := newSessionTestServer(&window{name: "0", root: root, active: root})

	s.command(tea.Key{Text: "s"})
	s.command(tea.Key{Text: "x"})
	if !s.sessionDeleting {
		t.Fatal("s+x should raise the delete-session confirmation")
	}
	if s.done {
		t.Fatal("s+x must not shut the daemon down before it is confirmed")
	}

	s.key(tea.Key{Text: "y"})
	if s.sessionDeleting {
		t.Fatal("confirming should close the dialog")
	}
	if !s.done {
		t.Fatal("y should shut the daemon down")
	}
}

// Anything but a y backs out, same contract as the quit dialog.
func TestSessionDeleteDialogCancelsOnAnythingButY(t *testing.T) {
	for _, k := range []tea.Key{
		{Code: tea.KeyEscape},
		{Text: "n"},
		{Text: "x"},
		{Code: tea.KeyEnter},
	} {
		root := &node{pane: &pane{}, weight: 1}
		s := newSessionTestServer(&window{name: "0", root: root, active: root})
		s.command(tea.Key{Text: "s"})
		s.command(tea.Key{Text: "x"})

		s.key(k)
		if s.sessionDeleting {
			t.Fatalf("%+v should close the dialog", k)
		}
		if s.done {
			t.Fatalf("%+v must not shut the daemon down", k)
		}
	}
}

// While the dialog is up it is modal, same as quit's.
func TestSessionDeleteDialogSwallowsKeysAndDrawsItself(t *testing.T) {
	events := make(chan event, 256)
	p, err := newPane(0, 20, 6, events)
	if err != nil {
		t.Fatal(err)
	}
	defer p.close()
	root := &node{pane: p, weight: 1}
	s := newSessionTestServer(&window{name: "0", root: root, active: root})
	s.w, s.h = 60, 12

	s.command(tea.Key{Text: "s"})
	s.command(tea.Key{Text: "x"})
	if got := s.frame().Content; !strings.Contains(got, `delete session "default"?`) {
		t.Fatalf("frame does not show the delete-session dialog:\n%s", got)
	}

	s.key(tea.Key{Text: "z"}) // cancels, and must not reach the shell
	if len(p.cmdBuf) != 0 {
		t.Fatalf("a key answering the dialog reached the pane: cmdBuf = %q", string(p.cmdBuf))
	}
}

// s+r opens the renamer targeting the session, seeded with its display
// name — mirrors TestWindowChordOpensRenamerForActiveWindow.
func TestSessionChordOpensRenamerForSession(t *testing.T) {
	win := &window{name: "0", root: &node{weight: 1, pane: &pane{}}, active: &node{weight: 1, pane: &pane{}}}
	s := newSessionTestServer(win)

	s.command(tea.Key{Text: "s"})
	s.command(tea.Key{Text: "r"})
	if s.renamer == nil {
		t.Fatal("s+r did not open the renamer")
	}
	if !s.renamer.forSession {
		t.Fatal("s+r must target the session, not a pane or window")
	}
	if s.renamer.text != "default" {
		t.Fatalf("renamer.text = %q, want the unnamed session's display name %q", s.renamer.text, "default")
	}
}

// Committing a session rename moves the socket to the new name, and a
// blank commit is a no-op rather than reverting to some fallback name — a
// session, unlike a pane or window, has none to revert to.
func TestSessionRenameMovesSocketAndBlankIsNoop(t *testing.T) {
	dir, err := proto.SessionDir()
	if err != nil {
		t.Fatal(err)
	}
	oldSock := dir + "/tile-test-old.sock"
	ln, err := net.Listen("unix", oldSock)
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	defer os.Remove(oldSock)

	win := &window{name: "0", root: &node{weight: 1, pane: &pane{}}, active: &node{weight: 1, pane: &pane{}}}
	s := newSessionTestServer(win)
	s.name = "tile-test-old"
	s.sock = oldSock

	// A blank commit must leave the session exactly as it was.
	s.openSessionRenamer()
	for range len([]rune("tile-test-old")) {
		s.renamerKey(tea.Key{Code: tea.KeyBackspace})
	}
	s.renamerKey(tea.Key{Code: tea.KeyEnter})
	if s.name != "tile-test-old" || s.sock != oldSock {
		t.Fatalf("blank rename changed the session: name=%q sock=%q", s.name, s.sock)
	}
	if _, err := os.Stat(oldSock); err != nil {
		t.Fatalf("blank rename must not touch the socket: %v", err)
	}

	newName := "tile-test-new"
	newSock := dir + "/" + newName + ".sock"
	defer os.Remove(newSock)

	s.openSessionRenamer()
	for range len([]rune("tile-test-old")) {
		s.renamerKey(tea.Key{Code: tea.KeyBackspace})
	}
	for _, r := range newName {
		s.renamerKey(tea.Key{Text: string(r)})
	}
	s.renamerKey(tea.Key{Code: tea.KeyEnter})

	if s.name != newName {
		t.Fatalf("s.name = %q, want %q", s.name, newName)
	}
	if s.sock != newSock {
		t.Fatalf("s.sock = %q, want %q", s.sock, newSock)
	}
	if _, err := os.Stat(newSock); err != nil {
		t.Fatalf("socket did not move to the new name: %v", err)
	}
	if _, err := os.Stat(oldSock); !os.IsNotExist(err) {
		t.Fatal("old socket path should be gone after the rename")
	}

	// The moved socket must still answer as the same listener.
	conn, err := net.Dial("unix", newSock)
	if err != nil {
		t.Fatalf("dialing the renamed socket: %v", err)
	}
	conn.Close()
}

// Renaming to a name already claimed by another running session is
// refused rather than clobbering it.
func TestSessionRenameRefusesTakenName(t *testing.T) {
	dir, err := proto.SessionDir()
	if err != nil {
		t.Fatal(err)
	}
	takenSock := dir + "/tile-test-taken.sock"
	ln, err := net.Listen("unix", takenSock)
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	defer os.Remove(takenSock)

	ownSock := dir + "/tile-test-own.sock"
	ownLn, err := net.Listen("unix", ownSock)
	if err != nil {
		t.Fatal(err)
	}
	defer ownLn.Close()
	defer os.Remove(ownSock)

	win := &window{name: "0", root: &node{weight: 1, pane: &pane{}}, active: &node{weight: 1, pane: &pane{}}}
	s := newSessionTestServer(win)
	s.name, s.sock = "tile-test-own", ownSock

	s.renameSession("tile-test-taken")
	if s.name != "tile-test-own" || s.sock != ownSock {
		t.Fatalf("rename to a taken name must be refused: name=%q sock=%q", s.name, s.sock)
	}
}

// unusedSessionName skips any name already claimed by a running session,
// so a chain of new-session presses never collides with one just created.
func TestUnusedSessionNameSkipsTakenNames(t *testing.T) {
	dir, err := proto.SessionDir()
	if err != nil {
		t.Fatal(err)
	}
	takenSock := dir + "/session-1.sock"
	ln, err := net.Listen("unix", takenSock)
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	defer os.Remove(takenSock)

	if got := unusedSessionName(); got != "session-2" {
		t.Fatalf("unusedSessionName() = %q, want %q (session-1 already taken)", got, "session-2")
	}
}

// s+n detaches the attached client with a MsgDetach naming a fresh,
// unclaimed session — the same mechanism s+p's switch uses, just aimed at
// a name nothing is listening on yet, which is what makes attachOnce spawn
// a new daemon for it rather than reattach to an existing one.
func TestSessionChordNewSwitchesToFreshSession(t *testing.T) {
	win := &window{name: "0", root: &node{weight: 1, pane: &pane{}}, active: &node{weight: 1, pane: &pane{}}}
	s := newSessionTestServer(win)
	s.name = "tile-test-new-current"
	attached := &client{out: make(chan proto.ServerMsg, 4)}
	s.cli = attached

	s.command(tea.Key{Text: "s"})
	s.command(tea.Key{Text: "n"})

	if s.cli != nil {
		t.Fatal("creating a session must detach the attached client")
	}
	select {
	case m := <-attached.out:
		if m.Type != proto.MsgDetach {
			t.Fatalf("client got msg type %q, want %q", m.Type, proto.MsgDetach)
		}
		if !strings.HasPrefix(m.Content, "session-") {
			t.Fatalf("MsgDetach.Content = %q, want a fresh %q-prefixed name", m.Content, "session-")
		}
		if m.Content == s.name {
			t.Fatalf("new session got the current session's own name %q", m.Content)
		}
	default:
		t.Fatal("newSession did not send anything to the attached client")
	}
}

// s+p opens a picker listing every running session, highlighting the
// current one — mirrors TestPickerPreviewAndCancelRestoresOriginal's
// openPicker() contract.
func TestSessionPickerOpensWithCurrentSessionHighlighted(t *testing.T) {
	dir, err := proto.SessionDir()
	if err != nil {
		t.Fatal(err)
	}
	selfSock := dir + "/tile-test-self.sock"
	selfLn, err := net.Listen("unix", selfSock)
	if err != nil {
		t.Fatal(err)
	}
	defer selfLn.Close()
	defer os.Remove(selfSock)
	otherSock := dir + "/tile-test-picker-other.sock"
	otherLn, err := net.Listen("unix", otherSock)
	if err != nil {
		t.Fatal(err)
	}
	defer otherLn.Close()
	defer os.Remove(otherSock)

	win := &window{name: "0", root: &node{weight: 1, pane: &pane{}}, active: &node{weight: 1, pane: &pane{}}}
	s := newSessionTestServer(win)
	s.name = "tile-test-self"

	s.command(tea.Key{Text: "s"})
	s.command(tea.Key{Text: "p"})
	if s.sessions == nil {
		t.Fatal("s+p did not open the session picker")
	}
	if got, want := s.sessions.names[s.sessions.sel], "tile-test-self"; got != want {
		t.Fatalf("picker highlighted %q, want the current session %q", got, want)
	}

	s.sessionPickerKey(tea.Key{Code: tea.KeyEscape})
	if s.sessions != nil {
		t.Fatal("esc did not close the picker")
	}
}

// Enter on a different session detaches the attached client with a
// MsgDetach naming that session, so the CLI's attach loop can reconnect
// there — the server itself never dials another session for a switch.
func TestSessionPickerEnterSwitchesToHighlighted(t *testing.T) {
	dir, err := proto.SessionDir()
	if err != nil {
		t.Fatal(err)
	}
	selfSock := dir + "/tile-test-switch-self.sock"
	selfLn, err := net.Listen("unix", selfSock)
	if err != nil {
		t.Fatal(err)
	}
	defer selfLn.Close()
	defer os.Remove(selfSock)
	otherSock := dir + "/tile-test-switch-other.sock"
	otherLn, err := net.Listen("unix", otherSock)
	if err != nil {
		t.Fatal(err)
	}
	defer otherLn.Close()
	defer os.Remove(otherSock)

	win := &window{name: "0", root: &node{weight: 1, pane: &pane{}}, active: &node{weight: 1, pane: &pane{}}}
	s := newSessionTestServer(win)
	s.name = "tile-test-switch-self"
	attached := &client{out: make(chan proto.ServerMsg, 4)}
	s.cli = attached

	s.openSessionPicker()
	for s.sessions.names[s.sessions.sel] != "tile-test-switch-other" {
		s.sessionPickerKey(tea.Key{Code: tea.KeyDown})
	}
	s.sessionPickerKey(tea.Key{Code: tea.KeyEnter})

	if s.sessions != nil {
		t.Fatal("enter did not close the picker")
	}
	if s.cli != nil {
		t.Fatal("switching sessions must detach the attached client")
	}
	select {
	case m := <-attached.out:
		if m.Type != proto.MsgDetach {
			t.Fatalf("client got msg type %q, want %q", m.Type, proto.MsgDetach)
		}
		if m.Content != "tile-test-switch-other" {
			t.Fatalf("MsgDetach.Content = %q, want the target session %q", m.Content, "tile-test-switch-other")
		}
	default:
		t.Fatal("switchSession did not send anything to the attached client")
	}
}

// Selecting the session already attached is a no-op instead of a pointless
// detach/reattach round trip.
func TestSessionPickerEnterOnCurrentSessionIsNoop(t *testing.T) {
	win := &window{name: "0", root: &node{weight: 1, pane: &pane{}}, active: &node{weight: 1, pane: &pane{}}}
	s := newSessionTestServer(win)
	s.sessions = &sessionPicker{names: []string{"default"}, sel: 0}
	attached := &client{out: make(chan proto.ServerMsg, 4)}
	s.cli = attached

	s.sessionPickerKey(tea.Key{Code: tea.KeyEnter})

	if s.sessions != nil {
		t.Fatal("enter did not close the picker")
	}
	if s.cli == nil {
		t.Fatal("switching to the already-attached session must not detach the client")
	}
	select {
	case m := <-attached.out:
		t.Fatalf("switching to the current session must send nothing, got %+v", m)
	default:
	}
}

// s+o kills every other running session but leaves this one alone: it
// doesn't ask first, since it can never take down the session in front of
// the user.
func TestCloseOtherSessionsLeavesCurrentOneAlone(t *testing.T) {
	dir, err := proto.SessionDir()
	if err != nil {
		t.Fatal(err)
	}
	otherName := "tile-test-other"
	otherSock := dir + "/" + otherName + ".sock"
	ln, err := net.Listen("unix", otherSock)
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(otherSock)

	// proto.Sessions() (which closeOtherSessions calls to find the other
	// session in the first place) dials and immediately closes every
	// socket just to check it's alive, before killSession dials again to
	// actually send MsgKill — so the first connection or two may carry no
	// decodable message at all.
	kill := make(chan proto.ClientMsg, 1)
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			var m proto.ClientMsg
			err = json.NewDecoder(conn).Decode(&m)
			conn.Close()
			if err == nil {
				kill <- m
				return
			}
		}
	}()

	win := &window{name: "0", root: &node{weight: 1, pane: &pane{}}, active: &node{weight: 1, pane: &pane{}}}
	s := newSessionTestServer(win)
	s.name = "tile-test-current"

	s.command(tea.Key{Text: "s"})
	s.command(tea.Key{Text: "o"})

	select {
	case m := <-kill:
		if m.Type != proto.MsgKill {
			t.Fatalf("other session got msg type %q, want %q", m.Type, proto.MsgKill)
		}
	case <-time.After(time.Second):
		t.Fatal("closeOtherSessions did not reach the other session")
	}
	if s.done {
		t.Fatal("closeOtherSessions must not touch this session's own daemon flag")
	}
	ln.Close()
}
