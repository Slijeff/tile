package server

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
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

	cmdBuf     []rune // the line typed so far, tracked until the first Enter names the pane
	named      bool   // the border name has been set, either by trackCommand or a manual rename
	borderName string // set once named; the pane's own border name, distinct from title

	osc    oscState // filterTitle's progress through an OSC title, across PTY reads
	oscBuf []byte   // the OSC payload collected so far

	// Mouse text-selection, for panes whose program hasn't grabbed the
	// mouse for itself. selFrom/selTo are pane-relative cells; selecting is
	// true only while the button is still down, selDragged distinguishes an
	// actual drag from a plain click (which shouldn't leave a one-cell
	// selection behind), and hasSel is what view() checks to paint it.
	selecting  bool
	selDragged bool
	hasSel     bool
	selFrom    uv.Position
	selTo      uv.Position
}

func shellPath() string {
	if sh := os.Getenv("SHELL"); sh != "" {
		return sh
	}
	return "/bin/sh"
}

// newPane starts a shell on a new PTY and wires it to the event loop.
// TILE_SESSION_INO and TILE_PANE tell whatever runs inside where it is, so
// a bare "tile list" there targets this session rather than "default", and
// "tile" there refuses to attach the session to itself. The session goes by
// its socket's inode rather than its name, since env is fixed at spawn and
// a name would go stale on the first session rename. sockIno 0 (tests)
// leaves it out.
func newPane(id, w, h int, events chan<- event, sockIno uint64) (*pane, error) {
	sh := shellPath()
	cmd := exec.Command(sh)
	cmd.Env = append(os.Environ(), "TERM=xterm-256color", fmt.Sprintf("TILE_PANE=%%%d", id))
	if sockIno != 0 {
		cmd.Env = append(cmd.Env, fmt.Sprintf("TILE_SESSION_INO=%d", sockIno))
	}
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
	// Titles don't come through here — see filterTitle.
	p.emu.SetCallbacks(vt.Callbacks{
		CursorVisibility: func(v bool) { p.curVis = v },
		EnableMode:       func(m ansi.Mode) { p.setMouse(m, true) },
		DisableMode:      func(m ansi.Mode) { p.setMouse(m, false) },
	})

	// Keystrokes, pastes and device replies the emulator produces, on their
	// way to the shell. This must be running before any SendKey: the emulator
	// writes them into an io.Pipe, which blocks until somebody reads.
	go feed(ptmx, p.emu)

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

// feed copies src to dst through an unbounded queue, so src's writer never
// waits on dst. src is the emulator's input pipe, written from the event
// loop; dst is the PTY, whose kernel input buffer is only a few KB and
// stays full for as long as the program inside isn't reading (a busy
// build, output paused with Ctrl+S). A plain io.Copy there let one such
// pane stall the event loop — and with it every pane — on the next
// keystroke or paste.
// ponytail: unbounded, so a pane that never reads again holds whatever is
// typed into it until it closes. Cap it if that ever matters.
func feed(dst io.Writer, src io.Reader) {
	var (
		mu   sync.Mutex
		cond = sync.NewCond(&mu)
		q    []byte
		done bool
	)
	go func() {
		buf := make([]byte, 32*1024)
		for {
			n, err := src.Read(buf)
			mu.Lock()
			q = append(q, buf[:n]...)
			done = err != nil
			mu.Unlock()
			cond.Signal()
			if err != nil {
				return
			}
		}
	}()
	for {
		mu.Lock()
		for len(q) == 0 && !done {
			cond.Wait()
		}
		b, last := q, done
		q = nil
		mu.Unlock()
		if _, err := dst.Write(b); err != nil || last {
			return
		}
	}
}

type oscState byte

const (
	oscNone  oscState = iota
	oscEsc            // saw ESC
	oscTitle          // inside ESC ] — collecting the payload
)

// filterTitle pulls OSC 0/2 title sequences out of PTY output and sets
// p.title itself, passing everything else through for the emulator. The
// ansi parser ends an OSC at a 0x9C byte (C1 ST) even mid-UTF-8, so a
// title like Claude Code's "✳ …" (E2 9C B3) was cut to a lone 0xE2 and
// shown as U+FFFD. Here only BEL, ESC and CAN/SUB end the string.
func (p *pane) filterTitle(data []byte) []byte {
	out := make([]byte, 0, len(data))
	for _, b := range data {
		switch p.osc {
		case oscNone:
			if b == 0x1b {
				p.osc = oscEsc
				continue
			}
			out = append(out, b)
		case oscEsc:
			switch b {
			case ']':
				p.osc, p.oscBuf = oscTitle, p.oscBuf[:0]
			case 0x1b:
				out = append(out, 0x1b) // ESC ESC: the second may still start an OSC
			default:
				p.osc = oscNone
				out = append(out, 0x1b, b)
			}
		case oscTitle:
			switch {
			case b == 0x07:
				p.setTitle()
				p.osc = oscNone
			case b == 0x1b: // ESC \ (ST); the stray "\" is a no-op for the emulator
				p.setTitle()
				p.osc = oscEsc
			case b == 0x18 || b == 0x1a: // CAN/SUB abort the sequence
				p.osc = oscNone
			default:
				p.oscBuf = append(p.oscBuf, b)
				// Not a title (or runaway): hand the sequence back to the
				// emulator untouched and stop collecting.
				if n := len(p.oscBuf); n == 2 && !isTitleOSC(p.oscBuf) || n > 4096 {
					out = append(append(out, 0x1b, ']'), p.oscBuf...)
					p.osc = oscNone
				}
			}
		}
	}
	return out
}

func isTitleOSC(b []byte) bool { return (b[0] == '0' || b[0] == '2') && b[1] == ';' }

// setTitle applies a finished OSC title. TUIs (vim, less, fzf) emit an empty
// title on exit to hand it back to the shell prompt. Honoring that blanks the
// tab until the next prompt — or forever, if the shell never sets it — wiping
// whatever name was showing. Keep the last non-empty title instead.
func (p *pane) setTitle() {
	if len(p.oscBuf) < 2 || !isTitleOSC(p.oscBuf) {
		return
	}
	if s := strings.ToValidUTF8(string(p.oscBuf[2:]), ""); strings.TrimSpace(s) != "" {
		p.title = s
	}
}

// setMouse tracks whether the program inside wants mouse events forwarded.
func (p *pane) setMouse(m ansi.Mode, on bool) {
	switch m {
	case ansi.ModeMouseX10, ansi.ModeMouseNormal, ansi.ModeMouseHighlight,
		ansi.ModeMouseButtonEvent, ansi.ModeMouseAnyEvent:
		p.mouseOn = on
	}
}

// borderTitle returns what the pane's own border/header should show: an
// override name — auto-tracked from the first command, or set by a manual
// rename — once there is one, otherwise whatever its shell calls itself.
// Kept separate from title (which drives the window tab) so neither kind
// of override ever leaks into the tab.
func (p *pane) borderTitle() string {
	if p.named {
		return p.borderName
	}
	return p.title
}

// maxAutoTitleLen keeps an auto-derived pane title short enough to fit the
// border without dominating it.
const maxAutoTitleLen = 24

// trackCommand watches keystrokes on their way to the shell to auto-name
// the pane after the first command line it runs, tmux-style, truncated if
// it's too long. It stops watching once the pane has a border name — from
// this or a manual rename — so later commands don't keep relabeling it,
// and gives up on the in-progress line for anything it doesn't recognize
// as plain typing (arrows, tab-completion, Ctrl+key combos, …) rather than
// risk naming the pane after a fragment.
func (p *pane) trackCommand(k tea.Key) {
	if p.named {
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
			p.borderName, p.named = truncateTitle(cmd), true
		}
		p.cmdBuf = p.cmdBuf[:0]
	default:
		p.cmdBuf = p.cmdBuf[:0]
	}
}

// rename sets the pane's border name from the rename prompt. A blank name
// clears the override instead of pinning an empty label, reverting the
// border to the shell title and re-arming trackCommand to auto-name the
// pane after its next command — the same reset a manual window rename
// used to give the tab.
func (p *pane) rename(name string) {
	if name = strings.TrimSpace(name); name == "" {
		p.named, p.borderName = false, ""
		return
	}
	p.borderName, p.named = truncateTitle(name), true
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
// into scrollback history while scrolled back. A mouse selection, if one is
// up, gets painted in on top.
func (p *pane) view() string {
	lines := make(uv.Lines, p.h)
	for y := range lines {
		lines[y] = p.screenLine(y)
	}
	if p.hasSel {
		p.paintSelection(lines)
	}
	return lines.Render()
}

// screenLine returns pane-relative row y as currently displayed — scrollback
// while scrolled back, the live screen otherwise — as a freshly copied Line,
// safe for a caller to restyle without corrupting the emulator's own buffer
// or scrollback. Shared by view(), which paints a selection on top of what
// it returns, and selectedText(), which reads the same cells back as text.
func (p *pane) screenLine(y int) uv.Line {
	line := uv.NewLine(p.w)
	if p.scroll == 0 || p.emu.IsAltScreen() {
		for x := range p.w {
			line.Set(x, p.emu.CellAt(x, y))
		}
		return line
	}
	sbLen := p.emu.ScrollbackLen()
	row := sbLen - p.scroll + y // this row's position in scrollback+screen space
	switch {
	case row < 0:
		// past the top of scrollback: leave it blank
	case row < sbLen:
		for x, c := range p.emu.Scrollback().Line(row) {
			if x >= p.w {
				break
			}
			cell := c
			line.Set(x, &cell)
		}
	default:
		for x := range p.w {
			line.Set(x, p.emu.CellAt(x, row-sbLen))
		}
	}
	return line
}

// selectStart begins a mouse-drag text selection at a pane-relative cell,
// discarding whatever selection was there before.
func (p *pane) selectStart(x, y int) {
	p.selecting, p.selDragged, p.hasSel = true, false, false
	p.selFrom = uv.Pos(x, y)
	p.selTo = p.selFrom
}

// selectExtend moves the live end of an in-progress drag. A plain click that
// never moves off its starting cell never sets selDragged, which is what
// keeps a no-drag click from leaving a one-character selection behind.
func (p *pane) selectExtend(x, y int) {
	if !p.selecting {
		return
	}
	to := uv.Pos(x, y)
	if to != p.selFrom {
		p.selDragged = true
	}
	p.selTo = to
	p.hasSel = p.selDragged
}

// selectEnd finishes the drag and returns the selected text, ready to hand
// to the client's clipboard. A plain click (no drag) or a drag that landed
// on only blank cells returns "" and clears the selection rather than
// leaving a highlight with nothing meaningful to have copied.
func (p *pane) selectEnd() string {
	p.selecting = false
	if !p.selDragged {
		p.hasSel = false
		return ""
	}
	text := p.selectedText()
	p.hasSel = text != ""
	return text
}

// clearSelection drops any selection outside of a drag — before ordinary
// typing resumes, or a fresh click lands elsewhere — so a stale highlight
// never lingers over content that has moved on.
func (p *pane) clearSelection() {
	p.selecting, p.selDragged, p.hasSel = false, false, false
}

// selBounds returns the selection's two endpoints in reading order (top to
// bottom, left to right within a row), regardless of which one the drag
// started or ended on.
func (p *pane) selBounds() (from, to uv.Position) {
	from, to = p.selFrom, p.selTo
	if from.Y > to.Y || (from.Y == to.Y && from.X > to.X) {
		from, to = to, from
	}
	return from, to
}

// paintSelection reverses the fg/bg of every cell the selection covers, read
// linearly from its start to its end rather than as a rectangular block —
// the whole width of every row in between, just the tail from the start
// column on its first row and the head up to the end column on its last —
// which is how a plain terminal's own click-drag selection reads.
func (p *pane) paintSelection(lines uv.Lines) {
	from, to := p.selBounds()
	for y := max(from.Y, 0); y <= to.Y && y < len(lines); y++ {
		lo, hi := 0, p.w-1
		if y == from.Y {
			lo = from.X
		}
		if y == to.Y {
			hi = to.X
		}
		line := lines[y]
		for x := max(lo, 0); x <= hi && x < len(line); x++ {
			if line[x].IsZero() {
				continue // a wide rune's placeholder column: nothing to invert
			}
			line[x].Style.Attrs |= uv.AttrReverse
		}
	}
}

// selectedText reads the same span paintSelection highlights back out as
// plain text, one joined line per row, each right-trimmed of the padding
// every row is stored with.
func (p *pane) selectedText() string {
	from, to := p.selBounds()
	var rows []string
	for y := max(from.Y, 0); y <= to.Y && y < p.h; y++ {
		lo, hi := 0, p.w-1
		if y == from.Y {
			lo = from.X
		}
		if y == to.Y {
			hi = to.X
		}
		line := p.screenLine(y)
		var row strings.Builder
		for x := max(lo, 0); x <= hi && x < len(line); x++ {
			if c := line[x]; c.Width > 0 {
				row.WriteString(c.Content)
			}
		}
		rows = append(rows, strings.TrimRight(row.String(), " "))
	}
	return strings.Join(rows, "\n")
}

// capture returns the pane's last n lines of text, for the CLI to hand an
// agent. Unlike view it ignores p.scroll — a capture asks "what has this pane
// printed", not "what is the user looking at" — and it strips the styling,
// which is noise to a reader that isn't a terminal. n <= 0 means everything.
//
// The blank rows below the cursor are dropped before n is applied: a caller
// asking for the last 20 lines means the last 20 lines with something on
// them, not the bottom 20 rows of a mostly empty screen.
func (p *pane) capture(n int) string {
	var text string
	if p.emu.IsAltScreen() {
		// A full-screen program (vim, less) redraws its own viewport, and the
		// scrollback holds whatever scrolled past before it started — not what
		// it is showing. The screen is the whole truth here.
		text = ansi.Strip(p.emu.Render())
	} else {
		sbLen := p.emu.ScrollbackLen()
		lines := make(uv.Lines, sbLen+p.h)
		for i := range lines {
			if i < sbLen {
				lines[i] = p.emu.Scrollback().Line(i)
				continue
			}
			line := uv.NewLine(p.w)
			for x := range p.w {
				line.Set(x, p.emu.CellAt(x, i-sbLen))
			}
			lines[i] = line
		}
		text = ansi.Strip(lines.Render())
	}

	rows := strings.Split(text, "\n")
	// Every row is padded out to the pane width; that padding is noise to a
	// reader that isn't drawing a terminal.
	for i, r := range rows {
		rows[i] = strings.TrimRight(r, " ")
	}
	for len(rows) > 0 && rows[len(rows)-1] == "" {
		rows = rows[:len(rows)-1]
	}
	if n > 0 && n < len(rows) {
		rows = rows[len(rows)-n:]
	}
	return strings.Join(rows, "\n")
}

func (p *pane) resize(w, h int) {
	if w < 1 || h < 1 || (w == p.w && h == p.h) {
		return
	}
	p.clearSelection() // its coordinates are relative to the old geometry
	if h < p.h && !p.emu.IsAltScreen() {
		p.shrinkHeight(h)
	}
	p.w, p.h = w, h
	p.emu.Resize(w, h)
	_ = pty.Setsize(p.ptmx, &pty.Winsize{Rows: uint16(h), Cols: uint16(w)})
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
