package server

import (
	"encoding/json"
	"fmt"
	"net"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/charmbracelet/x/ansi"

	"yatm/internal/proto"
)

// Event kinds arriving on the server's single event channel.
const (
	evOutput = iota // a pane produced output
	evExit          // a pane's shell died
	evClient        // a message from a connected client
	evGone          // a client's connection dropped
	evSignal        // SIGTERM/SIGINT
)

type event struct {
	kind int
	pane *pane
	data []byte
	msg  proto.ClientMsg
	cli  *client
}

type client struct {
	conn net.Conn
	out  chan proto.ServerMsg
}

// send drops the frame rather than blocking the event loop on a slow client.
func (c *client) send(m proto.ServerMsg) {
	select {
	case c.out <- m:
	default:
	}
}

type window struct {
	// id addresses the window from the CLI. It comes from the same counter as
	// pane ids, so no window and pane ever share a number, and unlike the
	// window's index it does not shift when an earlier window closes.
	id     int
	name   string
	named  bool // name was set by a manual rename, overriding the active pane's title
	root   *node
	active *node
	l      *layout // geometry from the last frame; mouse and resize read it
	zoomed bool    // active pane fills the window, hiding the rest of the tree

	// The floating terminal, laid out over the tiled tree rather than in
	// it — see float.go. float is nil until one is opened and again once
	// its last layer dies; while floatOn it is what owns input.
	float   *node
	floatOn bool    // shown, and holding focus
	fl      *layout // floating geometry from the last frame; mouse reads it
}

// displayName returns what the window's tab should show: a manual rename
// if it has one, otherwise the active pane's own title — the same source
// tabLine used before window rename existed. w.name only ever holds a
// manual rename, so there is nothing else to fall back to.
func (w *window) displayName() string {
	if w.named {
		return w.name
	}
	if w.active != nil && w.active.pane != nil {
		return w.active.pane.title
	}
	return ""
}

// rename sets the window's manual tab name. A blank name clears the
// override, reverting the tab to following the active pane's title again —
// the same reset a manual pane rename gives that pane's border.
func (w *window) rename(name string) {
	if name = strings.TrimSpace(name); name == "" {
		w.named, w.name = false, ""
		return
	}
	w.name, w.named = truncateTitle(name), true
}

// server owns every pane, tree and mode flag. All of it is touched from the
// event loop goroutine only, which is why none of it is locked.
type server struct {
	sock    string
	ln      net.Listener
	events  chan event
	windows []*window
	cur     int
	w, h    int
	cli     *client
	prefix  bool   // the prefix key was pressed; the next key is a command
	chord   string // first key of a layered binding (e.g. "p"), waiting on its second
	locked  bool
	drag    *sep // separator being dragged, if any
	dragPos int
	dirty   bool
	nextID  int
	done    bool

	km         keymap
	prefixSpec keySpec
	lockSpec   keySpec

	theme   theme       // current colorscheme, used for all chrome rendering
	margin  int         // blank gutter cells between panes, from config
	themes  []theme     // colorschemes offered by the picker
	picker  *picker     // open colorscheme picker, nil when closed
	renamer *renamer    // open pane/window rename prompt, nil when closed
	panes   *panePicker // open floating pane picker, nil when closed

	presetPrompt *presetPrompt // open save-preset name prompt, nil when closed
	presetList   *presetList   // open load-preset picker, nil when closed

	quitting bool // the quit confirmation is up, waiting on a y

	tabs []tabHit // column ranges from the last frame's tab bar; mouse reads it
}

// RunServer runs the yatm daemon: one event loop owning every window, pane
// and mode flag, until the last window closes or it is told to shut down.
func RunServer() error {
	sock, err := proto.SocketPath()
	if err != nil {
		return err
	}
	ln, err := net.Listen("unix", sock)
	if err != nil {
		return err
	}
	cfg := loadConfig()
	margin := max(cfg.Margin, 0)
	s := &server{
		sock:       sock,
		ln:         ln,
		events:     make(chan event, 256),
		w:          80,
		h:          24,
		km:         cfg.Keymap,
		prefixSpec: parseKeySpec(cfg.Keymap.Prefix),
		lockSpec:   parseKeySpec(cfg.Keymap.Lock),
		theme:      resolveTheme(cfg.Theme),
		themes:     catppuccinThemes,
		margin:     margin,
		// Ids start at 1 so that 0 always means "no id": the CLI's JSON omits
		// a zero id, which is how a branch node is told from a pane.
		nextID: 1,
	}
	if err := s.newWindow(); err != nil {
		return err
	}
	go s.accept()

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGTERM, syscall.SIGINT)
	go func() {
		<-sig
		s.events <- event{kind: evSignal}
	}()

	s.loop()
	_ = ln.Close()
	_ = os.Remove(sock)
	return nil
}

func (s *server) accept() {
	for {
		conn, err := s.ln.Accept()
		if err != nil {
			return
		}
		c := &client{conn: conn, out: make(chan proto.ServerMsg, 4)}
		go c.write()
		go s.read(c)
	}
}

func (c *client) write() {
	enc := json.NewEncoder(c.conn)
	for m := range c.out {
		if err := enc.Encode(m); err != nil {
			return
		}
	}
}

// read pumps one connection's messages onto the event loop. Connecting does
// not attach: a client that wants the frames says so with MsgAttach, so a
// one-shot CLI command on the same socket leaves the attached session alone.
func (s *server) read(c *client) {
	dec := json.NewDecoder(c.conn)
	for {
		var m proto.ClientMsg
		if err := dec.Decode(&m); err != nil {
			s.events <- event{kind: evGone, cli: c}
			_ = c.conn.Close()
			return
		}
		s.events <- event{kind: evClient, msg: m, cli: c}
	}
}

// loop is the whole server: one goroutine, one owner of the state. Frames are
// pushed on a tick so a chatty pane cannot cause a render per byte.
func (s *server) loop() {
	tick := time.NewTicker(time.Second / 60)
	defer tick.Stop()
	for !s.done {
		select {
		case e := <-s.events:
			s.handle(e)
		case <-tick.C:
			if s.dirty && s.cli != nil && s.h > 2 {
				s.cli.send(s.frame())
				s.dirty = false
			}
		}
	}
}

func (s *server) handle(e event) {
	switch e.kind {
	case evOutput:
		// While scrolled back, keep the viewport pinned to the same history
		// instead of drifting as new lines push into the scrollback buffer.
		before := e.pane.emu.ScrollbackLen()
		_, _ = e.pane.emu.Write(e.data)
		if e.pane.scroll > 0 {
			e.pane.scroll += e.pane.emu.ScrollbackLen() - before
		}
		s.dirty = true
	case evExit:
		s.paneExited(e.pane)
	case evGone:
		if s.cli == e.cli {
			s.cli = nil
		}
	case evClient:
		s.client(e.cli, e.msg)
	case evSignal:
		s.shutdown()
	}
}

func (s *server) client(c *client, m proto.ClientMsg) {
	switch m.Type {
	case proto.MsgAttach:
		if s.cli != nil && s.cli != c {
			// ponytail: one client at a time, so there is no smallest-size
			// negotiation. Mirroring needs a per-client layout and frame.
			s.cli.send(proto.ServerMsg{Type: proto.MsgDetach})
		}
		s.cli = c
		s.dirty = true
	case proto.MsgCmd:
		s.cliCmd(c, m)
	case proto.MsgResize:
		if m.W > 0 && m.H > 0 {
			s.w, s.h = m.W, m.H
			s.dirty = true
		}
	case proto.MsgKey:
		s.key(m.Key)
	case proto.MsgMouse:
		s.mouse(m)
	case proto.MsgDetach:
		s.cli = nil
	case proto.MsgKill:
		s.shutdown()
	}
}

func (s *server) win() *window { return s.windows[s.cur] }

// body is the tree's rect: a row is reserved above it for the tab bar and
// one below for the status bar.
func (s *server) body() rect { return rect{0, 1, s.w, s.h - 2} }

// tabBar renders the window-switcher row and records the clicked ranges.
func (s *server) tabBar() string {
	txt, hits := tabLine(s.windows, s.cur, s.w, s.theme)
	s.tabs = hits
	return txt
}

// layoutNow returns fresh geometry, for the paths that run between frames.
func (s *server) layoutNow() *layout {
	w := s.win()
	w.l = computeLayout(w.root, s.body(), s.margin)
	return w.l
}

func (s *server) activePane() *pane {
	n := s.win().focus()
	if n == nil {
		return nil
	}
	return n.pane
}

// frame lays the window out, syncs pane sizes to it, and renders. Zoomed,
// the active pane is resized and drawn to fill the whole body instead of its
// tree rect; every other pane keeps its normal size, unresized and hidden,
// so unzooming needs no restore step. The floating terminal, if it is up, is
// laid out and stamped over the finished tiled body last, so it covers
// whatever it overlaps.
func (s *server) frame() proto.ServerMsg {
	w := s.win()
	l := computeLayout(w.root, s.body(), s.margin)
	w.l = l
	bd := s.body()
	for _, leaf := range l.leaves {
		r := l.rects[leaf]
		if w.zoomed && leaf == w.active {
			r = bd
		}
		cr := contentRect(r)
		leaf.pane.resize(cr.w, cr.h)
	}
	// While the float is up it owns focus, so no tiled pane draws itself as
	// the focused one — two accent borders would be a lie about where the
	// next keystroke lands.
	tiledActive := w.active
	if w.floatOn {
		tiledActive = nil
	}
	var body string
	activeRect, hasActive := l.rects[w.active]
	if w.zoomed && w.active != nil && w.active.pane != nil {
		body = borderPane(w.active.pane, bd, tiledActive != nil, s.theme)
		activeRect, hasActive = bd, true
	} else {
		body = render(w.root, l, tiledActive, s.theme)
	}
	if w.floatOn && w.float != nil {
		fl, fr := s.floatGeom(), floatRect(bd)
		for _, leaf := range fl.leaves {
			cr := contentRect(fl.rects[leaf])
			leaf.pane.resize(cr.w, cr.h)
		}
		fa := w.focus()
		body = overlayAt(body, bd.w, fr.x-bd.x, fr.y-bd.y,
			strings.Split(render(w.float, fl, fa, s.theme), "\n"))
		if r, ok := fl.rects[fa]; ok {
			activeRect, hasActive = r, true
		}
	}
	switch {
	case s.quitting:
		body = overlayCenter(body, bd.w, bd.h, quitBox(s.theme))
	case s.picker != nil:
		body = overlayCenter(body, bd.w, bd.h, pickerBox(themeNames(s.themes), s.picker.sel, s.theme))
	case s.panes != nil:
		body = overlayCenter(body, bd.w, bd.h, panePickerBox(s.panes, s.theme, bd.w, bd.h))
	case s.renamer != nil:
		body = overlayCenter(body, bd.w, bd.h, renameBox(s.renamer.text, s.renamer.forWindow, s.theme))
	case s.presetPrompt != nil:
		body = overlayCenter(body, bd.w, bd.h, presetPromptBox(s.presetPrompt.text, s.theme))
	case s.presetList != nil:
		body = overlayCenter(body, bd.w, bd.h, presetListBox(s.presetList, s.km, s.theme))
	case s.chord != "":
		body = overlay(body, bd.w, bd.h, chordBox(s.chord, s.km, s.theme))
	case s.prefix:
		body = overlay(body, bd.w, bd.h, helpBox(s.km, s.theme))
	}
	m := proto.ServerMsg{Type: proto.MsgFrame, Content: s.tabBar() + "\n" + body + "\n" + s.statusBar()}
	if focus := w.focus(); hasActive && focus != nil && focus.pane != nil {
		c := focus.pane.emu.CursorPosition()
		cr := contentRect(activeRect)
		m.CurX, m.CurY, m.CurVis = cr.x+c.X, cr.y+c.Y, focus.pane.curVis
	}
	return m
}

// statusBar is a colored badge for the current mode (normal/prefix/locked)
// on the left, and window/layer position on the right.
func (s *server) statusBar() string {
	mode, badge := "NORMAL", s.theme.Green
	switch {
	case s.locked:
		mode, badge = "LOCKED", s.theme.Red
	case s.prefix || s.chord != "":
		mode, badge = "PREFIX", s.theme.Yellow
	}
	left := " " + mode + " "

	var right string
	w := s.win()
	if f := w.focus(); f != nil {
		if p := f.stackAncestor(); p != nil {
			right += fmt.Sprintf("layer %d/%d  ", p.activeLayer()+1, len(p.children))
		}
	}
	if w.floatOn {
		right += "float  "
	}
	if w.zoomed {
		right += "zoom  "
	}
	right += fmt.Sprintf("window %d/%d ", s.cur+1, len(s.windows))

	fill := s.w - ansi.StringWidth(left) - 1 - ansi.StringWidth(right)
	if fill < 0 {
		fill = 0
	}
	line := fmt.Sprintf("\x1b[1m%s%s%s\x1b[22m%s %s%s\x1b[m",
		fg(s.theme.Base), bg(badge), left,
		fg(s.theme.Subtext)+bg(s.theme.Mantle), strings.Repeat(" ", fill), right)
	if ansi.StringWidth(line) > s.w {
		line = ansi.Truncate(line, s.w, "")
	}
	return line
}

func (s *server) newWindow() error {
	h := s.h - 2
	if h < 1 {
		h = 1
	}
	p, err := newPane(s.nextID, s.w, h, s.events)
	if err != nil {
		return err
	}
	s.nextID++
	root := &node{pane: p, weight: 1}
	s.windows = append(s.windows, &window{id: s.nextID, root: root, active: root})
	s.nextID++
	s.cur = len(s.windows) - 1
	s.dirty = true
	return nil
}

// paneExited tears down a dead pane and heals the tree around it — the
// floating tree first, since a floating pane is not in w.root at all.
func (s *server) paneExited(p *pane) {
	for wi, w := range s.windows {
		if s.floatExited(w, p) {
			return
		}
		for _, leaf := range leaves(w.root) {
			if leaf.pane != p {
				continue
			}
			next := closeNode(leaf)
			if next == nil {
				s.closeWindow(wi) // it was the window's last pane
				return
			}
			p.close()
			// closeNode may fold a branch away, orphaning the active node.
			if indexOf(leaves(w.root), w.active) < 0 {
				w.active = next
			}
			s.dirty = true
			return
		}
	}
}

func (s *server) closeWindow(i int) {
	if i < 0 || i >= len(s.windows) {
		return
	}
	for _, leaf := range s.windows[i].panes() {
		leaf.pane.close()
	}
	s.windows = append(s.windows[:i:i], s.windows[i+1:]...)
	if len(s.windows) == 0 {
		s.shutdown()
		return
	}
	if s.cur >= len(s.windows) {
		s.cur = len(s.windows) - 1
	}
	s.dirty = true
}

func (s *server) shutdown() {
	if s.done {
		return
	}
	s.done = true
	for _, w := range s.windows {
		for _, leaf := range w.panes() {
			leaf.pane.close()
		}
	}
	if s.cli != nil {
		s.cli.send(proto.ServerMsg{Type: proto.MsgDetach})
		time.Sleep(50 * time.Millisecond) // let the goodbye reach the client
	}
}
