package server

import (
	"os"
	"testing"

	tea "charm.land/bubbletea/v2"
)

func TestParseKeySpec(t *testing.T) {
	cases := []struct {
		in   string
		want keySpec
	}{
		{"ctrl+b", keySpec{code: 'b', mod: tea.ModCtrl}},
		{"f12", keySpec{code: tea.KeyF12}},
		{"q", keySpec{code: 'q'}},
	}
	for _, c := range cases {
		if got := parseKeySpec(c.in); got != c.want {
			t.Fatalf("parseKeySpec(%q) = %+v, want %+v", c.in, got, c.want)
		}
	}
}

func TestLoadKeymapWritesDefaultsWhenMissing(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", "")

	if got := loadKeymap(); got != defaultKeymap {
		t.Fatalf("loadKeymap() = %+v, want defaults %+v", got, defaultKeymap)
	}
	path, err := keymapPath()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("keybinds.yaml was not written: %v", err)
	}
}
