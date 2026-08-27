package server

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"tile/internal/proto"
)

// floatServer builds a one-window, one-tiled-pane server sized so the body
// is 80x24 — the float lands at 60x18 offset (10, 4) in screen space.
func floatServer(t *testing.T) (*server, *node) {
	t.Helper()
	events := make(chan event, 256)
	p, err := newPane(0, 80, 24, events)
	if err != nil {
		t.Fatal(err)
	}
	root := &node{pane: p, weight: 1}
	win := &window{name: "0", root: root, active: root}
	s := &server{windows: []*window{win}, w: 80, h: 26, nextID: 1, events: events, margin: 1}
	t.Cleanup(func() {
		for _, w := range s.windows {
			for _, leaf := range w.panes() {
				leaf.pane.close()
			}
		}
	})
	return s, root
}

// Opening the float centers it over the body at three quarters of each axis,
// hands it focus, and sizes its shell to that rect's interior — not to
// anything in the tiled tree. Toggling it off returns focus to the tiled
// pane while leaving the floating shell running for the next toggle.
func TestFloatCentersSizesAndKeepsShellAcrossToggle(t *testing.T) {
	s, tiled := floatServer(t)

	s.toggleFloat()
	if !s.win().floatOn || s.win().float == nil {
		t.Fatal("toggleFloat should open a floating terminal")
	}
	fp := s.win().float.pane
	if s.activePane() != fp {
		t.Fatal("the float should take focus when opened")
	}
	if got, want := floatRect(s.body()), (rect{10, 4, 60, 18}); got != want {
		t.Fatalf("floatRect = %+v, want %+v (centered, three quarters of an 80x24 body)", got, want)
	}

	s.frame()
	if fp.w != 58 || fp.h != 16 {
		t.Fatalf("floating shell sized %dx%d, want 58x16 (its rect inset by its border)", fp.w, fp.h)
	}

	s.toggleFloat()
	if s.win().floatOn {
		t.Fatal("a second toggle should hide the float")
	}
	if s.activePane() != tiled.pane {
		t.Fatal("hiding the float should hand focus back to the tiled pane")
	}
	if s.win().float == nil || s.win().float.pane != fp {
		t.Fatal("hiding the float must leave its shell running for the next toggle")
	}

	s.toggleFloat()
	if s.activePane() != fp {
		t.Fatal("re-toggling should bring the same floating shell back with focus")
	}
}

// A floating terminal is one rect: splitting it or zooming it would have to
// subdivide or discard that rect, so both are refused outright and the tiled
// tree they would otherwise act on is left alone.
func TestFloatRefusesSplitAndZoom(t *testing.T) {
	s, _ := floatServer(t)
	s.toggleFloat()
	w := s.win()

	s.split(dirHoriz)
	s.split(dirVert)
	s.addPane()
	if w.float.pane == nil || len(w.float.children) != 0 {
		t.Fatalf("the float was subdivided: dir=%v children=%d", w.float.dir, len(w.float.children))
	}
	if len(leaves(w.root)) != 1 {
		t.Fatalf("a refused float split leaked into the tiled tree: %d tiled panes", len(leaves(w.root)))
	}

	s.toggleZoom()
	if w.zoomed {
		t.Fatal("zoom must be refused while the float owns focus")
	}
}

// Stacking is the one way a floating terminal may grow, since a stack shares
// its parent's rect instead of carving it up. The new layer takes focus, the
// status bar counts the layers, and an arrow — which has no neighbour to
// reach inside a float — flips between them.
func TestFloatStacksLayersAndArrowsCycleThem(t *testing.T) {
	s, _ := floatServer(t)
	s.toggleFloat()
	w := s.win()
	first := w.float.pane

	s.stack()
	if w.float.dir != dirStack || len(w.float.children) != 2 {
		t.Fatalf("stacking a float gave dir=%v with %d children, want a 2-layer stack", w.float.dir, len(w.float.children))
	}
	second := w.focus().pane
	if second == nil || second == first {
		t.Fatal("the stacked floating layer should have its own shell and hold focus")
	}
	if got := s.statusBar(); !strings.Contains(got, "layer 2/2") || !strings.Contains(got, "float") {
		t.Fatalf("statusBar() = %q, want both a float badge and \"layer 2/2\"", got)
	}

	s.command(tea.Key{Code: tea.KeyLeft})
	if s.activePane() != first {
		t.Fatal("an arrow inside the float should flip to the previous layer")
	}
	s.command(tea.Key{Code: tea.KeyRight})
	if s.activePane() != second {
		t.Fatal("an arrow inside the float should flip to the next layer")
	}

	// The tiled tree must not have been touched by any of it.
	if len(leaves(w.root)) != 1 {
		t.Fatalf("floating stack leaked into the tiled tree: %d tiled panes", len(leaves(w.root)))
	}
}

// Killing a floating layer falls back to the one below it; killing the last
// one closes the float entirely and returns focus to the tiled pane, without
// taking the window down with it.
func TestFloatCloseFallsBackThenReleasesFocus(t *testing.T) {
	s, tiled := floatServer(t)
	s.toggleFloat()
	w := s.win()
	first := w.float.pane
	s.stack()

	s.paneExited(s.activePane()) // kill the top floating layer
	if !w.floatOn || w.float == nil {
		t.Fatal("killing one of two floating layers should leave the float open")
	}
	if s.activePane() != first {
		t.Fatal("killing a floating layer should fall back to the layer below it")
	}

	s.paneExited(s.activePane()) // kill the last one
	if w.float != nil || w.floatOn || w.fl != nil {
		t.Fatal("killing the last floating layer should close the float")
	}
	if s.activePane() != tiled.pane {
		t.Fatal("closing the float should return focus to the tiled pane")
	}
	if len(s.windows) != 1 {
		t.Fatalf("closing the float must not close the window: %d windows left", len(s.windows))
	}
}

// The float owns the mouse while it is up: a click landing outside it is
// swallowed rather than reaching the tiled pane it covers, and a click
// inside it stays inside.
func TestFloatSwallowsClicksOutsideIt(t *testing.T) {
	s, _ := floatServer(t)
	w := s.win()
	l := computeLayout(w.root, s.body(), s.margin)
	right := split(w.root, dirHoriz, l)
	if right == nil {
		t.Fatal("split should have room in an 80-wide body")
	}
	rp, err := newPane(9, 10, 10, s.events)
	if err != nil {
		t.Fatal(err)
	}
	right.pane = rp
	leftLeaf := w.root.children[0]
	w.active = leftLeaf

	s.toggleFloat()
	s.frame() // establishes w.l and w.fl
	fp := w.float.pane

	// (78, 2) is inside the right tiled pane and outside the 60x18 float.
	s.mouse(proto.ClientMsg{Type: proto.MsgMouse, Kind: proto.MouseClick, Mouse: tea.Mouse{X: 78, Y: 2}})
	if w.active != leftLeaf {
		t.Fatal("a click outside the float must not retarget the tiled tree underneath it")
	}
	if s.activePane() != fp {
		t.Fatal("a click outside the float must not steal focus from it")
	}

	// Dead center is inside the float.
	s.mouse(proto.ClientMsg{Type: proto.MsgMouse, Kind: proto.MouseClick, Mouse: tea.Mouse{X: 40, Y: 13}})
	if s.activePane() != fp {
		t.Fatal("a click inside the float should keep focus there")
	}
}

// The float is stamped over the finished tiled body: its rows carry its own
// content, the rows above it are untouched tiled output, and every body line
// keeps its exact width so nothing downstream shifts.
func TestFrameStampsFloatOverBodyKeepingWidths(t *testing.T) {
	s, _ := floatServer(t)
	s.toggleFloat()
	s.win().float.pane.rename("scratch")

	lines := strings.Split(s.frame().Content, "\n")
	if len(lines) != s.h {
		t.Fatalf("frame has %d lines, want %d", len(lines), s.h)
	}
	fr := floatRect(s.body())
	if got := lines[fr.y]; !strings.Contains(got, "scratch") {
		t.Fatalf("float's top border row %d = %q, want the float's own title", fr.y, got)
	}
	if got := lines[fr.y-1]; strings.Contains(got, "scratch") {
		t.Fatalf("row %d above the float should be untouched tiled output, got %q", fr.y-1, got)
	}
	for i, l := range lines[1 : s.h-1] {
		if got := ansi.StringWidth(l); got != s.w {
			t.Fatalf("body line %d width %d, want %d", i+1, got, s.w)
		}
	}
}
