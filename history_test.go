package main

import (
	"os"
	"path/filepath"
	"testing"
)

// keepNone drops every line; keepAll keeps every line.
func keepNone(string) bool { return false }
func keepAll(string) bool  { return true }

func TestRewriteHistoryMissingFile(t *testing.T) {
	root := t.TempDir()
	n, err := rewriteHistory(root, keepNone, true)
	if err != nil {
		t.Fatalf("missing file should be a no-op, got %v", err)
	}
	if n != 0 {
		t.Errorf("dropped %d from a missing file, want 0", n)
	}
}

func TestRewriteHistoryStripsAndKeeps(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, historyFile)
	body := `{"display":"a","sessionId":"keep"}` + "\n" +
		`{"display":"b","sessionId":"drop"}` + "\n" +
		`{"display":"c","sessionId":"keep"}` + "\n" +
		`{"display":"d"}` + "\n" // no sessionId
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	keep := func(id string) bool { return id != "drop" }

	// dry run: counts, no write
	n, err := rewriteHistory(root, keep, false)
	if err != nil || n != 1 {
		t.Fatalf("dry run = (%d, %v), want (1, nil)", n, err)
	}
	if got, _ := os.ReadFile(path); string(got) != body {
		t.Error("dry run rewrote the file")
	}

	// apply: drops the one line, keeps the rest (including the no-id line)
	n, err = rewriteHistory(root, keep, true)
	if err != nil || n != 1 {
		t.Fatalf("apply = (%d, %v), want (1, nil)", n, err)
	}
	want := `{"display":"a","sessionId":"keep"}` + "\n" +
		`{"display":"c","sessionId":"keep"}` + "\n" +
		`{"display":"d"}` + "\n"
	if got, _ := os.ReadFile(path); string(got) != want {
		t.Errorf("after strip =\n%q\nwant\n%q", got, want)
	}
}

// TestRewriteHistoryNoDropNoWrite proves a rewrite that drops nothing leaves the
// original file untouched (no temp/rename churn, mtime preserved).
func TestRewriteHistoryNoDropNoWrite(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, historyFile)
	if err := os.WriteFile(path, []byte(`{"sessionId":"x"}`+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	before, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	n, err := rewriteHistory(root, keepAll, true)
	if err != nil || n != 0 {
		t.Fatalf("keepAll = (%d, %v), want (0, nil)", n, err)
	}
	after, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if !before.ModTime().Equal(after.ModTime()) {
		t.Error("no-drop rewrite touched the file (mtime changed)")
	}
}

// TestRewriteHistoryPreservesMode checks the atomic replace keeps the original
// file mode rather than the temp file's.
func TestRewriteHistoryPreservesMode(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, historyFile)
	if err := os.WriteFile(path, []byte(`{"sessionId":"drop"}`+"\n"+`{"sessionId":"keep"}`+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := rewriteHistory(root, func(id string) bool { return id == "keep" }, true); err != nil {
		t.Fatal(err)
	}
	fi, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode().Perm() != 0o600 {
		t.Errorf("mode after rewrite = %o, want 600", fi.Mode().Perm())
	}
}
