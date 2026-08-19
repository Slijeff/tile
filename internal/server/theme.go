package server

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

// theme is the palette yatm's own chrome (tab bar, status bar, tooltip and
// picker overlays) draws with. Pane content keeps whatever colors the
// program running inside it produces; theme only skins yatm itself.
type theme struct {
	Name string

	Text    string // primary foreground
	Subtext string // secondary/dim foreground
	Base    string // floating-panel background; also text-on-accent
	Mantle  string // status/tab bar background
	Surface string // borders and separators
	Accent  string // active tab, selection highlight
	Green   string // normal-mode badge
	Yellow  string // prefix-mode badge
	Red     string // locked-mode badge
}

func hex3(h string) (r, g, b int) {
	fmt.Sscanf(h, "%02x%02x%02x", &r, &g, &b)
	return
}

func fg(hex string) string { r, g, b := hex3(hex); return fmt.Sprintf("\x1b[38;2;%d;%d;%dm", r, g, b) }
func bg(hex string) string { r, g, b := hex3(hex); return fmt.Sprintf("\x1b[48;2;%d;%d;%dm", r, g, b) }

// catppuccinThemes holds the four official Catppuccin flavors, light to
// dark, in picker order. Hex values from the published Catppuccin palette.
var catppuccinThemes = []theme{
	{
		Name: "Catppuccin Latte",
		Text: "4c4f69", Subtext: "6c6f85", Base: "eff1f5", Mantle: "e6e9ef",
		Surface: "ccd0da", Accent: "1e66f5", Green: "40a02b", Yellow: "df8e1d", Red: "d20f39",
	},
	{
		Name: "Catppuccin Frappe",
		Text: "c6d0f5", Subtext: "a5adce", Base: "303446", Mantle: "292c3c",
		Surface: "414559", Accent: "8caaee", Green: "a6d189", Yellow: "e5c890", Red: "e78284",
	},
	{
		Name: "Catppuccin Macchiato",
		Text: "cad3f5", Subtext: "a5adcb", Base: "24273a", Mantle: "1e2030",
		Surface: "363a4f", Accent: "8aadf4", Green: "a6da95", Yellow: "eed49f", Red: "ed8796",
	},
	{
		Name: "Catppuccin Mocha",
		Text: "cdd6f4", Subtext: "a6adc8", Base: "1e1e2e", Mantle: "181825",
		Surface: "313244", Accent: "89b4fa", Green: "a6e3a1", Yellow: "f9e2af", Red: "f38ba8",
	},
}

func themeNames(ts []theme) []string {
	names := make([]string, len(ts))
	for i, t := range ts {
		names[i] = t.Name
	}
	return names
}

// picker is the colorscheme picker's open state.
type picker struct {
	before theme // theme active when the picker opened, restored on cancel
	sel    int
}

func (s *server) openPicker() {
	sel := 0
	for i, t := range s.themes {
		if t.Name == s.theme.Name {
			sel = i
		}
	}
	s.picker = &picker{before: s.theme, sel: sel}
}

// pickerKey handles one keystroke while the picker is open. Moving the
// selection previews it live by swapping s.theme immediately; enter keeps
// it and persists the choice, esc/q restores whatever was active before
// the picker opened.
func (s *server) pickerKey(k tea.Key) {
	s.dirty = true
	move := func(d int) {
		s.picker.sel = (s.picker.sel + d + len(s.themes)) % len(s.themes)
		s.theme = s.themes[s.picker.sel]
	}
	switch {
	case k.Code == tea.KeyUp || k.Text == "k":
		move(-1)
	case k.Code == tea.KeyDown || k.Text == "j":
		move(1)
	case k.Code == tea.KeyEnter:
		s.picker = nil
		saveTheme(s.theme.Name)
	case k.Code == tea.KeyEscape || k.Text == "q":
		s.theme = s.picker.before
		s.picker = nil
	}
}

// pickerBox renders the picker as a bordered floating panel, one line per
// theme, the highlighted row shown in that theme's own accent color.
func pickerBox(names []string, sel int, th theme) []string {
	title := "colorscheme  (↑↓/jk preview · enter keep · esc cancel)"
	textWidth := ansi.StringWidth(title)
	for _, n := range names {
		if w := ansi.StringWidth(n) + 2; w > textWidth {
			textWidth = w
		}
	}
	pad := func(s string) string {
		if d := textWidth - ansi.StringWidth(s); d > 0 {
			return s + strings.Repeat(" ", d)
		}
		return s
	}
	b := fg(th.Surface)
	box := make([]string, 0, len(names)+3)
	box = append(box, b+"┌"+strings.Repeat("─", textWidth+2)+"┐\x1b[m")
	box = append(box, b+"│ \x1b[m"+pad("\x1b[1m"+fg(th.Text)+title+"\x1b[22m\x1b[m")+b+" │\x1b[m")
	box = append(box, b+"│ "+pad(strings.Repeat("─", textWidth))+" │\x1b[m")
	for i, n := range names {
		row, style := "  "+n, fg(th.Text)
		if i == sel {
			row, style = "› "+n, "\x1b[1m"+fg(th.Base)+bg(th.Accent)
		}
		box = append(box, b+"│ \x1b[m"+pad(style+row+"\x1b[m")+b+" │\x1b[m")
	}
	box = append(box, b+"└"+strings.Repeat("─", textWidth+2)+"┘\x1b[m")
	return box
}

// overlayCenter stamps box onto the middle of a w-by-h grid, the same way
// overlay pins one to the bottom-right corner but floating instead.
func overlayCenter(base string, w, h int, box []string) string {
	if len(box) == 0 || len(box) > h {
		return base
	}
	bw := 0
	for _, l := range box {
		if lw := ansi.StringWidth(l); lw > bw {
			bw = lw
		}
	}
	if bw >= w {
		return base
	}
	lines := strings.Split(base, "\n")
	top := (len(lines) - len(box)) / 2
	left := (w - bw) / 2
	for i, bl := range box {
		y := top + i
		if y < 0 || y >= len(lines) {
			continue
		}
		pre := ansi.Truncate(lines[y], left, "")
		if d := left - ansi.StringWidth(pre); d > 0 {
			pre += strings.Repeat(" ", d)
		}
		trail := w - left - bw
		if trail < 0 {
			trail = 0
		}
		lines[y] = pre + "\x1b[m" + bl + "\x1b[m" + strings.Repeat(" ", trail)
	}
	return strings.Join(lines, "\n")
}
