package server

// The floating terminal is a second, tiny tree per window, laid out in a
// centered rect stamped over the tiled one instead of inside it. Keeping it
// out of w.root is the whole trick: the tiled layout, its gutters, spatial
// neighbour search, resize maths and zoom never see a floating pane, and the
// floating tree in turn is only ever a lone leaf or a dirStack of layers —
// never a split — so its rect is never subdivided.

// floatFrac is the share of each body axis the floating terminal covers,
// as a fraction (num/den) so the maths stays integral. Three quarters
// leaves enough of the tiled layout showing to see what it floats over.
const floatNum, floatDen = 3, 4

// floatRect centers the floating terminal in the body.
func floatRect(b rect) rect {
	w := clamp(b.w*floatNum/floatDen, minCell, b.w)
	h := clamp(b.h*floatNum/floatDen, minCell, b.h)
	return rect{b.x + (b.w-w)/2, b.y + (b.h-h)/2, w, h}
}

// focus returns the node that owns input: the floating tree's active layer
// while the float is up, otherwise the tiled tree's active pane. Everything
// that acts on "the pane the user is looking at" — sending keys, renaming,
// killing, the status bar's layer indicator, the cursor position — goes
// through here rather than reading w.active directly.
//
// The float needs no active-layer field of its own: it is only ever a lone
// leaf or a stack of layers, and firstLeaf already descends a stack's own
// layer index, so there is no second copy of "which layer" to keep in sync.
func (w *window) focus() *node {
	if w.floatOn {
		return firstLeaf(w.float)
	}
	return w.active
}

// setFocus points the tiled tree at n. Focusing a floating layer is the
// same act as bringing it forward — see focus — so its callers set the
// stack's layer index and this has nothing left to do.
func (w *window) setFocus(n *node) {
	if w.floatOn {
		return
	}
	w.active = n
}

// panes lists every pane the window owns, tiled and floating, for teardown.
func (w *window) panes() []*node {
	ns := leaves(w.root)
	if w.float != nil {
		ns = append(ns, leaves(w.float)...)
	}
	return ns
}

// toggleFloat shows or hides the window's floating terminal, starting its
// shell the first time. Hiding it leaves every floating pane running and
// sized as it was, exactly like detaching — the next toggle brings the same
// shells back — and hands focus straight back to the tiled pane that had it.
func (s *server) toggleFloat() {
	w := s.win()
	s.dirty = true
	if w.float != nil {
		w.floatOn = !w.floatOn
		return
	}
	r := contentRect(floatRect(s.body()))
	p, err := newPane(s.nextID, max(r.w, 1), max(r.h, 1), s.events)
	if err != nil {
		return
	}
	s.nextID++
	w.float = &node{pane: p, weight: 1}
	w.floatOn = true
	s.floatGeom()
}

// floatGeom recomputes the floating tree's geometry in its centered rect,
// the floating counterpart of layoutNow. nil when the window has nothing
// floating to lay out.
func (s *server) floatGeom() *layout {
	w := s.win()
	if w.float == nil {
		w.fl = nil
		return nil
	}
	w.fl = computeLayout(w.float, floatRect(s.body()), s.margin)
	return w.fl
}

// floatExited tears down a dead floating pane: the layer below takes over,
// and the float itself disappears once its last layer is gone, dropping
// focus back to the tiled tree. Reports whether p was floating at all, so
// paneExited can stop before hunting through the tiled tree for it.
func (s *server) floatExited(w *window, p *pane) bool {
	if w.float == nil {
		return false
	}
	for _, leaf := range leaves(w.float) {
		if leaf.pane != p {
			continue
		}
		if closeNode(leaf) == nil { // it was the float's last layer
			w.float, w.floatOn, w.fl = nil, false, nil
		}
		p.close()
		s.dirty = true
		return true
	}
	return false
}
