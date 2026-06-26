package main

import (
	"regexp"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
)

var ansiRe = regexp.MustCompile("\x1b\\[[0-9;]*m")

func stripANSI(s string) string { return ansiRe.ReplaceAllString(s, "") }

func sample() []Session {
	base := time.Date(2026, 6, 24, 12, 0, 0, 0, time.UTC)
	ss := []Session{
		{ID: "aaaaaaaa-1", Path: "~/work/api", Title: "Fix auth middleware", FirstMsg: "auth bug", Msgs: 18, Updated: base},
		{ID: "bbbbbbbb-2", Path: "~/work/api", Title: "Add rate limiting", FirstMsg: "limit", Msgs: 7, Updated: base.Add(-24 * time.Hour)},
		{ID: "cccccccc-3", Path: "~/misc/tui", Title: "Picker design", FirstMsg: "tui", Msgs: 31, Updated: base.Add(-time.Hour)},
		{ID: "dddddddd-4", Path: "~/configs", Title: "Nushell helpers", FirstMsg: "nu", Msgs: 4, Updated: base.Add(-72 * time.Hour)},
	}
	for i := range ss { // mimic what parseSession computes
		ss[i].Haystack = strings.ToLower(ss[i].Title + " " + ss[i].Path + " " + ss[i].FirstMsg)
	}
	return ss
}

// loaded returns a model already populated with sessions, as if the async load
// had completed.
func loaded(sessions []Session) model {
	m := initialModel(time.Date(2026, 6, 24, 12, 0, 0, 0, time.UTC), defaultConfig(), nil, "")
	m.loading = false
	m.all = sessions
	m.rebuild()
	return m
}

func TestTextHelpers(t *testing.T) {
	if got := truncate("hello world foo", 8); got != "hello w…" {
		t.Errorf("truncate clip = %q", got)
	}
	if got := truncate("hi", 8); got != "hi" {
		t.Errorf("truncate noclip = %q", got)
	}
	if got := stripPrompt("<command-name>foo</command-name> bar"); got != "foo bar" {
		t.Errorf("stripPrompt = %q", got)
	}
	if got := contentText([]byte(`"plain"`)); got != "plain" {
		t.Errorf("contentText string = %q", got)
	}
	if got := contentText([]byte(`[{"type":"text","text":"a"},{"type":"tool_use"},{"type":"text","text":"b"}]`)); got != "a b" {
		t.Errorf("contentText blocks = %q", got)
	}
}

// TestTruncatePerf: truncate is O(n) so a long no-newline string truncates
// near-instantly rather than stalling.
func TestTruncatePerf(t *testing.T) {
	big := strings.Repeat("x", 1_000_000)
	start := time.Now()
	got := truncate(big, 40)
	// O(n) is sub-ms; allow ample headroom for -race instrumentation. Under a
	// second proves linear.
	if d := time.Since(start); d > 1*time.Second {
		t.Fatalf("truncate took %v (expected <1s) — quadratic regression", d)
	}
	if w := len([]rune(got)); w > 40 {
		t.Errorf("truncate output width %d exceeds 40", w)
	}
	if !strings.HasSuffix(got, "…") {
		t.Errorf("truncate of long input should end with ellipsis: %q", got)
	}
}

func TestTruncateWideRunes(t *testing.T) {
	// CJK runes are 2 cells each; truncate must respect display width, not count.
	got := truncate(strings.Repeat("世", 100), 10)
	if w := lipglossWidth(got); w > 10 {
		t.Errorf("wide-rune truncate width %d > 10 (%q)", w, got)
	}
}

// TestWrapClipHardBreaks: a message with a long unbroken token (URL / path)
// must not produce a line wider than w. An over-width line soft-wraps in the
// terminal, making the detail pane render taller than the list pane and
// knocking the borders + help bar out of line.
func TestWrapClipHardBreaks(t *testing.T) {
	long := "open https://example.com/some/very/long/unbroken/path/that/exceeds/the/pane/width please"
	for _, w := range []int{37, 30, 24, 18} {
		out := stripANSI(wrapClip(long, w, 4))
		for line := range strings.SplitSeq(out, "\n") {
			if lipglossWidth(line) > w {
				t.Errorf("wrapClip(w=%d) produced a %d-wide line: %q", w, lipglossWidth(line), line)
			}
		}
	}
}

// TestDetailPaneHeightMatchesList guards the same bug at the layout level: no
// matter which session is selected, the detail pane must render the same number
// of rows as the list pane so their borders align.
func TestDetailPaneHeightMatchesList(t *testing.T) {
	now := time.Date(2026, 6, 24, 12, 0, 0, 0, time.UTC)
	ss := []Session{
		{ID: "a", Path: "~/work/api", Title: "short one", FirstMsg: "hi", LastMsg: "ok", Msgs: 3, Updated: now.Add(-time.Minute)},
		{ID: "b", Path: "~/work/api", Title: "long url one", Msgs: 7, Branch: "feature/a-very-long-branch-name-indeed", Model: "claude-sonnet-4-6", FirstMsg: "see https://example.com/some/very/long/unbroken/path/that/exceeds/the/pane", LastMsg: "fixed in /home/user/project/src/internal/handler.go:1284", Updated: now.Add(-time.Hour)},
		{ID: "c", Path: "~/work/api", Title: "third", FirstMsg: "z", Msgs: 1, Updated: now.Add(-2 * time.Hour)},
	}
	for i := range ss {
		ss[i].Haystack = ss[i].Title
	}
	m := loaded(ss)
	for _, dim := range [][2]int{{74, 16}, {60, 12}, {90, 20}, {52, 10}} {
		tm, _ := m.Update(tea.WindowSizeMsg{Width: dim[0], Height: dim[1]})
		mm := tm.(model)
		for cur := range mm.rows {
			if mm.rows[cur].kind == rowHeader {
				continue
			}
			mm.cursor = cur
			out := stripANSI(mm.View().Content)
			lines := strings.Split(out, "\n")
			if len(lines) > dim[1] {
				t.Errorf("W=%d H=%d cursor=%d: %d lines exceeds terminal height", dim[0], dim[1], cur, len(lines))
			}
			for _, line := range lines {
				if lipglossWidth(line) > dim[0] {
					t.Errorf("W=%d H=%d cursor=%d: line wider than terminal (%d): %q", dim[0], dim[1], cur, lipglossWidth(line), line)
				}
			}
		}
	}
}

func TestGroupingRecency(t *testing.T) {
	m := loaded(sample())
	if m.rows[0].kind != rowHeader || m.rows[0].header != "~/work/api" {
		t.Fatalf("first row = %+v", m.rows[0])
	}
	if s := m.selected(); s == nil || s.ID != "aaaaaaaa-1" {
		t.Fatalf("selected = %v", s)
	}
	for i, r := range m.rows {
		if r.kind == rowHeader {
			n := 0
			for j := i + 1; j < len(m.rows) && m.rows[j].kind == rowSession; j++ {
				n++
			}
			if n != r.count {
				t.Errorf("header %q count=%d actual=%d", r.header, r.count, n)
			}
		}
	}
}

func TestFilter(t *testing.T) {
	m := loaded(sample())
	m.filter.SetValue("auth")
	m.rebuild()
	got := 0
	for _, r := range m.rows {
		if r.kind == rowSession {
			got++
			if r.session.ID != "aaaaaaaa-1" {
				t.Errorf("unexpected match %s", r.session.ID)
			}
		}
	}
	if got != 1 {
		t.Errorf("auth filter matched %d, want 1", got)
	}
	m.filter.SetValue("api rate")
	m.rebuild()
	if s := onlySession(m); s == nil || s.ID != "bbbbbbbb-2" {
		t.Errorf("multi-token filter = %v", s)
	}
}

func TestSortCycle(t *testing.T) {
	m := loaded(sample())
	m.sort = sortMsgs
	m.rebuild()
	if s := m.selected(); s == nil || s.ID != "cccccccc-3" {
		t.Errorf("sortMsgs top = %v", s)
	}
	m.sort = sortProject
	m.rebuild()
	if m.rows[0].header != "~/configs" {
		t.Errorf("sortProject first group = %q", m.rows[0].header)
	}
}

func TestCursorSkipsHeaders(t *testing.T) {
	m := loaded(sample())
	seen := map[string]bool{}
	for range 10 {
		if s := m.selected(); s != nil {
			seen[s.ID] = true
		}
		m.moveCursor(1)
		if m.rows[m.cursor].kind == rowHeader {
			t.Fatalf("cursor landed on header at row %d", m.cursor)
		}
	}
	if len(seen) != 4 {
		t.Errorf("visited %d sessions, want 4", len(seen))
	}
}

func TestDeleteRemovesFromModel(t *testing.T) {
	m := loaded(sample())
	m.removeByID(map[string]bool{"aaaaaaaa-1": true})
	m.rebuild()
	for _, s := range m.all {
		if s.ID == "aaaaaaaa-1" {
			t.Fatal("deleted session still present")
		}
	}
	if len(m.all) != 3 {
		t.Errorf("after delete len=%d want 3", len(m.all))
	}
}

func TestRenderNoPanic(t *testing.T) {
	m := loaded(sample())
	for _, dim := range [][2]int{{90, 24}, {40, 10}, {200, 50}, {30, 6}, {10, 4}, {50, 24}} {
		tm, _ := m.Update(tea.WindowSizeMsg{Width: dim[0], Height: dim[1]})
		out := stripANSI(tm.(model).View().Content)
		if strings.TrimSpace(out) == "" {
			t.Errorf("empty render at %v", dim)
		}
		// no rendered line may exceed the terminal width
		for line := range strings.SplitSeq(out, "\n") {
			if lipglossWidth(line) > dim[0] {
				t.Errorf("at %v a line is %d cols wide (overflow): %q", dim, lipglossWidth(line), line)
			}
		}
	}
}

func TestNarrowLayoutSinglePane(t *testing.T) {
	m := loaded(sample())
	tm, _ := m.Update(tea.WindowSizeMsg{Width: 40, Height: 20})
	out := stripANSI(tm.(model).View().Content)
	// single-pane mode: a body line should not contain two vertical borders
	for line := range strings.SplitSeq(out, "\n") {
		if strings.Count(line, "│") > 2 {
			t.Errorf("narrow layout still renders two panes: %q", line)
		}
	}
}

func TestLoadingState(t *testing.T) {
	m := initialModel(time.Now(), defaultConfig(), nil, "")
	tm, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	out := stripANSI(tm.(model).View().Content)
	if !strings.Contains(out, "gathering your chats") {
		t.Errorf("expected loading view, got %q", out)
	}
	// loaded message flips out of loading
	tm, _ = tm.(model).Update(sessionsLoadedMsg{res: LoadResult{Sessions: sample()}})
	m2 := tm.(model)
	if m2.loading {
		t.Error("still loading after sessionsLoadedMsg")
	}
	if len(m2.all) != 4 {
		t.Errorf("loaded %d sessions, want 4", len(m2.all))
	}
}

func onlySession(m model) *Session {
	var found *Session
	for _, r := range m.rows {
		if r.kind == rowSession {
			if found != nil {
				return nil
			}
			found = r.session
		}
	}
	return found
}

// helper so tests don't import lipgloss directly
func lipglossWidth(s string) int { return displayWidth(s) }
