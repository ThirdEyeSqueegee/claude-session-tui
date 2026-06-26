package main

import (
	"strconv"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
)

// leftColumn slices a rendered line to its first pane column (between the first
// two box-drawing verticals) so the detail pane's mirror of the title can't
// mask a list-pane clip. In single-pane mode the whole inner line is returned.
func leftColumn(line string) string {
	if _, rest, ok := strings.Cut(line, "│"); ok {
		if col, _, ok := strings.Cut(rest, "│"); ok {
			return col
		}
		return rest
	}
	return line
}

func TestCursorAlwaysVisibleFuzz(t *testing.T) {
	base := time.Date(2026, 6, 24, 12, 0, 0, 0, time.UTC)
	// build a worst case: many tiny groups (each header costs a blank line) plus
	// a couple of fat groups, with unique greppable titles.
	var ss []Session
	add := func(proj string, n int) {
		for range n {
			idx := len(ss)
			ss = append(ss, Session{
				ID:      "id" + strconv.Itoa(idx),
				Path:    proj,
				Title:   "UNIQ" + strconv.Itoa(idx),
				Msgs:    1,
				Updated: base.Add(-time.Duration(idx) * time.Minute),
			})
		}
	}
	for g := range 8 {
		add("~/solo"+strconv.Itoa(g), 1)
	}
	add("~/fat", 5)
	for g := range 4 {
		add("~/more"+strconv.Itoa(g), 1)
	}
	for i := range ss {
		ss[i].Haystack = ss[i].Title
	}

	m := loaded(ss)
	dims := [][2]int{{90, 24}, {80, 14}, {80, 9}, {100, 8}, {70, 12}, {120, 30}, {80, 7}}
	for _, dim := range dims {
		tm, _ := m.Update(tea.WindowSizeMsg{Width: dim[0], Height: dim[1]})
		mm := tm.(model)
		for cur := range mm.rows {
			if mm.rows[cur].kind == rowHeader {
				continue
			}
			mm.cursor = cur
			mm.top = 0 // force ensureVisible to do the work from the top
			mm.ensureVisible()
			sel := mm.selected()
			out := stripANSI(mm.View().Content)
			found := false
			for line := range strings.SplitSeq(out, "\n") {
				if strings.Contains(leftColumn(line), sel.Title) {
					found = true
					break
				}
			}
			if !found {
				t.Errorf("W=%d H=%d cursor=%d: selected %q clipped from list pane:\n%s",
					dim[0], dim[1], cur, sel.Title, out)
			}
		}
	}
}

// TestNoVerticalOverflow guards the narrow-width soft-wrap bug: across a wide
// span of terminal sizes and every cursor row, the rendered frame must fit
// within the terminal — no line wider than W (which the terminal would
// soft-wrap, inflating the height past H). A composed list row floored at a
// minimum title width could exceed a tiny pane and wrap; renderList now clamps
// each line to the inner width.
func TestNoVerticalOverflow(t *testing.T) {
	now := time.Date(2026, 6, 24, 12, 0, 0, 0, time.UTC)
	ss := []Session{
		{ID: "a", Path: "~/work/api", Title: "short one", FirstMsg: "hi", LastMsg: "ok", Msgs: 3, Updated: now.Add(-time.Minute)},
		{ID: "b", Path: "~/work/api", Title: "long url one", Msgs: 7, Branch: "feature/a-very-long-branch-name", Model: "claude-sonnet-4-6", FirstMsg: "see https://example.com/some/very/long/unbroken/path/that/exceeds/the/pane", LastMsg: "fixed in /home/user/project/src/internal/handler.go:1284", Updated: now.Add(-time.Hour)},
		{ID: "c", Path: "~/misc/tui", Title: "third", FirstMsg: "z", Msgs: 1, Updated: now.Add(-2 * time.Hour)},
	}
	for i := range ss {
		ss[i].Haystack = ss[i].Title
	}
	m := loaded(ss)
	for w := 1; w <= 120; w++ {
		for _, h := range []int{6, 8, 12, 24, 40} {
			tm, _ := m.Update(tea.WindowSizeMsg{Width: w, Height: h})
			mm := tm.(model)
			for cur := range mm.rows {
				if mm.rows[cur].kind == rowHeader {
					continue
				}
				mm.cursor = cur
				out := stripANSI(mm.View().Content)
				lines := strings.Split(out, "\n")
				if len(lines) > h {
					t.Errorf("W=%d H=%d cur=%d: %d rendered lines exceed terminal height", w, h, cur, len(lines))
				}
				for _, line := range lines {
					if lipglossWidth(line) > w {
						t.Errorf("W=%d H=%d cur=%d: line %d cols wide (soft-wraps): %q", w, h, cur, lipglossWidth(line), line)
					}
				}
			}
		}
	}
}
