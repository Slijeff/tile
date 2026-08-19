package server

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

func newRenameTestServer(win *window) *server {
	return &server{
		windows:    []*window{win},
		w:          40,
		h:          10,
		km:         defaultKeymap,
		prefixSpec: parseKeySpec(defaultKeymap.Prefix),
		lockSpec:   parseKeySpec(defaultKeymap.Lock),
	}
}

func TestRenameKeybindOpensRenamer(t *testing.T) {
	p := &pane{title: "zsh"}
	win := &window{name: "zsh", root: &node{pane: p, weight: 1}, active: &node{pane: p, weight: 1}}
	s := newRenameTestServer(win)

	s.command(tea.Key{Text: s.km.Rename})
	if s.renamer == nil {
		t.Fatal("Rename keybind did not open the renamer")
	}
	if s.renamer.text != "zsh" {
		t.Fatalf("renamer seeded with %q, want the active pane's title", s.renamer.text)
	}
}

func TestRenameFlowTypeBackspaceEnterUpdatesTab(t *testing.T) {
	p := &pane{title: "zsh"}
	win := &window{name: "zsh", root: &node{pane: p, weight: 1}, active: &node{pane: p, weight: 1}}
	s := newRenameTestServer(win)

	txt, _ := tabLine(s.windows, 0, 40, theme{})
	if !strings.Contains(txt, "zsh") {
		t.Fatalf("expected pane title before rename, got %q", txt)
	}

	s.openRenamer()
	for range 3 { // backspace the seeded "zsh" off first
		s.renamerKey(tea.Key{Code: tea.KeyBackspace})
	}
	if s.renamer.text != "" {
		t.Fatalf("renamer.text = %q after clearing seed, want empty", s.renamer.text)
	}
	for _, r := range "scratch" {
		s.renamerKey(tea.Key{Text: string(r)})
	}
	if s.renamer.text != "scratch" {
		t.Fatalf("renamer.text = %q after typing", s.renamer.text)
	}

	s.renamerKey(tea.Key{Code: tea.KeyEnter})
	if s.renamer != nil {
		t.Fatal("enter did not close the renamer")
	}
	if !win.customName || win.name != "scratch" {
		t.Fatalf("window not renamed: customName=%v name=%q", win.customName, win.name)
	}

	txt, _ = tabLine(s.windows, 0, 40, theme{})
	if !strings.Contains(txt, "scratch") || strings.Contains(txt, "zsh") {
		t.Fatalf("tab should show the custom name, got %q", txt)
	}

	// New shell output must not override a custom name anymore.
	p.title = "vim"
	txt, _ = tabLine(s.windows, 0, 40, theme{})
	if !strings.Contains(txt, "scratch") {
		t.Fatalf("custom name should survive pane title changes, got %q", txt)
	}
}

func TestRenameEscapeCancels(t *testing.T) {
	p := &pane{title: "zsh"}
	win := &window{name: "zsh", root: &node{pane: p, weight: 1}, active: &node{pane: p, weight: 1}}
	s := newRenameTestServer(win)

	s.openRenamer()
	s.renamerKey(tea.Key{Text: "x"})
	s.renamerKey(tea.Key{Code: tea.KeyEscape})

	if s.renamer != nil {
		t.Fatal("esc did not close the renamer")
	}
	if win.customName {
		t.Fatal("esc must not commit the edit")
	}
}

func TestRenameEmptyRevertsToAuto(t *testing.T) {
	p := &pane{title: "zsh"}
	win := &window{name: "custom", customName: true, root: &node{pane: p, weight: 1}, active: &node{pane: p, weight: 1}}
	s := newRenameTestServer(win)

	s.openRenamer()
	for range 6 { // clear "custom"
		s.renamerKey(tea.Key{Code: tea.KeyBackspace})
	}
	s.renamerKey(tea.Key{Code: tea.KeyEnter})

	if win.customName {
		t.Fatal("blank rename should clear customName, reverting to the pane title")
	}
}
