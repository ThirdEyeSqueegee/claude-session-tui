package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestBulkDeleteOnDisk exercises the full doDelete path with real files: the
// marked set drives deleteTargets (which returns &m.all[i] pointers), each is
// deleted on disk, then removeByID compacts m.all in place. Guards against a
// use-after-mutate in that pointer-then-compact sequence.
func TestBulkDeleteOnDisk(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	root := filepath.Join(home, ".claude")
	proj := filepath.Join(root, "projects", "-work")
	if err := os.MkdirAll(proj, 0o755); err != nil {
		t.Fatal(err)
	}
	ids := []string{
		"aaaaaaaa-1111-1111-1111-111111111111",
		"bbbbbbbb-2222-2222-2222-222222222222",
		"cccccccc-3333-3333-3333-333333333333",
	}
	var ss []Session
	base := time.Now()
	for i, id := range ids {
		jp := filepath.Join(proj, id+".jsonl")
		if err := os.WriteFile(jp, []byte("{}\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		ss = append(ss, Session{ID: id, Path: "~/work", PathReal: "/work", JsonlPath: jp, Title: "t" + id, Updated: base.Add(-time.Duration(i) * time.Hour), Haystack: id})
	}
	m := loaded(ss)
	m.marked[ids[0]] = true
	m.marked[ids[2]] = true
	m.doDelete()

	if _, err := os.Stat(filepath.Join(proj, ids[0]+".jsonl")); !os.IsNotExist(err) {
		t.Errorf("marked id0 transcript not deleted")
	}
	if _, err := os.Stat(filepath.Join(proj, ids[2]+".jsonl")); !os.IsNotExist(err) {
		t.Errorf("marked id2 transcript not deleted")
	}
	if _, err := os.Stat(filepath.Join(proj, ids[1]+".jsonl")); err != nil {
		t.Errorf("unmarked id1 transcript wrongly deleted: %v", err)
	}
	if len(m.all) != 1 || m.all[0].ID != ids[1] {
		t.Errorf("after bulk delete m.all has %d sessions, want only id1", len(m.all))
	}
	if m.marked[ids[0]] || m.marked[ids[2]] {
		t.Errorf("deleted ids still marked: %v", m.marked)
	}
}
