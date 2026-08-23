package server

import (
	"os"
	"path/filepath"
	"strings"

	tea "charm.land/bubbletea/v2"
	"gopkg.in/yaml.v3"
)

// presetNode is one node of a saved window's split tree: enough to rebuild
// the tree's shape (dir, relative weight, stack layer) and, for a leaf, the
// pane's manual border name if it had one. A pane's shell/working directory
// is never captured — a preset restores the layout, not the processes that
// were running in it, so restoring one always starts fresh shells.
type presetNode struct {
	Dir      dir          `yaml:"dir"`
	Weight   float64      `yaml:"weight"`
	Layer    int          `yaml:"layer,omitempty"`
	Name     string       `yaml:"name,omitempty"`
	Named    bool         `yaml:"named,omitempty"`
	Children []presetNode `yaml:"children,omitempty"`
}

// presetWindow is one saved window: its manual tab name, if any, and its
// pane tree.
type presetWindow struct {
	Name  string     `yaml:"name,omitempty"`
	Named bool       `yaml:"named,omitempty"`
	Root  presetNode `yaml:"root"`
}

// preset is a named, saved arrangement of windows, panes and their layout —
// what the save-preset prompt writes and the load-preset picker reads back.
type preset struct {
	Name    string         `yaml:"name"`
	Windows []presetWindow `yaml:"windows"`
}

// capturePaneNode walks one window's live tree into its saved form.
func capturePaneNode(n *node) presetNode {
	pn := presetNode{Dir: n.dir, Weight: n.weight, Layer: n.layer}
	if n.pane != nil && n.pane.named {
		pn.Name, pn.Named = n.pane.borderName, true
	}
	for _, c := range n.children {
		pn.Children = append(pn.Children, capturePaneNode(c))
	}
	return pn
}

func captureWindow(w *window) presetWindow {
	pw := presetWindow{Root: capturePaneNode(w.root)}
	if w.named {
		pw.Name, pw.Named = w.name, true
	}
	return pw
}

// capturePreset snapshots every window currently open into a named preset,
// ready to be persisted by savePreset.
func (s *server) capturePreset(name string) preset {
	pr := preset{Name: name}
	for _, w := range s.windows {
		pr.Windows = append(pr.Windows, captureWindow(w))
	}
	return pr
}

// pendingLeaf is a placeholder leaf built from a presetNode by buildNode:
// its node has no pane yet, and its name/named carry what buildNode read
// off the presetNode so applyPreset can rename the real pane once it exists.
type pendingLeaf struct {
	node  *node
	name  string
	named bool
}

// buildNode rebuilds one presetNode as a live (pane-less) node, collecting
// every leaf it creates into *out so the caller can fill each in with a
// freshly started pane afterward.
func buildNode(pn presetNode, parent *node, out *[]pendingLeaf) *node {
	n := &node{dir: pn.Dir, weight: pn.Weight, layer: pn.Layer, parent: parent}
	for _, c := range pn.Children {
		n.children = append(n.children, buildNode(c, n, out))
	}
	if len(pn.Children) == 0 {
		*out = append(*out, pendingLeaf{node: n, name: pn.Name, named: pn.Named})
	}
	return n
}

// applyPreset restores a saved arrangement as new windows appended after
// whatever is already open, each with freshly started shells laid out into
// the saved tree shape and renamed wherever a pane or window had a manual
// name. Left as an addition rather than a replacement, so loading a preset
// never destroys work already in progress.
func (s *server) applyPreset(pr preset) {
	for _, pw := range pr.Windows {
		var pending []pendingLeaf
		root := buildNode(pw.Root, nil, &pending)
		w := &window{id: s.nextID, root: root}
		s.nextID++
		s.windows = append(s.windows, w)
		s.cur = len(s.windows) - 1

		l := s.layoutNow()
		for _, pl := range pending {
			r := l.rects[pl.node]
			p, err := newPane(s.nextID, max(r.w, 1), max(r.h, 1), s.events)
			if err != nil {
				continue
			}
			s.nextID++
			pl.node.pane = p
			if pl.named {
				p.rename(pl.name)
			}
		}
		w.active = firstLeaf(root)
		if pw.Named {
			w.rename(pw.Name)
		}
		s.layoutNow()
	}
	s.dirty = true
}

// presetsPath returns where saved presets live, alongside config.yaml.
func presetsPath() (string, error) {
	dir, err := configDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "presets.yaml"), nil
}

// presetFile is the on-disk shape of presets.yaml: every saved preset in
// one file, so there's a single place to look, edit or back up.
type presetFile struct {
	Presets []preset `yaml:"presets"`
}

// loadPresets reads every saved preset, or nil if there is no file yet or
// it can't be parsed — the same "fall back quietly" contract loadConfig
// uses, since a corrupt presets file shouldn't keep the daemon from
// starting.
func loadPresets() []preset {
	path, err := presetsPath()
	if err != nil {
		return nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var f presetFile
	if err := yaml.Unmarshal(data, &f); err != nil {
		return nil
	}
	return f.Presets
}

// savePreset persists pr to presets.yaml, replacing any existing preset with
// the same name in place so saving under a name already in use updates it
// instead of piling up a duplicate.
func savePreset(pr preset) {
	path, err := presetsPath()
	if err != nil {
		return
	}
	prs := loadPresets()
	found := false
	for i, p := range prs {
		if p.Name == pr.Name {
			prs[i] = pr
			found = true
			break
		}
	}
	if !found {
		prs = append(prs, pr)
	}
	data, err := yaml.Marshal(presetFile{Presets: prs})
	if err != nil {
		return
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return
	}
	_ = os.WriteFile(path, data, 0o600)
}

// deletePreset removes the named preset from presets.yaml, if it exists.
// A no-op, like save/load, if the file can't be read or written.
func deletePreset(name string) {
	path, err := presetsPath()
	if err != nil {
		return
	}
	prs := loadPresets()
	out := prs[:0]
	for _, p := range prs {
		if p.Name != name {
			out = append(out, p)
		}
	}
	data, err := yaml.Marshal(presetFile{Presets: out})
	if err != nil {
		return
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return
	}
	_ = os.WriteFile(path, data, 0o600)
}

func presetNames(prs []preset) []string {
	names := make([]string, len(prs))
	for i, p := range prs {
		names[i] = p.Name
	}
	return names
}

func findPreset(prs []preset, name string) (preset, bool) {
	for _, p := range prs {
		if p.Name == name {
			return p, true
		}
	}
	return preset{}, false
}

// presetPrompt is the save-preset prompt's open state: a single-line text
// buffer the user types the new preset's name into, the same shape as
// renamer.
type presetPrompt struct {
	text string
}

// openPresetPrompt opens the save-preset prompt with a blank name — unlike
// renaming a pane or window, there is no existing preset name to seed it
// with.
func (s *server) openPresetPrompt() {
	s.presetPrompt = &presetPrompt{}
}

// presetPromptKey handles one keystroke while the save-preset prompt is
// open. Enter saves the current layout under the typed name, provided it
// isn't blank; Esc discards the prompt without saving anything.
func (s *server) presetPromptKey(k tea.Key) {
	s.dirty = true
	switch {
	case k.Code == tea.KeyEnter:
		if name := strings.TrimSpace(s.presetPrompt.text); name != "" {
			savePreset(s.capturePreset(name))
		}
		s.presetPrompt = nil
	case k.Code == tea.KeyEscape:
		s.presetPrompt = nil
	case k.Code == tea.KeyBackspace:
		if r := []rune(s.presetPrompt.text); len(r) > 0 {
			s.presetPrompt.text = string(r[:len(r)-1])
		}
	case k.Text != "" && k.Mod&(tea.ModCtrl|tea.ModAlt) == 0:
		s.presetPrompt.text += k.Text
	}
}

// presetPromptBox renders the save-preset prompt, styled like renameBox: a
// title row and one editable line with a trailing cursor.
func presetPromptBox(text string, th theme) []string {
	input := fg(th.Text) + text + "▏\x1b[m"
	return panel("save preset  (enter confirm · esc cancel)", []string{input}, 24, th)
}

// presetList is the load-preset picker's open state: every saved preset's
// name, and the row index of the current highlight.
type presetList struct {
	names []string
	sel   int
}

// openPresetList opens the load-preset picker over whatever is saved on
// disk right now. A no-op when there's nothing saved yet, the same way a
// blank keymap leaf drops an action rather than opening an empty picker.
func (s *server) openPresetList() {
	names := presetNames(loadPresets())
	if len(names) == 0 {
		return
	}
	s.presetList = &presetList{names: names}
}

// presetListKey handles one keystroke while the load-preset picker is open.
// Enter restores the highlighted preset as new windows; km.DeletePreset
// removes the highlighted one from disk instead, closing the picker once
// the last preset is gone rather than leaving it open on an empty list;
// Esc/q cancels.
func (s *server) presetListKey(k tea.Key) {
	s.dirty = true
	switch {
	case k.Code == tea.KeyUp || k.Text == "k":
		n := len(s.presetList.names)
		s.presetList.sel = (s.presetList.sel - 1 + n) % n
	case k.Code == tea.KeyDown || k.Text == "j":
		s.presetList.sel = (s.presetList.sel + 1) % len(s.presetList.names)
	case k.Code == tea.KeyEnter:
		name := s.presetList.names[s.presetList.sel]
		if pr, ok := findPreset(loadPresets(), name); ok {
			s.applyPreset(pr)
		}
		s.presetList = nil
	case s.km.DeletePreset != "" && k.Text == s.km.DeletePreset:
		deletePreset(s.presetList.names[s.presetList.sel])
		names := presetNames(loadPresets())
		if len(names) == 0 {
			s.presetList = nil
			return
		}
		s.presetList.names = names
		if s.presetList.sel >= len(names) {
			s.presetList.sel = len(names) - 1
		}
	case k.Code == tea.KeyEscape || k.Text == "q":
		s.presetList = nil
	}
}

// presetListBox renders the load-preset picker as a bordered panel, one row
// per saved preset, the highlighted one picked out in the theme's accent
// color — styled like pickerBox (the colorscheme picker). Its title lists
// the delete key only when one is bound, the same way a blank
// windows/panes layer leaf drops that action from the which-key tooltip.
func presetListBox(pl *presetList, km keymap, th theme) []string {
	rows := make([]string, len(pl.names))
	for i, n := range pl.names {
		row, style := "  "+n, fg(th.Text)
		if i == pl.sel {
			row, style = "› "+n, "\x1b[1m"+fg(th.Base)+bg(th.Accent)
		}
		rows[i] = style + row + "\x1b[m"
	}
	title := "presets  (↑↓/jk move · enter load"
	if km.DeletePreset != "" {
		title += " · " + km.DeletePreset + " delete"
	}
	title += " · esc cancel)"
	return panel(title, rows, 0, th)
}
