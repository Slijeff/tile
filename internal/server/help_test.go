package server

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

func TestHelpBoxUniformWidthAndRemap(t *testing.T) {
	km := defaultKeymap
	km.Quit = "Q" // remapped keys must show up, not the default
	box := helpBox(km, theme{})

	w := ansi.StringWidth(box[0])
	for i, l := range box {
		if got := ansi.StringWidth(l); got != w {
			t.Fatalf("line %d width %d, want %d (uniform box width)", i, got, w)
		}
	}
	if !strings.Contains(strings.Join(box, "\n"), "Q") {
		t.Fatal("helpBox did not reflect the remapped quit key")
	}
}

// The top-level tooltip must show a single "windows…"/"panes…" submenu row
// for each sub-layer instead of its individual actions — those only appear
// once the user presses the layer's key and chordBox takes over, see
// TestChordEntriesStripsLeaderAndFiltersUnrelatedBindings.
func TestHelpEntriesShowsSubmenusNotIndividualActions(t *testing.T) {
	entries := helpEntries(defaultKeymap)

	var sawWindows, sawPanes bool
	for _, e := range entries {
		if e.key == defaultKeymap.Windows.Key && e.desc == "windows…" {
			sawWindows = true
		}
		if e.key == defaultKeymap.Panes.Key && e.desc == "panes…" {
			sawPanes = true
		}
		switch e.desc {
		case "new window", "kill window", "rename window":
			t.Fatalf("helpEntries leaked a window sub-action %+v, want only the windows… submenu row", e)
		case "kill pane", "rename pane":
			t.Fatalf("helpEntries leaked a pane sub-action %+v, want only the panes… submenu row", e)
		}
	}
	if !sawWindows {
		t.Fatalf("helpEntries() = %+v, want a %q/\"windows…\" row", entries, defaultKeymap.Windows.Key)
	}
	if !sawPanes {
		t.Fatalf("helpEntries() = %+v, want a %q/\"panes…\" row", entries, defaultKeymap.Panes.Key)
	}
}

func TestChordEntriesStripsLeaderAndFiltersUnrelatedBindings(t *testing.T) {
	km := defaultKeymap // Panes: {Key: "p", Kill: "x", Rename: "r"}
	entries := chordEntries(km, "p")

	got := map[string]string{}
	for _, e := range entries {
		got[e.key] = e.desc
	}
	if got["r"] != "rename pane" {
		t.Fatalf("chordEntries(km, \"p\")[\"r\"] = %q, want \"rename pane\"", got["r"])
	}
	if got["x"] != "kill pane" {
		t.Fatalf("chordEntries(km, \"p\")[\"x\"] = %q, want \"kill pane\"", got["x"])
	}
	for key := range got {
		if len(key) != 1 {
			t.Fatalf("chordEntries did not strip the leader from key %q", key)
		}
	}
	if _, ok := got[km.Quit]; ok {
		t.Fatal("chordEntries(km, \"p\") must not include bindings outside the p layer")
	}
}

// A keymap isn't limited to two-key chords: chordEntries must keep
// narrowing correctly at any depth, not just the first level.
func TestChordEntriesSupportsArbitraryDepth(t *testing.T) {
	km := defaultKeymap
	km.Panes = paneLayer{Key: "pa", Rename: "b"} // a 3-key chord: p, then a, then b; Kill left blank to isolate it

	first := chordEntries(km, "p")
	if len(first) != 1 || first[0].key != "ab" {
		t.Fatalf("chordEntries(km, \"p\") = %+v, want a single \"ab\" entry", first)
	}
	second := chordEntries(km, "pa")
	if len(second) != 1 || second[0].key != "b" || second[0].desc != "rename pane" {
		t.Fatalf("chordEntries(km, \"pa\") = %+v, want a single \"b\" entry", second)
	}
	third := chordEntries(km, "pab")
	if len(third) != 0 {
		t.Fatalf("chordEntries(km, \"pab\") = %+v, want none left once the chord is complete", third)
	}
}

func TestChordBoxTitleShowsAccumulatedSequence(t *testing.T) {
	km := defaultKeymap
	box := chordBox("p", km, theme{})
	joined := strings.Join(box, "\n")
	if !strings.Contains(joined, "» "+km.Prefix+" p") {
		t.Fatalf("chordBox title missing accumulated sequence, got:\n%s", joined)
	}
	if !strings.Contains(joined, "rename pane") || !strings.Contains(joined, "kill pane") {
		t.Fatalf("chordBox missing pane-layer entries, got:\n%s", joined)
	}
	if strings.Contains(joined, "quit") {
		t.Fatalf("chordBox must not show bindings outside the current chord, got:\n%s", joined)
	}
}

func TestOverlayStampsBottomRightWithoutDisturbingOtherLines(t *testing.T) {
	w, h := 20, 5
	lines := make([]string, h)
	for i := range lines {
		lines[i] = strings.Repeat("x", w)
	}
	base := strings.Join(lines, "\n")

	box := []string{"┌────┐", "│ hi │", "└────┘"}
	got := overlay(base, w, h, box)
	gotLines := strings.Split(got, "\n")

	if len(gotLines) != h {
		t.Fatalf("overlay changed line count: got %d, want %d", len(gotLines), h)
	}
	for i, l := range gotLines[:h-len(box)] {
		if ansi.StringWidth(l) != w {
			t.Fatalf("untouched line %d width %d, want %d", i, ansi.StringWidth(l), w)
		}
		if !strings.HasPrefix(l, strings.Repeat("x", w)) {
			t.Fatalf("line %d above the box was disturbed: %q", i, l)
		}
	}
	for i, l := range gotLines[h-len(box):] {
		if got := ansi.StringWidth(l); got != w {
			t.Fatalf("boxed line %d width %d, want %d", i, got, w)
		}
		if !strings.Contains(l, box[i]) {
			t.Fatalf("boxed line %d = %q, want it to contain %q", i, l, box[i])
		}
	}
}

func TestOverlaySkipsWhenBoxTallerThanScreen(t *testing.T) {
	base := "x\ny"
	box := []string{"a", "b", "c"}
	if got := overlay(base, 1, 2, box); got != base {
		t.Fatalf("overlay should refuse a box taller than the screen, got %q", got)
	}
}
