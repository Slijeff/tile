package server

import (
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"

	tea "charm.land/bubbletea/v2"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"
	"github.com/charmbracelet/x/vt"
	"github.com/creack/pty"
)

// pane is one shell: a PTY plus the terminal emulator that interprets its
// output. Every field here is read and written only on the server event loop.
type pane struct {
	id      int
	emu     *vt.Emulator
	ptmx    *os.File
	cmd     *exec.Cmd
	title   string // the shell/program's own name — drives the window tab
	curVis  bool
	mouseOn bool // the program inside asked for mouse reporting
	scroll  int  // lines scrolled back from live output; 0 = following live
	w, h    int

	cmdBuf   []rune // the line typed so far, tracked until the first Enter names the pane
	cmdNamed bool   // the one-time auto-rename from the first command has already happened
	autoName string // set once cmdNamed, from the first command; the pane's own border name

	wideCols int       // widest column count snapshotted since full recovery; 0 = none held
	wideRows []uv.Line // rows captured at wideCols, for restoring what a width shrink truncated
}

func shellPath() string {
	if sh := os.Getenv("SHELL"); sh != "" {
		return sh
	}
	return "/bin/sh"
}

// newPane starts a shell on a new PTY and wires it to the event loop.
func newPane(id, w, h int, events chan<- event) (*pane, error) {
	sh := shellPath()
	cmd := exec.Command(sh)
	cmd.Env = append(os.Environ(), "TERM=xterm-256color")
	ptmx, err := pty.StartWithSize(cmd, &pty.Winsize{Rows: uint16(h), Cols: uint16(w)})
	if err != nil {
		return nil, err
	}

	p := &pane{
		id:     id,
		emu:    vt.NewEmulator(w, h),
		ptmx:   ptmx,
		cmd:    cmd,
		title:  filepath.Base(sh),
		curVis: true,
		w:      w,
		h:      h,
	}
	// These fire from inside emu.Write, which only ever runs on the event loop.
	p.emu.SetCallbacks(vt.Callbacks{
		Title:            func(s string) { p.title = s },
		CursorVisibility: func(v bool) { p.curVis = v },
		EnableMode:       func(m ansi.Mode) { p.setMouse(m, true) },
		DisableMode:      func(m ansi.Mode) { p.setMouse(m, false) },
	})

	// Keystrokes and device replies the emulator produces, on their way to the
	// shell. This must be running before any SendKey: the emulator writes them
	// into an io.Pipe, which blocks until somebody reads.
	// ponytail: a shell that stops reading its PTY can therefore stall the
	// event loop on the next keystroke. Buffer the writes if that ever bites.
	go func() { _, _ = io.Copy(ptmx, p.emu) }()

	go func() {
		buf := make([]byte, 32*1024)
		for {
			n, err := ptmx.Read(buf)
			if n > 0 {
				events <- event{kind: evOutput, pane: p, data: append([]byte(nil), buf[:n]...)}
			}
			if err != nil {
				events <- event{kind: evExit, pane: p}
				return
			}
		}
	}()
	return p, nil
}

// setMouse tracks whether the program inside wants mouse events forwarded.
func (p *pane) setMouse(m ansi.Mode, on bool) {
	switch m {
	case ansi.ModeMouseX10, ansi.ModeMouseNormal, ansi.ModeMouseHighlight,
		ansi.ModeMouseButtonEvent, ansi.ModeMouseAnyEvent:
		p.mouseOn = on
	}
}

// borderTitle returns what the pane's own border/header should show: the
// first command it ran, once there's been one, otherwise whatever its
// shell calls itself. Kept separate from title (which drives the window
// tab) so an auto-rename shows up on the pane but never leaks into the tab.
func (p *pane) borderTitle() string {
	if p.cmdNamed {
		return p.autoName
	}
	return p.title
}

// maxAutoTitleLen keeps an auto-derived pane title short enough to fit the
// border without dominating it.
const maxAutoTitleLen = 24

// trackCommand watches keystrokes on their way to the shell to auto-name
// the pane after the first command line it runs, tmux-style, truncated if
// it's too long. It stops watching once that's happened, so later commands
// don't keep renaming the pane, and gives up on the in-progress line for
// anything it doesn't recognize as plain typing (arrows, tab-completion,
// Ctrl+key combos, …) rather than risk naming the pane after a fragment.
func (p *pane) trackCommand(k tea.Key) {
	if p.cmdNamed {
		return
	}
	switch {
	case k.Text != "" && k.Mod == 0:
		p.cmdBuf = append(p.cmdBuf, []rune(k.Text)...)
	case k.Code == tea.KeyBackspace:
		if n := len(p.cmdBuf); n > 0 {
			p.cmdBuf = p.cmdBuf[:n-1]
		}
	case k.Code == tea.KeyEnter, k.Code == tea.KeyReturn:
		if cmd := strings.TrimSpace(string(p.cmdBuf)); cmd != "" {
			p.autoName, p.cmdNamed = truncateTitle(cmd), true
		}
		p.cmdBuf = p.cmdBuf[:0]
	default:
		p.cmdBuf = p.cmdBuf[:0]
	}
}

func truncateTitle(s string) string {
	r := []rune(s)
	if len(r) <= maxAutoTitleLen {
		return s
	}
	return string(r[:maxAutoTitleLen-1]) + "…"
}

// scrollBy moves the scrollback viewport by delta lines (positive = back in
// history, negative = toward live output), clamped to what's available.
// A no-op in alternate-screen mode, which has no scrollback.
func (p *pane) scrollBy(delta int) {
	if p.emu.IsAltScreen() {
		return
	}
	p.scroll += delta
	if max := p.emu.ScrollbackLen(); p.scroll > max {
		p.scroll = max
	}
	if p.scroll < 0 {
		p.scroll = 0
	}
}

// view renders the pane's current viewport: the live screen, or a window
// into scrollback history while scrolled back.
func (p *pane) view() string {
	if p.scroll == 0 || p.emu.IsAltScreen() {
		return p.emu.Render()
	}
	sbLen := p.emu.ScrollbackLen()
	top := sbLen - p.scroll // first row of the viewport, in scrollback+screen space
	lines := make(uv.Lines, p.h)
	for i := range lines {
		row := top + i
		switch {
		case row < 0:
			lines[i] = uv.NewLine(p.w)
		case row < sbLen:
			lines[i] = p.emu.Scrollback().Line(row)
		default:
			line := uv.NewLine(p.w)
			for x := range p.w {
				line.Set(x, p.emu.CellAt(x, row-sbLen))
			}
			lines[i] = line
		}
	}
	return lines.Render()
}

func (p *pane) resize(w, h int) {
	if w < 1 || h < 1 || (w == p.w && h == p.h) {
		return
	}
	oldW := p.w
	if h < p.h && !p.emu.IsAltScreen() {
		p.shrinkHeight(h)
	}
	if w < oldW && !p.emu.IsAltScreen() {
		p.snapshotWidth()
	}
	p.w, p.h = w, h
	p.emu.Resize(w, h)
	_ = pty.Setsize(p.ptmx, &pty.Winsize{Rows: uint16(h), Cols: uint16(w)})
	// Runs after Resize: the wider columns must already exist in the buffer
	// before we can write saved content back into them.
	if w > oldW && !p.emu.IsAltScreen() {
		p.restoreWidth(oldW, w)
	}
}

// shrinkHeight preserves content across a height reduction. vt's own Resize
// keeps the buffer's first h rows and silently drops the rest — since
// output grows downward, that throws away the most recent lines (and the
// cursor) while leaving stale old ones on screen, and growing back later
// can't recover what was dropped. This pushes the rows above the cursor's
// new position into scrollback and shifts the recent, cursor-bearing rows
// up to take their place, so a split can be shrunk and regrown without
// eating its content.
//
// drop is measured from the cursor, not from p.h - h: a pane grown taller
// than its content (zoom grows a pane far past what it has ever output)
// has the cursor well above row p.h-1, and dropping p.h-h rows off the top
// would discard the real content while keeping the blank padding below it.
// Capped at p.h-h, the most shrinking by that much can structurally drop.
func (p *pane) shrinkHeight(h int) {
	c := p.emu.CursorPosition()
	drop := c.Y - (h - 1)
	if drop < 0 {
		drop = 0
	}
	if max := p.h - h; drop > max {
		drop = max
	}
	sb := p.emu.Scrollback()
	for y := range drop {
		line := uv.NewLine(p.w)
		for x := range p.w {
			line.Set(x, p.emu.CellAt(x, y))
		}
		if sb != nil {
			sb.Push(line)
		}
	}
	// Forward copy is safe: the source row (y+drop) is always read before
	// any later iteration writes to it as a destination.
	for y := range h {
		for x := range p.w {
			p.emu.SetCell(x, y, p.emu.CellAt(x, y+drop))
		}
	}
	cy := c.Y - drop
	if cy < 0 {
		cy = 0
	}
	if cy >= h {
		cy = h - 1
	}
	_, _ = p.emu.Write([]byte(ansi.CursorPosition(c.X+1, cy+1)))
}

// snapshotWidth captures the pane's current full-width rows before a width
// shrink truncates them — vt's Resize drops trailing columns with no
// scrollback equivalent to catch them. Only takes a new snapshot when there
// isn't already a wider one held, so a second, narrower shrink in the same
// drag can't overwrite the original content with an already-truncated copy.
func (p *pane) snapshotWidth() {
	if p.wideCols != 0 && p.w <= p.wideCols {
		return
	}
	rows := make([]uv.Line, p.h)
	for y := range rows {
		line := uv.NewLine(p.w)
		for x := range p.w {
			line.Set(x, p.emu.CellAt(x, y))
		}
		rows[y] = line
	}
	p.wideCols = p.w
	p.wideRows = rows
}

// restoreWidth re-fills columns a prior shrink truncated, for every row
// whose still-visible prefix (up to oldW, the width just before this call)
// is unchanged since the snapshot — i.e. nothing was written there in
// between, so restoring can't clobber newer output. Must run after the
// buffer has already been grown to w columns.
func (p *pane) restoreWidth(oldW, w int) {
	if p.wideCols == 0 {
		return
	}
	upto := min(w, p.wideCols)
	for y, saved := range p.wideRows {
		if y >= p.h {
			break
		}
		fresh := true
		for x := range oldW {
			if !p.emu.CellAt(x, y).Equal(saved.At(x)) {
				fresh = false
				break
			}
		}
		if !fresh {
			continue
		}
		for x := oldW; x < upto; x++ {
			p.emu.SetCell(x, y, saved.At(x))
		}
	}
	if w >= p.wideCols {
		p.wideCols = 0
		p.wideRows = nil
	}
}

func (p *pane) close() {
	if p.cmd.Process != nil {
		// pty.Start puts the child in its own session, so the negative pid
		// hangs up the whole job tree rather than just the shell.
		_ = syscall.Kill(-p.cmd.Process.Pid, syscall.SIGHUP)
		go func() { _ = p.cmd.Wait() }()
	}
	_ = p.ptmx.Close()
	_ = p.emu.Close()
}
