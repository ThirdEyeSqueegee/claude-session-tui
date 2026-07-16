package main

import (
	"strings"
	"testing"
	"time"
)

// TestSortBySize orders sessions by transcript size, largest first.
func TestSortBySize(t *testing.T) {
	ss := sample()
	ss[0].Size = 100
	ss[1].Size = 5000 // biggest
	ss[2].Size = 300
	ss[3].Size = 50
	m := loaded(ss)
	m.sort = sortSize
	m.rebuild()
	if s := m.selected(); s == nil || s.ID != "bbbbbbbb-2" {
		t.Errorf("sortSize top = %v, want bbbbbbbb-2 (largest)", s)
	}
}

// TestGroupByBranch buckets sessions under their git branch, with a placeholder
// header for sessions that have no branch.
func TestGroupByBranch(t *testing.T) {
	ss := sample()
	ss[0].Branch = "main"
	ss[1].Branch = "main"
	ss[2].Branch = "dev"
	ss[3].Branch = "" // → "(no branch)"
	for i := range ss {
		ss[i].Path = "~/same" // collapse project so only branch grouping differs
	}
	m := loaded(ss)
	m.group = groupBranch
	m.rebuild()
	headers := map[string]int{}
	for _, r := range m.rows {
		if r.kind == rowHeader {
			headers[r.header] = r.count
		}
	}
	if headers["main"] != 2 {
		t.Errorf("branch 'main' count = %d, want 2", headers["main"])
	}
	if headers["dev"] != 1 {
		t.Errorf("branch 'dev' count = %d, want 1", headers["dev"])
	}
	if headers["(no branch)"] != 1 {
		t.Errorf("'(no branch)' count = %d, want 1", headers["(no branch)"])
	}
}

// TestGroupByDate buckets by recency relative to the model's `now`.
func TestGroupByDate(t *testing.T) {
	m := loaded(sample())
	m.group = groupDate
	m.rebuild()
	headers := map[string]int{}
	for _, r := range m.rows {
		if r.kind == rowHeader {
			headers[r.header] = r.count
		}
	}
	// sample offsets from now (2026-06-24 12:00): 0h & 1h → today (2),
	// 24h → yesterday (1), 72h → this week (1).
	if headers["today"] != 2 {
		t.Errorf("today count = %d, want 2", headers["today"])
	}
	if headers["yesterday"] != 1 {
		t.Errorf("yesterday count = %d, want 1", headers["yesterday"])
	}
	if headers["this week"] != 1 {
		t.Errorf("this week count = %d, want 1", headers["this week"])
	}
}

func TestDateBucket(t *testing.T) {
	now := time.Date(2026, 6, 24, 12, 0, 0, 0, time.UTC)
	cases := []struct {
		ago  time.Duration
		want string
	}{
		{1 * time.Hour, "today"},
		{30 * time.Hour, "yesterday"},
		{4 * 24 * time.Hour, "this week"},
		{15 * 24 * time.Hour, "this month"},
		{100 * 24 * time.Hour, "this year"},
		{500 * 24 * time.Hour, "older"},
	}
	for _, c := range cases {
		if got := dateBucket(now.Add(-c.ago), now); got != c.want {
			t.Errorf("dateBucket(-%v) = %q, want %q", c.ago, got, c.want)
		}
	}
}

// TestSortCycleWraps proves the s-key cycle visits every mode and returns to
// the start, so adding sortSize didn't leave a mode unreachable.
func TestSortCycleWraps(t *testing.T) {
	seen := map[sortMode]bool{}
	s := sortRecency
	for range sortModeCount {
		seen[s] = true
		s = (s + 1) % sortModeCount
	}
	if s != sortRecency {
		t.Errorf("cycle of %d did not return to recency (got %v)", sortModeCount, s)
	}
	for _, want := range []sortMode{sortRecency, sortProject, sortMsgs, sortSize} {
		if !seen[want] {
			t.Errorf("sort mode %v unreachable in cycle", want)
		}
	}
}

// TestGroupLabelsDistinct guards the group cycle labels.
func TestGroupLabelsDistinct(t *testing.T) {
	labels := map[string]bool{}
	g := groupProject
	for range groupModeCount {
		labels[g.label()] = true
		g = (g + 1) % groupModeCount
	}
	for _, want := range []string{"project", "date", "branch"} {
		if !labels[want] {
			t.Errorf("group label %q missing from cycle", want)
		}
	}
	if !strings.Contains(strings.Join(keys(labels), ","), "branch") {
		t.Error("branch label not produced")
	}
}

func keys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
