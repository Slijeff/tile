package server

import (
	"encoding/json"
	"strings"
	"testing"

	"tile/internal/proto"
)

// testPane starts a real pane, the way every other test here does — a live
// PTY and emulator, so close, SendKey and the scrollback all behave.
func testPane(t *testing.T, id int) *pane {
	t.Helper()
	p, err := newPane(id, 40, 10, make(chan event, 256))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(p.close)
	return p
}

// newCLITestServer builds a session with one window holding one pane: pane %1
// in window @2, the ids the CLI addresses them by.
func newCLITestServer(t *testing.T) *server {
	t.Helper()
	root := &node{pane: testPane(t, 1), weight: 1}
	return &server{
		windows: []*window{{id: 2, root: root, active: root}},
		events:  make(chan event, 256),
		w:       40,
		h:       12,
		nextID:  3,
		km:      defaultKeymap,
	}
}

// A one-shot command must not displace whoever is attached. This is the whole
// reason attaching became an explicit message rather than a side effect of
// connecting.
func TestCommandConnectionDoesNotDetachAttachedClient(t *testing.T) {
	s := newCLITestServer(t)
	attached := &client{out: make(chan proto.ServerMsg, 4)}
	tool := &client{out: make(chan proto.ServerMsg, 4)}

	s.client(attached, proto.ClientMsg{Type: proto.MsgAttach})
	if s.cli != attached {
		t.Fatal("MsgAttach must make that client the attached one")
	}

	s.client(tool, proto.ClientMsg{Type: proto.MsgCmd, Cmd: "list"})
	if s.cli != attached {
		t.Fatal("a CLI command stole the session from the attached client")
	}
	select {
	case m := <-attached.out:
		t.Fatalf("attached client was sent %q; it must not be disturbed", m.Type)
	default:
	}
	if len(tool.out) != 1 {
		t.Fatalf("tool got %d replies, want exactly 1", len(tool.out))
	}
}

// A second client asking to attach does still take over, as it always has.
func TestAttachStillReplacesTheAttachedClient(t *testing.T) {
	s := newCLITestServer(t)
	first := &client{out: make(chan proto.ServerMsg, 4)}
	second := &client{out: make(chan proto.ServerMsg, 4)}

	s.client(first, proto.ClientMsg{Type: proto.MsgAttach})
	s.client(second, proto.ClientMsg{Type: proto.MsgAttach})
	if s.cli != second {
		t.Fatal("the newer attach must take over the session")
	}
	select {
	case m := <-first.out:
		if m.Type != proto.MsgDetach {
			t.Fatalf("first client got %q, want %q", m.Type, proto.MsgDetach)
		}
	default:
		t.Fatal("the replaced client was never told to detach")
	}
}

func TestListJSONReportsIDsAndFocus(t *testing.T) {
	s := newCLITestServer(t)
	if _, err := s.dispatch("split", parseArgs([]string{"%1", "-h"})); err != nil {
		t.Fatalf("split: %v", err)
	}
	out, err := s.dispatch("list", parseArgs([]string{"--json"}))
	if err != nil {
		t.Fatalf("list --json: %v", err)
	}

	var ws []jsonWindow
	if err := json.Unmarshal([]byte(out), &ws); err != nil {
		t.Fatalf("list --json emitted invalid JSON: %v\n%s", err, out)
	}
	if len(ws) != 1 {
		t.Fatalf("got %d windows, want 1", len(ws))
	}
	if ws[0].ID != 2 {
		t.Errorf("window id = %d, want 2", ws[0].ID)
	}
	if ws[0].Root.Dir != "horiz" {
		t.Errorf("root dir = %q after a horizontal split, want %q", ws[0].Root.Dir, "horiz")
	}

	var ids []int
	var focused int
	var walk func(jsonNode)
	walk = func(n jsonNode) {
		if len(n.Children) == 0 {
			ids = append(ids, n.ID)
		}
		if n.Focused {
			focused++
		}
		for _, c := range n.Children {
			walk(c)
		}
	}
	walk(ws[0].Root)
	if len(ids) != 2 {
		t.Fatalf("got %d panes after a split, want 2 (ids %v)", len(ids), ids)
	}
	if ids[0] == ids[1] {
		t.Errorf("both panes report id %d; ids must be unique", ids[0])
	}
	if focused != 1 {
		t.Errorf("%d panes marked focused, want exactly 1", focused)
	}
}

// The new pane's id must be the one the reply advertised, or a caller can't
// act on what it just created.
func TestSplitReturnsAResolvableID(t *testing.T) {
	s := newCLITestServer(t)
	out, err := s.dispatch("split", parseArgs([]string{"%1", "-v"}))
	if err != nil {
		t.Fatalf("split: %v", err)
	}
	if !strings.HasPrefix(out, "%") {
		t.Fatalf("split printed %q, want a %%<id>", out)
	}
	if _, err := s.dispatch("capture", parseArgs([]string{out})); err != nil {
		t.Fatalf("the id split reported (%s) does not resolve: %v", out, err)
	}
}

func TestCaptureReturnsPlainText(t *testing.T) {
	s := newCLITestServer(t)
	p := s.windows[0].root.pane
	// Bold red text: whatever comes back must not carry the escapes.
	if _, err := p.emu.Write([]byte("\x1b[1;31mhello-from-agent\x1b[m\r\n")); err != nil {
		t.Fatalf("write: %v", err)
	}

	out, err := s.dispatch("capture", parseArgs([]string{"%1"}))
	if err != nil {
		t.Fatalf("capture: %v", err)
	}
	if !strings.Contains(out, "hello-from-agent") {
		t.Fatalf("capture = %q, want it to contain the pane's text", out)
	}
	if strings.Contains(out, "\x1b") {
		t.Fatalf("capture leaked escape sequences: %q", out)
	}
}

func TestCaptureLinesLimitsFromTheEnd(t *testing.T) {
	s := newCLITestServer(t)
	p := s.windows[0].root.pane
	for _, l := range []string{"one", "two", "three"} {
		if _, err := p.emu.Write([]byte(l + "\r\n")); err != nil {
			t.Fatalf("write: %v", err)
		}
	}
	out, err := s.dispatch("capture", parseArgs([]string{"%1", "--lines", "1"}))
	if err != nil {
		t.Fatalf("capture: %v", err)
	}
	// The last line holds the cursor and is blank, so one line back is "".
	// What matters is that it took the tail, not the head.
	if strings.Contains(out, "one") {
		t.Fatalf("--lines 1 returned %q; it must take the tail, not the start", out)
	}
}

// send-keys must reach the named pane without going through the prefix state
// machine. trackCommand naming the pane after the typed line proves it did.
func TestSendKeysTypesIntoTheNamedPane(t *testing.T) {
	s := newCLITestServer(t)
	p := s.windows[0].root.pane

	if _, err := s.dispatch("send-keys", parseArgs([]string{"%1", "--enter", "echo", "hi"})); err != nil {
		t.Fatalf("send-keys: %v", err)
	}
	if !p.named {
		t.Fatal("the typed line never reached the pane: it was not auto-named")
	}
	if p.borderName != "echo hi" {
		t.Errorf("pane named %q, want %q", p.borderName, "echo hi")
	}
	if s.prefix || s.chord != "" {
		t.Error("send-keys must not disturb the prefix state machine")
	}
}

func TestSendKeysKeySpec(t *testing.T) {
	s := newCLITestServer(t)
	if _, err := s.dispatch("send-keys", parseArgs([]string{"%1", "--key", "ctrl+c"})); err != nil {
		t.Fatalf("send-keys --key ctrl+c: %v", err)
	}
	_, err := s.dispatch("send-keys", parseArgs([]string{"%1", "--key", "not-a-key"}))
	if err == nil {
		t.Fatal("an unparseable --key must be an error, not a silently dropped keystroke")
	}
}

// Window ids are the point of having them: they must survive an earlier
// window closing, where indices would shift.
func TestWindowIDsSurviveAnEarlierWindowClosing(t *testing.T) {
	s := newCLITestServer(t)
	for range 2 {
		root := &node{pane: testPane(t, s.nextID), weight: 1}
		s.nextID++
		s.windows = append(s.windows, &window{id: s.nextID, root: root, active: root})
		s.nextID++
	}
	third := s.windows[2].id

	if _, err := s.dispatch("kill-window", parseArgs([]string{"@2"})); err != nil {
		t.Fatalf("kill-window: %v", err)
	}
	if len(s.windows) != 2 {
		t.Fatalf("got %d windows after kill-window, want 2", len(s.windows))
	}
	if i, w := s.findWindow(third); w == nil {
		t.Fatalf("window @%d vanished when an earlier window closed", third)
	} else if i != 1 {
		t.Errorf("window @%d is at index %d, want 1", third, i)
	}
}

func TestBadTargetsAreErrorsNotPanics(t *testing.T) {
	s := newCLITestServer(t)
	for _, tc := range []struct {
		name, cmd string
		args      []string
	}{
		{"bare number", "capture", []string{"3"}},
		{"unknown pane", "capture", []string{"%999"}},
		{"unknown window", "kill-window", []string{"@999"}},
		{"window given to a pane command", "capture", []string{"@2"}},
		{"pane given to a window command", "kill-window", []string{"%1"}},
		{"missing target", "capture", nil},
		{"non-numeric id", "capture", []string{"%abc"}},
		{"unknown command", "frobnicate", nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := s.dispatch(tc.cmd, parseArgs(tc.args)); err == nil {
				t.Fatalf("%s %v returned no error", tc.cmd, tc.args)
			}
		})
	}
}

// A bare flag must not swallow the text after it, and "--" must let text
// starting with a dash through.
func TestParseArgs(t *testing.T) {
	a := parseArgs([]string{"%1", "--enter", "echo", "hi"})
	if !a.has("enter") {
		t.Error("--enter not recognized")
	}
	if got := strings.Join(a.pos, " "); got != "%1 echo hi" {
		t.Errorf("positionals = %q, want %q — a bare flag ate the text", got, "%1 echo hi")
	}

	a = parseArgs([]string{"%1", "--lines", "20"})
	if n, err := a.int("lines", 0); err != nil || n != 20 {
		t.Errorf("--lines 20 = %d (%v), want 20", n, err)
	}
	a = parseArgs([]string{"%1", "--lines=20"})
	if n, err := a.int("lines", 0); err != nil || n != 20 {
		t.Errorf("--lines=20 = %d (%v), want 20", n, err)
	}
	if _, err := parseArgs([]string{"--lines=x"}).int("lines", 0); err == nil {
		t.Error("a non-numeric --lines must be an error")
	}

	a = parseArgs([]string{"%1", "--", "-n", "--lines"})
	if got := strings.Join(a.pos, " "); got != "%1 -n --lines" {
		t.Errorf("after --, positionals = %q, want %q", got, "%1 -n --lines")
	}
}

func TestRenameTargetsPanesAndWindows(t *testing.T) {
	s := newCLITestServer(t)
	if _, err := s.dispatch("rename", parseArgs([]string{"%1", "build", "watcher"})); err != nil {
		t.Fatalf("rename pane: %v", err)
	}
	if got := s.windows[0].root.pane.borderName; got != "build watcher" {
		t.Errorf("pane border name = %q, want %q", got, "build watcher")
	}
	if _, err := s.dispatch("rename", parseArgs([]string{"@2", "logs"})); err != nil {
		t.Fatalf("rename window: %v", err)
	}
	if got := s.windows[0].displayName(); got != "logs" {
		t.Errorf("window name = %q, want %q", got, "logs")
	}
	// A blank name clears the override, reverting to the shell's own title.
	if _, err := s.dispatch("rename", parseArgs([]string{"@2"})); err != nil {
		t.Fatalf("rename window to blank: %v", err)
	}
	if s.windows[0].named {
		t.Error("a blank name must clear the manual window name")
	}
}

func TestFocusMovesTheActivePane(t *testing.T) {
	s := newCLITestServer(t)
	newID, err := s.dispatch("split", parseArgs([]string{"%1", "-h"}))
	if err != nil {
		t.Fatalf("split: %v", err)
	}
	if got := s.win().focus().pane.id; got == 1 {
		t.Fatal("split must leave focus on the new pane")
	}
	if _, err := s.dispatch("focus", parseArgs([]string{"%1"})); err != nil {
		t.Fatalf("focus: %v", err)
	}
	if got := s.win().focus().pane.id; got != 1 {
		t.Errorf("focus %%1 left focus on pane %%%d", got)
	}
	if _, err := s.dispatch("focus", parseArgs([]string{newID})); err != nil {
		t.Fatalf("focus %s: %v", newID, err)
	}
}

func TestKillPaneCollapsesTheTree(t *testing.T) {
	s := newCLITestServer(t)
	newID, err := s.dispatch("split", parseArgs([]string{"%1", "-v"}))
	if err != nil {
		t.Fatalf("split: %v", err)
	}
	if _, err := s.dispatch("kill-pane", parseArgs([]string{newID})); err != nil {
		t.Fatalf("kill-pane: %v", err)
	}
	if n := len(s.windows[0].panes()); n != 1 {
		t.Fatalf("window holds %d panes after killing one of two, want 1", n)
	}
	if _, n := s.findPane(1); n == nil {
		t.Fatal("the surviving pane %1 is gone")
	}
}

// paneWidths reports the current width of every pane in the first window,
// left to right.
func paneWidths(t *testing.T, s *server) []int {
	t.Helper()
	w := s.windows[0]
	l := computeLayout(w.root, s.body(), s.margin)
	var out []int
	for _, leaf := range leaves(w.root) {
		out = append(out, l.rects[leaf].w)
	}
	return out
}

func spread(ns []int) int {
	lo, hi := ns[0], ns[0]
	for _, n := range ns {
		lo, hi = min(lo, n), max(hi, n)
	}
	return hi - lo
}

// Two splits leave 50/25/25, because splitting a pane that already has a
// sibling halves that pane's share. even is the way back to thirds.
func TestEvenEqualisesSiblingPanes(t *testing.T) {
	s := newCLITestServer(t)
	mid, err := s.dispatch("split", parseArgs([]string{"%1", "-h"}))
	if err != nil {
		t.Fatalf("split: %v", err)
	}
	if _, err := s.dispatch("split", parseArgs([]string{mid, "-h"})); err != nil {
		t.Fatalf("split: %v", err)
	}

	before := paneWidths(t, s)
	if len(before) != 3 {
		t.Fatalf("got %d panes, want 3", len(before))
	}
	if spread(before) <= 1 {
		t.Fatalf("widths %v are already even; the test is not exercising anything", before)
	}

	if _, err := s.dispatch("even", parseArgs([]string{"%1"})); err != nil {
		t.Fatalf("even: %v", err)
	}
	after := paneWidths(t, s)
	if spread(after) > 1 {
		t.Errorf("widths %v after even, want them within 1 of each other (was %v)", after, before)
	}
}

// A window target evens every branch, not just one level.
func TestEvenWindowRecursesIntoNestedSplits(t *testing.T) {
	s := newCLITestServer(t)
	right, err := s.dispatch("split", parseArgs([]string{"%1", "-h"}))
	if err != nil {
		t.Fatalf("split: %v", err)
	}
	// A nested split down the right-hand column, then skew it.
	if _, err := s.dispatch("split", parseArgs([]string{right, "-v"})); err != nil {
		t.Fatalf("split: %v", err)
	}
	if _, err := s.dispatch("resize", parseArgs([]string{right, "down", "4"})); err != nil {
		t.Fatalf("resize: %v", err)
	}

	if _, err := s.dispatch("even", parseArgs([]string{"@2"})); err != nil {
		t.Fatalf("even @2: %v", err)
	}
	w := s.windows[0]
	var check func(*node)
	check = func(n *node) {
		for _, c := range n.children {
			if c.weight != 1 {
				t.Errorf("node weight = %v after evening the window, want 1", c.weight)
			}
			check(c)
		}
	}
	check(w.root)
}

// Evening a window that holds a single pane is a no-op, not an error: the
// postcondition (siblings share equally) already holds.
func TestEvenLonePaneIsANoOp(t *testing.T) {
	s := newCLITestServer(t)
	if _, err := s.dispatch("even", parseArgs([]string{"%1"})); err != nil {
		t.Fatalf("even on a lone pane: %v", err)
	}
	if _, err := s.dispatch("even", parseArgs([]string{"%999"})); err == nil {
		t.Error("even on an unknown pane must still be an error")
	}
}
