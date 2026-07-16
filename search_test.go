package main

import (
	"os"
	"path/filepath"
	"testing"
)

// TestJumpToMatch drives the transcript-search index math directly on the
// model's transcriptLines + viewport, covering forward, backward, wrap, and
// case-insensitivity.
func TestJumpToMatch(t *testing.T) {
	m := loaded(sample())
	m.transcriptLines = []string{
		"zero: intro",         // 0
		"one: FooBar here",    // 1
		"two: nothing",        // 2
		"three: foobar again", // 3
		"four: end",           // 4
	}
	m.vp.SetHeight(2)
	m.vp.SetContent("a\nb\nc\nd\ne") // 5 lines so YOffset can range 0..
	m.searchQuery = "foobar"

	// from top, forward → first match at line 1
	m.vp.SetYOffset(0)
	m.jumpToMatch(1)
	if got := m.vp.YOffset(); got != 1 {
		t.Errorf("forward from 0 = %d, want 1", got)
	}
	// forward again → next match at line 3
	m.jumpToMatch(1)
	if got := m.vp.YOffset(); got != 3 {
		t.Errorf("forward from 1 = %d, want 3", got)
	}
	// forward again → wraps back to line 1
	m.jumpToMatch(1)
	if got := m.vp.YOffset(); got != 1 {
		t.Errorf("forward wrap = %d, want 1", got)
	}
	// backward from 1 → wraps to line 3
	m.jumpToMatch(-1)
	if got := m.vp.YOffset(); got != 3 {
		t.Errorf("backward wrap = %d, want 3", got)
	}
	// empty query is a no-op
	m.vp.SetYOffset(2)
	m.searchQuery = ""
	m.jumpToMatch(1)
	if got := m.vp.YOffset(); got != 2 {
		t.Errorf("empty-query jump moved offset to %d, want 2", got)
	}
}

// TestProjectsFingerprintChanges proves the watch fingerprint moves when a
// transcript is added or its size changes, and is stable otherwise.
func TestProjectsFingerprintChanges(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	proj := filepath.Join(home, ".claude", "projects", "-work")
	if err := os.MkdirAll(proj, 0o755); err != nil {
		t.Fatal(err)
	}
	f := filepath.Join(proj, "11111111-1111-1111-1111-111111111111.jsonl")
	if err := os.WriteFile(f, []byte("{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	fp1 := projectsFingerprint()
	if fp1 == "" {
		t.Fatal("fingerprint empty with a transcript present")
	}
	if projectsFingerprint() != fp1 {
		t.Error("fingerprint not stable across calls with no change")
	}
	// grow the file → fingerprint must move
	if err := os.WriteFile(f, []byte(`{"a":1}`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if projectsFingerprint() == fp1 {
		t.Error("fingerprint did not change after the transcript grew")
	}
}
