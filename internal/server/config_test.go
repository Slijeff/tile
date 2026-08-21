package server

import (
	"os"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestLoadConfigWritesDefaultsWhenMissing(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", "")

	if got := loadConfig(); got != defaultConfig {
		t.Fatalf("loadConfig() = %+v, want defaults %+v", got, defaultConfig)
	}
	path, err := configPath()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("config.yaml was not written: %v", err)
	}
}

func TestReloadConfigAppliesLiveChanges(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", "")

	s := &server{km: defaultKeymap, prefixSpec: parseKeySpec(defaultKeymap.Prefix),
		theme: resolveTheme(defaultConfig.Theme), margin: defaultConfig.Margin}

	path, err := configPath()
	if err != nil {
		t.Fatal(err)
	}
	cfg := defaultConfig
	cfg.Margin = 3
	cfg.Theme = catppuccinThemes[0].Name
	cfg.Keymap.Quit = "Q"
	writeDefaultConfig(path) // ensure the dir exists
	data, err := yaml.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}

	s.reloadConfig()

	if s.margin != 3 {
		t.Fatalf("margin not reloaded: got %d, want 3", s.margin)
	}
	if s.theme.Name != catppuccinThemes[0].Name {
		t.Fatalf("theme not reloaded: got %q, want %q", s.theme.Name, catppuccinThemes[0].Name)
	}
	if s.km.Quit != "Q" {
		t.Fatalf("keymap not reloaded: got %q, want %q", s.km.Quit, "Q")
	}
}
