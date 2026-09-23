package server

import (
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
)

// saveCommand/loadCommands must round-trip through disk, and re-saving under
// a name already used should replace it in place rather than duplicating it;
// deleteCommand should remove only the named entry.
func TestSaveLoadDeleteCommandsRoundTripReplaceAndDelete(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	saveCommand(savedCommand{Name: "build", Cmd: "make"})
	got := loadCommands()
	if len(got) != 1 || got[0].Name != "build" || got[0].Cmd != "make" {
		t.Fatalf("loadCommands() = %+v, want one command named %q", got, "build")
	}

	saveCommand(savedCommand{Name: "build", Dir: "/tmp", Cmd: "make -j8"})
	got = loadCommands()
	if len(got) != 1 {
		t.Fatalf("re-saving %q should replace it, got %d commands", "build", len(got))
	}
	if got[0].Dir != "/tmp" || got[0].Cmd != "make -j8" {
		t.Fatalf("replaced command = %+v, want updated dir/cmd", got[0])
	}

	saveCommand(savedCommand{Name: "test", Cmd: "go test ./..."})
	got = loadCommands()
	if len(got) != 2 {
		t.Fatalf("saving a new name should append, got %d commands", len(got))
	}

	deleteCommand("build")
	got = loadCommands()
	if len(got) != 1 || got[0].Name != "test" {
		t.Fatalf("loadCommands() after delete = %+v, want only %q left", got, "test")
	}
}

// The add/edit form should move focus between its three fields, accept
// typed text into whichever one is focused, and only save (closing the
// form) on Enter with a non-blank alias.
func TestCommandFormKeyFocusTypingAndSave(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	win := &window{name: "0", root: &node{weight: 1, pane: &pane{}}, active: &node{weight: 1, pane: &pane{}}}
	s := newCommandTestServer(win)
	s.openCommandForm()

	for _, r := range "deploy" {
		s.commandFormKey(tea.Key{Code: r, Text: string(r)})
	}
	if s.commandForm.fields[fieldAlias] != "deploy" {
		t.Fatalf("alias field = %q, want %q", s.commandForm.fields[fieldAlias], "deploy")
	}

	s.commandFormKey(tea.Key{Code: tea.KeyDown})
	if s.commandForm.focus != fieldDir {
		t.Fatalf("focus after Down = %d, want fieldDir", s.commandForm.focus)
	}
	for _, r := range "/srv/app" {
		s.commandFormKey(tea.Key{Code: r, Text: string(r)})
	}

	s.commandFormKey(tea.Key{Code: tea.KeyDown})
	for _, r := range "./deploy.sh" {
		s.commandFormKey(tea.Key{Code: r, Text: string(r)})
	}

	// Blank the alias and confirm Enter is a no-op rather than saving —
	// Enter reads fields[fieldAlias] regardless of which field has focus.
	s.commandForm.fields[fieldAlias] = ""
	s.commandFormKey(tea.Key{Code: tea.KeyEnter})
	if s.commandForm == nil {
		t.Fatal("Enter with a blank alias should not close the form")
	}
	if len(loadCommands()) != 0 {
		t.Fatal("Enter with a blank alias should not save anything")
	}

	s.commandForm.fields[fieldAlias] = "deploy"
	s.commandFormKey(tea.Key{Code: tea.KeyEnter})
	if s.commandForm != nil {
		t.Fatal("Enter with a non-blank alias should close the form")
	}
	got := loadCommands()
	if len(got) != 1 || got[0].Name != "deploy" || got[0].Dir != "/srv/app" || got[0].Cmd != "./deploy.sh" {
		t.Fatalf("loadCommands() = %+v, want the typed command", got)
	}
}

// Opt/alt+Enter should insert a newline into the cmd field rather than
// saving the form, and only there — the same key on the alias field should
// fall through to the ordinary save behavior instead.
func TestCommandFormKeyAltEnterInsertsNewlineInCmdOnly(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	win := &window{name: "0", root: &node{weight: 1, pane: &pane{}}, active: &node{weight: 1, pane: &pane{}}}
	s := newCommandTestServer(win)
	s.openCommandForm()

	for _, r := range "deploy" {
		s.commandFormKey(tea.Key{Code: r, Text: string(r)})
	}
	s.commandFormKey(tea.Key{Code: tea.KeyDown})
	s.commandFormKey(tea.Key{Code: tea.KeyDown}) // focus now on cmd

	for _, r := range "echo one" {
		s.commandFormKey(tea.Key{Code: r, Text: string(r)})
	}
	s.commandFormKey(tea.Key{Code: tea.KeyEnter, Mod: tea.ModAlt})
	for _, r := range "echo two" {
		s.commandFormKey(tea.Key{Code: r, Text: string(r)})
	}
	if s.commandForm == nil {
		t.Fatal("alt+enter should insert a newline, not save/close the form")
	}
	want := "echo one\necho two"
	if got := s.commandForm.fields[fieldCmd]; got != want {
		t.Fatalf("cmd field = %q, want %q", got, want)
	}

	s.commandFormKey(tea.Key{Code: tea.KeyEnter})
	if s.commandForm != nil {
		t.Fatal("plain enter should still save and close the form")
	}
	got := loadCommands()
	if len(got) != 1 || got[0].Cmd != want {
		t.Fatalf("loadCommands() = %+v, want the multi-line cmd preserved", got)
	}
}

// ctrlKey builds the tea.Key a keymap spec like "ctrl+x" matches, so tests
// don't have to hardcode the exact default binding.
func ctrlKey(spec string) tea.Key {
	ks := parseKeySpec(spec)
	return tea.Key{Code: ks.code, Mod: ks.mod}
}

// The picker/manager's new/edit/delete keys (ctrl combos, since plain
// letters feed the search box) should add, edit and remove entries in
// place.
func TestCommandListKeyNewEditDelete(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	saveCommand(savedCommand{Name: "one", Cmd: "echo one"})
	saveCommand(savedCommand{Name: "two", Cmd: "echo two"})

	win := &window{name: "0", root: &node{weight: 1, pane: &pane{}}, active: &node{weight: 1, pane: &pane{}}}
	s := newCommandTestServer(win)
	s.openCommandList()
	if len(s.commandList.shown) != 2 {
		t.Fatalf("openCommandList loaded %d commands, want 2", len(s.commandList.shown))
	}

	s.commandListKey(ctrlKey(s.km.DeleteCommand))
	if got := loadCommands(); len(got) != 1 || got[0].Name != "two" {
		t.Fatalf("after delete, loadCommands() = %+v, want only %q left", got, "two")
	}
	if len(s.commandList.shown) != 1 {
		t.Fatalf("commandList.shown after delete = %d, want 1", len(s.commandList.shown))
	}

	s.commandListKey(ctrlKey(s.km.NewCommand))
	if s.commandForm == nil {
		t.Fatal("NewCommand should open the add form")
	}
	for _, r := range "three" {
		s.commandFormKey(tea.Key{Code: r, Text: string(r)})
	}
	s.commandFormKey(tea.Key{Code: tea.KeyEnter})
	if len(s.commandList.shown) != 2 {
		t.Fatalf("commandList should refresh after the form saves, got %d commands", len(s.commandList.shown))
	}
}

// Typing into the picker should narrow shown to matches and reset the
// highlight; Backspace should widen it back out.
func TestCommandListKeyTypingFiltersShown(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	saveCommand(savedCommand{Name: "one", Cmd: "echo one"})
	saveCommand(savedCommand{Name: "two", Cmd: "echo two"})

	win := &window{name: "0", root: &node{weight: 1, pane: &pane{}}, active: &node{weight: 1, pane: &pane{}}}
	s := newCommandTestServer(win)
	s.openCommandList()

	for _, r := range "two" {
		s.commandListKey(tea.Key{Code: r, Text: string(r)})
	}
	if len(s.commandList.shown) != 1 || s.commandList.shown[0].Name != "two" {
		t.Fatalf("shown after typing %q = %+v, want only %q", s.commandList.query, s.commandList.shown, "two")
	}

	s.commandListKey(tea.Key{Code: tea.KeyBackspace})
	s.commandListKey(tea.Key{Code: tea.KeyBackspace})
	s.commandListKey(tea.Key{Code: tea.KeyBackspace})
	if s.commandList.query != "" || len(s.commandList.shown) != 2 {
		t.Fatalf("shown after clearing query = %+v, want both commands back", s.commandList.shown)
	}
}

// substringFilter, the no-fzf fallback, should match either field,
// case-insensitively, and drop everything else.
func TestSubstringFilterMatchesNameOrCmdCaseInsensitively(t *testing.T) {
	cmds := []savedCommand{
		{Name: "build", Cmd: "make -j8"},
		{Name: "deploy", Cmd: "./ship.sh PROD"},
		{Name: "test", Cmd: "go test ./..."},
	}
	got := substringFilter(cmds, "PROD")
	if len(got) != 1 || got[0].Name != "deploy" {
		t.Fatalf("substringFilter(_, %q) = %+v, want only %q (matched via Cmd, case-insensitive)", "PROD", got, "deploy")
	}
	got = substringFilter(cmds, "test")
	if len(got) != 1 || got[0].Name != "test" {
		t.Fatalf("substringFilter(_, %q) = %+v, want only %q (matched via Name)", "test", got, "test")
	}
}

// When the fzf binary is on PATH, filterCommands should hand off to it and
// get back fuzzy (non-substring) matches. Skipped where fzf isn't
// installed, since that's the substringFilter path exercised above.
func TestFilterCommandsUsesFzfFuzzyMatchingWhenAvailable(t *testing.T) {
	if fzfPath() == "" {
		t.Skip("fzf not installed")
	}
	cmds := []savedCommand{
		{Name: "build", Cmd: "make -j8"},
		{Name: "test", Cmd: "go test ./..."},
	}
	got := filterCommands(cmds, "tst")
	if len(got) != 1 || got[0].Name != "test" {
		t.Fatalf("filterCommands(_, %q) = %+v, want only %q (fuzzy subsequence match)", "tst", got, "test")
	}
}

// runSavedCommand should type "cd <dir>" followed by Enter, then the
// command itself followed by Enter, into the active pane's shell.
func TestRunSavedCommandSendsCdThenCommand(t *testing.T) {
	events := make(chan event, 256)
	p, err := newPane(0, 40, 10, events, 0)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { p.close() })

	win := &window{name: "0", root: &node{weight: 1, pane: p}, active: &node{weight: 1, pane: p}}
	s := newCommandTestServer(win)

	s.runSavedCommand(savedCommand{Name: "x", Dir: "/tmp", Cmd: "echo hi-there"})

	var got []byte
	deadline := time.After(2 * time.Second)
	for {
		select {
		case ev := <-events:
			if ev.kind == evOutput {
				got = append(got, ev.data...)
				if strings.Contains(string(got), "cd /tmp") && strings.Contains(string(got), "echo hi-there") {
					return
				}
			}
		case <-deadline:
			t.Fatalf("shell never echoed the sent command, got %q", got)
		}
	}
}
