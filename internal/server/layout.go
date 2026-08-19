package server

import (
	"strings"

	"github.com/charmbracelet/x/ansi"
)

type dir int

const (
	dirNone  dir = iota // leaf
	dirHoriz            // children sit side by side
	dirVert             // children run top to bottom
	dirStack            // children share one rect; only the active layer shows
)

// minCell is the smallest width or height a pane may be squeezed down to.
const minCell = 3

// node is one cell of a window's split tree: either a leaf holding a pane, or
// a branch holding children laid out along dir.
type node struct {
	pane     *pane // non-nil => leaf
	dir      dir
	children []*node
	weight   float64 // this node's share of its parent's axis
	parent   *node
	layer    int // dirStack only: index of the child currently shown
}

// activeLayer returns the visible child's index for a dirStack node, clamped
// in case a close left it out of range.
func (n *node) activeLayer() int {
	if n.layer < 0 || n.layer >= len(n.children) {
		return 0
	}
	return n.layer
}

// stackAncestor returns the nearest dirStack ancestor of n — the stack n
// belongs to, even if a split inside that stack put a branch between them —
// or nil if n is not part of any stack.
func (n *node) stackAncestor() *node {
	for p := n.parent; p != nil; p = p.parent {
		if p.dir == dirStack {
			return p
		}
	}
	return nil
}

type rect struct{ x, y, w, h int }

func (r rect) contains(x, y int) bool {
	return x >= r.x && x < r.x+r.w && y >= r.y && y < r.y+r.h
}

// axis returns the rect's extent along d.
func (r rect) axis(d dir) int {
	if d == dirHoriz {
		return r.w
	}
	return r.h
}

// sep is a 1-cell gutter between two siblings. Dragging one resizes the pair.
type sep struct {
	branch *node
	idx    int // sits between children[idx] and children[idx+1]
	r      rect
}

// header is a collapsed (non-active) stack layer's one-row title bar —
// all that shows of it until a click or cycleLayer brings it forward.
type header struct {
	node *node
	r    rect
}

// layout is one geometry pass over a window's tree.
type layout struct {
	rects   map[*node]rect
	leaves  []*node // in tree order; only the active layer of each stack
	seps    []sep
	headers []header
}

// sizes splits avail cells between weighted siblings. Leftover cells from the
// rounding go to the leading children so the parts sum to exactly avail.
func sizes(avail int, ws []float64) []int {
	out := make([]int, len(ws))
	if len(ws) == 0 || avail <= 0 {
		return out
	}
	var total float64
	for _, w := range ws {
		total += w
	}
	if total <= 0 {
		return out
	}
	used := 0
	for i, w := range ws {
		out[i] = int(float64(avail) * w / total)
		used += out[i]
	}
	for i := 0; used < avail; i, used = i+1, used+1 {
		out[i%len(out)]++
	}
	return out
}

func computeLayout(root *node, r rect) *layout {
	l := &layout{rects: map[*node]rect{}}
	l.walk(root, r)
	return l
}

func (l *layout) walk(n *node, r rect) {
	l.rects[n] = r
	if n.pane != nil {
		l.leaves = append(l.leaves, n)
		return
	}
	if n.dir == dirStack {
		l.walkStack(n, r)
		return
	}
	ws := make([]float64, len(n.children))
	for i, c := range n.children {
		ws[i] = c.weight
	}
	gaps := len(n.children) - 1
	if n.dir == dirHoriz {
		ss := sizes(r.w-gaps, ws)
		x := r.x
		for i, c := range n.children {
			l.walk(c, rect{x, r.y, ss[i], r.h})
			x += ss[i]
			if i < gaps {
				l.seps = append(l.seps, sep{n, i, rect{x, r.y, 1, r.h}})
				x++
			}
		}
		return
	}
	ss := sizes(r.h-gaps, ws)
	y := r.y
	for i, c := range n.children {
		l.walk(c, rect{r.x, y, r.w, ss[i]})
		y += ss[i]
		if i < gaps {
			l.seps = append(l.seps, sep{n, i, rect{r.x, y, r.w, 1}})
			y++
		}
	}
}

// stackRows splits a stack's children into the ones that collapse to a
// header row above the active layer and the ones below it, trimming the
// oldest header first when there isn't room for all of them plus at least
// one row for the active layer's own content.
func stackRows(n *node, h int) (before, after []*node) {
	active := n.activeLayer()
	before, after = n.children[:active], n.children[active+1:]
	for len(before)+len(after)+1 > h {
		switch {
		case len(after) > 0:
			after = after[:len(after)-1]
		case len(before) > 0:
			before = before[1:]
		default:
			return
		}
	}
	return
}

// walkStack lays a stack's geometry out zellij-style: every layer but the
// active one collapses to a single header row, in the order it sits in
// n.children, and the active layer gets everything left over. Its rect
// still lands in l.rects/l.leaves via the ordinary l.walk, so resizing,
// spatial neighbour search and cycling a dead-end arrow all keep treating
// a stack as a single pane; the header rows are purely a rendering and
// click-target concern, recorded separately in l.headers.
func (l *layout) walkStack(n *node, r rect) {
	before, after := stackRows(n, r.h)
	y := r.y
	for _, c := range before {
		l.headers = append(l.headers, header{c, rect{r.x, y, r.w, 1}})
		y++
	}
	ah := r.h - len(before) - len(after)
	l.walk(n.children[n.activeLayer()], rect{r.x, y, r.w, ah})
	y += ah
	for _, c := range after {
		l.headers = append(l.headers, header{c, rect{r.x, y, r.w, 1}})
		y++
	}
}

// paneAt returns the leaf covering a screen coordinate.
func (l *layout) paneAt(x, y int) *node {
	for _, leaf := range l.leaves {
		if l.rects[leaf].contains(x, y) {
			return leaf
		}
	}
	return nil
}

// sepAt returns the gutter covering a screen coordinate.
func (l *layout) sepAt(x, y int) *sep {
	for i := range l.seps {
		if l.seps[i].r.contains(x, y) {
			return &l.seps[i]
		}
	}
	return nil
}

// headerAt returns the collapsed stack layer whose title bar covers a
// screen coordinate, so a click can bring it to the front.
func (l *layout) headerAt(x, y int) *node {
	for _, h := range l.headers {
		if h.r.contains(x, y) {
			return h.node
		}
	}
	return nil
}

// render draws the tree as one frame. Every pane is squared off to its exact
// rect first, so blocks line up by construction, then boxed with its own
// border — so the gap a split or stack leaves between siblings only needs
// to stay blank, not draw a second divider on top of it.
func render(n *node, l *layout, active *node, th theme) string {
	r := l.rects[n]
	if n.pane != nil {
		return borderPane(n.pane, r, n == active, th)
	}
	if n.dir == dirStack {
		before, after := stackRows(n, r.h)
		rows := make([]string, 0, len(before)+1+len(after))
		for _, c := range before {
			rows = append(rows, collapsedHeader(c, r.w, th, false))
		}
		rows = append(rows, render(n.children[n.activeLayer()], l, active, th))
		for _, c := range after {
			rows = append(rows, collapsedHeader(c, r.w, th, true))
		}
		return strings.Join(rows, "\n")
	}
	parts := make([]string, len(n.children))
	for i, c := range n.children {
		parts[i] = render(c, l, active, th)
	}
	if n.dir == dirVert {
		return strings.Join(parts, "\n"+strings.Repeat(" ", r.w)+"\n")
	}
	return joinH(parts, r.h)
}

// joinH glues equal-height blocks side by side across a blank gutter.
func joinH(parts []string, h int) string {
	cols := make([][]string, len(parts))
	for i, p := range parts {
		cols[i] = strings.Split(p, "\n")
	}
	var b strings.Builder
	for y := range h {
		if y > 0 {
			b.WriteByte('\n')
		}
		for i, c := range cols {
			if i > 0 {
				b.WriteByte(' ')
			}
			if y < len(c) {
				b.WriteString(c[y])
			}
		}
	}
	return b.String()
}

// fit pads or truncates a rendered screen to exactly w by h cells. vt.Render
// right-trims its lines, so without this the panes to the right would shift.
func fit(s string, w, h int) string {
	lines := strings.Split(s, "\n")
	out := make([]string, h)
	for i := range out {
		var l string
		if i < len(lines) {
			l = lines[i]
		}
		// reset before padding so a pane's colours cannot bleed into its neighbour
		if d := w - ansi.StringWidth(l); d >= 0 {
			l += "\x1b[m" + strings.Repeat(" ", d)
		} else {
			l = ansi.Truncate(l, w, "") + "\x1b[m"
		}
		out[i] = l
	}
	return strings.Join(out, "\n")
}

// split adds a pane next to leaf along d: a sibling when the parent already
// runs that way, otherwise the leaf becomes a branch of two. Returns the new
// leaf, or nil when there is no room for one.
func split(leaf *node, d dir, l *layout) *node {
	if l.rects[leaf].axis(d) < minCell*2+1 {
		return nil
	}
	n := &node{weight: 1}
	if p := leaf.parent; p != nil && p.dir == d {
		i := indexOf(p.children, leaf)
		leaf.weight /= 2
		n.weight = leaf.weight
		n.parent = p
		cs := make([]*node, 0, len(p.children)+1)
		cs = append(cs, p.children[:i+1]...)
		cs = append(cs, n)
		cs = append(cs, p.children[i+1:]...)
		p.children = cs
		return n
	}
	// Convert the leaf in place, so the parent's slice needs no patching.
	moved := &node{pane: leaf.pane, weight: 1, parent: leaf}
	leaf.pane = nil
	leaf.dir = d
	leaf.children = []*node{moved, n}
	n.parent = leaf
	return n
}

// stack adds a pane layered behind leaf, sharing its exact rect rather than
// splitting it — like a new layer in an image editor. cycleLayer switches
// which one is visible. Returns the new (still empty) leaf.
func stack(leaf *node) *node {
	n := &node{}
	if p := leaf.parent; p != nil && p.dir == dirStack {
		i := indexOf(p.children, leaf)
		n.parent = p
		cs := make([]*node, 0, len(p.children)+1)
		cs = append(cs, p.children[:i+1]...)
		cs = append(cs, n)
		cs = append(cs, p.children[i+1:]...)
		p.children = cs
		p.layer = i + 1
		return n
	}
	// Convert the leaf in place, so the parent's slice needs no patching.
	moved := &node{pane: leaf.pane, parent: leaf}
	leaf.pane = nil
	leaf.dir = dirStack
	leaf.children = []*node{moved, n}
	leaf.layer = 1
	n.parent = leaf
	return n
}

// closeNode removes a leaf and collapses any branch left holding one child.
// Returns the leaf that should take focus, or nil if the window is now empty.
func closeNode(leaf *node) *node {
	p := leaf.parent
	if p == nil {
		return nil // the window's only pane
	}
	i := indexOf(p.children, leaf)
	p.children = append(p.children[:i:i], p.children[i+1:]...)
	if len(p.children) == 1 {
		c := p.children[0] // pull the survivor up, keeping p's own weight
		p.pane, p.dir, p.children, p.layer = c.pane, c.dir, c.children, c.layer
		for _, g := range p.children {
			g.parent = p
		}
		return firstLeaf(p)
	}
	if p.dir == dirStack {
		switch {
		case p.layer > i:
			p.layer--
		case p.layer >= len(p.children):
			p.layer = len(p.children) - 1
		}
		return firstLeaf(p.children[p.layer])
	}
	if i >= len(p.children) {
		i = len(p.children) - 1
	}
	return firstLeaf(p.children[i])
}

// resizeActive moves the border between the active pane and a neighbour by
// delta cells along d. Sizes are relative, so this is a weight transfer; it is
// reverted wholesale if it would squash any sibling below minCell.
func resizeActive(active *node, d dir, delta int, l *layout) {
	n := active
	for n.parent != nil && n.parent.dir != d {
		n = n.parent
	}
	b := n.parent
	if b == nil {
		return // nothing splits that way
	}
	i := indexOf(b.children, n)
	j := i + 1
	if j >= len(b.children) {
		j, delta = i-1, -delta // last child: move its leading border instead
	}
	if j < 0 {
		return
	}
	resizeChildren(b, i, j, delta, l.rects[b].axis(d))
}

func resizeChildren(b *node, i, j, delta, extent int) {
	avail := extent - (len(b.children) - 1)
	if avail <= 0 || delta == 0 {
		return
	}
	var total float64
	for _, c := range b.children {
		total += c.weight
	}
	dw := float64(delta) * total / float64(avail)
	oi, oj := b.children[i].weight, b.children[j].weight
	b.children[i].weight, b.children[j].weight = oi+dw, oj-dw

	ws := make([]float64, len(b.children))
	for k, c := range b.children {
		ws[k] = c.weight
	}
	for _, s := range sizes(avail, ws) {
		if s < minCell {
			b.children[i].weight, b.children[j].weight = oi, oj
			return
		}
	}
}

// neighbour finds the nearest leaf in a direction, using the drawn rects.
func neighbour(l *layout, active *node, d dir, forward bool) *node {
	a := l.rects[active]
	var best *node
	bestDist := 1 << 30
	for _, leaf := range l.leaves {
		if leaf == active {
			continue
		}
		r := l.rects[leaf]
		var dist int
		if d == dirHoriz {
			if !overlaps(a.y, a.h, r.y, r.h) {
				continue
			}
			dist = r.x - a.x
		} else {
			if !overlaps(a.x, a.w, r.x, r.w) {
				continue
			}
			dist = r.y - a.y
		}
		if !forward {
			dist = -dist
		}
		if dist > 0 && dist < bestDist {
			best, bestDist = leaf, dist
		}
	}
	return best
}

func overlaps(a, alen, b, blen int) bool { return a < b+blen && b < a+alen }

// nextLeaf cycles to the following pane in tree order.
func nextLeaf(l *layout, active *node) *node {
	for i, leaf := range l.leaves {
		if leaf == active {
			return l.leaves[(i+1)%len(l.leaves)]
		}
	}
	return active
}

func firstLeaf(n *node) *node {
	for n != nil && n.pane == nil {
		if len(n.children) == 0 {
			return nil
		}
		i := 0
		if n.dir == dirStack {
			i = n.activeLayer()
		}
		n = n.children[i]
	}
	return n
}

// leaves collects every pane-bearing node under n, in tree order.
func leaves(n *node) []*node {
	if n.pane != nil {
		return []*node{n}
	}
	var out []*node
	for _, c := range n.children {
		out = append(out, leaves(c)...)
	}
	return out
}

func indexOf(ns []*node, n *node) int {
	for i, c := range ns {
		if c == n {
			return i
		}
	}
	return -1
}
