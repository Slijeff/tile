package server

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

// buildSplitWindow makes a window whose root is a horizontal split of two
// named panes, active on the right one — the shared fixture for the tree
// and navigation tests below.
func buildSplitWindow(name string, leftTitle, rightTitle string) *window {
	root := &node{dir: dirHoriz, weight: 1}
	left := &node{weight: 1, pane: &pane{title: leftTitle}, parent: root}
	right := &node{weight: 1, pane: &pane{title: rightTitle}, parent: root}
	root.children = []*node{left, right}
	return &window{name: name, root: root, active: right}
}

// openPanePicker must flatten every window's split tree into rows — a
// header row per window plus one tree row per pane — and default the
// highlight to the current window's active pane.
func TestOpenPanePickerBuildsTreeAndHighlightsActivePane(t *testing.T) {
	win := buildSplitWindow("0", "left", "right")
	s := newCommandTestServer(win)
	s.openPanePicker()

	if s.panes == nil {
		t.Fatal("openPanePicker did not open the picker")
	}
	// header + left pane + right pane
	if len(s.panes.rows) != 3 {
		t.Fatalf("rows = %d, want 3 (header, left, right)", len(s.panes.rows))
	}
	if s.panes.rows[0].node != nil {
		t.Fatal("the window header row must not be selectable")
	}
	if !strings.HasPrefix(s.panes.rows[0].label, "1:") {
		t.Fatalf("header label %q does not start with the 1-indexed window number", s.panes.rows[0].label)
	}
	if s.panes.rows[1].node != win.root.children[0] {
		t.Fatal("row 1 must be the left pane's node")
	}
	if s.panes.rows[2].node != win.root.children[1] {
		t.Fatal("row 2 must be the right pane's node")
	}
	if s.panes.rows[s.panes.cur].node != win.active {
		t.Fatalf("cur = %d, want the row for the active (right) pane", s.panes.cur)
	}
}

// A split directly at a window's root draws no row for itself — only its
// panes, connected by the usual ├─/└─ tree glyphs — but a split nested
// under another split or stack gets its own labeled branch row, with its
// own children indented further still.
func TestPaneTreeRowsLabelsNestedBranchesNotTheRoot(t *testing.T) {
	top := &pane{title: "top"}
	left := &pane{title: "left"}
	right := &pane{title: "right"}
	branch := &node{dir: dirHoriz, weight: 1}
	leftNode := &node{weight: 1, pane: left, parent: branch}
	rightNode := &node{weight: 1, pane: right, parent: branch}
	branch.children = []*node{leftNode, rightNode}
	root := &node{dir: dirVert, weight: 1}
	topNode := &node{weight: 1, pane: top, parent: root}
	branch.parent = root
	root.children = []*node{topNode, branch}
	win := &window{name: "0", root: root, active: topNode}

	rows := windowTreeRows(win, 0)
	if len(rows) != 4 {
		t.Fatalf("rows = %+v, want 4 (top pane, nested split branch, left, right)", rows)
	}
	if rows[0].node != topNode || !strings.Contains(rows[0].label, "top") || !strings.Contains(rows[0].label, "├─") {
		t.Fatalf("row 0 = %+v, want the top pane leaf with a not-last connector", rows[0])
	}
	if rows[1].node != nil || !strings.Contains(rows[1].label, "split (side by side)") || !strings.Contains(rows[1].label, "└─") {
		t.Fatalf("row 1 = %+v, want the nested split's own branch row (last child)", rows[1])
	}
	if rows[2].node != leftNode || !strings.Contains(rows[2].label, "left") || !strings.Contains(rows[2].label, "├─") {
		t.Fatalf("row 2 = %+v, want the nested left pane", rows[2])
	}
	if rows[3].node != rightNode || !strings.Contains(rows[3].label, "right") || !strings.Contains(rows[3].label, "└─") {
		t.Fatalf("row 3 = %+v, want the nested right pane", rows[3])
	}
}

// move must land only on pane rows, skipping window headers and branch
// rows, and wrap at either end.
func TestPanePickerMoveSkipsHeadersAndBranches(t *testing.T) {
	win0 := buildSplitWindow("0", "a", "b")
	win1 := &window{name: "1", root: &node{weight: 1, pane: &pane{title: "solo"}}}
	win1.active = win1.root
	s := newCommandTestServer(win0)
	s.windows = append(s.windows, win1)
	s.openPanePicker()

	start := s.panes.cur // the active pane, "b"
	s.panes.move(1)
	if s.panes.rows[s.panes.cur].node == nil {
		t.Fatal("move(1) landed on a non-selectable row")
	}
	if s.panes.rows[s.panes.cur].label != "  solo" {
		t.Fatalf("move(1) from the last pane of window 0 = %q, want window 1's solo pane", s.panes.rows[s.panes.cur].label)
	}
	s.panes.move(1) // wraps back to the top
	if s.panes.rows[s.panes.cur].node == nil {
		t.Fatal("move(1) wrapped onto a non-selectable row")
	}
	if s.panes.cur >= start {
		t.Fatalf("cur = %d after wrapping, want it back near the first selectable row", s.panes.cur)
	}
}

// Enter jumps focus to the highlighted pane's window and closes the picker;
// moving the highlight alone must not have switched anything yet.
func TestPanePickerEnterSwitchesWindowAndPane(t *testing.T) {
	win0 := buildSplitWindow("0", "a", "b")
	win1 := &window{name: "1", root: &node{weight: 1, pane: &pane{title: "solo"}}}
	win1.active = win1.root
	s := newCommandTestServer(win0)
	s.windows = append(s.windows, win1)
	s.openPanePicker()

	s.panes.move(1) // "b" -> window 1's solo pane
	if s.cur != 0 {
		t.Fatal("moving the highlight must not switch the active window before Enter")
	}

	s.panesKey(tea.Key{Code: tea.KeyEnter})
	if s.panes != nil {
		t.Fatal("Enter did not close the picker")
	}
	if s.cur != 1 {
		t.Fatalf("cur = %d after Enter, want 1 (the highlighted pane's window)", s.cur)
	}
	if s.win().active != win1.root {
		t.Fatal("Enter did not focus the highlighted pane")
	}
}

// Esc/q must close the picker without touching the active window or pane.
func TestPanePickerEscapeCancelsWithoutSwitching(t *testing.T) {
	win0 := buildSplitWindow("0", "a", "b")
	win1 := &window{name: "1", root: &node{weight: 1, pane: &pane{title: "solo"}}}
	win1.active = win1.root
	s := newCommandTestServer(win0)
	s.windows = append(s.windows, win1)
	s.openPanePicker()
	s.panes.move(1)

	s.panesKey(tea.Key{Code: tea.KeyEscape})
	if s.panes != nil {
		t.Fatal("Escape did not close the picker")
	}
	if s.cur != 0 || s.win().active != win0.active {
		t.Fatal("Escape must leave the active window/pane untouched")
	}
}

// "p"+"p" (the default keymap) must open the pane picker as a completed
// chord, the same shape as the existing p+x/p+r pane-layer bindings.
func TestPaneChordOpensPanePicker(t *testing.T) {
	win := buildSplitWindow("0", "a", "b")
	s := newCommandTestServer(win)

	s.command(tea.Key{Text: "p"})
	if s.chord != "p" {
		t.Fatalf("chord = %q after the leader key, want %q", s.chord, "p")
	}
	s.command(tea.Key{Text: "p"})
	if s.panes == nil {
		t.Fatal("p+p did not open the pane picker")
	}
}

// The rendered box must stay within the given bounds and show both the
// highlighted pane's tree row and its preview name, side by side.
func TestPanePickerBoxFitsBoundsAndShowsPreview(t *testing.T) {
	leftPane, err := newPane(0, 20, 10, make(chan event, 256))
	if err != nil {
		t.Fatal(err)
	}
	defer leftPane.close()
	rightPane, err := newPane(1, 20, 10, make(chan event, 256))
	if err != nil {
		t.Fatal(err)
	}
	defer rightPane.close()

	root := &node{dir: dirHoriz, weight: 1}
	left := &node{weight: 1, pane: leftPane, parent: root}
	right := &node{weight: 1, pane: rightPane, parent: root}
	leftPane.rename("alpha")
	rightPane.rename("beta")
	root.children = []*node{left, right}
	win := &window{name: "0", root: root, active: right}

	s := newCommandTestServer(win)
	s.openPanePicker()

	box := panePickerBox(s.panes, theme{}, 100, 30)
	if len(box) == 0 {
		t.Fatal("panePickerBox returned no rows")
	}
	w := ansi.StringWidth(box[0])
	for i, l := range box {
		if got := ansi.StringWidth(l); got != w {
			t.Fatalf("box[%d] width = %d, want uniform %d", i, got, w)
		}
	}
	if w > 100 {
		t.Fatalf("box width = %d, exceeds the 100-cell bound it was given", w)
	}
	if len(box) > 30 {
		t.Fatalf("box height = %d, exceeds the 30-cell bound it was given", len(box))
	}
	joined := strings.Join(box, "\n")
	if !strings.Contains(joined, "alpha") || !strings.Contains(joined, "beta") {
		t.Fatalf("box does not list both pane names:\n%s", joined)
	}
	if !strings.Contains(joined, "beta") {
		t.Fatal("preview column must show the highlighted (active) pane's name")
	}
}

// Regression: a nested split/stack branch row (e.g. "split (top/bottom)")
// sits right at the tree column's clamp boundary. treeW must account for
// the "  "/"› " selection marker every row gets prepended with in the
// render loop — not just the label itself — or that one row renders wider
// than the box's own border, and overlayCenter silently drops the whole
// picker (it did: split "|" then "-" on an 80x24 terminal made the picker
// vanish entirely).
func TestPanePickerBoxFitsNestedSplitBranchAt80Columns(t *testing.T) {
	leftPane, err := newPane(0, 20, 10, make(chan event, 256))
	if err != nil {
		t.Fatal(err)
	}
	defer leftPane.close()
	topPane, err := newPane(1, 20, 10, make(chan event, 256))
	if err != nil {
		t.Fatal(err)
	}
	defer topPane.close()
	bottomPane, err := newPane(2, 20, 10, make(chan event, 256))
	if err != nil {
		t.Fatal(err)
	}
	defer bottomPane.close()

	// root = dirHoriz{ leftLeaf, rightBranch = dirVert{ topLeaf, bottomLeaf } }
	// — exactly split "|" (left/right) then split "-" (up/down) on the
	// right pane, the reported repro steps.
	root := &node{dir: dirHoriz, weight: 1}
	left := &node{weight: 1, pane: leftPane, parent: root}
	rightBranch := &node{dir: dirVert, weight: 1, parent: root}
	top := &node{weight: 1, pane: topPane, parent: rightBranch}
	bottom := &node{weight: 1, pane: bottomPane, parent: rightBranch}
	rightBranch.children = []*node{top, bottom}
	root.children = []*node{left, rightBranch}
	win := &window{name: "0", root: root, active: bottom}

	s := newCommandTestServer(win)
	s.openPanePicker()

	const maxW, maxH = 80, 22 // an 80x24 terminal's body rect (h-2 for the tab/status bars)
	box := panePickerBox(s.panes, theme{}, maxW, maxH)
	if len(box) == 0 {
		t.Fatal("panePickerBox returned no rows")
	}
	w := ansi.StringWidth(box[0])
	for i, l := range box {
		if got := ansi.StringWidth(l); got != w {
			t.Fatalf("box[%d] width = %d, want uniform %d (box=%q)", i, got, w, box)
		}
	}
	if w >= maxW {
		t.Fatalf("box width = %d, must land strictly under maxW=%d or overlayCenter drops it", w, maxW)
	}

	lines := make([]string, maxH)
	for i := range lines {
		lines[i] = strings.Repeat("x", maxW)
	}
	base := strings.Join(lines, "\n")
	if got := overlayCenter(base, maxW, maxH, box); got == base {
		t.Fatal("overlayCenter dropped the pane picker box instead of compositing it")
	}
}
