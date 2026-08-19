package server

import (
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
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
	if got := p.emu.ScrollbackLen(); got != 6 {
		t.Fatalf("expected 6 rows pushed to scrollback, got %d", got)
	}

	p.resize(20, 10) // grow back
	sb := p.emu.Scrollback()
	for i, want := range []string{"row00", "row01", "row02", "row03", "row04", "row05"} {
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
	want := []string{"row00", "row01", "row02", "row03", "row04", "row05", "row06"}
	if sb.Len() != len(want) {
		t.Fatalf("expected %d scrollback lines, got %d", len(want), sb.Len())
	}
	for i, w := range want {
		if got := sb.Line(i).String(); got != w {
			t.Fatalf("sb[%d] = %q, want %q", i, got, w)
		}
	}
}

// Zoom grows a pane far past whatever it has ever output before shrinking
// it back down. shrinkHeight must not assume the cursor sits near the old
// (inflated) height: dropping p.h-h rows off the top in that case would
// discard the real content and keep the blank padding below it instead.
func TestResizeShrinkAfterGrowPastContentPreservesContent(t *testing.T) {
	p, err := newPane(0, 19, 9, make(chan event, 256))
	if err != nil {
		t.Fatal(err)
	}
	defer p.close()

	for i := range 6 {
		_, _ = p.emu.Write([]byte(fmt.Sprintf("row%02d\r\n", i)))
	}

	p.resize(40, 20) // zoom: grow well beyond the 6 lines ever written
	p.resize(19, 9)  // unzoom: shrink back to the original size

	got := p.emu.Render()
	if !strings.Contains(got, "row05") {
		t.Fatalf("shrinking back after an oversized grow lost content, got:\n%s", got)
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

// typeText feeds each rune of s through trackCommand as a plain keystroke,
// the shape tea reports for printable characters (Text set, no modifier).
func typeText(p *pane, s string) {
	for _, r := range s {
		p.trackCommand(tea.Key{Text: string(r), Code: r})
	}
}

// The pane's default title (the shell's own name) should give way to the
// first command line the user actually runs, tmux-style — on the pane's
// own border only; the window tab (which reads title, not borderTitle)
// must be untouched, see TestTrackCommandDoesNotRenameWindowTab.
func TestTrackCommandNamesPaneAfterFirstCommand(t *testing.T) {
	p := &pane{title: "zsh"}
	typeText(p, "git status")
	p.trackCommand(tea.Key{Code: tea.KeyEnter})

	if got := p.borderTitle(); got != "git status" {
		t.Fatalf("borderTitle() = %q, want %q", got, "git status")
	}
	if !p.named {
		t.Fatal("named should be set once the pane has been auto-named")
	}
	if p.title != "zsh" {
		t.Fatalf("title = %q, the auto-rename must not touch the shell/tab title", p.title)
	}
}

// The auto-rename must only ever affect the pane's own border, never the
// window tab it lives in — tabLine reads pane.title, not borderTitle, and
// title is only ever set by the shell's own OSC title escape.
func TestTrackCommandDoesNotRenameWindowTab(t *testing.T) {
	p := &pane{title: "zsh"}
	win := &window{name: "0", root: &node{pane: p, weight: 1}, active: &node{pane: p, weight: 1}}

	typeText(p, "git status")
	p.trackCommand(tea.Key{Code: tea.KeyEnter})

	txt, _ := tabLine([]*window{win}, 0, 40, theme{})
	if strings.Contains(txt, "git status") {
		t.Fatalf("tab = %q, the pane auto-rename must not leak into the window tab", txt)
	}
	if !strings.Contains(txt, "zsh") {
		t.Fatalf("tab = %q, want it to keep following the pane's shell title", txt)
	}
}

// Only the first command should ever rename the pane — later ones must
// leave the name alone, or the pane would keep relabeling itself forever.
func TestTrackCommandIgnoresLaterCommands(t *testing.T) {
	p := &pane{title: "zsh"}
	typeText(p, "ls")
	p.trackCommand(tea.Key{Code: tea.KeyEnter})
	typeText(p, "vim main.go")
	p.trackCommand(tea.Key{Code: tea.KeyEnter})

	if got := p.borderTitle(); got != "ls" {
		t.Fatalf("borderTitle() = %q, want the first command %q to stick", got, "ls")
	}
}

// Backspace must edit the tracked line the same way it edits what the
// shell shows, so the captured command matches what actually ran.
func TestTrackCommandBackspaceEditsBuffer(t *testing.T) {
	p := &pane{}
	typeText(p, "hels")
	p.trackCommand(tea.Key{Code: tea.KeyBackspace})
	typeText(p, "p")
	p.trackCommand(tea.Key{Code: tea.KeyEnter})

	if got := p.borderTitle(); got != "help" {
		t.Fatalf("borderTitle() = %q, want %q", got, "help")
	}
}

// An arrow key (or any other key that isn't plain typing) means the
// in-progress line no longer matches what the shell will run — history
// recall, cursor movement, tab-completion, and so on — so tracking must
// give up on it rather than risk naming the pane after a fragment.
func TestTrackCommandResetsOnUnrecognizedKey(t *testing.T) {
	p := &pane{}
	typeText(p, "abc")
	p.trackCommand(tea.Key{Code: tea.KeyUp})
	typeText(p, "ls -la")
	p.trackCommand(tea.Key{Code: tea.KeyEnter})

	if got := p.borderTitle(); got != "ls -la" {
		t.Fatalf("borderTitle() = %q, want the fragment before the arrow key dropped, got %q", got, "ls -la")
	}
}

// An empty line (just pressing Enter at a bare prompt) isn't a command:
// naming must wait for one that actually has content.
func TestTrackCommandIgnoresBlankLine(t *testing.T) {
	p := &pane{title: "zsh"}
	p.trackCommand(tea.Key{Code: tea.KeyEnter})
	if p.named {
		t.Fatal("an empty line should not count as the first command")
	}
	if got := p.borderTitle(); got != "zsh" {
		t.Fatalf("borderTitle() = %q, want it to fall back to the shell title until named", got)
	}

	typeText(p, "top")
	p.trackCommand(tea.Key{Code: tea.KeyEnter})
	if got := p.borderTitle(); got != "top" {
		t.Fatalf("borderTitle() = %q, want %q once a real command runs", got, "top")
	}
}

// A command longer than the auto-title budget must be truncated, not left
// to blow out the border or tab bar.
func TestTruncateTitleTruncatesLongCommand(t *testing.T) {
	long := strings.Repeat("a", maxAutoTitleLen+10)
	got := truncateTitle(long)
	if n := len([]rune(got)); n != maxAutoTitleLen {
		t.Fatalf("truncated length = %d, want %d", n, maxAutoTitleLen)
	}
	if !strings.HasSuffix(got, "…") {
		t.Fatalf("truncated title = %q, want an ellipsis marking the cut", got)
	}

	short := "ls"
	if got := truncateTitle(short); got != short {
		t.Fatalf("truncateTitle(%q) = %q, want it unchanged", short, got)
	}
}
