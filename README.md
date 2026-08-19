# yatm — yet another terminal multiplexer

A small tmux-like multiplexer: windows, splittable panes, a detachable daemon,
mouse support, and a lock mode for when a nested program wants the prefix key.

## Running

```sh
devbox run start        # or: devbox run build && ./yatm
```

`yatm` attaches to the running session, starting the daemon if there isn't one.

| Command | What it does |
|---|---|
| `yatm` / `yatm attach` | attach, spawning the daemon if needed |
| `yatm kill-server` | stop the daemon and every shell in it |

## Keys

The prefix is **Ctrl+B**. Press it, then:

| Key | Action |
|---|---|
| `]` `[` / `0`–`9` | next / previous window / select by number |
| `a` | new pane, auto-splitting whichever axis has more room |
| `|` / `-` | split side-by-side / split top-to-bottom |
| `s` | stack a pane behind the active one |
| `z` | zoom the active pane to fill the window, or restore it |
| `o` / arrows | cycle panes / move to the pane in that direction |
| arrows, at a dead end in a stack | switch which stacked layer shows |
| `Ctrl+arrow` / `Alt+arrow` | resize the active pane by 1 / by 5 |
| `w` `c` / `&` | new window / kill the active window |
| `w` `r` | rename the active window's tab |
| `p` `x` | kill the active pane |
| `p` `r` | rename the active pane |
| `T` | open the colorscheme picker |
| `d` | detach, leaving everything running |
| `q` | quit and stop the daemon |
| `Ctrl+B` | send a literal Ctrl+B to the shell |

Holding the prefix pops up a which-key-style tooltip in the bottom-right
corner listing every binding, read live from `config.yaml`.

**F12** toggles lock mode at any time, with or without the prefix. While locked
every key — the prefix included — goes straight through to the program in the
pane, so a nested yatm, tmux, or anything else bound to Ctrl+B keeps
working. F12 is the only key yatm never forwards.

`w` and `p` are sub-layers: press either and the which-key tooltip switches
to a second list — `w` opens `c` new, `&` kill, `r` rename for windows; `p`
opens `r` rename, `x` kill for panes — instead of running on its own, the
same way the prefix works, one level deeper; the top-level tooltip shows a
single `windows…`/`panes…` row for each rather than listing their actions
individually. Every key above (except arrows, digits and the Ctrl+B
passthrough, which are structural) is remappable — see
[Configuration](#configuration).

### Panes, stacking, and borders

Every pane is drawn in its own titled border, the title taken from the
shell's own title (or whatever program is running). The window tab shown
at the top follows that same title, unless the window itself was renamed
with `w` `r` — see [Renaming windows](#renaming-windows). The active
pane's border is highlighted in the theme's accent color; every other
pane's is dimmed. The first command you run in a pane renames its border
to that command line — truncated with `…` if it's too long — overriding
whatever the shell called it, and later commands leave that name alone.
`p` `r` renames a pane's border directly instead; see
[Renaming panes](#renaming-panes).

A **split** (`|` / `-`) divides the screen; both panes stay visible side by
side. A **stack** (`s`) does not divide anything — the new pane shares the
exact same rect as the one it was stacked behind, like a new layer in an
image editor. Only one layer runs full-speed rendering at a time, but every
other layer still leaves a one-row title bar on screen, stacked above or
below the active layer in the order it was added — zellij's compact stacked
look. Click a layer's title bar, or press an arrow key that has nowhere
spatial to go (a stack has no side-by-side neighbours), to bring it to the
front. Closing a layer (`p` `x`) falls back to the one below it. The status bar
shows `layer 2/3` whenever the active pane is part of a stack, so it's clear
how many layers there are and which one you're looking at.

### Zoom

`z` grows the active pane to fill the entire window, hiding every other
pane without closing or resizing them. You can still switch focus while
zoomed — `o`, the arrows, clicking a tab — and whichever pane becomes
active fills the screen in its place. Pressing `z` again restores the
original layout, focused on whichever pane was active when you unzoomed.
The status bar shows `zoom` while it's on.

### Renaming panes

A pane's border normally follows its shell's own title, or the first
command you run in it (see above). `p` `r` opens a rename prompt over the
layout, seeded with the active pane's current border name; type a new one
and `Enter` confirms it, `Esc` cancels. Once renamed, the border keeps that
name regardless of what runs in the pane, until you rename it again with a
blank name, which reverts to auto-tracking the next command. The rename is
scoped to the pane's border only — the window tab keeps following the
pane's shell title independently, so renaming a pane never touches its tab.

### Renaming windows

A window's tab normally follows its active pane's border title (see
above). `w` `r` opens a rename prompt over the layout, seeded with the
window's current tab name; type a new one and `Enter` confirms it, `Esc`
cancels. Once renamed, the tab keeps that name regardless of which pane is
active or what runs in it, until you rename it again with a blank name,
which reverts to following the active pane's title.

## Colorschemes

`T` opens a floating colorscheme picker over the current layout. `↑`/`↓`
(or `j`/`k`) move the highlight and preview it live — the tab bar, status
bar and tooltip repaint immediately so you can see it before committing.
`Enter` keeps the highlighted scheme and remembers it in `config.yaml`
(same file as the keymap); `Esc`/`q` cancels back to whatever was active
before you opened the picker.

Only the four [Catppuccin](https://catppuccin.com) flavors — Latte, Frappé,
Macchiato, Mocha — ship today. Pane content is untouched either way; the
picker only skins yatm's own chrome.

## Configuration

Theme and keymap are both read from `~/.config/yatm/config.yaml` (same path
on macOS and Linux). The daemon writes out the full default file the first
time it starts, so every setting is listed explicitly and ready to edit:

```yaml
theme: Catppuccin Macchiato
keymap:
  prefix: ctrl+b
  lock: f12
  next_window: "]"
  prev_window: "["
  cycle_pane: o
  new_pane: a
  split_horiz: "|"
  split_vert: "-"
  stack: s
  zoom: z
  theme: T
  detach: d
  quit: q
  windows:
    key: w
    new: c
    kill: "&"
    rename: r
  panes:
    key: p
    kill: x
    rename: r
```

`theme` is one of the four [Catppuccin](https://catppuccin.com) flavor names
(Latte, Frappé, Macchiato, Mocha). Under `keymap`, `prefix` and `lock` accept
an optional `ctrl+`/`alt+`/`shift+` modifier before the key; every other
top-level binding is one character. `windows` and `panes` are nested
sub-layers: `key` is the leader that opens each, and its other fields are
the one-character actions reached after pressing it (`new`, `kill`,
`rename` for windows; `kill`, `rename` for panes) — leave any of them
blank to drop that action from the layer. Restart the daemon
(`yatm kill-server`, then `yatm`) after editing.

## Mouse

Click a tab at the top to switch windows, click a pane to focus it, and drag
a gutter between panes to resize. When the program inside a pane turns on
mouse reporting (vim, htop, …), its clicks are forwarded to it instead.

## Layout

```
 0:bash │ 1:vim │ 2:htop                                    ← tab bar (click to switch)
┌─ window 1 ─────────────────┐
│  pane │ pane               │
│       ├────                │
│       │ pane               │
└────────────────────────────┘
 NORMAL              layer 2/3  window 2/3                  ← status bar
```

Panes live in a tree of splits with relative weights, so resizing the terminal
keeps the proportions. The daemon owns the tree, the PTYs and the terminal
emulators; the client just draws frames and forwards input, which is why
detaching leaves everything running.

## Development

```sh
devbox run test     # layout, resize and hit-testing maths
devbox run build
```
