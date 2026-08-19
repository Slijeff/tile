package server

import (
	"strings"
	"testing"
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
		want           string
	}{
		{"normal", false, false, "NORMAL"},
		{"prefix", true, false, "PREFIX"},
		{"locked", false, true, "LOCKED"},
	}
	for _, c := range cases {
		s := &server{windows: []*window{win}, w: 80, prefix: c.prefix, locked: c.locked}
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

	l := computeLayout(root, r)
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

	s.cycleLayer()
	if s.win().active.pane != orig {
		t.Fatal("cycleLayer should switch back to the other stack layer")
	}
}
