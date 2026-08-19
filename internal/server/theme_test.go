package server

import (
	"testing"

	tea "charm.land/bubbletea/v2"
)

func TestPickerPreviewAndCancelRestoresOriginal(t *testing.T) {
	s := &server{theme: catppuccinThemes[0], themes: catppuccinThemes}
	s.openPicker()
	if s.picker.sel != 0 {
		t.Fatalf("openPicker sel = %d, want 0 (current theme)", s.picker.sel)
	}

	s.pickerKey(tea.Key{Code: tea.KeyDown})
	if s.theme.Name != catppuccinThemes[1].Name {
		t.Fatalf("down did not preview the next theme: got %q", s.theme.Name)
	}

	s.pickerKey(tea.Key{Code: tea.KeyEscape})
	if s.picker != nil {
		t.Fatal("esc did not close the picker")
	}
	if s.theme.Name != catppuccinThemes[0].Name {
		t.Fatalf("esc did not restore the original theme: got %q", s.theme.Name)
	}
}

func TestPickerEnterKeepsAndPersistsSelection(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", "")

	s := &server{theme: catppuccinThemes[0], themes: catppuccinThemes}
	s.openPicker()
	s.pickerKey(tea.Key{Code: tea.KeyUp}) // wraps to the last theme

	want := catppuccinThemes[len(catppuccinThemes)-1]
	s.pickerKey(tea.Key{Code: tea.KeyEnter})
	if s.picker != nil {
		t.Fatal("enter did not close the picker")
	}
	if s.theme.Name != want.Name {
		t.Fatalf("theme = %q, want %q", s.theme.Name, want.Name)
	}
	if got := loadTheme(); got.Name != want.Name {
		t.Fatalf("loadTheme() = %q, want persisted %q", got.Name, want.Name)
	}
}

func TestLoadThemeFallsBackWhenUnset(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", "")

	want := catppuccinThemes[len(catppuccinThemes)-1] // Mocha
	if got := loadTheme(); got.Name != want.Name {
		t.Fatalf("loadTheme() = %q, want default %q", got.Name, want.Name)
	}
}

func TestHex3ParsesRGB(t *testing.T) {
	r, g, b := hex3("1e66f5")
	if r != 0x1e || g != 0x66 || b != 0xf5 {
		t.Fatalf("hex3 = %d,%d,%d, want 30,102,245", r, g, b)
	}
}
