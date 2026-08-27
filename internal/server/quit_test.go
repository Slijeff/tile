package server

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

// Quit takes the daemon and every shell in it down, so the binding raises a
// confirmation instead of acting: nothing is torn down until the y arrives.
func TestQuitAsksBeforeShuttingDown(t *testing.T) {
	events := make(chan event, 256)
	p, err := newPane(0, 20, 6, events)
	if err != nil {
		t.Fatal(err)
	}
	root := &node{pane: p, weight: 1}
	s := newCommandTestServer(&window{name: "0", root: root, active: root})

	s.command(tea.Key{Text: defaultKeymap.Quit})
	if !s.quitting {
		t.Fatal("quit should raise the confirmation dialog")
	}
	if s.done {
		t.Fatal("quit must not shut the daemon down before it is confirmed")
	}

	s.key(tea.Key{Text: "y"})
	if s.quitting {
		t.Fatal("confirming should close the dialog")
	}
	if !s.done {
		t.Fatal("y should shut the daemon down")
	}
}

// Anything but a y backs out — a stray keystroke landing on this dialog can
// neither end the session nor leave the user stuck in front of it.
func TestQuitDialogCancelsOnAnythingButY(t *testing.T) {
	for _, k := range []tea.Key{
		{Code: tea.KeyEscape},
		{Text: "n"},
		{Text: "q"},
		{Code: tea.KeyEnter},
		{Text: "x"},
	} {
		root := &node{pane: &pane{}, weight: 1}
		s := newCommandTestServer(&window{name: "0", root: root, active: root})
		s.command(tea.Key{Text: defaultKeymap.Quit})

		s.key(k)
		if s.quitting {
			t.Fatalf("%+v should close the dialog", k)
		}
		if s.done {
			t.Fatalf("%+v must not shut the daemon down", k)
		}
	}
}

// While the dialog is up it is modal: keys steer it instead of reaching the
// shell, so the y that confirms can't also be typed into a pane.
func TestQuitDialogSwallowsKeysAndDrawsItself(t *testing.T) {
	events := make(chan event, 256)
	p, err := newPane(0, 20, 6, events)
	if err != nil {
		t.Fatal(err)
	}
	defer p.close()
	root := &node{pane: p, weight: 1}
	s := newCommandTestServer(&window{name: "0", root: root, active: root})
	s.w, s.h = 60, 12

	s.command(tea.Key{Text: defaultKeymap.Quit})
	if got := s.frame().Content; !strings.Contains(got, "quit tile?") {
		t.Fatalf("frame does not show the quit dialog:\n%s", got)
	}

	s.key(tea.Key{Text: "z"}) // cancels, and must not reach the shell
	if len(p.cmdBuf) != 0 {
		t.Fatalf("a key answering the dialog reached the pane: cmdBuf = %q", string(p.cmdBuf))
	}
}
