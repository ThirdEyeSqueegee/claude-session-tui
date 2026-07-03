package main

import (
	"os"
	"path/filepath"
	"testing"
)

// TestDeleteShortIDCollision guards F2: deleting one session must not reap the
// tasks dir of a still-live session that shares the first 8 UUID hex chars.
func TestDeleteShortIDCollision(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	root := filepath.Join(home, ".claude")
	const a = "abcdef12-1111-1111-1111-111111111111"
	const b = "abcdef12-2222-2222-2222-222222222222"

	proj := filepath.Join(root, "projects", "-work")
	if err := os.MkdirAll(proj, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(proj, a+".jsonl"), []byte("{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// live session b's transcript shares the short id with a
	if err := os.WriteFile(filepath.Join(proj, b+".jsonl"), []byte("{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// all three short-id-keyed dirs share a's (== b's) 8-char prefix
	shared := map[string]string{
		"tasks": filepath.Join(root, "tasks", "session-"+a[:8]),
		"jobs":  filepath.Join(root, "jobs", a[:8]),
		"teams": filepath.Join(root, "teams", "session-"+a[:8]),
	}
	for _, d := range shared {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	s := Session{ID: a, JsonlPath: filepath.Join(proj, a+".jsonl")}
	if err := deleteSession(s); err != nil {
		t.Fatalf("deleteSession: %v", err)
	}
	for kind, d := range shared {
		if _, err := os.Stat(d); os.IsNotExist(err) {
			t.Errorf("deleting %s reaped %s dir shared with live %s", a, kind, b)
		}
	}
}

// TestDeleteRemovesUncollidedTaskDir proves the F2 guard still removes the
// tasks dir in the normal (no collision) case.
func TestDeleteRemovesUncollidedTaskDir(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	root := filepath.Join(home, ".claude")
	const a = "abcdef12-1111-1111-1111-111111111111"
	proj := filepath.Join(root, "projects", "-work")
	if err := os.MkdirAll(proj, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(proj, a+".jsonl"), []byte("{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	dirs := map[string]string{
		"tasks": filepath.Join(root, "tasks", "session-"+a[:8]),
		"jobs":  filepath.Join(root, "jobs", a[:8]),
		"teams": filepath.Join(root, "teams", "session-"+a[:8]),
	}
	for _, d := range dirs {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	s := Session{ID: a, JsonlPath: filepath.Join(proj, a+".jsonl")}
	if err := deleteSession(s); err != nil {
		t.Fatalf("deleteSession: %v", err)
	}
	for kind, d := range dirs {
		if _, err := os.Stat(d); !os.IsNotExist(err) {
			t.Errorf("uncollided %s dir not removed", kind)
		}
	}
}
