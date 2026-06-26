package main

import (
	"os"
	"path/filepath"
	"testing"
)

// TestHuskKeepsLiveSubagentDir reproduces F1: a project dir that holds a live
// session's subagent dir but no top-level transcript must NOT be swept as a
// husk — doing so deletes live subagent state.
func TestHuskKeepsLiveSubagentDir(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	root := filepath.Join(home, ".claude")
	const live = "11111111-1111-1111-1111-111111111111"

	// live transcript under one encoded project dir
	mk := func(parts ...string) string {
		p := filepath.Join(append([]string{root}, parts...)...)
		if err := os.MkdirAll(p, 0o755); err != nil {
			t.Fatal(err)
		}
		return p
	}
	a := mk("projects", "-work-a")
	if err := os.WriteFile(filepath.Join(a, live+".jsonl"), []byte("{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// live session's subagent dir under a DIFFERENT encoded project dir, which
	// itself has no top-level transcript
	subagent := mk("projects", "-work-b", live)

	orphans := findOrphans(root)
	for _, o := range orphans {
		if o == filepath.Join(root, "projects", "-work-b") {
			t.Errorf("F1: husk sweep flagged a dir holding live subagent state: %s", o)
		}
		if o == subagent {
			t.Errorf("F1: live subagent dir flagged as orphan: %s", o)
		}
	}
}

// TestStrayFileNotHusk reproduces F3: a plain file under projects/ (e.g.
// .DS_Store) must not be flagged as a session-dir husk.
func TestStrayFileNotHusk(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	root := filepath.Join(home, ".claude")
	if err := os.MkdirAll(filepath.Join(root, "projects"), 0o755); err != nil {
		t.Fatal(err)
	}
	stray := filepath.Join(root, "projects", ".DS_Store")
	if err := os.WriteFile(stray, []byte("junk"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, o := range findOrphans(root) {
		if o == stray {
			t.Errorf("F3: stray file flagged as a project husk: %s", o)
		}
	}
}
