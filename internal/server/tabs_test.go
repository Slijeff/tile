package server

import (
	"testing"

	"github.com/charmbracelet/x/ansi"
)

func TestTabLineHitRangesCoverEachWindow(t *testing.T) {
	windows := []*window{
		{name: "zero"},
		{name: "one"},
		{name: "two"},
	}
	_, hits := tabLine(windows, 1, 80, theme{})
	if len(hits) != len(windows) {
		t.Fatalf("got %d hit ranges, want %d", len(hits), len(windows))
	}
	for i, h := range hits {
		if h.win != i {
			t.Fatalf("hit %d targets window %d, want %d", i, h.win, i)
		}
		if h.lo >= h.hi {
			t.Fatalf("hit %d has an empty range: %+v", i, h)
		}
	}
	// Ranges must not overlap and must run left to right in window order.
	for i := 1; i < len(hits); i++ {
		if hits[i].lo < hits[i-1].hi {
			t.Fatalf("hit ranges overlap: %+v then %+v", hits[i-1], hits[i])
		}
	}
}

func TestTabLineTruncatesToWidth(t *testing.T) {
	windows := []*window{{name: "a-very-long-window-name-that-does-not-fit"}}
	txt, _ := tabLine(windows, 0, 10, theme{})
	if got := ansi.StringWidth(txt); got > 10 {
		t.Fatalf("tabLine width %d exceeds requested 10", got)
	}
}
