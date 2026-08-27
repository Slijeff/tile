# tile — a terminal multiplexer

A small tmux-like multiplexer: windows, splittable panes, a detachable daemon,
mouse support, and a lock mode for when a nested program wants the prefix key.

## Running

```sh
devbox run start        # or: devbox run build && ./tile
```

`tile` attaches to the running session, starting the daemon if there isn't one.

| Command | What it does |
|---|---|
| `tile` / `tile attach` | attach, spawning the daemon if needed |
| `tile kill-server` | stop the daemon and every shell in it |
| `tile ls` | list running sessions |
| `tile help` | the full command list, no daemon required |

Every command takes `-t name` to act on a session other than the default —
`tile -t work` attaches to (or starts) a session named "work", independent of
any other session's windows and panes.

There is also a [scripting interface](#scripting) for driving a session from
outside it — enumerating panes, reading what they printed, and typing into
them — which is how a script or an AI agent works with tile.

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
pane, so a nested tile, tmux, or anything else bound to Ctrl+B keeps
working. F12 is the only key tile never forwards.

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
Presets are stored in `~/.config/tile/presets.yaml`, alongside
`config.yaml`.

### Quitting

`q` is the only key that ends more than it starts: it stops the daemon, and
with it every shell in every window, whether or not anything is attached.
So it asks first, in a box over the layout — `y` goes through with it,
**any** other key backs out, so a stray keystroke can neither end the
session nor leave you stuck in front of the dialog. `d` (detach) is the one
you want if you'd rather leave everything running.

`tile kill-server` does the same thing from outside, without asking: the
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
picker only skins tile's own chrome.

## Configuration

Theme, keymap and the pane margin are all read from
`~/.config/tile/config.yaml` (same path on macOS and Linux). The daemon
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

## Scripting

Every command below acts on a running session over the same socket the TUI
uses, and answers on stdout. None of them attach, so they can run while you
are using the session and will not detach you — that is what makes them safe
to hand to a script or an AI agent.

Panes are addressed as `%<id>`, windows as `@<id>`, both printed by
`tile list`. Ids come from one counter, so a pane and a window never share a
number, and unlike a position in the tab bar an id does not shift when
something before it closes.

```
tile list [--json]               every window and pane
tile capture   %p [--lines N]    a pane's text, no escape codes
tile send-keys %p [--key SPEC] [--enter] [text...]
tile split     %p [-h|-v]        split a pane, prints the new pane's id
tile stack     %p                layer a pane behind it, prints its id
tile new-window                  prints the new window's id
tile kill-pane   %p
tile kill-window @w
tile focus       %p
tile resize      %p <left|right|up|down> <cells>
tile even    %p|@w               equal shares: the pane's branch, or a window
tile rename  %p|@w [name]        blank name reverts to the shell's title
```

Anything that fails exits non-zero with the reason on stderr.

### Looking at a session

```console
$ tile list
* @2    1: zsh
  %1      ├─ go test ./...
  %3      └─ tail -f server.log   <- focused
```

`--json` gives the same tree with geometry and focus, for a program to read:

```console
$ tile list --json
[
  {
    "id": 2,
    "index": 1,
    "name": "zsh",
    "active": true,
    "root": {
      "dir": "horiz",
      "w": 80, "h": 22,
      "children": [
        { "id": 1, "title": "zsh", "name": "go test ./...", "w": 40, "h": 22 },
        { "id": 3, "title": "zsh", "w": 40, "h": 22, "focused": true }
      ]
    }
  }
]
```

A node with children is a split or a stack and has a `dir`; a node with an
`id` is a pane. A window also carries `float` when it has a floating
terminal, and `zoomed` when one of its panes is zoomed.

### Driving a pane

`send-keys` types into the pane you name without going through the prefix
key, so the session's mode — prefix pending, locked, a picker open — never
changes what a script sends. Text is passed as arguments; `--enter` appends
a newline, and `--key` sends a single named or modified key.

```sh
tile send-keys %1 --enter 'go test ./...'
tile send-keys %1 --key ctrl+c            # interrupt it
tile send-keys %1 --key escape
tile send-keys %1 -- '-n leading dashes'  # -- ends flag parsing
```

Typing a command this way also names the pane after it, exactly as if you
had typed it yourself.

### Reading it back

`capture` returns the pane's text with the styling stripped and the blank
rows below the cursor dropped, so `--lines 20` means the last twenty lines
that have something on them. `--lines 0` returns the whole scrollback.

```console
$ tile capture %1 --lines 3
ok      tile/internal/server    0.51s
❯
```

A terminal pane is a screen, not a stream of command results: nothing marks
where one command's output ends. So there is no "run and wait" — send the
keys, then poll `capture` until what comes back looks finished.

```sh
tile send-keys %1 --enter 'go test ./...'
sleep 2
tile capture %1 --lines 30
```

### Rearranging

Splitting, stacking and killing act at the pane you name and leave focus
where pressing the equivalent key would — `split` focuses the pane it just
made, `kill-pane` focuses the survivor. `tile focus` moves it back.

```sh
new=$(tile split %1 -v)     # prints e.g. %7
tile send-keys "$new" --enter 'tail -f server.log'
tile rename "$new" logs
tile focus %1               # hand focus back
```

Splitting does not divide evenly. A split halves the share of the pane it
splits, so splitting the same pane twice leaves 50/25/25 rather than thirds.
`even` is the way back: given a pane it evens that pane's branch, given a
window it evens every branch in it.

```console
$ tile split %1 -h; tile split %3 -h     # 50/25/25
$ tile even %1                           # 34/33/33
```

`resize` moves one border, in cells, and leaves focus alone. It works in
weights underneath, so a step is approximate at small sizes — for equal
shares reach for `even` rather than resizing your way there.

```sh
tile resize %5 down 2       # grow %5 downward, taking from the pane below
```

Sizes are relative, so a layout built at one terminal size holds its
proportions at any other.

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
