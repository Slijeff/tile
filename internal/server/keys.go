package server

import (
	"strings"

	tea "charm.land/bubbletea/v2"
)

// keymap lists every key yatm binds. Prefix and Lock take a "mod+key" spec
// (e.g. "ctrl+b", "f12"); every other top-level action is the single
// character typed right after the prefix. Windows and Panes are
// sub-layers: pressing their Key doesn't run an action by itself, it opens
// a second which-key tooltip for the actions nested inside it. Arrow-key
// navigation/resize, cycling a stacked pane's layers at an arrow's dead
// end, the 0-9 window selectors, and pressing the prefix twice to send it
// through are structural and not remapped here.
type keymap struct {
	Prefix       string      `yaml:"prefix"` // enters command mode
	Lock         string      `yaml:"lock"`   // toggles lock mode, prefix or not
	NextWindow   string      `yaml:"next_window"`
	PrevWindow   string      `yaml:"prev_window"`
	CyclePane    string      `yaml:"cycle_pane"`    // move to the next pane in tree order
	NewPane      string      `yaml:"new_pane"`      // split along whichever axis has more room
	SplitHoriz   string      `yaml:"split_horiz"`   // side by side
	SplitVert    string      `yaml:"split_vert"`    // top to bottom
	Stack        string      `yaml:"stack"`         // layer a new pane behind the active one
	Zoom         string      `yaml:"zoom"`          // grow the active pane to fill the window
	Float        string      `yaml:"float"`         // toggle the centered floating terminal
	Preset       string      `yaml:"preset"`        // save the current layout as a named preset
	LoadPreset   string      `yaml:"load_preset"`   // opens the preset picker to restore a saved layout
	DeletePreset string      `yaml:"delete_preset"` // inside the preset picker, deletes the highlighted preset
	Theme        string      `yaml:"theme"`         // opens the colorscheme picker
	Reload       string      `yaml:"reload"`        // re-reads config.yaml from disk
	Detach       string      `yaml:"detach"`
	Quit         string      `yaml:"quit"`
	Windows      windowLayer `yaml:"windows"` // sub-layer: press Key, then New, Kill or Rename
	Panes        paneLayer   `yaml:"panes"`   // sub-layer: press Key, then Kill or Rename
}

// windowLayer is the "w" sub-layer: press Key to open it, then New, Kill,
// or Rename to act on windows. A blank New/Kill/Rename leaves that action
// out of the layer entirely, rather than collapsing to Key alone.
type windowLayer struct {
	Key    string `yaml:"key"`    // leader that opens the layer, e.g. "w"
	New    string `yaml:"new"`    // create a new window
	Kill   string `yaml:"kill"`   // kill the active window
	Rename string `yaml:"rename"` // rename the active window
}

// paneLayer is the "p" sub-layer: press Key to open it, then Kill, Rename,
// or Picker to act on panes. A blank Kill/Rename/Picker leaves that action
// out of the layer entirely, rather than collapsing to Key alone.
type paneLayer struct {
	Key    string `yaml:"key"`    // leader that opens the layer, e.g. "p"
	Kill   string `yaml:"kill"`   // kill the active pane
	Rename string `yaml:"rename"` // rename the active pane
	Picker string `yaml:"picker"` // opens the floating pane picker
}

var defaultKeymap = keymap{
	Prefix:       "ctrl+b",
	Lock:         "f12",
	NextWindow:   "]",
	PrevWindow:   "[",
	CyclePane:    "o",
	NewPane:      "a",
	SplitHoriz:   "|",
	SplitVert:    "-",
	Stack:        "s",
	Zoom:         "z",
	Float:        "f",
	Preset:       "S",
	LoadPreset:   "L",
	DeletePreset: "x",
	Theme:        "T",
	Reload:       "R",
	Detach:       "d",
	Quit:         "q",
	Windows:      windowLayer{Key: "w", New: "c", Kill: "&", Rename: "r"},
	Panes:        paneLayer{Key: "p", Kill: "x", Rename: "r", Picker: "p"},
}

// helpEntry pairs one remappable action's label with its currently bound
// key or chord — the shared source for the chord dispatcher in command.go,
// the full prefix tooltip, and the which-key box shown while a chord is in
// progress.
type helpEntry struct{ key, desc string }

// actionEntries lists every remappable action once, sub-layer actions keyed
// by their full Key+leaf sequence. Prefix and lock aren't included — they're
// matched structurally by keySpec, not as literal text, and can never be
// part of a chord. A blank leaf (Windows.New/Kill/Rename, Panes.Kill/Rename)
// omits that action rather than degenerating to the layer's Key alone.
func actionEntries(km keymap) []helpEntry {
	entries := []helpEntry{
		{km.NextWindow, "next window"},
		{km.PrevWindow, "prev window"},
		{km.CyclePane, "cycle panes"},
		{km.NewPane, "new pane (auto direction)"},
		{km.SplitHoriz, "split side-by-side"},
		{km.SplitVert, "split top-to-bottom"},
		{km.Stack, "stack a pane"},
		{km.Zoom, "zoom the active pane"},
		{km.Float, "toggle floating terminal"},
		{km.Preset, "save preset"},
		{km.LoadPreset, "load preset"},
		{km.Theme, "colorscheme picker"},
		{km.Reload, "reload config"},
		{km.Detach, "detach"},
		{km.Quit, "quit"},
	}
	for _, sub := range []struct{ leader, leaf, desc string }{
		{km.Windows.Key, km.Windows.New, "new window"},
		{km.Windows.Key, km.Windows.Kill, "kill window"},
		{km.Windows.Key, km.Windows.Rename, "rename window"},
		{km.Panes.Key, km.Panes.Kill, "kill pane"},
		{km.Panes.Key, km.Panes.Rename, "rename pane"},
		{km.Panes.Key, km.Panes.Picker, "pane picker"},
	} {
		if sub.leaf != "" {
			entries = append(entries, helpEntry{sub.leader + sub.leaf, sub.desc})
		}
	}
	return entries
}

// keySpec matches a tea.Key by code and modifier, for the two bindings
// (prefix, lock) that need a modifier rather than a plain character.
type keySpec struct {
	code rune
	mod  tea.KeyMod
}

func parseKeySpec(s string) keySpec {
	var mod tea.KeyMod
loop:
	for {
		switch {
		case strings.HasPrefix(s, "ctrl+"):
			mod |= tea.ModCtrl
			s = s[len("ctrl+"):]
		case strings.HasPrefix(s, "alt+"):
			mod |= tea.ModAlt
			s = s[len("alt+"):]
		case strings.HasPrefix(s, "shift+"):
			mod |= tea.ModShift
			s = s[len("shift+"):]
		default:
			break loop
		}
	}
	if strings.EqualFold(s, "f12") {
		return keySpec{code: tea.KeyF12, mod: mod}
	}
	r := []rune(s)
	if len(r) == 1 {
		return keySpec{code: r[0], mod: mod}
	}
	return keySpec{}
}

func (ks keySpec) matches(k tea.Key) bool {
	return k.Code == ks.code && k.Mod == ks.mod
}
