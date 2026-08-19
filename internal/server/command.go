package server

import (
	tea "charm.land/bubbletea/v2"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/vt"

	"yatm/internal/proto"
)

// key routes a keystroke: lock toggle first, then the prefix state machine,
// then straight through to the focused shell.
func (s *server) key(k tea.Key) {
	s.dirty = true

	// The picker is modal: every key steers it until it closes.
	if s.picker != nil {
		s.pickerKey(k)
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
	if !s.prefix {
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
		p.emu.SendKey(vt.KeyPressEvent(uv.Key(k)))
	}
}

// command runs one prefixed keystroke.
func (s *server) command(k tea.Key) {
	w := s.win()
	l := w.l
	if l == nil {
		l = s.layoutNow()
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

	switch k.Text {
	case s.km.NewWindow:
		_ = s.newWindow()
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
	case s.km.CycleLayer:
		s.cycleLayer()
	case s.km.KillPane:
		if p := s.activePane(); p != nil {
			s.paneExited(p)
		}
	case s.km.KillWindow:
		s.closeWindow(s.cur)
	case s.km.Theme:
		s.openPicker()
	case s.km.Detach:
		s.detach()
	case s.km.Quit:
		s.shutdown()
	default:
		if len(k.Text) == 1 && k.Text[0] >= '0' && k.Text[0] <= '9' {
			if i := int(k.Text[0] - '0'); i < len(s.windows) {
				s.cur = i
			}
		}
	}
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

// cycleLayer switches to the next pane stacked behind the active one, like
// flipping between layers in an image editor. No-op if the active pane is
// not part of a stack.
func (s *server) cycleLayer() {
	w := s.win()
	p := w.active.stackAncestor()
	if p == nil {
		return
	}
	p.layer = (p.activeLayer() + 1) % len(p.children)
	w.active = firstLeaf(p.children[p.layer])
	s.dirty = true
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

	if m.Kind == proto.MouseClick {
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
			w.active = leaf // plain shell: a click just moves focus
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
