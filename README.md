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
| `f` | toggle a floating terminal in the center of the window |
| `o` / arrows | cycle panes / move to the pane in that direction |
| arrows, at a dead end in a stack | switch which stacked layer shows |
| `Ctrl+arrow` / `Alt+arrow` | resize the active pane by 1 / by 5 |
| `w` `c` / `&` | new window / kill the active window |
| `w` `r` | rename the active window's tab |
| `p` `x` | kill the active pane |
| `p` `r` | rename the active pane |
| `p` `p` | open the floating pane picker |
| `S` | save the current layout to a named preset |
| `L` | load a saved preset |
| `T` | open the colorscheme picker |
| `R` | reload config.yaml (keymap, theme, margin) |
| `d` | detach, leaving everything running |
| `q` | quit and stop the daemon, after confirming |
| `Ctrl+B` | send a literal Ctrl+B to the shell |

Holding the prefix pops up a which-key-style tooltip in the bottom-right
corner listing every binding, read live from `config.yaml`.

**F12** toggles lock mode at any time, with or without the prefix. While locked
every key — the prefix included — goes straight through to the program in the
pane, so a nested yatm, tmux, or anything else bound to Ctrl+B keeps
working. F12 is the only key yatm never forwards.

`w` and `p` are sub-layers: press either and the which-key tooltip switches
to a second list — `w` opens `c` new, `&` kill, `r` rename for windows; `p`
opens `r` rename, `x` kill, `p` pane picker for panes — instead of running
on its own, the same way the prefix works, one level deeper; the top-level
tooltip shows a single `windows…`/`panes…` row for each rather than listing
their actions individually. Every key above (except arrows, digits and the
Ctrl+B passthrough, which are structural) is remappable — see
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

### Floating terminal

`f` toggles a floating terminal: a bordered pane covering three quarters of
the window, centered over the tiled layout instead of taking a slice out of
it. While it's up it holds focus — every unprefixed key goes to it, the
tiled borders all dim, and the status bar shows `float`. Pressing `f` again
hides it and hands focus back to whichever pane had it; the floating shell
keeps running, so the next `f` brings the same session back, scrollback and
all. `p` `x` kills a floating layer, and killing the last one closes the
float for good.

A float is one rect, so `|`, `-`, `a` and `z` are refused while it has
focus — there is nothing to subdivide and nothing to grow into. `s` is the
exception: stacking shares a rect instead of carving it up, so it works,
and a floating stack behaves exactly like a tiled one — collapsed title
bars for the background layers, `layer 2/3` in the status bar, arrows or
`o` to flip between them, a click on a title bar to bring one forward.

Clicks outside the float are swallowed rather than passed through to the
panes it covers, so a stray click can't quietly move focus out from under
it. It's per-window: each window has its own, and picking a tiled pane in
the pane picker leaves that window's float.

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

### Pane picker

`p` `p` opens a floating pane picker over the current layout: every window
and its split/stack tree on the left, a live preview of the highlighted
pane's content on the right. `↑`/`↓` (or `j`/`k`) move the highlight —
skipping over window headers and the branch rows a nested split or stack
draws for itself — and the preview column updates to match. `Enter`
switches to the highlighted pane, jumping windows if it's in a different
one; `Esc`/`q` cancels back to whatever was focused before you opened it.

### Presets

`S` saves the current arrangement of windows, panes and their layout as a
named preset: a prompt opens over the layout the same way `p` `r`/`w` `r`'s
rename prompt does — type a name and `Enter` confirms it, `Esc` cancels
without saving anything. Saving under a name that's already used overwrites
that preset instead of creating a duplicate. A preset remembers each
window's split/stack tree and relative pane sizes, plus any manual window
or pane names set with `w` `r` / `p` `r`; it does not remember what was
running in a pane, so loading one always starts fresh shells.

`L` opens a picker listing every saved preset — `↑`/`↓` (or `j`/`k`) move
the highlight, `Enter` restores the highlighted one, `Esc`/`q` cancels.
Restoring a preset adds its windows after whatever is already open rather
than replacing it, so loading one never throws away work in progress. `x`
deletes the highlighted preset from disk instead — with a confirmation
nowhere in sight, so double-check the highlight before pressing it; deleting
the last one closes the picker rather than leaving it open on an empty list.
Presets are stored in `~/.config/yatm/presets.yaml`, alongside
`config.yaml`.

### Quitting

`q` is the only key that ends more than it starts: it stops the daemon, and
with it every shell in every window, whether or not anything is attached.
So it asks first, in a box over the layout — `y` goes through with it,
**any** other key backs out, so a stray keystroke can neither end the
session nor leave you stuck in front of the dialog. `d` (detach) is the one
you want if you'd rather leave everything running.

`yatm kill-server` does the same thing from outside, without asking: the
dialog guards the keystroke you can hit by accident, and spelling out
`kill-server` at a shell already says it. It reports "no server running"
rather than pretending it did something.

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

Theme, keymap and the pane margin are all read from
`~/.config/yatm/config.yaml` (same path on macOS and Linux). The daemon
writes out the full default file the first time it starts, so every setting
is listed explicitly and ready to edit:

```yaml
theme: Catppuccin Macchiato
margin: 1 # blank gutter cells between panes; 0 makes them touch
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
  float: f
  preset: S
  load_preset: L
  delete_preset: x
  theme: T
  reload: R
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
    picker: p
```

`theme` is one of the four [Catppuccin](https://catppuccin.com) flavor names
(Latte, Frappé, Macchiato, Mocha). Under `keymap`, `prefix` and `lock` accept
an optional `ctrl+`/`alt+`/`shift+` modifier before the key; every other
top-level binding is one character. `windows` and `panes` are nested
sub-layers: `key` is the leader that opens each, and its other fields are
the one-character actions reached after pressing it (`new`, `kill`,
`rename` for windows; `kill`, `rename`, `picker` for panes) — leave any of
them blank to drop that action from the layer. Press `reload` (`R` by
default) after editing to apply changes live, no restart needed.

## Mouse

Click a tab at the top to switch windows, click a pane to focus it, and drag
a gutter between panes to resize. When the program inside a pane turns on
mouse reporting (vim, htop, …), its clicks are forwarded to it instead.
While a floating terminal is up it takes the mouse: clicks outside it are
ignored rather than reaching the panes underneath.

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
