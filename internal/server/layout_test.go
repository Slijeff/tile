package server

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

// tree builds a branch of n equal-weight leaves along d. The leaves carry no
// pane, which is fine: geometry never dereferences one.
func tree(d dir, n int) *node {
	b := &node{dir: d, weight: 1}
	for range n {
		c := &node{weight: 1, parent: b, pane: &pane{}}
		b.children = append(b.children, c)
	}
	return b
}

func widths(t *testing.T, root *node, r rect) []int {
	t.Helper()
	l := computeLayout(root, r, 1)
	out := make([]int, len(root.children))
	for i, c := range root.children {
		out[i] = l.rects[c].w
	}
	return out
}

func eq(t *testing.T, got, want []int, what string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("%s: got %v want %v", what, got, want)
	}
	for i := range got {
		if got[i] != want[i] {
			t.Fatalf("%s: got %v want %v", what, got, want)
		}
	}
}

func TestSizesExact(t *testing.T) {
	// 3 panes across 80 columns leaves 78 after two 1-cell gutters.
	eq(t, widths(t, tree(dirHoriz, 3), rect{0, 0, 80, 24}), []int{26, 26, 26}, "80 cols")
	// The odd cell goes to the leading pane, never lost.
	eq(t, widths(t, tree(dirHoriz, 3), rect{0, 0, 81, 24}), []int{27, 26, 26}, "81 cols")
}

func TestMarginWidensGutter(t *testing.T) {
	root := tree(dirHoriz, 2)
	l := computeLayout(root, rect{0, 0, 21, 10}, 3)

	if got, want := l.rects[root.children[0]], (rect{0, 0, 9, 10}); got != want {
		t.Fatalf("first child: got %+v, want %+v", got, want)
	}
	if got, want := l.rects[root.children[1]], (rect{12, 0, 9, 10}); got != want {
		t.Fatalf("second child should start 3 cells past the first (margin=3): got %+v, want %+v", got, want)
	}
	if len(l.seps) != 1 || l.seps[0].r.w != 3 {
		t.Fatalf("gutter separator should be margin cells wide: got %+v", l.seps)
	}
}

// The top-to-bottom split's gutter is measured in rows and the side-by-side
// split's in columns; since terminal cells run roughly twice as tall as
// they are wide, the row gutter must be about half as many cells so both
// read as the same visual thickness.
func TestVerticalGutterHalvesMargin(t *testing.T) {
	hRoot := tree(dirHoriz, 2)
	hl := computeLayout(hRoot, rect{0, 0, 21, 10}, 4)
	if len(hl.seps) != 1 || hl.seps[0].r.w != 4 {
		t.Fatalf("side-by-side gutter should stay the full margin: got %+v", hl.seps)
	}

	vRoot := tree(dirVert, 2)
	vl := computeLayout(vRoot, rect{0, 0, 10, 21}, 4)
	if len(vl.seps) != 1 || vl.seps[0].r.h != 2 {
		t.Fatalf("top-to-bottom gutter should be half the margin: got %+v", vl.seps)
	}
}

func TestResizeTransfersCells(t *testing.T) {
	b := tree(dirHoriz, 3)
	r := rect{0, 0, 80, 24}
	before := widths(t, b, r)

	resizeActive(b.children[0], dirHoriz, 5, computeLayout(b, r, 1))
	after := widths(t, b, r)

	if after[0] != before[0]+5 || after[1] != before[1]-5 {
		t.Fatalf("resize: %v -> %v, want first +5 / second -5", before, after)
	}
	if after[2] != before[2] {
		t.Fatalf("resize disturbed an uninvolved pane: %v -> %v", before, after)
	}
	if sum(after) != sum(before) {
		t.Fatalf("resize changed the total: %v -> %v", before, after)
	}
}

func TestResizeClampsAtMinimum(t *testing.T) {
	b := tree(dirHoriz, 2)
	r := rect{0, 0, 40, 24}
	before := widths(t, b, r)

	// Far more than the neighbour can give up: must be refused outright.
	resizeActive(b.children[0], dirHoriz, 100, computeLayout(b, r, 1))
	after := widths(t, b, r)

	eq(t, after, before, "over-large resize should be reverted")
	for _, w := range after {
		if w < minCell {
			t.Fatalf("pane squashed below minCell: %v", after)
		}
	}
}

func TestSplitCloseRoundTrip(t *testing.T) {
	root := &node{weight: 1, pane: &pane{}}
	r := rect{0, 0, 80, 24}

	a := split(root, dirHoriz, computeLayout(root, r, 1))
	if a == nil {
		t.Fatal("first split refused")
	}
	a.pane = &pane{}
	b := split(a, dirVert, computeLayout(root, r, 1))
	if b == nil {
		t.Fatal("second split refused")
	}
	b.pane = &pane{}
	if got := len(leaves(root)); got != 3 {
		t.Fatalf("after two splits: %d leaves, want 3", got)
	}

	closeNode(b)
	if got := len(leaves(root)); got != 2 {
		t.Fatalf("after one close: %d leaves, want 2", got)
	}
	next := closeNode(a)
	if got := len(leaves(root)); got != 1 {
		t.Fatalf("after two closes: %d leaves, want 1", got)
	}
	// The tree must collapse all the way back to a plain leaf.
	if root.pane == nil || next != root {
		t.Fatalf("tree did not collapse: root.pane=%v next=%p root=%p", root.pane, next, root)
	}
}

func TestStackShowsOnlyActiveLayer(t *testing.T) {
	orig := &pane{}
	root := &node{weight: 1, pane: orig}
	r := rect{0, 0, 80, 24}

	b := stack(root)
	b.pane = &pane{}

	l := computeLayout(root, r, 1)
	if got := len(l.leaves); got != 1 {
		t.Fatalf("a stack should expose one leaf at a time, got %d", got)
	}
	if l.leaves[0] != b {
		t.Fatal("the newest layer should be the active one")
	}
	// The other layer collapses to a one-row header above the active one,
	// so the active layer's rect shrinks by that row.
	if got, want := l.rects[b], (rect{0, 1, 80, 23}); got != want {
		t.Fatalf("the active layer should fill the rect minus the header row: got %+v, want %+v", got, want)
	}
	if len(l.headers) != 1 || l.headers[0].node.pane != orig || l.headers[0].r != (rect{0, 0, 80, 1}) {
		t.Fatalf("the inactive layer should collapse to a header row above the active one, got %+v", l.headers)
	}

	root.layer = 0
	l = computeLayout(root, r, 1)
	if l.leaves[0].pane != orig {
		t.Fatal("switching layer should change which pane is visible")
	}
}

func TestStackCloseFallsBackToRemainingLayer(t *testing.T) {
	root := &node{weight: 1, pane: &pane{}}
	b := stack(root)
	b.pane = &pane{}
	c := stack(b) // grows the same stack: root now layers 3 panes
	c.pane = &pane{}

	if got := len(leaves(root)); got != 3 {
		t.Fatalf("expected 3 layered panes, got %d", got)
	}
	if closeNode(c) == nil {
		t.Fatal("closing one layer must not empty the stack")
	}
	if got := len(leaves(root)); got != 2 {
		t.Fatalf("after closing a layer: %d panes, want 2", got)
	}
}

// A stack with more layers than the rect has rows must still leave at
// least one row for the active layer's own content, trimming the header
// farthest from it first.
func TestStackRowsTrimsFarthestHeaderWhenTooShort(t *testing.T) {
	b := &node{dir: dirStack, weight: 1}
	for range 3 {
		b.children = append(b.children, &node{parent: b, weight: 1})
	}
	b.layer = 2 // last child active

	before, after := stackRows(b, 2) // 2 rows: room for 1 header plus 1 content row
	if len(before) != 1 || before[0] != b.children[1] || len(after) != 0 {
		t.Fatalf("got before=%v after=%v, want only children[1]'s header kept", before, after)
	}
}

func TestSplitRefusedWhenTooNarrow(t *testing.T) {
	root := &node{weight: 1, pane: &pane{}}
	r := rect{0, 0, minCell * 2, 24} // one cell short of fitting two panes plus a gutter
	if split(root, dirHoriz, computeLayout(root, r, 1)) != nil {
		t.Fatal("split should be refused when there is no room")
	}
}

func TestHitTesting(t *testing.T) {
	b := tree(dirHoriz, 3)
	l := computeLayout(b, rect{0, 0, 80, 24}, 1)

	second := b.children[1]
	r := l.rects[second]
	if got := l.paneAt(r.x+2, 5); got != second {
		t.Fatalf("paneAt inside pane 2 returned the wrong leaf")
	}
	// The gutter sits immediately left of the second pane.
	sp := l.sepAt(r.x-1, 5)
	if sp == nil || sp.branch != b || sp.idx != 0 {
		t.Fatalf("sepAt did not find the first gutter: %+v", sp)
	}
	if l.paneAt(r.x-1, 5) != nil {
		t.Fatal("a gutter column must not belong to a pane")
	}
}

// With margin=0 the rendered gap between panes disappears entirely, but the
// boundary must stay draggable — sepAt should still find it, landing on the
// next child's leading border cell instead of a blank gutter cell.
func TestSepAtHitsZeroMarginBoundary(t *testing.T) {
	hb := tree(dirHoriz, 2)
	hl := computeLayout(hb, rect{0, 0, 20, 10}, 0)
	second := hb.children[1]
	r := hl.rects[second]
	if sp := hl.sepAt(r.x, 5); sp == nil || sp.branch != hb || sp.idx != 0 {
		t.Fatalf("sepAt missed the zero-margin horizontal boundary: %+v", sp)
	}

	vb := tree(dirVert, 2)
	vl := computeLayout(vb, rect{0, 0, 10, 20}, 0)
	bottom := vb.children[1]
	rv := vl.rects[bottom]
	if sp := vl.sepAt(5, rv.y); sp == nil || sp.branch != vb || sp.idx != 0 {
		t.Fatalf("sepAt missed the zero-margin vertical boundary: %+v", sp)
	}
}

func TestFitSquaresOffPanes(t *testing.T) {
	// vt.Render right-trims; every line must come back exactly w wide.
	got := fit("hi\n\x1b[31mred\x1b[m", 6, 3)
	lines := strings.Split(got, "\n")
	if len(lines) != 3 {
		t.Fatalf("got %d lines, want 3", len(lines))
	}
	for i, l := range lines {
		if w := ansi.StringWidth(l); w != 6 {
			t.Fatalf("line %d width %d, want 6 (%q)", i, w, l)
		}
	}
}

func sum(xs []int) int {
	t := 0
	for _, x := range xs {
		t += x
	}
	return t
}
