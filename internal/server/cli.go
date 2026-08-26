package server

// The agent-facing surface: one-shot commands that arrive on the same socket
// the TUI attaches to, but never attach. Everything here runs on the event
// loop like any other message, so it reads and writes the same state the
// keybindings do — with no locking — and every mutation goes through the
// method the equivalent keystroke would have called.
//
// Focus follows the operation: splitting a pane focuses the new one, killing
// it focuses the survivor, exactly as pressing the key would. "yatm focus"
// is how a caller moves it back.

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"

	"yatm/internal/proto"
)

// cliCmd runs one CLI command and answers it. Every path replies exactly
// once: the client is blocked on that single message.
func (s *server) cliCmd(c *client, m proto.ClientMsg) {
	out, err := s.dispatch(m.Cmd, parseArgs(m.Args))
	reply := proto.ServerMsg{Type: proto.MsgReply, Content: out}
	if err != nil {
		reply.Err = err.Error()
	}
	c.send(reply)
}

// dispatch is the whole command surface. Adding a command means adding a case
// here — the client forwards its arguments untouched, so there is nowhere
// else to teach.
func (s *server) dispatch(cmd string, a args) (string, error) {
	switch cmd {
	case "list":
		if a.has("json") {
			return s.listJSON()
		}
		return s.listText(), nil
	case "capture":
		return s.cliCapture(a)
	case "send-keys":
		return "", s.cliSendKeys(a)
	case "split", "stack":
		return s.cliSplit(cmd, a)
	case "new-window":
		if err := s.newWindow(); err != nil {
			return "", err
		}
		s.dirty = true
		return fmt.Sprintf("@%d", s.win().id), nil
	case "kill-pane":
		return "", s.cliKillPane(a)
	case "kill-window":
		return "", s.cliKillWindow(a)
	case "focus":
		return "", s.cliFocus(a)
	case "resize":
		return "", s.cliResize(a)
	case "even":
		return "", s.cliEven(a)
	case "rename":
		return "", s.cliRename(a)
	}
	return "", fmt.Errorf("unknown command %q", cmd)
}

// --- arguments -------------------------------------------------------------

// valueFlags are the only flags that take a value, so "--enter echo hi" can't
// silently swallow the text that follows it. Everything else is a bare
// switch, and "--flag=value" works for either.
var valueFlags = map[string]bool{"lines": true, "key": true}

type args struct {
	pos   []string
	flags map[string]string
}

// parseArgs splits raw command-line arguments into positionals and flags.
// A bare "--" ends flag parsing, so text beginning with a dash can still be
// sent to a pane.
func parseArgs(in []string) args {
	a := args{flags: map[string]string{}}
	for i := 0; i < len(in); i++ {
		s := in[i]
		if s == "--" {
			a.pos = append(a.pos, in[i+1:]...)
			break
		}
		name, ok := strings.CutPrefix(s, "--")
		if !ok {
			if name, ok = strings.CutPrefix(s, "-"); !ok || name == "" {
				a.pos = append(a.pos, s)
				continue
			}
		}
		if k, v, found := strings.Cut(name, "="); found {
			a.flags[k] = v
			continue
		}
		if valueFlags[name] && i+1 < len(in) {
			a.flags[name] = in[i+1]
			i++
			continue
		}
		a.flags[name] = ""
	}
	return a
}

func (a args) has(name string) bool { _, ok := a.flags[name]; return ok }

func (a args) int(name string, def int) (int, error) {
	v, ok := a.flags[name]
	if !ok || v == "" {
		return def, nil
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return 0, fmt.Errorf("--%s: %q is not a number", name, v)
	}
	return n, nil
}

func (a args) arg(i int) string {
	if i < len(a.pos) {
		return a.pos[i]
	}
	return ""
}

// --- targets ---------------------------------------------------------------

// parseTarget reads a "%3" pane or "@1" window reference. The sigil is
// required: pane and window ids come from one counter, so a bare number would
// be a coin flip between two different objects.
func parseTarget(s string) (sigil byte, id int, err error) {
	if len(s) < 2 || (s[0] != '%' && s[0] != '@') {
		return 0, 0, fmt.Errorf("bad target %q: want %%<pane-id> or @<window-id>, as printed by \"yatm list\"", s)
	}
	id, convErr := strconv.Atoi(s[1:])
	if convErr != nil {
		return 0, 0, fmt.Errorf("bad target %q: %q is not an id", s, s[1:])
	}
	return s[0], id, nil
}

// paneArg resolves positional argument i as a pane target.
func (s *server) paneArg(a args, i int) (*window, *node, error) {
	sigil, id, err := parseTarget(a.arg(i))
	if err != nil {
		return nil, nil, err
	}
	if sigil != '%' {
		return nil, nil, fmt.Errorf("%s is a window; this command wants a pane (%%<id>)", a.arg(i))
	}
	w, n := s.findPane(id)
	if n == nil {
		return nil, nil, fmt.Errorf("no pane %%%d", id)
	}
	return w, n, nil
}

// findPane locates a pane by id, in any window — floating panes included,
// since window.panes covers both trees.
func (s *server) findPane(id int) (*window, *node) {
	for _, w := range s.windows {
		for _, leaf := range w.panes() {
			if leaf.pane.id == id {
				return w, leaf
			}
		}
	}
	return nil, nil
}

func (s *server) findWindow(id int) (int, *window) {
	for i, w := range s.windows {
		if w.id == id {
			return i, w
		}
	}
	return -1, nil
}

// floating reports whether n lives in w's floating tree rather than its tiled
// one — the two need opposite handling when focusing.
func (w *window) floating(n *node) bool {
	if w.float == nil {
		return false
	}
	for _, leaf := range leaves(w.float) {
		if leaf == n {
			return true
		}
	}
	return false
}

// focusNode makes n the pane that input and the next command act on, bringing
// its window forward and raising or dismissing the float as needed. Focusing
// a tiled pane while the float is up has to lower the float: setFocus is a
// no-op while floating, so otherwise the focus would silently not move.
func (s *server) focusNode(w *window, n *node) {
	for i, cand := range s.windows {
		if cand == w {
			s.cur = i
			break
		}
	}
	if w.floating(n) {
		w.floatOn = true
		if st := n.stackAncestor(); st != nil {
			st.layer = indexOf(st.children, n)
		}
	} else {
		w.floatOn = false
		w.active = n
		if st := n.stackAncestor(); st != nil {
			st.layer = indexOf(st.children, n)
		}
	}
	s.dirty = true
}

// --- commands --------------------------------------------------------------

func (s *server) cliCapture(a args) (string, error) {
	_, n, err := s.paneArg(a, 0)
	if err != nil {
		return "", err
	}
	lines, err := a.int("lines", n.pane.h)
	if err != nil {
		return "", err
	}
	return n.pane.capture(lines), nil
}

// cliSendKeys types into a named pane without focusing it. It goes straight to
// the pane rather than through server.key, so the prefix, lock mode and any
// open picker are all bypassed: a caller addressing a pane by id is not
// simulating a user at the keyboard.
func (s *server) cliSendKeys(a args) error {
	_, n, err := s.paneArg(a, 0)
	if err != nil {
		return err
	}
	if spec, ok := a.flags["key"]; ok {
		ks := parseKeySpec(spec)
		if ks == (keySpec{}) {
			return fmt.Errorf("--key: cannot parse %q (try \"ctrl+c\", \"escape\", \"a\")", spec)
		}
		sendKeyTo(n.pane, tea.Key{Code: ks.code, Mod: ks.mod})
	}
	if text := strings.Join(a.pos[1:], " "); text != "" {
		for _, r := range text {
			sendKeyTo(n.pane, tea.Key{Code: r, Text: string(r)})
		}
	}
	if a.has("enter") {
		sendKeyTo(n.pane, tea.Key{Code: tea.KeyEnter})
	}
	s.dirty = true
	return nil
}

// cliSplit grows the tree at a named pane. Both splitting and stacking act on
// the focused pane, so this focuses the target first — which is also where
// focus is left, matching what the keybinding does.
func (s *server) cliSplit(cmd string, a args) (string, error) {
	w, n, err := s.paneArg(a, 0)
	if err != nil {
		return "", err
	}
	s.focusNode(w, n)
	before := w.focus()
	switch {
	case cmd == "stack":
		s.stack()
	case w.floatOn:
		return "", fmt.Errorf("%s is floating: a floating terminal is one rect and is never split — stack onto it instead", a.arg(0))
	case a.has("h"):
		s.split(dirHoriz)
	case a.has("v"):
		s.split(dirVert)
	default:
		s.addPane() // whichever axis has more room
	}
	got := w.focus()
	if got == before || got == nil || got.pane == nil {
		return "", fmt.Errorf("no room to %s %s", cmd, a.arg(0))
	}
	s.dirty = true
	return fmt.Sprintf("%%%d", got.pane.id), nil
}

func (s *server) cliKillPane(a args) error {
	// paneExited hunts down the pane itself and heals whichever tree held it,
	// so there is no focus to arrange first.
	_, n, err := s.paneArg(a, 0)
	if err != nil {
		return err
	}
	s.paneExited(n.pane)
	s.dirty = true
	return nil
}

func (s *server) cliKillWindow(a args) error {
	sigil, id, err := parseTarget(a.arg(0))
	if err != nil {
		return err
	}
	if sigil != '@' {
		return fmt.Errorf("%s is a pane; this command wants a window (@<id>)", a.arg(0))
	}
	i, _ := s.findWindow(id)
	if i < 0 {
		return fmt.Errorf("no window @%d", id)
	}
	s.closeWindow(i)
	s.dirty = true
	return nil
}

// cliEven gives sibling panes an equal share. split does not do this on its
// own: splitting a pane that already has a sibling halves that pane's share,
// so two splits leave 50/25/25 rather than thirds.
//
// A pane target evens the branch that pane sits in; a window target evens
// every branch in the window.
func (s *server) cliEven(a args) error {
	sigil, id, err := parseTarget(a.arg(0))
	if err != nil {
		return err
	}
	var w *window
	if sigil == '@' {
		if _, w = s.findWindow(id); w == nil {
			return fmt.Errorf("no window @%d", id)
		}
		evenNode(w.root, true)
	} else {
		var n *node
		if w, n = s.findPane(id); n == nil {
			return fmt.Errorf("no pane %%%d", id)
		}
		if n.parent == nil {
			return nil // the window's only pane: already even
		}
		evenNode(n.parent, false)
	}
	w.l = computeLayout(w.root, s.body(), s.margin)
	s.dirty = true
	return nil
}

// evenNode gives n's children an equal share of it, recursing when deep.
// Stacked children share one rect whatever their weights, so evening a stack
// changes nothing — which is the right answer for one.
func evenNode(n *node, deep bool) {
	for _, c := range n.children {
		c.weight = 1
		if deep {
			evenNode(c, true)
		}
	}
}

// cliResize moves the border between a pane and its neighbour, in cells.
// Unlike the other mutations it leaves focus alone: nudging a border is
// purely geometric, so there is no reason to bring the pane forward to do it.
func (s *server) cliResize(a args) error {
	w, n, err := s.paneArg(a, 0)
	if err != nil {
		return err
	}
	if w.floating(n) {
		return fmt.Errorf("%s is floating: a floating terminal is one rect and has no border to move", a.arg(0))
	}
	var d dir
	var grow bool
	switch a.arg(1) {
	case "left":
		d, grow = dirHoriz, false
	case "right":
		d, grow = dirHoriz, true
	case "up":
		d, grow = dirVert, false
	case "down":
		d, grow = dirVert, true
	default:
		return fmt.Errorf("resize: want a direction (left, right, up, down), got %q", a.arg(1))
	}
	cells, convErr := strconv.Atoi(a.arg(2))
	if convErr != nil {
		return fmt.Errorf("resize: %q is not a number of cells", a.arg(2))
	}
	if !grow {
		cells = -cells
	}
	// The target may be in a window that isn't current, so lay that window
	// out rather than reaching for s.layoutNow's view of the current one.
	w.l = computeLayout(w.root, s.body(), s.margin)
	resizeActive(n, d, cells, w.l)
	w.l = computeLayout(w.root, s.body(), s.margin)
	s.dirty = true
	return nil
}

func (s *server) cliFocus(a args) error {
	w, n, err := s.paneArg(a, 0)
	if err != nil {
		return err
	}
	s.focusNode(w, n)
	return nil
}

// cliRename sets a pane's border name or a window's tab name. Both rename
// methods already treat a blank name as "clear the override", so passing no
// name reverts the target to following its shell's own title.
func (s *server) cliRename(a args) error {
	sigil, id, err := parseTarget(a.arg(0))
	if err != nil {
		return err
	}
	name := strings.Join(a.pos[1:], " ")
	if sigil == '@' {
		_, w := s.findWindow(id)
		if w == nil {
			return fmt.Errorf("no window @%d", id)
		}
		w.rename(name)
	} else {
		_, n := s.findPane(id)
		if n == nil {
			return fmt.Errorf("no pane %%%d", id)
		}
		n.pane.rename(name)
	}
	s.dirty = true
	return nil
}

// --- list ------------------------------------------------------------------

// listText reuses the pane picker's tree rendering and hangs an id off the
// front of every pane row, so what the CLI prints and what the picker draws
// can't drift apart.
func (s *server) listText() string {
	var b strings.Builder
	focus := s.win().focus()
	for wi, w := range s.windows {
		marker := "  "
		if wi == s.cur {
			marker = "* "
		}
		fmt.Fprintf(&b, "%s@%-4d %d: %s\n", marker, w.id, wi+1, w.displayName())
		for _, r := range windowTreeRows(w, wi) {
			id := "      "
			if r.node != nil && r.node.pane != nil {
				id = fmt.Sprintf("%%%-5d", r.node.pane.id)
			}
			b.WriteString("  " + id + r.label)
			if r.node != nil && r.node == focus {
				b.WriteString("   <- focused")
			}
			b.WriteByte('\n')
		}
	}
	return strings.TrimRight(b.String(), "\n")
}

// jsonNode mirrors one node of a window's split tree. Leaves carry a pane,
// branches carry children — the same shape capturePaneNode walks for presets,
// with the live details a caller needs to act on it.
type jsonNode struct {
	ID       int        `json:"id,omitempty"`  // panes only
	Dir      string     `json:"dir,omitempty"` // branches only
	Title    string     `json:"title,omitempty"`
	Name     string     `json:"name,omitempty"` // manual or auto-tracked border name
	W        int        `json:"w,omitempty"`
	H        int        `json:"h,omitempty"`
	Focused  bool       `json:"focused,omitempty"`
	Layer    int        `json:"layer,omitempty"` // stacks: which child is showing
	Children []jsonNode `json:"children,omitempty"`
}

type jsonWindow struct {
	ID      int       `json:"id"`
	Index   int       `json:"index"`
	Name    string    `json:"name"`
	Active  bool      `json:"active,omitempty"`
	Zoomed  bool      `json:"zoomed,omitempty"`
	Root    jsonNode  `json:"root"`
	Float   *jsonNode `json:"float,omitempty"` // the floating terminal, if it has one
	FloatOn bool      `json:"float_on,omitempty"`
}

func dirName(d dir) string {
	switch d {
	case dirHoriz:
		return "horiz"
	case dirVert:
		return "vert"
	case dirStack:
		return "stack"
	}
	return ""
}

func describeNode(n *node, l *layout, focus *node) jsonNode {
	jn := jsonNode{Dir: dirName(n.dir), Focused: n == focus}
	if l != nil {
		r := l.rects[n]
		jn.W, jn.H = r.w, r.h
	}
	if n.pane != nil {
		jn.ID, jn.Title = n.pane.id, n.pane.title
		if n.pane.named {
			jn.Name = n.pane.borderName
		}
		jn.Dir = ""
		return jn
	}
	if n.dir == dirStack {
		jn.Layer = n.activeLayer()
	}
	for _, c := range n.children {
		jn.Children = append(jn.Children, describeNode(c, l, focus))
	}
	return jn
}

func (s *server) listJSON() (string, error) {
	focus := s.win().focus()
	out := make([]jsonWindow, 0, len(s.windows))
	for wi, w := range s.windows {
		// Geometry is only recomputed on a frame, and a window that has never
		// been drawn has none — lay each one out so sizes are real.
		l := computeLayout(w.root, s.body(), s.margin)
		w.l = l
		jw := jsonWindow{
			ID: w.id, Index: wi + 1, Name: w.displayName(),
			Active: wi == s.cur, Zoomed: w.zoomed, FloatOn: w.floatOn,
			Root: describeNode(w.root, l, focus),
		}
		if w.float != nil {
			fl := computeLayout(w.float, floatRect(s.body()), s.margin)
			fn := describeNode(w.float, fl, focus)
			jw.Float = &fn
		}
		out = append(out, jw)
	}
	b, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		return "", err
	}
	return string(b), nil
}
