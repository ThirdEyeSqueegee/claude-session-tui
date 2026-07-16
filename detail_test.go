package main

import (
	"strings"
	"testing"
	"time"
)

// TestRenderDetailShowsNewFields is an end-to-end render guard: the detail pane
// must surface transcript size, the token/cost summary, and git-gone flags.
func TestRenderDetailShowsNewFields(t *testing.T) {
	base := time.Date(2026, 6, 24, 12, 0, 0, 0, time.UTC)
	s := Session{
		ID:    "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee",
		Path:  "~/work/api",
		Title: "Fix auth", Msgs: 12, Updated: base,
		Size:  4_600_000,
		Model: "claude-opus-4-8",
		InTok: 1_000_000, OutTok: 20_000,
		Branch: "main", BranchGone: true, PathGone: true,
	}
	m := loaded([]Session{s})
	m.width, m.height, m.now = 120, 40, base
	out := stripANSI(m.renderDetail(60))
	for _, want := range []string{"4.4 MB", "main (gone)", "(gone)", "out", "$"} {
		if !strings.Contains(out, want) {
			t.Errorf("detail pane missing %q:\n%s", want, out)
		}
	}
}
