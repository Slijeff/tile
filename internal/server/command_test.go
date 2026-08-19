package server

import (
	"testing"

	tea "charm.land/bubbletea/v2"
)

func newCommandTestServer(win *window) *server {
	return &server{
		windows:    []*window{win},
		w:          40,
		h:          10,
		km:         defaultKeymap,
		prefixSpec: parseKeySpec(defaultKeymap.Prefix),
		lockSpec:   parseKeySpec(defaultKeymap.Lock),
	}
}

// The leader of a layered binding (e.g. "p") must not act on its own — it
// stashes itself in s.chord and waits for the key that completes it.
func TestChordLeaderWaitsForSecondKey(t *testing.T) {
	win := &window{name: "0", root: &node{weight: 1, pane: &pane{}}, active: &node{weight: 1, pane: &pane{}}}
	s := newCommandTestServer(win)

	s.command(tea.Key{Text: "p"})
	if s.chord != "p" {
		t.Fatalf("chord = %q after the leader key, want %q", s.chord, "p")
	}
	if s.renamer != nil {
		t.Fatal("the leader key alone must not run any action")
	}
}

// A completion that doesn't match any chord cancels it silently instead of
// running the leader key standalone or leaving the chord dangling.
func TestChordCancelsOnUnknownCompletion(t *testing.T) {
	win := &window{name: "0", root: &node{weight: 1, pane: &pane{}}, active: &node{weight: 1, pane: &pane{}}}
	s := newCommandTestServer(win)

	s.command(tea.Key{Text: "p"})
	s.command(tea.Key{Text: "q"}) // "pq" binds nothing
	if s.chord != "" {
		t.Fatalf("chord = %q after an unrecognized completion, want cleared", s.chord)
	}
	if s.renamer != nil || s.picker != nil {
		t.Fatal("an unrecognized chord completion must not run any action")
	}
}

// p+r opens the renamer seeded from the active pane, same action the old
// single-key "rename" ran, but now targeting the pane's border.
func TestPaneChordOpensRenamerForActivePane(t *testing.T) {
	p := &pane{title: "zsh"}
	win := &window{name: "zsh", root: &node{pane: p, weight: 1}, active: &node{pane: p, weight: 1}}
	s := newCommandTestServer(win)

	s.command(tea.Key{Text: "p"})
	s.command(tea.Key{Text: "r"})
	if s.renamer == nil {
		t.Fatal("p+r did not open the renamer")
	}
	if s.renamer.text != "zsh" {
		t.Fatalf("renamer.text = %q, want the active pane's border title %q", s.renamer.text, "zsh")
	}

	s.renamerKey(tea.Key{Text: "!"})
	s.renamerKey(tea.Key{Code: tea.KeyEnter})
	if !p.named || p.borderTitle() != "zsh!" {
		t.Fatalf("pane not renamed: named=%v borderTitle=%q", p.named, p.borderTitle())
	}
}

// p+x kills the active pane, same action the old single-key "kill_pane" ran.
func TestPaneChordKillsActivePane(t *testing.T) {
	leftPane, err := newPane(0, 20, 10, make(chan event, 256))
	if err != nil {
		t.Fatal(err)
	}
	defer leftPane.close()
	rightPane, err := newPane(1, 20, 10, make(chan event, 256))
	if err != nil {
		t.Fatal(err)
	}
	defer rightPane.close()

	root := &node{dir: dirHoriz, weight: 1}
	left := &node{weight: 1, pane: leftPane, parent: root}
	right := &node{weight: 1, pane: rightPane, parent: root}
	root.children = []*node{left, right}
	win := &window{name: "0", root: root, active: right}
	s := newCommandTestServer(win)

	s.command(tea.Key{Text: "p"})
	s.command(tea.Key{Text: "x"})

	if len(s.windows) != 1 {
		t.Fatalf("killing one of two panes must not close the window, got %d windows", len(s.windows))
	}
	if got := leaves(s.win().root); len(got) != 1 || got[0].pane != leftPane {
		t.Fatalf("expected only the surviving pane to remain, got %d leaves", len(got))
	}
}

// Freed by the "p" pane layer, "n"/"p" no longer navigate windows —
// "]"/"[" do.
func TestNextPrevWindowUsesBrackets(t *testing.T) {
	win0 := &window{name: "0", root: &node{weight: 1, pane: &pane{}}, active: &node{weight: 1, pane: &pane{}}}
	win1 := &window{name: "1", root: &node{weight: 1, pane: &pane{}}, active: &node{weight: 1, pane: &pane{}}}
	s := newCommandTestServer(win0)
	s.windows = append(s.windows, win1)

	s.command(tea.Key{Text: "]"})
	if s.cur != 1 {
		t.Fatalf("cur = %d after \"]\", want 1 (next window)", s.cur)
	}
	s.command(tea.Key{Text: "["})
	if s.cur != 0 {
		t.Fatalf("cur = %d after \"[\", want 0 (prev window)", s.cur)
	}

	s.command(tea.Key{Text: "n"})
	if s.cur != 0 {
		t.Fatal("\"n\" must be a free key now, not next-window")
	}
	s.command(tea.Key{Text: "p"})
	if s.chord != "p" {
		t.Fatal("\"p\" must be free for the pane chord, not prev-window")
	}
}

// w+c opens the window layer and creates a new window, same action the
// old single-key "new_window" ran.
func TestWindowChordCreatesNewWindow(t *testing.T) {
	win := &window{name: "0", root: &node{weight: 1, pane: &pane{}}, active: &node{weight: 1, pane: &pane{}}}
	s := newCommandTestServer(win)

	s.command(tea.Key{Text: "w"})
	if s.chord != "w" {
		t.Fatalf("chord = %q after the leader key, want %q", s.chord, "w")
	}
	s.command(tea.Key{Text: "c"})
	if len(s.windows) != 2 {
		t.Fatalf("len(s.windows) = %d after w+c, want 2", len(s.windows))
	}
	if s.cur != 1 {
		t.Fatalf("cur = %d after creating a window, want the new one focused", s.cur)
	}
	for _, leaf := range leaves(s.windows[1].root) {
		leaf.pane.close()
	}
}

// w+& kills the active window, same action the old single-key
// "kill_window" ran.
func TestWindowChordKillsActiveWindow(t *testing.T) {
	p0, err := newPane(0, 20, 10, make(chan event, 256))
	if err != nil {
		t.Fatal(err)
	}
	defer p0.close()
	p1, err := newPane(1, 20, 10, make(chan event, 256))
	if err != nil {
		t.Fatal(err)
	}

	win0 := &window{name: "0", root: &node{weight: 1, pane: p0}, active: &node{weight: 1, pane: p0}}
	win1 := &window{name: "1", root: &node{weight: 1, pane: p1}, active: &node{weight: 1, pane: p1}}
	s := newCommandTestServer(win0)
	s.windows = append(s.windows, win1)
	s.cur = 1

	s.command(tea.Key{Text: "w"})
	s.command(tea.Key{Text: "&"})
	if len(s.windows) != 1 {
		t.Fatalf("len(s.windows) = %d after w+&, want 1", len(s.windows))
	}
	if s.windows[0] != win0 {
		t.Fatal("w+& must kill the active window, not the other one")
	}
}

// w+r opens the renamer targeting the active window's tab, seeded with
// whatever it's currently showing (the active pane's title) — mirrors
// TestPaneChordOpensRenamerForActivePane's pane-side contract.
func TestWindowChordOpensRenamerForActiveWindow(t *testing.T) {
	p := &pane{title: "zsh"}
	win := &window{name: "zsh", root: &node{pane: p, weight: 1}, active: &node{pane: p, weight: 1}}
	s := newCommandTestServer(win)

	s.command(tea.Key{Text: "w"})
	s.command(tea.Key{Text: "r"})
	if s.renamer == nil {
		t.Fatal("w+r did not open the renamer")
	}
	if !s.renamer.forWindow {
		t.Fatal("w+r must target the window, not the active pane")
	}
	if s.renamer.text != "zsh" {
		t.Fatalf("renamer.text = %q, want the window's current tab name %q", s.renamer.text, "zsh")
	}

	s.renamerKey(tea.Key{Text: "!"})
	s.renamerKey(tea.Key{Code: tea.KeyEnter})
	if !win.named || win.name != "zsh!" {
		t.Fatalf("window not renamed: named=%v name=%q", win.named, win.name)
	}
}

// Freed by the "w" window layer, plain "c"/"&" no longer create or kill a
// window on their own.
func TestNewKillWindowRequireWindowChord(t *testing.T) {
	win := &window{name: "0", root: &node{weight: 1, pane: &pane{}}, active: &node{weight: 1, pane: &pane{}}}
	s := newCommandTestServer(win)

	s.command(tea.Key{Text: "c"})
	if len(s.windows) != 1 {
		t.Fatal("\"c\" alone must be a free key now, not new-window")
	}
	s.command(tea.Key{Text: "&"})
	if len(s.windows) != 1 {
		t.Fatal("\"&\" alone must be a free key now, not kill-window")
	}
}

// A chord isn't limited to two keys: command() must keep extending it
// through as many leader keys as a user's keymap defines before dispatching.
func TestChordDispatchesAtArbitraryDepth(t *testing.T) {
	p := &pane{title: "zsh"}
	win := &window{name: "zsh", root: &node{pane: p, weight: 1}, active: &node{pane: p, weight: 1}}
	s := newCommandTestServer(win)
	s.km.Panes.Key, s.km.Panes.Rename = "pa", "b" // a 3-key chord: p, then a, then b

	s.command(tea.Key{Text: "p"})
	if s.chord != "p" {
		t.Fatalf("chord = %q after \"p\", want \"p\"", s.chord)
	}
	s.command(tea.Key{Text: "a"})
	if s.chord != "pa" {
		t.Fatalf("chord = %q after \"p\",\"a\", want \"pa\" (still waiting on \"b\")", s.chord)
	}
	if s.renamer != nil {
		t.Fatal("a partial chord must not run any action")
	}
	s.command(tea.Key{Text: "b"})
	if s.chord != "" {
		t.Fatalf("chord = %q after completing \"pab\", want cleared", s.chord)
	}
	if s.renamer == nil {
		t.Fatal("\"pab\" did not run rename once fully typed")
	}
}

// Escape must back out of a pending chord without running anything, so a
// user isn't stuck mid-chord after a wrong start.
func TestEscapeCancelsPendingChord(t *testing.T) {
	win := &window{name: "0", root: &node{weight: 1, pane: &pane{}}, active: &node{weight: 1, pane: &pane{}}}
	s := newCommandTestServer(win)

	s.command(tea.Key{Text: "p"})
	s.command(tea.Key{Code: tea.KeyEscape})
	if s.chord != "" {
		t.Fatalf("chord = %q after Escape, want cleared", s.chord)
	}
	if s.renamer != nil || s.picker != nil {
		t.Fatal("Escape must not run any pane-layer action")
	}
}

// Regression: key(), not just command(), must keep routing to the pane
// layer while a chord is pending. command() clears s.prefix as soon as the
// leader key is seen, so the real per-keystroke path (key(), which every
// actual keypress goes through) must consult s.chord too — otherwise the
// completing key falls through to the "not in prefix mode" branch and gets
// typed into the shell instead of finishing the chord.
func TestKeyRoutesThroughFullChordNotJustFirstKey(t *testing.T) {
	p, err := newPane(0, 20, 10, make(chan event, 256))
	if err != nil {
		t.Fatal(err)
	}
	defer p.close()
	win := &window{name: "0", root: &node{pane: p, weight: 1}, active: &node{pane: p, weight: 1}}
	s := newCommandTestServer(win)

	s.key(tea.Key{Code: 'b', Mod: tea.ModCtrl}) // the prefix
	if !s.prefix {
		t.Fatal("ctrl+b did not enter prefix mode")
	}
	s.key(tea.Key{Text: "p"}) // leader of the pane chord
	if s.chord != "p" {
		t.Fatalf("chord = %q after \"p\", want \"p\"", s.chord)
	}
	s.key(tea.Key{Text: "r"}) // should complete "pr"
	if s.chord != "" {
		t.Fatalf("chord = %q after \"r\", want cleared", s.chord)
	}
	if s.renamer == nil {
		t.Fatal(`"r" was sent to the shell instead of completing the pane-rename chord`)
	}
}
