package server

import (
	"os"
	"path/filepath"
	"strings"

	tea "charm.land/bubbletea/v2"
	"gopkg.in/yaml.v3"
)

// keymap lists every key yatm binds. Prefix and Lock take a "mod+key" spec
// (e.g. "ctrl+b", "f12"); every other action is the single character typed
// right after the prefix. Arrow-key navigation/resize, the 0-9 window
// selectors, and pressing the prefix twice to send it through are structural
// and not remapped here.
type keymap struct {
	Prefix     string `yaml:"prefix"` // enters command mode
	Lock       string `yaml:"lock"`   // toggles lock mode, prefix or not
	NewWindow  string `yaml:"new_window"`
	NextWindow string `yaml:"next_window"`
	PrevWindow string `yaml:"prev_window"`
	CyclePane  string `yaml:"cycle_pane"`  // move to the next pane in tree order
	NewPane    string `yaml:"new_pane"`    // split along whichever axis has more room
	SplitHoriz string `yaml:"split_horiz"` // side by side
	SplitVert  string `yaml:"split_vert"`  // top to bottom
	Stack      string `yaml:"stack"`       // layer a new pane behind the active one
	CycleLayer string `yaml:"cycle_layer"` // switch which stacked layer shows
	KillPane   string `yaml:"kill_pane"`
	KillWindow string `yaml:"kill_window"`
	Theme      string `yaml:"theme"` // opens the colorscheme picker
	Detach     string `yaml:"detach"`
	Quit       string `yaml:"quit"`
}

var defaultKeymap = keymap{
	Prefix:     "ctrl+b",
	Lock:       "f12",
	NewWindow:  "c",
	NextWindow: "n",
	PrevWindow: "p",
	CyclePane:  "o",
	NewPane:    "a",
	SplitHoriz: "%",
	SplitVert:  `"`,
	Stack:      "s",
	CycleLayer: "z",
	KillPane:   "x",
	KillWindow: "&",
	Theme:      "T",
	Detach:     "d",
	Quit:       "q",
}

// configDir is where yatm keeps its config files (keybinds.yaml, theme.yaml):
// always ~/.config/yatm, on every OS, rather than os.UserConfigDir's
// platform default (e.g. ~/Library/Application Support on macOS).
func configDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".config", "yatm"), nil
}

// keymapPath returns where the config file lives, creating no directories.
func keymapPath() (string, error) {
	dir, err := configDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "keybinds.yaml"), nil
}

// loadKeymap reads the config file, writing out the defaults (so the file
// explicitly lists every keybind for the user to edit) if none exists yet.
// Any error falls back to the defaults rather than failing startup.
func loadKeymap() keymap {
	path, err := keymapPath()
	if err != nil {
		return defaultKeymap
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			writeDefaultKeymap(path)
		}
		return defaultKeymap
	}
	km := defaultKeymap
	if err := yaml.Unmarshal(data, &km); err != nil {
		return defaultKeymap
	}
	return km
}

func writeDefaultKeymap(path string) {
	data, err := yaml.Marshal(defaultKeymap)
	if err != nil {
		return
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return
	}
	_ = os.WriteFile(path, data, 0o600)
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
