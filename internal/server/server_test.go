package server

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"tile/internal/proto"
)

func TestStatusBarShowsStackLayer(t *testing.T) {
	root := &node{weight: 1}
	b := stack(root) // root becomes a 2-layer stack; b is the active (2nd) layer
	win := &window{name: "0", root: root, active: b}
	s := &server{windows: []*window{win}, w: 80}

	got := s.statusBar()
	if !strings.Contains(got, "layer 2/2") {
		t.Fatalf("statusBar() = %q, want a layer indicator", got)
	}
}

func TestStatusBarOmitsLayerWhenNotStacked(t *testing.T) {
	win := &window{name: "0", root: &node{weight: 1}, active: &node{weight: 1}}
	s := &server{windows: []*window{win}, w: 80}

	if got := s.statusBar(); strings.Contains(got, "layer") {
		t.Fatalf("statusBar() = %q, want no layer indicator for a plain pane", got)
	}
}

func TestStatusBarModeBadge(t *testing.T) {
	win := &window{name: "0", root: &node{weight: 1}, active: &node{weight: 1}}

	cases := []struct {
		name           string
		prefix, locked bool
		chord          string
		want           string
	}{
		{"normal", false, false, "", "NORMAL"},
		{"prefix", true, false, "", "PREFIX"},
		{"locked", false, true, "", "LOCKED"},
		{"chord pending", false, false, "p", "PREFIX"},
	}
	for _, c := range cases {
		s := &server{windows: []*window{win}, w: 80, prefix: c.prefix, locked: c.locked, chord: c.chord}
		if got := s.statusBar(); !strings.Contains(got, c.want) {
			t.Fatalf("%s: statusBar() = %q, want mode badge %q", c.name, got, c.want)
		}
	}
}

// A split inside a stack converts the active layer's leaf into a branch in
// place (see split()), so the active pane's immediate parent is that branch,
// not the stack — the stack becomes its grandparent. Both the status bar's
// layer indicator and cycleLayer must walk up to find it, not just check the
// immediate parent.
func TestLayerIndicatorAndCycleSurviveSplitInsideStack(t *testing.T) {
	orig := &pane{}
	root := &node{weight: 1, pane: orig}
	r := rect{0, 0, 80, 24}

	top := stack(root)
	top.pane = &pane{}

	l := computeLayout(root, r, 1)
	right := split(top, dirHoriz, l)
	if right == nil {
		t.Fatal("split should have room in an 80-wide rect")
	}
	right.pane = &pane{}

	win := &window{name: "0", root: root, active: right}
	s := &server{windows: []*window{win}, w: 80, h: 24}

	if got := s.statusBar(); !strings.Contains(got, "layer 2/2") {
		t.Fatalf("statusBar() = %q, want a layer indicator despite the split between the active pane and its stack", got)
	}

	s.cycleLayer(true)
	if s.win().active.pane != orig {
		t.Fatal("cycleLayer should switch back to the other stack layer")
	}
}

// An arrow press that finds no spatial neighbour (a stacked pane's layers
// share one rect, so there is never one) falls back to cycling the stack
// instead of doing nothing.
func TestArrowCyclesStackLayerAtDeadEnd(t *testing.T) {
	orig := &pane{}
	root := &node{weight: 1, pane: orig}
	top := stack(root) // root becomes a 2-layer stack; top is the active (2nd) layer
	second := &pane{}
	top.pane = second

	win := &window{name: "0", root: root, active: top}
	s := &server{windows: []*window{win}, w: 80, h: 24}

	s.command(tea.Key{Code: tea.KeyLeft})
	if s.win().active.pane != orig {
		t.Fatal("left arrow at a dead end should cycle back to the previous layer")
	}

	s.command(tea.Key{Code: tea.KeyRight})
	if s.win().active.pane != second {
		t.Fatal("right arrow at a dead end should cycle forward to the next layer")
	}
}

// A real spatial neighbour still wins over layer cycling — the fallback
// must only kick in when neighbour() finds nothing.
func TestArrowStillMovesFocusBetweenSplitPanes(t *testing.T) {
	leftPane, rightPane := &pane{}, &pane{}
	root := &node{dir: dirHoriz, weight: 1}
	l1 := &node{weight: 1, pane: leftPane, parent: root}
	l2 := &node{weight: 1, pane: rightPane, parent: root}
	root.children = []*node{l1, l2}

	win := &window{name: "0", root: root, active: l1}
	s := &server{windows: []*window{win}, w: 80, h: 24}

	s.command(tea.Key{Code: tea.KeyRight})
	if s.win().active.pane != rightPane {
		t.Fatal("right arrow should move focus to the neighbouring pane, not fall back to layer cycling")
	}
}

// Zooming must resize and render only the active pane to fill the whole
// body (minus its own border), leave the other pane's size untouched (so
// unzooming needs no restore step), and put it back once zoom is toggled
// off again.
func TestZoomFillsBodyAndRestoresOnExit(t *testing.T) {
	events := make(chan event, 256)
	left, err := newPane(0, 10, 10, events)
	if err != nil {
		t.Fatal(err)
	}
	defer left.close()

	root := &node{pane: left, weight: 1}
	l := computeLayout(root, rect{0, 0, 40, 20}, 1)
	right := split(root, dirHoriz, l)
	if right == nil {
		t.Fatal("split should have room in a 40-wide rect")
	}
	leftLeaf := root.children[0] // split converts root in place; the original pane moved here
	rp, err := newPane(1, 10, 10, events)
	if err != nil {
		t.Fatal(err)
	}
	defer rp.close()
	right.pane = rp

	win := &window{name: "0", root: root, active: leftLeaf}
	s := &server{windows: []*window{win}, w: 40, h: 22} // body: 40x20

	s.frame()
	splitW, splitH := left.w, left.h
	if splitW >= 40 {
		t.Fatalf("active pane should be split, not already full width: got %d", splitW)
	}
	rightW := right.pane.w

	s.toggleZoom()
	s.frame()
	if left.w != 38 || left.h != 18 {
		t.Fatalf("zoomed active pane should fill the body inset by its border, got %dx%d, want 38x18", left.w, left.h)
	}
	if right.pane.w != rightW {
		t.Fatalf("hidden pane's size should be untouched by zoom, got %d, want %d", right.pane.w, rightW)
	}

	s.toggleZoom()
	s.frame()
	if left.w != splitW || left.h != splitH {
		t.Fatalf("unzooming should restore the active pane's split size, got %dx%d, want %dx%d", left.w, left.h, splitW, splitH)
	}
}

// While zoomed, only the active pane is on screen, so a click must not
// hit-test the (hidden) tree geometry and steal focus.
func TestMouseIgnoresTreeHitTestingWhileZoomed(t *testing.T) {
	events := make(chan event, 256)
	left, err := newPane(0, 10, 10, events)
	if err != nil {
		t.Fatal(err)
	}
	defer left.close()

	root := &node{pane: left, weight: 1}
	l := computeLayout(root, rect{0, 0, 40, 20}, 1)
	right := split(root, dirHoriz, l)
	if right == nil {
		t.Fatal("split should have room in a 40-wide rect")
	}
	leftLeaf := root.children[0] // split converts root in place; the original pane moved here
	rp, err := newPane(1, 10, 10, events)
	if err != nil {
		t.Fatal(err)
	}
	defer rp.close()
	right.pane = rp

	win := &window{name: "0", root: root, active: leftLeaf, zoomed: true}
	s := &server{windows: []*window{win}, w: 40, h: 22}
	s.frame() // establishes w.l geometry

	// This coordinate lands inside the right pane's real (hidden) rect.
	s.mouse(proto.ClientMsg{Type: proto.MsgMouse, Kind: proto.MouseClick, Mouse: tea.Mouse{X: 35, Y: 5}})
	if s.win().active != leftLeaf {
		t.Fatal("a click should not retarget focus away from the zoomed pane")
	}
}

// Clicking a stacked layer's collapsed header brings it forward, like
// clicking a background window's title bar — render, the status bar's
// layer indicator and the next click/cycle must all agree on the new
// active layer afterward.
func TestClickHeaderFocusesStackLayer(t *testing.T) {
	events := make(chan event, 256)
	bottom, err := newPane(0, 10, 10, events)
	if err != nil {
		t.Fatal(err)
	}
	defer bottom.close()

	root := &node{pane: bottom, weight: 1}
	b := stack(root) // root becomes a 2-layer stack; b is the active (2nd) layer
	top, err := newPane(1, 10, 10, events)
	if err != nil {
		t.Fatal(err)
	}
	defer top.close()
	b.pane = top

	win := &window{name: "0", root: root, active: b}
	s := &server{windows: []*window{win}, w: 20, h: 12} // body: 20x10
	s.frame()                                           // establishes w.l

	// The inactive (bottom) layer collapses to a one-row header at the top
	// of the body, row 1 in screen space (row 0 is the tab bar).
	s.mouse(proto.ClientMsg{Type: proto.MsgMouse, Kind: proto.MouseClick, Mouse: tea.Mouse{X: 0, Y: 1}})
	if s.win().active.pane != bottom {
		t.Fatalf("clicking the collapsed header should focus its layer, got pane %p, want %p", s.win().active.pane, bottom)
	}
	if got := root.activeLayer(); got != 0 {
		t.Fatalf("clicking a header should update the stack's own layer index, got %d, want 0", got)
	}
}

// A click into a pane must move focus even when the program running inside
// it has enabled mouse reporting (e.g. vim, less, htop) — the click should
// both focus the pane and be forwarded to the program.
func TestClickFocusesMouseAwarePane(t *testing.T) {
	events := make(chan event, 256)
	left, err := newPane(0, 10, 10, events)
	if err != nil {
		t.Fatal(err)
	}
	defer left.close()

	root := &node{pane: left, weight: 1}
	l := computeLayout(root, rect{0, 0, 40, 20}, 1)
	right := split(root, dirHoriz, l)
	if right == nil {
		t.Fatal("split should have room in a 40-wide rect")
	}
	leftLeaf := root.children[0] // split converts root in place; the original pane moved here
	rp, err := newPane(1, 10, 10, events)
	if err != nil {
		t.Fatal(err)
	}
	defer rp.close()
	right.pane = rp
	right.pane.mouseOn = true // the program inside asked for mouse reporting

	win := &window{name: "0", root: root, active: leftLeaf}
	s := &server{windows: []*window{win}, w: 40, h: 22} // body: 40x20
	s.frame()                                           // establishes w.l

	// This coordinate lands inside the right (mouse-aware) pane's rect.
	s.mouse(proto.ClientMsg{Type: proto.MsgMouse, Kind: proto.MouseClick, Mouse: tea.Mouse{X: 35, Y: 5}})
	if s.win().active != right {
		t.Fatalf("a click into a mouse-aware pane should still move focus, got %p, want %p", s.win().active, right)
	}
}

// While swap mode is armed, two pane clicks should trade the panes' contents
// (not their tree nodes/weights), focus the second-clicked node, and leave
// swap mode so a third click resumes normal behavior.
func TestSwapModeTradesTwoPanesOnDrag(t *testing.T) {
	events := make(chan event, 256)
	left, err := newPane(0, 10, 10, events)
	if err != nil {
		t.Fatal(err)
	}
	defer left.close()

	root := &node{pane: left, weight: 1}
	l := computeLayout(root, rect{0, 0, 40, 20}, 1)
	right := split(root, dirHoriz, l)
	if right == nil {
		t.Fatal("split should have room in a 40-wide rect")
	}
	leftLeaf := root.children[0] // split converts root in place; the original pane moved here
	rp, err := newPane(1, 10, 10, events)
	if err != nil {
		t.Fatal(err)
	}
	defer rp.close()
	right.pane = rp

	win := &window{name: "0", root: root, active: leftLeaf}
	s := &server{windows: []*window{win}, w: 40, h: 22} // body: 40x20
	s.frame() // establishes w.l

	s.toggleSwapMode()
	if !s.swapMode {
		t.Fatal("toggleSwapMode should arm swap mode")
	}

	s.mouse(proto.ClientMsg{Type: proto.MsgMouse, Kind: proto.MouseClick, Mouse: tea.Mouse{X: 5, Y: 5}}) // press on left pane
	if s.swapSrc != leftLeaf {
		t.Fatalf("pressing a pane should mark it as the drag source, got %p, want %p", s.swapSrc, leftLeaf)
	}

	s.mouse(proto.ClientMsg{Type: proto.MsgMouse, Kind: proto.MouseMotion, Mouse: tea.Mouse{X: 35, Y: 5}}) // drag onto right pane
	if s.hover != right {
		t.Fatalf("dragging onto a pane should mark it as the hover target, got %p, want %p", s.hover, right)
	}

	s.mouse(proto.ClientMsg{Type: proto.MsgMouse, Kind: proto.MouseRelease, Mouse: tea.Mouse{X: 35, Y: 5}}) // release on right pane

	if leftLeaf.pane != rp || right.pane != left {
		t.Fatalf("swap should trade pane contents, got left=%p right=%p, want left=%p right=%p", leftLeaf.pane, right.pane, rp, left)
	}
	if s.swapMode || s.swapSrc != nil || s.hover != nil {
		t.Fatal("a completed swap should leave swap mode")
	}
	if s.win().active != right {
		t.Fatalf("swap should focus the released-on node, got %p, want %p", s.win().active, right)
	}
}

// Dragging the mouse across a plain (non-mouse-aware) pane should highlight
// the text it crosses and, on release, hand the covered text to the
// attached client to put on the system clipboard.
func TestMouseDragSelectsAndCopies(t *testing.T) {
	events := make(chan event, 256)
	p, err := newPane(0, 20, 10, events)
	if err != nil {
		t.Fatal(err)
	}
	defer p.close()
	if _, err := p.emu.Write([]byte("hello world")); err != nil {
		t.Fatal(err)
	}

	win := &window{name: "0", root: &node{pane: p, weight: 1}, active: &node{pane: p, weight: 1}}
	win.active = win.root
	s := &server{windows: []*window{win}, w: 20, h: 12} // body: 20x10 at screen row 1; bordered, so content starts at (1,2)
	cli := &client{out: make(chan proto.ServerMsg, 4)}
	s.cli = cli
	s.frame() // establishes w.l

	// Row 2 in screen space is pane row 0 once the 1-cell border is
	// accounted for; columns 1-5 cover "hello".
	s.mouse(proto.ClientMsg{Type: proto.MsgMouse, Kind: proto.MouseClick, Mouse: tea.Mouse{X: 1, Y: 2, Button: tea.MouseLeft}})
	if s.selPane != p {
		t.Fatalf("a left-button press on a plain pane should start a selection drag, selPane = %p, want %p", s.selPane, p)
	}

	s.mouse(proto.ClientMsg{Type: proto.MsgMouse, Kind: proto.MouseMotion, Mouse: tea.Mouse{X: 5, Y: 2, Button: tea.MouseLeft}})
	if !p.hasSel {
		t.Fatal("dragging off the starting cell should mark a selection as active")
	}

	s.mouse(proto.ClientMsg{Type: proto.MsgMouse, Kind: proto.MouseRelease, Mouse: tea.Mouse{X: 5, Y: 2, Button: tea.MouseLeft}})
	if s.selPane != nil {
		t.Fatal("release should end the selection drag")
	}
	if !p.hasSel {
		t.Fatal("a non-empty selection should stay highlighted after release")
	}

	select {
	case m := <-cli.out:
		if m.Type != proto.MsgClipboard || m.Content != "hello" {
			t.Fatalf("got %+v, want a clipboard message with content %q", m, "hello")
		}
	default:
		t.Fatal("expected a clipboard message to be sent to the client")
	}
}

// A plain click with no drag must not leave a one-character selection behind
// or copy anything — it should behave exactly as it always has: focus.
func TestMouseClickWithoutDragDoesNotSelect(t *testing.T) {
	events := make(chan event, 256)
	p, err := newPane(0, 20, 10, events)
	if err != nil {
		t.Fatal(err)
	}
	defer p.close()

	win := &window{name: "0", root: &node{pane: p, weight: 1}}
	win.active = win.root
	s := &server{windows: []*window{win}, w: 20, h: 12}
	cli := &client{out: make(chan proto.ServerMsg, 4)}
	s.cli = cli
	s.frame()

	s.mouse(proto.ClientMsg{Type: proto.MsgMouse, Kind: proto.MouseClick, Mouse: tea.Mouse{X: 1, Y: 2, Button: tea.MouseLeft}})
	s.mouse(proto.ClientMsg{Type: proto.MsgMouse, Kind: proto.MouseRelease, Mouse: tea.Mouse{X: 1, Y: 2, Button: tea.MouseLeft}})

	if p.hasSel {
		t.Fatal("a click with no drag should not leave a selection highlighted")
	}
	select {
	case m := <-cli.out:
		t.Fatalf("a click with no drag should not send a clipboard message, got %+v", m)
	default:
	}
}

// Releasing a swap drag back on its own source pane (a click with no real
// drag) should cancel that pick without swapping, but leave swap mode armed
// so the user can retry without pressing the keybind again.
func TestSwapModeReleaseOnSourceCancelsPick(t *testing.T) {
	events := make(chan event, 256)
	left, err := newPane(0, 10, 10, events)
	if err != nil {
		t.Fatal(err)
	}
	defer left.close()

	root := &node{pane: left, weight: 1}
	win := &window{name: "0", root: root, active: root}
	s := &server{windows: []*window{win}, w: 20, h: 12}
	s.frame()

	s.toggleSwapMode()
	s.mouse(proto.ClientMsg{Type: proto.MsgMouse, Kind: proto.MouseClick, Mouse: tea.Mouse{X: 5, Y: 5}})
	s.mouse(proto.ClientMsg{Type: proto.MsgMouse, Kind: proto.MouseRelease, Mouse: tea.Mouse{X: 5, Y: 5}})

	if !s.swapMode {
		t.Fatal("releasing on the source pane should leave swap mode armed for a retry")
	}
	if s.swapSrc != nil || s.hover != nil {
		t.Fatal("releasing on the source pane should clear the pending pick")
	}
}
