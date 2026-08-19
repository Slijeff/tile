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
