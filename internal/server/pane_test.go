package server

import (
	"fmt"
	"strings"
	"testing"
)

// A split being dragged shrinks a pane's height and then grows it back. vt's
// own Resize keeps the buffer's first h rows and silently drops the rest —
// since output grows downward, that discards the most recent lines (and the
// cursor) rather than the oldest, and growing back can't recover what it
// dropped. shrinkHeight must instead push the true old rows into scrollback
// and keep the recent, cursor-bearing rows live.
func TestResizeShrinkPreservesContent(t *testing.T) {
	p, err := newPane(0, 20, 10, make(chan event, 256))
	if err != nil {
		t.Fatal(err)
	}
	defer p.close()

	for i := range 8 {
		_, _ = p.emu.Write([]byte(fmt.Sprintf("row%02d\r\n", i)))
	}

	p.resize(20, 3) // shrink height: only room for the most recent rows
	live := p.emu.Render()
	if !strings.Contains(live, "row07") {
		t.Fatalf("shrunk view should keep the most recent line visible, got:\n%s", live)
	}
	if strings.Contains(live, "row00") {
		t.Fatalf("shrunk view should not show stale old lines, got:\n%s", live)
	}
	if got := p.emu.ScrollbackLen(); got != 7 {
		t.Fatalf("expected 7 rows pushed to scrollback, got %d", got)
	}

	p.resize(20, 10) // grow back
	sb := p.emu.Scrollback()
	for i, want := range []string{"row00", "row01", "row02", "row03", "row04", "row05", "row06"} {
		if got := sb.Line(i).String(); got != want {
			t.Fatalf("scrollback[%d] = %q, want %q: history corrupted or out of order", i, got, want)
		}
	}
}

// Alternate-screen apps (vim, less) redraw themselves on SIGWINCH and don't
// use scrollback; shrinkHeight must leave vt's own resize alone there.
func TestResizeShrinkSkipsAltScreen(t *testing.T) {
	p, err := newPane(0, 20, 10, make(chan event, 256))
	if err != nil {
		t.Fatal(err)
	}
	defer p.close()

	_, _ = p.emu.Write([]byte("\x1b[?1049h")) // enter alt screen
	if !p.emu.IsAltScreen() {
		t.Fatal("expected alt screen")
	}
	for i := range 8 {
		_, _ = p.emu.Write([]byte(fmt.Sprintf("alt%02d\r\n", i)))
	}

	p.resize(20, 3)
	p.resize(20, 10)
	if got := p.emu.ScrollbackLen(); got != 0 {
		t.Fatalf("alt screen must not feed the main-screen scrollback, got %d lines", got)
	}
}

// Multiple shrinks in a row (a real mouse drag reports many intermediate
// sizes) must accumulate correctly rather than losing or duplicating lines.
func TestResizeShrinkSequenceAccumulates(t *testing.T) {
	p, err := newPane(0, 20, 10, make(chan event, 256))
	if err != nil {
		t.Fatal(err)
	}
	defer p.close()

	for i := range 8 {
		_, _ = p.emu.Write([]byte(fmt.Sprintf("row%02d\r\n", i)))
	}
	p.resize(20, 8)
	p.resize(15, 5)
	p.resize(15, 2)
	p.resize(18, 6)
	p.resize(20, 10)

	sb := p.emu.Scrollback()
	want := []string{"row00", "row01", "row02", "row03", "row04", "row05", "row06", "row07"}
	if sb.Len() != len(want) {
		t.Fatalf("expected %d scrollback lines, got %d", len(want), sb.Len())
	}
	for i, w := range want {
		if got := sb.Line(i).String(); got != w {
			t.Fatalf("sb[%d] = %q, want %q", i, got, w)
		}
	}
}

// A left/right split being dragged shrinks a pane's width and then grows it
// back. vt's own Resize truncates each row to the new width and, unlike
// height, has no scrollback-shaped place to preserve the cut columns — so
// growing back only reappends blank cells and the truncated text is gone
// for good. snapshotWidth/restoreWidth must recover it losslessly whenever
// nothing wrote over the row in between.
func TestResizeWidthShrinkRegrowRestoresContent(t *testing.T) {
	p, err := newPane(0, 30, 4, make(chan event, 256))
	if err != nil {
		t.Fatal(err)
	}
	defer p.close()

	_, _ = p.emu.Write([]byte("row00 hello world\r\nrow01 hello world\r\n"))

	p.resize(10, 4) // shrink width: rows truncate to 10 cols
	shrunk := p.emu.Render()
	if !strings.Contains(shrunk, "row00 hell") || strings.Contains(shrunk, "hello world") {
		t.Fatalf("shrunk view should show only the visible 10 cols, got:\n%s", shrunk)
	}

	p.resize(30, 4) // grow back to the original width
	got := p.emu.Render()
	if !strings.Contains(got, "row00 hello world") || !strings.Contains(got, "row01 hello world") {
		t.Fatalf("regrown view should restore the truncated tail, got:\n%s", got)
	}
}

// A drag reports many intermediate widths, progressively narrower then back
// to the original: the widest-ever snapshot must survive every intermediate
// (narrower) shrink, not just the most recent one.
func TestResizeWidthShrinkSequenceRestoresContent(t *testing.T) {
	p, err := newPane(0, 30, 4, make(chan event, 256))
	if err != nil {
		t.Fatal(err)
	}
	defer p.close()

	_, _ = p.emu.Write([]byte("row00 hello world\r\n"))
	for _, w := range []int{25, 20, 15, 10, 8, 12, 18, 22, 28, 30} {
		p.resize(w, 4)
	}
	if got := p.emu.Render(); !strings.Contains(got, "row00 hello world") {
		t.Fatalf("full drag sequence back to 30 should restore the original line, got:\n%s", got)
	}
}

// Restoring saved columns must never clobber a row that received new output
// while shrunk — the stale saved tail belongs to text that's gone.
func TestResizeWidthRestoreSkipsOverwrittenRow(t *testing.T) {
	p, err := newPane(0, 30, 4, make(chan event, 256))
	if err != nil {
		t.Fatal(err)
	}
	defer p.close()

	_, _ = p.emu.Write([]byte("AAAAAAAAAA BBBBBBBBBB"))
	p.resize(10, 4)
	_, _ = p.emu.Write([]byte("\rZZZZZZZZZZ")) // overwrite the visible row while shrunk
	p.resize(30, 4)

	got := p.emu.Render()
	if strings.Contains(got, "BBBBBBBBBB") {
		t.Fatalf("regrow must not resurrect content overwritten during the shrink, got:\n%s", got)
	}
	if !strings.Contains(got, "ZZZZZZZZZZ") {
		t.Fatalf("regrow must keep the row's actual (overwritten) content, got:\n%s", got)
	}
}
