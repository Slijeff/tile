package server

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

// pickerRow is one line of the pane picker's tree: a window header or split/
// stack branch (node == nil, not selectable) or a pane leaf (node != nil,
// selectable and jumped to on Enter).
type pickerRow struct {
	label string
	win   int
	node  *node
}

// panePicker is the pane picker's open state: every window's split tree
// flattened into rows, and the row index of the current highlight.
type panePicker struct {
	rows []pickerRow
	cur  int
}

// openPanePicker builds the tree over every window and pane, defaulting the
// highlight to the active window's active pane.
func (s *server) openPanePicker() {
	var rows []pickerRow
	for wi, w := range s.windows {
		rows = append(rows, pickerRow{label: fmt.Sprintf("%d: %s", wi+1, w.displayName()), win: wi})
		rows = append(rows, windowTreeRows(w, wi)...)
	}
	cur := 0
	if active := s.win().active; active != nil {
		for i, r := range rows {
			if r.node == active {
				cur = i
				break
			}
		}
	}
	s.panes = &panePicker{rows: rows, cur: cur}
}

// windowTreeRows renders one window's split tree as indented rows, skipping
// a branch line for the root itself so its direct children start the tree
// at the window header's own indent.
func windowTreeRows(w *window, wi int) []pickerRow {
	root := w.root
	if root.pane != nil {
		return []pickerRow{{label: "  " + root.pane.borderTitle(), win: wi, node: root}}
	}
	var rows []pickerRow
	for i, c := range root.children {
		rows = append(rows, paneTreeRows(c, wi, "  ", i == len(root.children)-1)...)
	}
	return rows
}

// paneTreeRows recurses n as a tree-drawn row (├─/└─ connectors, │ guides
// down the left edge) followed by its children, if any.
func paneTreeRows(n *node, win int, prefix string, isLast bool) []pickerRow {
	connector, childPrefix := "├─ ", prefix+"│  "
	if isLast {
		connector, childPrefix = "└─ ", prefix+"   "
	}
	if n.pane != nil {
		return []pickerRow{{label: prefix + connector + n.pane.borderTitle(), win: win, node: n}}
	}
	rows := []pickerRow{{label: prefix + connector + dirLabel(n.dir), win: win}}
	for i, c := range n.children {
		rows = append(rows, paneTreeRows(c, win, childPrefix, i == len(n.children)-1)...)
	}
	return rows
}

func dirLabel(d dir) string {
	switch d {
	case dirHoriz:
		return "split (side by side)"
	case dirVert:
		return "split (top/bottom)"
	case dirStack:
		return "stack"
	default:
		return "?"
	}
}

// move steps the highlight to the next or previous selectable (pane) row,
// wrapping and skipping over window headers and branch rows.
func (pp *panePicker) move(delta int) {
	n := len(pp.rows)
	if n == 0 {
		return
	}
	i := pp.cur
	for range n {
		i = (i + delta + n) % n
		if pp.rows[i].node != nil {
			pp.cur = i
			return
		}
	}
}

// panesKey handles one keystroke while the pane picker is open. Moving the
// selection only changes the preview drawn beside the tree; Enter is what
// actually switches the active window and pane.
func (s *server) panesKey(k tea.Key) {
	s.dirty = true
	switch {
	case k.Code == tea.KeyUp || k.Text == "k":
		s.panes.move(-1)
	case k.Code == tea.KeyDown || k.Text == "j":
		s.panes.move(1)
	case k.Code == tea.KeyEnter:
		row := s.panes.rows[s.panes.cur]
		s.cur = row.win
		// The picker only ever lists tiled panes, so jumping to one means
		// leaving that window's floating terminal, if it had focus.
		s.win().floatOn = false
		s.win().active = row.node
		s.panes = nil
	case k.Code == tea.KeyEscape || k.Text == "q":
		s.panes = nil
	}
}

// panePickerBox renders the picker as a bordered floating panel: the tree on
// the left, a live preview of the highlighted pane's content on the right.
// maxW/maxH are the body rect's size, so the box never asks overlayCenter to
// place something bigger than the screen.
func panePickerBox(pp *panePicker, th theme, maxW, maxH int) []string {
	// +2 for the "  "/"› " selection marker every row gets prepended in the
	// render loop below — not part of r.label, but part of the rendered
	// width that must fit inside treeW.
	treeW := 0
	for _, r := range pp.rows {
		if w := ansi.StringWidth(r.label) + 2; w > treeW {
			treeW = w
		}
	}
	treeW = clamp(treeW, 24, 40)

	bodyH := clamp(maxH-4, 6, 20)
	// -8, not -7: overlayCenter rejects a box whose width is >= maxW, so the
	// total box width (previewW+treeW+3, plus 4 of frame) must land strictly
	// under it.
	previewW := max(maxW-treeW-8, 30)

	start := 0
	if len(pp.rows) > bodyH {
		start = clamp(pp.cur-bodyH/2, 0, len(pp.rows)-bodyH)
	}
	visible := pp.rows[start:min(start+bodyH, len(pp.rows))]
	preview := previewLines(pp, th, previewW, bodyH)

	rows := make([]string, bodyH)
	for i := range rows {
		var tree string
		if i < len(visible) {
			r := visible[i]
			style, prefix := fg(th.Text), "  "
			if start+i == pp.cur {
				style, prefix = "\x1b[1m"+fg(th.Base)+bg(th.Accent), "› "
			}
			tree = style + prefix + r.label + "\x1b[m"
		}
		// Both columns are padded to exactly their own width, so every row
		// lands on the same total width and panel frames it without padding.
		rows[i] = padTo(tree, treeW) + fg(th.Surface) + " │ \x1b[m" + padTo(preview[i], previewW)
	}
	return panel("panes  (↑↓/jk move · enter select · esc cancel)", rows, 0, th)
}

// previewLines renders the highlighted row's pane name, a rule, and its
// live content — fit to exactly w by h-2 cells, cropped or padded like any
// pane's own view — for the picker box's right-hand column.
func previewLines(pp *panePicker, th theme, w, h int) []string {
	out := make([]string, h)
	if h == 0 {
		return out
	}
	row := pp.rows[pp.cur]
	name := ""
	if row.node != nil && row.node.pane != nil {
		name = row.node.pane.borderTitle()
	}
	out[0] = "\x1b[1m" + fg(th.Text) + name + "\x1b[22m\x1b[m"
	if h == 1 {
		return out
	}
	out[1] = fg(th.Surface) + strings.Repeat("─", w) + "\x1b[m"

	contentH := h - 2
	var content string
	if row.node != nil && row.node.pane != nil {
		content = fit(row.node.pane.view(), w, contentH)
	} else {
		content = fit("", w, contentH)
	}
	lines := strings.Split(content, "\n")
	for i := 0; i < contentH && i < len(lines); i++ {
		out[2+i] = lines[i]
	}
	return out
}

func clamp(v, lo, hi int) int {
	if hi < lo {
		hi = lo
	}
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}
