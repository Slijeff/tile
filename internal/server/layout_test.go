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
	l := computeLayout(root, r)
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

func TestResizeTransfersCells(t *testing.T) {
	b := tree(dirHoriz, 3)
	r := rect{0, 0, 80, 24}
	before := widths(t, b, r)

	resizeActive(b.children[0], dirHoriz, 5, computeLayout(b, r))
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
	resizeActive(b.children[0], dirHoriz, 100, computeLayout(b, r))
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

	a := split(root, dirHoriz, computeLayout(root, r))
	if a == nil {
		t.Fatal("first split refused")
	}
	a.pane = &pane{}
	b := split(a, dirVert, computeLayout(root, r))
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

	l := computeLayout(root, r)
	if got := len(l.leaves); got != 1 {
		t.Fatalf("a stack should expose one leaf at a time, got %d", got)
	}
	if l.leaves[0] != b {
		t.Fatal("the newest layer should be the active one")
	}
	if got := l.rects[b]; got != r {
		t.Fatalf("the active layer should take the whole rect: %+v", got)
	}

	root.layer = 0
	l = computeLayout(root, r)
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

func TestSplitRefusedWhenTooNarrow(t *testing.T) {
	root := &node{weight: 1, pane: &pane{}}
	r := rect{0, 0, minCell * 2, 24} // one cell short of fitting two panes plus a gutter
	if split(root, dirHoriz, computeLayout(root, r)) != nil {
		t.Fatal("split should be refused when there is no room")
	}
}

func TestHitTesting(t *testing.T) {
	b := tree(dirHoriz, 3)
	l := computeLayout(b, rect{0, 0, 80, 24})

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
