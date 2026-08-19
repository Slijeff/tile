package server

import (
	"testing"

	tea "charm.land/bubbletea/v2"
)

func TestParseKeySpec(t *testing.T) {
	cases := []struct {
		in   string
		want keySpec
	}{
		{"ctrl+b", keySpec{code: 'b', mod: tea.ModCtrl}},
		{"f12", keySpec{code: tea.KeyF12}},
		{"q", keySpec{code: 'q'}},
	}
	for _, c := range cases {
		if got := parseKeySpec(c.in); got != c.want {
			t.Fatalf("parseKeySpec(%q) = %+v, want %+v", c.in, got, c.want)
		}
	}
}
