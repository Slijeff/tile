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
| `c` / `n` / `p` / `0`–`9` | new window / next / previous / select by number |
| `a` | new pane, auto-splitting whichever axis has more room |
| `%` / `"` | split side-by-side / split top-to-bottom |
| `s` / `z` | stack a pane behind the active one / switch which layer shows |
| `o` / arrows | cycle panes / move to the pane in that direction |
| `Ctrl+arrow` / `Alt+arrow` | resize the active pane by 1 / by 5 |
| `x` / `&` | kill the pane / kill the window |
| `T` | open the colorscheme picker |
| `d` | detach, leaving everything running |
| `q` | quit and stop the daemon |
| `Ctrl+B` | send a literal Ctrl+B to the shell |

Holding the prefix pops up a which-key-style tooltip in the bottom-right
corner listing every binding, read live from `keybinds.yaml`.

**F12** toggles lock mode at any time, with or without the prefix. While locked
every key — the prefix included — goes straight through to the program in the
pane, so a nested yatm, tmux, or anything else bound to Ctrl+B keeps
working. F12 is the only key yatm never forwards.

Every key above (except arrows, digits and the Ctrl+B passthrough, which are
structural) is remappable — see [Keybinds](#keybinds).

### Stacked panes

A **split** (`%` / `"`) divides the screen; both panes stay visible side by
side. A **stack** (`s`) does not divide anything — the new pane shares the
exact same rect as the one it was stacked behind, like a new layer in an
image editor. Only one layer is visible (and running full-speed rendering)
at a time; `z` cycles to the next one. Closing a layer (`x`) falls back to
the one below it. The status bar shows `layer 2/3` whenever the active
pane is part of a stack, so it's clear how many layers there are and which
one you're looking at.

## Colorschemes

`T` opens a floating colorscheme picker over the current layout. `↑`/`↓`
(or `j`/`k`) move the highlight and preview it live — the tab bar, status
bar and tooltip repaint immediately so you can see it before committing.
`Enter` keeps the highlighted scheme and remembers it in `theme.yaml`
(same config directory as `keybinds.yaml`); `Esc`/`q` cancels back to
whatever was active before you opened the picker.

Only the four [Catppuccin](https://catppuccin.com) flavors — Latte, Frappé,
Macchiato, Mocha — ship today. Pane content is untouched either way; the
picker only skins yatm's own chrome.

## Keybinds

Keys are read from `~/.config/yatm/keybinds.yaml` (same path on macOS and
Linux). The daemon writes out the full default file the first time it
starts, so every bindable key is listed explicitly and ready to edit:

```yaml
prefix: ctrl+b
lock: f12
new_window: c
next_window: n
prev_window: p
cycle_pane: o
new_pane: a
split_horiz: "%"
split_vert: "\""
stack: s
cycle_layer: z
kill_pane: x
kill_window: "&"
theme: T
detach: d
quit: q
```

`prefix` and `lock` accept an optional `ctrl+`/`alt+`/`shift+` modifier
before the key; every other binding is a single character. Restart the
daemon (`yatm kill-server`, then `yatm`) after editing.

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
