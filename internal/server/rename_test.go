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

	s.command(tea.Key{Text: "p"})
	s.command(tea.Key{Text: "r"})
	if s.renamer == nil {
		t.Fatal("Rename keybind did not open the renamer")
	}
	if s.renamer.text != "zsh" {
		t.Fatalf("renamer seeded with %q, want the active pane's border title", s.renamer.text)
	}
}

// Renaming a pane must only ever affect its own border, never the window
// tab it lives in — mirrors TestTrackCommandDoesNotRenameWindowTab's
// boundary for the auto-tracked name.
func TestRenameFlowTypeBackspaceEnterUpdatesBorderNotTab(t *testing.T) {
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
	if !p.named || p.borderTitle() != "scratch" {
		t.Fatalf("pane not renamed: named=%v borderTitle=%q", p.named, p.borderTitle())
	}

	txt, _ = tabLine(s.windows, 0, 40, theme{})
	if !strings.Contains(txt, "zsh") || strings.Contains(txt, "scratch") {
		t.Fatalf("tab should keep following the pane's shell title, got %q", txt)
	}

	// New shell output must not override a manual rename.
	p.title = "vim"
	if p.borderTitle() != "scratch" {
		t.Fatalf("manual rename should survive pane title changes, got %q", p.borderTitle())
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
	if p.named {
		t.Fatal("esc must not commit the edit")
	}
}

func TestRenameEmptyRevertsToAuto(t *testing.T) {
	p := &pane{title: "zsh", named: true, borderName: "custom"}
	win := &window{name: "custom", root: &node{pane: p, weight: 1}, active: &node{pane: p, weight: 1}}
	s := newRenameTestServer(win)

	s.openRenamer()
	for range 6 { // clear "custom"
		s.renamerKey(tea.Key{Code: tea.KeyBackspace})
	}
	s.renamerKey(tea.Key{Code: tea.KeyEnter})

	if p.named {
		t.Fatal("blank rename should clear named, reverting to the pane title")
	}
	if got := p.borderTitle(); got != "zsh" {
		t.Fatalf("borderTitle() = %q after blank rename, want the shell title %q", got, "zsh")
	}
}

// Renaming a window overrides what the tab shows regardless of the active
// pane's own title, and a blank rename reverts to following it again —
// mirrors TestRenameFlowTypeBackspaceEnterUpdatesBorderNotTab's pane-side
// contract.
func TestWindowRenameFlowOverridesTabThenRevertsOnBlank(t *testing.T) {
	p := &pane{title: "zsh"}
	win := &window{name: "zsh", root: &node{pane: p, weight: 1}, active: &node{pane: p, weight: 1}}
	s := newRenameTestServer(win)

	s.openWindowRenamer()
	for range 3 { // backspace the seeded "zsh" off first
		s.renamerKey(tea.Key{Code: tea.KeyBackspace})
	}
	for _, r := range "scratch" {
		s.renamerKey(tea.Key{Text: string(r)})
	}
	s.renamerKey(tea.Key{Code: tea.KeyEnter})

	if !win.named || win.name != "scratch" {
		t.Fatalf("window not renamed: named=%v name=%q", win.named, win.name)
	}
	txt, _ := tabLine(s.windows, 0, 40, theme{})
	if !strings.Contains(txt, "scratch") {
		t.Fatalf("tab = %q, want the manual window rename", txt)
	}

	// A pane title change must not override the manual window rename.
	p.title = "vim"
	if win.displayName() != "scratch" {
		t.Fatalf("displayName() = %q, want the manual rename to survive pane title changes", win.displayName())
	}

	// Blank rename reverts to following the active pane's title again.
	s.openWindowRenamer()
	for range len([]rune("scratch")) {
		s.renamerKey(tea.Key{Code: tea.KeyBackspace})
	}
	s.renamerKey(tea.Key{Code: tea.KeyEnter})
	if win.named {
		t.Fatal("blank rename should clear named, reverting to the pane title")
	}
	if got := win.displayName(); got != "vim" {
		t.Fatalf("displayName() = %q after blank rename, want the pane's current title %q", got, "vim")
	}
}
