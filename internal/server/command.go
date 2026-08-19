package server

import (
	"strings"

	tea "charm.land/bubbletea/v2"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/vt"

	"yatm/internal/proto"
)

// key routes a keystroke: lock toggle first, then the prefix state machine
// (a pending chord counts as still being in it), then straight through to
// the focused shell.
func (s *server) key(k tea.Key) {
	s.dirty = true

	// The picker and rename prompt are modal: every key steers them until
	// they close.
	if s.picker != nil {
		s.pickerKey(k)
		return
	}
	if s.renamer != nil {
		s.renamerKey(k)
		return
	}

	// Lock is never forwarded; it is the way back out of lock mode.
	if s.lockSpec.matches(k) {
		s.locked = !s.locked
		s.prefix = false
		return
	}
	if s.locked {
		s.sendKey(k)
		return
	}
	if !s.prefix && s.chord == "" {
		if s.prefixSpec.matches(k) {
			s.prefix = true
			return
		}
		s.sendKey(k)
		return
	}
	s.prefix = false
	s.command(k)
}

// sendKey hands the keystroke to the emulator, which encodes it for the shell
// in whatever cursor/keypad mode the running program has asked for. Typing
// jumps the pane back to live output, like any other terminal.
func (s *server) sendKey(k tea.Key) {
	if p := s.activePane(); p != nil {
		p.scroll = 0
		p.trackCommand(k)
		p.emu.SendKey(vt.KeyPressEvent(uv.Key(k)))
	}
}

// command runs one prefixed keystroke. A key that leads a layered binding
// (e.g. "p" leading to "pr") doesn't run immediately: it extends s.chord
// and waits for the next keystroke, which extends or completes it in turn —
// so a chord can be any number of keys deep, not just two.
func (s *server) command(k tea.Key) {
	w := s.win()
	l := w.l
	if l == nil {
		l = s.layoutNow()
	}

	if s.chord != "" {
		if k.Code == tea.KeyEscape {
			s.chord = ""
			return
		}
		s.match(s.chord+k.Text, w, l)
		return
	}

	// Arrows move between panes; with a modifier they move the border instead.
	if d, fwd, ok := arrow(k.Code); ok {
		step := 0
		switch {
		case k.Mod&tea.ModCtrl != 0:
			step = 1
		case k.Mod&tea.ModAlt != 0:
			step = 5
		}
		if step == 0 {
			if n := neighbour(l, w.active, d, fwd); n != nil {
				w.active = n
			} else if w.active.stackAncestor() != nil {
				// No spatial neighbour that way: a stacked pane has none, so
				// treat the dead end as a request to cycle its layer instead.
				s.cycleLayer(fwd)
			}
			return
		}
		if !fwd {
			step = -step
		}
		resizeActive(w.active, d, step, l)
		return
	}

	// Prefix twice sends the prefix itself through.
	if s.prefixSpec.matches(k) {
		s.sendKey(k)
		return
	}

	s.match(k.Text, w, l)
}

// match runs seq if it completes a binding, extends s.chord if seq leads a
// longer one, or — falling back to digit window-select at the top level —
// drops it. This is the whole which-key state machine: called with a
// single key or a chord-in-progress plus one more key, either way.
func (s *server) match(seq string, w *window, l *layout) {
	if s.run(seq, w, l) {
		s.chord = ""
		return
	}
	if s.km.leads(seq) {
		s.chord = seq
		return
	}
	s.chord = ""
	if len(seq) == 1 && seq[0] >= '0' && seq[0] <= '9' {
		if i := int(seq[0] - '0'); i < len(s.windows) {
			s.cur = i
		}
	}
}

// leads reports whether seq is a strict prefix of some longer binding
// (e.g. "p" leads "pr" and "px", and "pr" would in turn lead "prx" if a
// keymap bound one), meaning the next keystroke should extend the chord
// rather than cancel it.
func (km keymap) leads(seq string) bool {
	for _, spec := range km.bindings() {
		if len(spec) > len(seq) && strings.HasPrefix(spec, seq) {
			return true
		}
	}
	return false
}

// run executes the action bound to seq — a single key or a completed
// chord — and reports whether one matched.
func (s *server) run(seq string, w *window, l *layout) bool {
	switch seq {
	case s.km.NextWindow:
		s.cur = (s.cur + 1) % len(s.windows)
	case s.km.PrevWindow:
		s.cur = (s.cur - 1 + len(s.windows)) % len(s.windows)
	case s.km.CyclePane:
		w.active = nextLeaf(l, w.active)
	case s.km.NewPane:
		s.addPane()
	case s.km.SplitHoriz:
		s.split(dirHoriz)
	case s.km.SplitVert:
		s.split(dirVert)
	case s.km.Stack:
		s.stack()
	case s.km.Zoom:
		s.toggleZoom()
	case s.km.Windows.Key + s.km.Windows.New:
		_ = s.newWindow()
	case s.km.Windows.Key + s.km.Windows.Kill:
		s.closeWindow(s.cur)
	case s.km.Windows.Key + s.km.Windows.Rename:
		s.openWindowRenamer()
	case s.km.Panes.Key + s.km.Panes.Kill:
		if p := s.activePane(); p != nil {
			s.paneExited(p)
		}
	case s.km.Panes.Key + s.km.Panes.Rename:
		s.openRenamer()
	case s.km.Theme:
		s.openPicker()
	case s.km.Detach:
		s.detach()
	case s.km.Quit:
		s.shutdown()
	default:
		return false
	}
	return true
}

func arrow(c rune) (d dir, forward, ok bool) {
	switch c {
	case tea.KeyLeft:
		return dirHoriz, false, true
	case tea.KeyRight:
		return dirHoriz, true, true
	case tea.KeyUp:
		return dirVert, false, true
	case tea.KeyDown:
		return dirVert, true, true
	}
	return dirNone, false, false
}

// split adds a pane beside the active one and starts a shell in it.
func (s *server) split(d dir) {
	w := s.win()
	if w.l == nil {
		s.layoutNow()
	}
	n := split(w.active, d, w.l)
	if n == nil {
		return // not enough room
	}
	// Lay out again so the new pane's shell starts at its real size.
	l := s.layoutNow()
	r := l.rects[n]
	p, err := newPane(s.nextID, max(r.w, 1), max(r.h, 1), s.events)
	if err != nil {
		closeNode(n)
		s.layoutNow()
		return
	}
	s.nextID++
	n.pane = p
	w.active = n
	s.layoutNow()
}

// stack adds a pane layered behind the active one, sharing its rect rather
// than splitting it, and starts a shell in it.
func (s *server) stack() {
	w := s.win()
	if w.l == nil {
		s.layoutNow()
	}
	n := stack(w.active)
	l := s.layoutNow()
	r := l.rects[n]
	p, err := newPane(s.nextID, max(r.w, 1), max(r.h, 1), s.events)
	if err != nil {
		closeNode(n)
		s.layoutNow()
		return
	}
	s.nextID++
	n.pane = p
	w.active = n
	s.layoutNow()
}

// cycleLayer switches to the next (forward) or previous pane stacked behind
// the active one, like flipping between layers in an image editor. No-op if
// the active pane is not part of a stack.
func (s *server) cycleLayer(forward bool) {
	w := s.win()
	p := w.active.stackAncestor()
	if p == nil {
		return
	}
	n := len(p.children)
	if forward {
		p.layer = (p.activeLayer() + 1) % n
	} else {
		p.layer = (p.activeLayer() - 1 + n) % n
	}
	w.active = firstLeaf(p.children[p.layer])
	s.dirty = true
}

// focusLayer makes n's own leaf — or, if n is itself a branch, its first
// leaf — the window's active pane, and, when n sits inside a stack, brings
// that layer to the front so render, the status bar and the next click or
// cycle all agree on which one that is. Used for a plain click and for
// clicking a collapsed stack layer's header to bring it forward.
func (s *server) focusLayer(n *node) {
	w := s.win()
	w.active = firstLeaf(n)
	p := n.stackAncestor()
	if p == nil {
		return
	}
	c := n
	for c.parent != p {
		c = c.parent
	}
	p.layer = indexOf(p.children, c)
}

// addPane splits the active pane along whichever axis currently has more
// room, so there's no need to pick between split-horiz and split-vert.
func (s *server) addPane() {
	w := s.win()
	l := w.l
	if l == nil {
		l = s.layoutNow()
	}
	d := dirHoriz
	if r := l.rects[w.active]; r.h > r.w {
		d = dirVert
	}
	s.split(d)
}

// toggleZoom flips the window's zoom flag. Zoomed, the active pane's node
// still lives at its normal place in the tree — only rendering and pane
// sizing treat it specially — so unzooming needs no restore step, and
// switching w.active while zoomed (arrows, cycle-pane, …) just changes
// which pane fills the window.
func (s *server) toggleZoom() {
	w := s.win()
	w.zoomed = !w.zoomed
	s.dirty = true
}

func (s *server) detach() {
	if s.cli != nil {
		s.cli.send(proto.ServerMsg{Type: proto.MsgDetach})
		s.cli = nil
	}
}

// scrollLines is how far one wheel tick moves a scrolled-back pane.
const scrollLines = 3

// mouse either drags a separator, focuses a pane, or forwards the event to the
// program running inside the pane if it asked for mouse reporting.
func (s *server) mouse(m proto.ClientMsg) {
	w := s.win()
	l := w.l
	if l == nil {
		l = s.layoutNow()
	}
	s.dirty = true
	mo := m.Mouse

	// A drag in progress owns every event until the button comes back up.
	if s.drag != nil {
		d := s.drag.branch.dir
		pos := mo.X
		if d == dirVert {
			pos = mo.Y
		}
		switch m.Kind {
		case proto.MouseMotion:
			resizeChildren(s.drag.branch, s.drag.idx, s.drag.idx+1,
				pos-s.dragPos, l.rects[s.drag.branch].axis(d))
			s.dragPos = pos
		case proto.MouseRelease:
			s.drag = nil
		}
		return
	}

	// Row 0 is the tab bar, not part of the pane tree: a click there
	// switches windows, anything else on that row is ignored.
	if mo.Y == 0 {
		if m.Kind == proto.MouseClick {
			for _, t := range s.tabs {
				if mo.X >= t.lo && mo.X < t.hi {
					s.cur = t.win
					break
				}
			}
		}
		return
	}

	// Zoomed: only the active pane is visible, filling the whole body, so
	// route every event straight to it instead of hit-testing the (hidden)
	// tree geometry.
	if w.zoomed {
		leaf := w.active
		if leaf == nil || leaf.pane == nil {
			return
		}
		if !leaf.pane.mouseOn {
			if m.Kind == proto.MouseWheel {
				switch mo.Button {
				case tea.MouseWheelUp:
					leaf.pane.scrollBy(scrollLines)
				case tea.MouseWheelDown:
					leaf.pane.scrollBy(-scrollLines)
				}
			}
			return
		}
		r := s.body()
		mo.X, mo.Y = mo.X-r.x, mo.Y-r.y
		leaf.pane.emu.SendMouse(toVTMouse(m.Kind, mo))
		return
	}

	if m.Kind == proto.MouseClick {
		if h := l.headerAt(mo.X, mo.Y); h != nil {
			s.focusLayer(h) // clicking a collapsed stack layer's header brings it forward
			return
		}
		if sp := l.sepAt(mo.X, mo.Y); sp != nil {
			grabbed := *sp // the layout is rebuilt every frame; keep a copy
			s.drag = &grabbed
			s.dragPos = mo.X
			if sp.branch.dir == dirVert {
				s.dragPos = mo.Y
			}
			return
		}
	}

	leaf := l.paneAt(mo.X, mo.Y)
	if leaf == nil || leaf.pane == nil {
		return
	}
	if !leaf.pane.mouseOn {
		switch m.Kind {
		case proto.MouseClick:
			s.focusLayer(leaf) // plain shell: a click just moves focus
		case proto.MouseWheel:
			switch mo.Button {
			case tea.MouseWheelUp:
				leaf.pane.scrollBy(scrollLines)
			case tea.MouseWheelDown:
				leaf.pane.scrollBy(-scrollLines)
			}
		}
		return
	}
	r := l.rects[leaf]
	mo.X, mo.Y = mo.X-r.x, mo.Y-r.y
	leaf.pane.emu.SendMouse(toVTMouse(m.Kind, mo))
}

func toVTMouse(kind string, m tea.Mouse) vt.Mouse {
	u := uv.Mouse(m)
	switch kind {
	case proto.MouseRelease:
		return vt.MouseRelease(u)
	case proto.MouseMotion:
		return vt.MouseMotion(u)
	case proto.MouseWheel:
		return vt.MouseWheel(u)
	}
	return vt.MouseClick(u)
}
