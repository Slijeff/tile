package server

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"

	tea "charm.land/bubbletea/v2"
	"gopkg.in/yaml.v3"
)

// savedCommand is a named shell command a user wants to run again later,
// optionally in a specific working directory. Unlike a preset it captures
// no layout — just enough to re-type the command for the user.
type savedCommand struct {
	Name string `yaml:"name"` // alias, shown in the picker
	Dir  string `yaml:"dir,omitempty"`
	Cmd  string `yaml:"cmd"`
}

// commandsPath returns where saved commands live, alongside config.yaml.
func commandsPath() (string, error) {
	dir, err := configDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "commands.yaml"), nil
}

// commandFile is the on-disk shape of commands.yaml: every saved command in
// one file, so there's a single place to look, edit or back up.
type commandFile struct {
	Commands []savedCommand `yaml:"commands"`
}

// loadCommands reads every saved command, or nil if there is no file yet or
// it can't be parsed — the same "fall back quietly" contract loadPresets
// uses, since a corrupt file shouldn't keep the daemon from starting.
func loadCommands() []savedCommand {
	path, err := commandsPath()
	if err != nil {
		return nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var f commandFile
	if err := yaml.Unmarshal(data, &f); err != nil {
		return nil
	}
	return f.Commands
}

// saveCommand persists c to commands.yaml, replacing any existing command
// with the same name in place so saving under a name already in use updates
// it instead of piling up a duplicate.
func saveCommand(c savedCommand) {
	path, err := commandsPath()
	if err != nil {
		return
	}
	cs := loadCommands()
	found := false
	for i, existing := range cs {
		if existing.Name == c.Name {
			cs[i] = c
			found = true
			break
		}
	}
	if !found {
		cs = append(cs, c)
	}
	data, err := yaml.Marshal(commandFile{Commands: cs})
	if err != nil {
		return
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return
	}
	_ = os.WriteFile(path, data, 0o600)
}

// deleteCommand removes the named command from commands.yaml, if it exists.
// A no-op, like save/load, if the file can't be read or written.
func deleteCommand(name string) {
	path, err := commandsPath()
	if err != nil {
		return
	}
	cs := loadCommands()
	out := cs[:0]
	for _, c := range cs {
		if c.Name != name {
			out = append(out, c)
		}
	}
	data, err := yaml.Marshal(commandFile{Commands: out})
	if err != nil {
		return
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return
	}
	_ = os.WriteFile(path, data, 0o600)
}

// runSavedCommand types c into the active pane: cd to its saved directory
// first, if it has one, then the command itself, each followed by Enter.
// Pasted rather than sent key-by-key so bracketed-paste mode is honored,
// the same path command.go's paste() uses for pane text.
func (s *server) runSavedCommand(c savedCommand) {
	p := s.activePane()
	if p == nil {
		return
	}
	if c.Dir != "" {
		p.emu.Paste("cd " + c.Dir)
		sendKeyTo(p, tea.Key{Code: tea.KeyEnter})
	}
	p.emu.Paste(c.Cmd)
	sendKeyTo(p, tea.Key{Code: tea.KeyEnter})
	s.dirty = true
}

// --- search -------------------------------------------------------------

// fzfPath is the fzf binary's location, looked up once — a fresh
// exec.LookPath per keystroke would be wasteful, and fzf's absence isn't
// going to change while tile is running.
var fzfPath = sync.OnceValue(func() string {
	p, _ := exec.LookPath("fzf")
	return p
})

// filterCommands narrows cmds to those matching query against their alias
// or command text, ranked by fzf's fuzzy algorithm when the fzf binary is
// on PATH, falling back to a plain case-insensitive substring match
// otherwise. A blank query matches everything, in its original order.
func filterCommands(cmds []savedCommand, query string) []savedCommand {
	if query == "" {
		return cmds
	}
	if path := fzfPath(); path != "" {
		if filtered, ok := fzfFilter(path, cmds, query); ok {
			return filtered
		}
	}
	return substringFilter(cmds, query)
}

// fzfFilter runs fzf non-interactively (--filter) over cmds' name/cmd text,
// returning them fuzzy-matched and ranked. Each line is prefixed with its
// index into cmds so a match can be mapped back to its savedCommand;
// --nth restricts fzf's matching to the name/cmd fields so the index
// itself is never part of the search. ok is false only when fzf couldn't
// be run at all — a clean "no matches" is reported as an empty, ok slice,
// since fzf --filter exits 1 for that.
func fzfFilter(path string, cmds []savedCommand, query string) (filtered []savedCommand, ok bool) {
	var in strings.Builder
	for i, c := range cmds {
		// oneLine: a candidate is one stdin line to fzf, so an embedded
		// newline in a multi-line command would otherwise split it into two
		// candidates and break the index-prefix mapping back to cmds[idx].
		fmt.Fprintf(&in, "%d\t%s\t%s\n", i, c.Name, oneLine(c.Cmd))
	}
	cmd := exec.Command(path, "--filter", query, "--delimiter", "\t", "--nth", "2,3")
	cmd.Stdin = strings.NewReader(in.String())
	out, err := cmd.Output()
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) && exitErr.ExitCode() == 1 {
			return nil, true // ran fine, just no matches
		}
		return nil, false
	}
	for _, line := range strings.Split(strings.TrimRight(string(out), "\n"), "\n") {
		idxStr, _, found := strings.Cut(line, "\t")
		if !found {
			continue
		}
		idx, err := strconv.Atoi(idxStr)
		if err != nil || idx < 0 || idx >= len(cmds) {
			continue
		}
		filtered = append(filtered, cmds[idx])
	}
	return filtered, true
}

// substringFilter is the fallback search when fzf isn't installed: a plain
// case-insensitive substring match against either field, in the original
// (unranked) order.
func substringFilter(cmds []savedCommand, query string) []savedCommand {
	q := strings.ToLower(query)
	var out []savedCommand
	for _, c := range cmds {
		if strings.Contains(strings.ToLower(c.Name), q) || strings.Contains(strings.ToLower(c.Cmd), q) {
			out = append(out, c)
		}
	}
	return out
}

// --- add/edit form -----------------------------------------------------

// commandForm is the add/edit-command prompt's open state: three text
// fields (alias, directory, command) and which one is focused. editName is
// set to the command's original name when editing, so commandFormKey knows
// to replace it rather than add a new entry — and to delete the old name if
// the alias itself was changed.
type commandForm struct {
	fields   [3]string
	focus    int
	editing  bool
	editName string
}

const (
	fieldAlias = iota
	fieldDir
	fieldCmd
)

// openCommandForm opens the add-command form blank.
func (s *server) openCommandForm() {
	s.commandForm = &commandForm{}
}

// openCommandFormFor opens the form seeded with c's fields, in edit mode.
func (s *server) openCommandFormFor(c savedCommand) {
	s.commandForm = &commandForm{
		fields:   [3]string{c.Name, c.Dir, c.Cmd},
		editing:  true,
		editName: c.Name,
	}
}

// commandFormKey handles one keystroke while the add/edit form is open.
// Up/Down moves the focused field; Enter saves from any field, provided the
// alias isn't blank; opt/alt+Enter inserts a newline instead, but only in
// the cmd field — the only one a multi-line value makes sense for; Esc
// discards the edit without saving.
func (s *server) commandFormKey(k tea.Key) {
	s.dirty = true
	f := s.commandForm
	switch {
	case k.Code == tea.KeyUp || (k.Code == tea.KeyTab && k.Mod&tea.ModShift != 0):
		f.focus = (f.focus - 1 + len(f.fields)) % len(f.fields)
	case k.Code == tea.KeyDown || k.Code == tea.KeyTab:
		f.focus = (f.focus + 1) % len(f.fields)
	case k.Code == tea.KeyEnter && k.Mod&tea.ModAlt != 0 && f.focus == fieldCmd:
		f.fields[fieldCmd] += "\n"
	case k.Code == tea.KeyEnter:
		name := strings.TrimSpace(f.fields[fieldAlias])
		if name == "" {
			return
		}
		saveCommand(savedCommand{Name: name, Dir: strings.TrimSpace(f.fields[fieldDir]), Cmd: f.fields[fieldCmd]})
		if f.editing && f.editName != name {
			deleteCommand(f.editName)
		}
		if s.commandList != nil {
			s.commandList.reload()
		}
		s.commandForm = nil
	case k.Code == tea.KeyEscape:
		s.commandForm = nil
	case k.Code == tea.KeyBackspace:
		if r := []rune(f.fields[f.focus]); len(r) > 0 {
			f.fields[f.focus] = string(r[:len(r)-1])
		}
	case k.Text != "" && k.Mod&(tea.ModCtrl|tea.ModAlt) == 0:
		f.fields[f.focus] += k.Text
	}
}

// fieldRows renders one form field as one row per line of its value (more
// than one only ever happens for the cmd field), labeled on the first line
// and indented to match on any continuation, with the trailing cursor on
// its last line while focused.
func fieldRows(label, text string, focused bool, th theme) []string {
	prefix := label + ": "
	pad := strings.Repeat(" ", len(prefix))
	lines := strings.Split(text, "\n")
	rows := make([]string, len(lines))
	for i, line := range lines {
		p := prefix
		if i > 0 {
			p = pad
		}
		if focused && i == len(lines)-1 {
			line += "▏"
		}
		rows[i] = fg(th.Text) + p + line + "\x1b[m"
	}
	return rows
}

// commandFormBox renders the add/edit form as a bordered panel, one
// labeled row per field (more for cmd, if it spans multiple lines), the
// focused field carrying the trailing cursor.
func commandFormBox(f *commandForm, th theme) []string {
	labels := [3]string{"alias", "dir", "cmd"}
	var rows []string
	for i, label := range labels {
		rows = append(rows, fieldRows(label, f.fields[i], i == f.focus, th)...)
	}
	title := "add command  (tab/↑↓ move · enter save · opt+enter newline in cmd · esc cancel)"
	if f.editing {
		title = "edit command  (tab/↑↓ move · enter save · opt+enter newline in cmd · esc cancel)"
	}
	return panel(title, rows, 40, th)
}

// --- picker / manager ----------------------------------------------------

// commandList is the saved-commands picker's open state: every saved
// command, the live search query typed into it, query's matches (all of
// them, unfiltered, when query is blank), and the row index of the current
// highlight within shown. Doubles as the manager —
// km.NewCommand/EditCommand/DeleteCommand act on it directly rather than
// opening a separate screen. Because every other keystroke feeds the
// search query (fzf-style — there's no separate "start searching" mode),
// navigation is arrow-keys-only and New/Edit/Delete are bound to ctrl
// combos by default, so plain letters always type into the query.
type commandList struct {
	all   []savedCommand
	shown []savedCommand
	query string
	sel   int
}

// openCommandList opens the picker over whatever is saved on disk right
// now. Unlike the preset list, it opens even when empty so there's a way to
// add the first entry.
func (s *server) openCommandList() {
	all := loadCommands()
	s.commandList = &commandList{all: all, shown: all}
}

// reload re-reads commands.yaml and re-applies the current search query,
// for after an add/edit/delete changes what's on disk. clamps sel back into
// range in case the change shrank shown.
func (cl *commandList) reload() {
	cl.all = loadCommands()
	cl.shown = filterCommands(cl.all, cl.query)
	if cl.sel >= len(cl.shown) {
		cl.sel = len(cl.shown) - 1
	}
	if cl.sel < 0 {
		cl.sel = 0
	}
}

// commandListKey handles one keystroke while the picker/manager is open.
// Up/Down move the highlight; Enter runs the highlighted command;
// km.NewCommand/EditCommand open the add/edit form; km.DeleteCommand
// removes the highlighted entry from disk; Esc cancels. Backspace edits the
// search query and anything else typed (with no ctrl/alt) appends to it,
// re-filtering shown from scratch each time.
func (s *server) commandListKey(k tea.Key) {
	s.dirty = true
	cl := s.commandList
	switch {
	case k.Code == tea.KeyUp:
		if n := len(cl.shown); n > 0 {
			cl.sel = (cl.sel - 1 + n) % n
		}
	case k.Code == tea.KeyDown:
		if n := len(cl.shown); n > 0 {
			cl.sel = (cl.sel + 1) % n
		}
	case k.Code == tea.KeyEnter:
		if len(cl.shown) > 0 {
			s.runSavedCommand(cl.shown[cl.sel])
		}
		s.commandList = nil
	case parseKeySpec(s.km.NewCommand).matches(k):
		s.openCommandForm()
	case parseKeySpec(s.km.EditCommand).matches(k) && len(cl.shown) > 0:
		s.openCommandFormFor(cl.shown[cl.sel])
	case parseKeySpec(s.km.DeleteCommand).matches(k) && len(cl.shown) > 0:
		deleteCommand(cl.shown[cl.sel].Name)
		cl.reload()
	case k.Code == tea.KeyEscape:
		s.commandList = nil
	case k.Code == tea.KeyBackspace:
		if r := []rune(cl.query); len(r) > 0 {
			cl.query = string(r[:len(r)-1])
			cl.shown = filterCommands(cl.all, cl.query)
			cl.sel = 0
		}
	case k.Text != "" && k.Mod&(tea.ModCtrl|tea.ModAlt) == 0:
		cl.query += k.Text
		cl.shown = filterCommands(cl.all, cl.query)
		cl.sel = 0
	}
}

// oneLine collapses a multi-line command down to a single display line for
// the picker's rows, which — unlike the add/edit form — have no room to
// show a command across several lines.
func oneLine(s string) string {
	return strings.ReplaceAll(s, "\n", " ⏎ ")
}

// commandListBox renders the picker/manager as a bordered panel: a search
// line first, then one row per matching command (alias — cmd, plus its
// directory if it has one), the highlighted one picked out in the theme's
// accent color — styled like presetListBox. Its title lists only the bound
// keys, the same way presetListBox's title conditionally lists
// DeletePreset.
func commandListBox(cl *commandList, km keymap, th theme) []string {
	rows := []string{fg(th.Text) + "search: " + cl.query + "▏\x1b[m"}
	switch {
	case len(cl.all) == 0:
		rows = append(rows, fg(th.Text)+"  no saved commands"+"\x1b[m")
	case len(cl.shown) == 0:
		rows = append(rows, fg(th.Text)+"  no matches"+"\x1b[m")
	default:
		for i, c := range cl.shown {
			label := c.Name + " — " + oneLine(c.Cmd)
			if c.Dir != "" {
				label += " (" + c.Dir + ")"
			}
			row, style := "  "+label, fg(th.Text)
			if i == cl.sel {
				row, style = "› "+label, "\x1b[1m"+fg(th.Base)+bg(th.Accent)
			}
			rows = append(rows, style+row+"\x1b[m")
		}
	}
	title := "commands  (type to search · ↑↓ move · enter run"
	if km.NewCommand != "" {
		title += " · " + km.NewCommand + " new"
	}
	if km.EditCommand != "" {
		title += " · " + km.EditCommand + " edit"
	}
	if km.DeleteCommand != "" {
		title += " · " + km.DeleteCommand + " delete"
	}
	title += " · esc cancel)"
	return panel(title, rows, 0, th)
}
