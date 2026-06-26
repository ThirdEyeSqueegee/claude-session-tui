package main

import (
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
)

func scopedSample() []Session {
	base := time.Date(2026, 6, 24, 12, 0, 0, 0, time.UTC)
	ss := []Session{
		{ID: "a", Path: "~/work/api", PathReal: "/work/api", Title: "api one", Msgs: 5, Updated: base},
		{ID: "b", Path: "~/work/api", PathReal: "/work/api", Title: "api two", Msgs: 7, Updated: base.Add(-time.Hour)},
		{ID: "c", Path: "~/work/web", PathReal: "/work/web", Title: "web one", Msgs: 3, Updated: base.Add(-2 * time.Hour)},
	}
	for i := range ss {
		ss[i].Haystack = strings.ToLower(ss[i].Title + " " + ss[i].Path)
	}
	return ss
}

func TestProjectScopeFilters(t *testing.T) {
	m := loaded(scopedSample())
	m.cwd = "/work/api"
	m.cwdScope = true
	m.rebuild()
	for _, r := range m.rows {
		if r.kind == rowSession && r.session.PathReal != "/work/api" {
			t.Errorf("scoped view leaked session from %s", r.session.PathReal)
		}
	}
	// two api sessions in scope
	n := 0
	for _, r := range m.rows {
		if r.kind == rowSession {
			n++
		}
	}
	if n != 2 {
		t.Errorf("scoped count = %d, want 2", n)
	}
	// toggling off shows all 3
	m.cwdScope = false
	m.rebuild()
	n = 0
	for _, r := range m.rows {
		if r.kind == rowSession {
			n++
		}
	}
	if n != 3 {
		t.Errorf("unscoped count = %d, want 3", n)
	}
}

func TestScopeEmptyHint(t *testing.T) {
	m := loaded(scopedSample())
	m.cwd = "/nowhere"
	m.cwdScope = true
	m.rebuild()
	tm, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 20})
	out := stripANSI(tm.(model).View().Content)
	if !strings.Contains(out, "no chats in this repo") {
		t.Errorf("expected scope-empty hint, got: %q", out)
	}
}

func TestMarkAndBulkTargets(t *testing.T) {
	m := loaded(scopedSample())
	// no marks → cursor row is the single target
	if got := len(m.deleteTargets()); got != 1 {
		t.Errorf("no-mark targets = %d, want 1", got)
	}
	// mark two
	m.marked["a"] = true
	m.marked["c"] = true
	targets := m.deleteTargets()
	if len(targets) != 2 {
		t.Fatalf("marked targets = %d, want 2", len(targets))
	}
	ids := map[string]bool{}
	for _, s := range targets {
		ids[s.ID] = true
	}
	if !ids["a"] || !ids["c"] {
		t.Errorf("wrong targets: %v", ids)
	}
}

// TestSpaceKeyMarks guards the actual keybind: the space key String() is
// "space", not " ", so the binding must accept both.
func TestSpaceKeyMarks(t *testing.T) {
	m := loaded(scopedSample())
	tm, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 20})
	m = tm.(model)
	first := m.selected().ID
	cur := m.cursor
	m2, _ := m.updateList(tea.KeyPressMsg{Code: ' ', Text: " "})
	m = m2.(model)
	if !m.marked[first] {
		t.Errorf("space did not mark the row (marked=%v)", m.marked)
	}
	if m.cursor != cur {
		t.Errorf("space advanced the cursor %d→%d; it should only mark", cur, m.cursor)
	}
	// space again unmarks, still no move
	m2, _ = m.updateList(tea.KeyPressMsg{Code: ' ', Text: " "})
	m = m2.(model)
	if m.marked[first] {
		t.Error("second space did not unmark")
	}
	if m.cursor != cur {
		t.Errorf("second space moved cursor to %d", m.cursor)
	}
}

func TestProjectHueStableAndWarm(t *testing.T) {
	a1, i1 := projectHue("/work/api")
	a2, i2 := projectHue("/work/api")
	if a1 != a2 || i1 != i2 {
		t.Error("projectHue not deterministic")
	}
	if projectHueActive(t, "/work/api") == projectHueActive(t, "/work/web") {
		t.Error("different projects produced identical hue (collision or constant)")
	}
	if !hexRe.MatchString(a1) || !hexRe.MatchString(i1) {
		t.Errorf("projectHue produced bad hex: %q %q", a1, i1)
	}
}

func projectHueActive(t *testing.T, p string) string {
	t.Helper()
	a, _ := projectHue(p)
	return a
}

func TestTabColorerPerProject(t *testing.T) {
	t.Setenv("KITTY_WINDOW_ID", "1") // pretend kitty so it's enabled
	on := true
	cfg := TabColorConfig{Enabled: &on, Active: "#d97757", Inactive: "#914f39", PerProject: true}
	tc := newTabColorer(cfg, "/work/api")
	// per-project overrides the fixed Active
	if tc.active == "#d97757" {
		t.Error("per_project did not override the fixed active color")
	}
	if !hexRe.MatchString(tc.active) {
		t.Errorf("per-project active not hex: %q", tc.active)
	}
}
