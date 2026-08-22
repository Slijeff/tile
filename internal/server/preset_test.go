package server

import (
	"testing"

	tea "charm.land/bubbletea/v2"
)

func newPresetTestServer(windows ...*window) *server {
	return &server{
		windows:    windows,
		w:          40,
		h:          10,
		events:     make(chan event, 256),
		km:         defaultKeymap,
		prefixSpec: parseKeySpec(defaultKeymap.Prefix),
		lockSpec:   parseKeySpec(defaultKeymap.Lock),
	}
}

// capturePreset must record a window's manual tab name, a pane's manual
// border name, and the split shape (dir/weight) between them — everything
// applyPreset needs to rebuild the same arrangement later.
func TestCapturePresetRecordsNamesAndLayout(t *testing.T) {
	left := &pane{title: "zsh", named: true, borderName: "server"}
	right := &pane{title: "vim"}
	root := &node{dir: dirHoriz, weight: 1}
	l := &node{pane: left, weight: 1, parent: root}
	r := &node{pane: right, weight: 1, parent: root}
	root.children = []*node{l, r}

	win := &window{name: "work", named: true, root: root, active: l}
	s := newPresetTestServer(win)

	pr := s.capturePreset("mine")
	if pr.Name != "mine" {
		t.Fatalf("preset.Name = %q, want %q", pr.Name, "mine")
	}
	if len(pr.Windows) != 1 {
		t.Fatalf("len(pr.Windows) = %d, want 1", len(pr.Windows))
	}
	pw := pr.Windows[0]
	if !pw.Named || pw.Name != "work" {
		t.Fatalf("window capture = %+v, want named %q", pw, "work")
	}
	if pw.Root.Dir != dirHoriz || len(pw.Root.Children) != 2 {
		t.Fatalf("root capture = %+v, want a 2-child horiz split", pw.Root)
	}
	if !pw.Root.Children[0].Named || pw.Root.Children[0].Name != "server" {
		t.Fatalf("left leaf capture = %+v, want named %q", pw.Root.Children[0], "server")
	}
	if pw.Root.Children[1].Named {
		t.Fatalf("right leaf capture = %+v, want unnamed (no manual rename)", pw.Root.Children[1])
	}
}

// savePreset/loadPresets must round-trip through disk, and re-saving under
// a name already used should replace it in place rather than duplicating it.
func TestSaveAndLoadPresetsRoundTripAndReplace(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	pr := preset{Name: "one", Windows: []presetWindow{{Root: presetNode{Weight: 1}}}}
	savePreset(pr)

	got := loadPresets()
	if len(got) != 1 || got[0].Name != "one" {
		t.Fatalf("loadPresets() = %+v, want one preset named %q", got, "one")
	}

	pr2 := preset{Name: "one", Windows: []presetWindow{
		{Root: presetNode{Weight: 1}}, {Root: presetNode{Weight: 1}},
	}}
	savePreset(pr2)
	got = loadPresets()
	if len(got) != 1 {
		t.Fatalf("re-saving %q should replace it, got %d presets", "one", len(got))
	}
	if len(got[0].Windows) != 2 {
		t.Fatalf("replaced preset has %d windows, want 2", len(got[0].Windows))
	}

	savePreset(preset{Name: "two"})
	got = loadPresets()
	if len(got) != 2 {
		t.Fatalf("saving a new name should append, got %d presets", len(got))
	}
}

// The save-preset prompt should open blank, accept typed text, and commit
// it to disk only on Enter with a non-blank name; Esc must discard it.
func TestPresetPromptSaveAndCancel(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	win := &window{root: &node{pane: &pane{}, weight: 1}}
	s := newPresetTestServer(win)

	s.openPresetPrompt()
	if s.presetPrompt.text != "" {
		t.Fatalf("presetPrompt seeded with %q, want blank", s.presetPrompt.text)
	}
	for _, r := range "mypreset" {
		s.presetPromptKey(tea.Key{Text: string(r)})
	}
	s.presetPromptKey(tea.Key{Code: tea.KeyEnter})
	if s.presetPrompt != nil {
		t.Fatal("enter did not close the prompt")
	}
	if got := loadPresets(); len(got) != 1 || got[0].Name != "mypreset" {
		t.Fatalf("loadPresets() = %+v, want one preset named %q", got, "mypreset")
	}

	s.openPresetPrompt()
	s.presetPromptKey(tea.Key{Text: "x"})
	s.presetPromptKey(tea.Key{Code: tea.KeyEscape})
	if s.presetPrompt != nil {
		t.Fatal("esc did not close the prompt")
	}
	if got := loadPresets(); len(got) != 1 {
		t.Fatalf("esc must not save, got %d presets", len(got))
	}

	// A blank name must not be saved either.
	s.openPresetPrompt()
	s.presetPromptKey(tea.Key{Code: tea.KeyEnter})
	if got := loadPresets(); len(got) != 1 {
		t.Fatalf("blank name must not be saved, got %d presets", len(got))
	}
}

// The save/load keybinds must reach the prompt and picker through the
// prefix dispatcher, like every other chord.
func TestPresetKeybindsOpenPromptAndList(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	savePreset(preset{Name: "saved", Windows: []presetWindow{{Root: presetNode{Weight: 1}}}})

	win := &window{root: &node{pane: &pane{}, weight: 1}}
	s := newPresetTestServer(win)

	s.command(tea.Key{Text: s.km.Preset})
	if s.presetPrompt == nil {
		t.Fatal("preset keybind did not open the save prompt")
	}
	s.presetPromptKey(tea.Key{Code: tea.KeyEscape})

	s.command(tea.Key{Text: s.km.LoadPreset})
	if s.presetList == nil {
		t.Fatal("load-preset keybind did not open the picker")
	}
	if len(s.presetList.names) != 1 || s.presetList.names[0] != "saved" {
		t.Fatalf("presetList.names = %v, want [saved]", s.presetList.names)
	}
}

// deletePreset must remove only the named preset, leaving the rest of the
// file intact.
func TestDeletePresetRemovesOnlyThatOne(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	savePreset(preset{Name: "one"})
	savePreset(preset{Name: "two"})

	deletePreset("one")

	got := loadPresets()
	if len(got) != 1 || got[0].Name != "two" {
		t.Fatalf("loadPresets() = %+v, want only %q left", got, "two")
	}

	// Deleting a name that isn't there is a no-op, not an error.
	deletePreset("missing")
	if got := loadPresets(); len(got) != 1 {
		t.Fatalf("deleting an unknown name should not change anything, got %+v", got)
	}
}

// The delete key in the preset picker must remove the highlighted preset
// from disk and from the open picker's own list, moving the highlight back
// onto the list rather than off the end of it, and it must not disturb any
// other saved preset.
func TestPresetListDeleteKeyRemovesHighlighted(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	savePreset(preset{Name: "one"})
	savePreset(preset{Name: "two"})

	win := &window{root: &node{pane: &pane{}, weight: 1}}
	s := newPresetTestServer(win)

	s.openPresetList()
	s.presetList.sel = 1 // highlight "two", the last row
	s.presetListKey(tea.Key{Text: s.km.DeletePreset})

	if s.presetList == nil {
		t.Fatal("deleting one of two presets should leave the picker open")
	}
	if len(s.presetList.names) != 1 || s.presetList.names[0] != "one" {
		t.Fatalf("presetList.names = %v, want [one]", s.presetList.names)
	}
	if s.presetList.sel != 0 {
		t.Fatalf("presetList.sel = %d after deleting the last row, want 0", s.presetList.sel)
	}
	if got := loadPresets(); len(got) != 1 || got[0].Name != "one" {
		t.Fatalf("loadPresets() = %+v, want only %q left on disk", got, "one")
	}

	// Deleting the last remaining preset closes the picker rather than
	// leaving it open on an empty list.
	s.presetListKey(tea.Key{Text: s.km.DeletePreset})
	if s.presetList != nil {
		t.Fatal("deleting the last preset should close the picker")
	}
	if got := loadPresets(); len(got) != 0 {
		t.Fatalf("loadPresets() = %+v, want none left", got)
	}
}

// applyPreset must rebuild the saved split shape as a brand new window,
// spawning real panes sized to their place in the tree and restoring any
// manual names, all without touching the window(s) already open.
func TestApplyPresetRebuildsWindowLayoutAndNames(t *testing.T) {
	pr := preset{
		Name: "restored",
		Windows: []presetWindow{{
			Name: "work", Named: true,
			Root: presetNode{
				Dir: dirHoriz, Weight: 1,
				Children: []presetNode{
					{Weight: 1, Name: "left", Named: true},
					{Weight: 1},
				},
			},
		}},
	}

	existing := &window{root: &node{pane: &pane{}, weight: 1}}
	s := newPresetTestServer(existing)

	s.applyPreset(pr)

	if len(s.windows) != 2 {
		t.Fatalf("len(s.windows) = %d, want 2 (existing + restored)", len(s.windows))
	}
	w := s.windows[1]
	defer func() {
		for _, leaf := range w.panes() {
			leaf.pane.close()
		}
	}()

	if !w.named || w.name != "work" {
		t.Fatalf("restored window = named:%v name:%q, want named %q", w.named, w.name, "work")
	}
	if w.root.dir != dirHoriz || len(w.root.children) != 2 {
		t.Fatalf("restored root = %+v, want a 2-child horiz split", w.root)
	}
	left, right := w.root.children[0], w.root.children[1]
	if left.pane == nil || right.pane == nil {
		t.Fatal("restored leaves should each have a live pane")
	}
	if !left.pane.named || left.pane.borderName != "left" {
		t.Fatalf("left pane = named:%v borderName:%q, want named %q", left.pane.named, left.pane.borderName, "left")
	}
	if right.pane.named {
		t.Fatal("right pane should not be renamed: it had no manual name saved")
	}
	if left.pane.w == 0 || left.pane.h == 0 {
		t.Fatalf("restored pane should be sized from the computed layout, got %dx%d", left.pane.w, left.pane.h)
	}
	if w.active == nil || w.active.pane == nil {
		t.Fatal("restored window should have an active pane set")
	}

	// The pre-existing window must be untouched.
	if s.windows[0] != existing {
		t.Fatal("applyPreset must append, not replace, existing windows")
	}
}
