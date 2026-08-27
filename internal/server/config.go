package server

import (
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// configDir is where tile keeps its config: always ~/.config/tile, on every
// OS, rather than os.UserConfigDir's platform default (e.g. ~/Library/
// Application Support on macOS).
func configDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".config", "tile"), nil
}

// configPath returns where the config file lives, creating no directories.
func configPath() (string, error) {
	dir, err := configDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "config.yaml"), nil
}

// config is everything tile persists across restarts: the keymap, the
// active colorscheme and the pane gutter width, all in one file so there's
// a single place to look.
type config struct {
	Theme  string `yaml:"theme"`
	Margin int    `yaml:"margin"`
	Keymap keymap `yaml:"keymap"`
}

var defaultConfig = config{
	Theme:  defaultTheme.Name,
	Margin: 1,
	Keymap: defaultKeymap,
}

// loadConfig reads config.yaml, writing out the defaults (so the file
// explicitly lists every keybind and the theme for the user to edit) if none
// exists yet. Any error falls back to the defaults rather than failing
// startup.
func loadConfig() config {
	path, err := configPath()
	if err != nil {
		return defaultConfig
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			writeDefaultConfig(path)
		}
		return defaultConfig
	}
	cfg := defaultConfig
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return defaultConfig
	}
	return cfg
}

func writeDefaultConfig(path string) {
	data, err := yaml.Marshal(defaultConfig)
	if err != nil {
		return
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return
	}
	_ = os.WriteFile(path, data, 0o600)
}

// saveTheme persists a colorscheme choice, leaving whatever keymap is
// currently on disk (or the defaults, if there's no file yet) untouched.
func saveTheme(name string) {
	path, err := configPath()
	if err != nil {
		return
	}
	cfg := loadConfig()
	cfg.Theme = name
	data, err := yaml.Marshal(cfg)
	if err != nil {
		return
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return
	}
	_ = os.WriteFile(path, data, 0o600)
}

// resolveTheme finds the palette matching name, falling back to the default
// theme if name is empty or doesn't match a known one.
func resolveTheme(name string) theme {
	for _, t := range catppuccinThemes {
		if t.Name == name {
			return t
		}
	}
	return defaultTheme
}

// reloadConfig re-reads config.yaml from disk and applies it live: keymap,
// prefix/lock bindings, theme and pane margin all take effect on the very
// next frame, no restart required. A missing or malformed file falls back
// to the defaults, same as startup.
func (s *server) reloadConfig() {
	cfg := loadConfig()
	s.km = cfg.Keymap
	s.prefixSpec = parseKeySpec(cfg.Keymap.Prefix)
	s.lockSpec = parseKeySpec(cfg.Keymap.Lock)
	s.theme = resolveTheme(cfg.Theme)
	s.margin = max(cfg.Margin, 0)
}
