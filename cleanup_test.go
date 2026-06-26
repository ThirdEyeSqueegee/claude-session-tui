package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestValidID(t *testing.T) {
	good := []string{
		"ffe03f95-4631-49eb-a856-2815959dc299",
		"00000000-0000-0000-0000-000000000000",
	}
	bad := []string{"", ".", "..", "foo", "../etc", "a/b", `a\b`, "ffe03f95", "ffe03f95-4631-49eb-a856-2815959dc29"}
	for _, id := range good {
		if !validID(id) {
			t.Errorf("validID(%q) = false, want true", id)
		}
	}
	for _, id := range bad {
		if validID(id) {
			t.Errorf("validID(%q) = true, want false", id)
		}
	}
}

// TestDeleteRejectsBadID checks deleteSession refuses any non-UUID id (e.g.
// "..", which would otherwise RemoveAll the whole ~/.claude tree) before
// touching the filesystem.
func TestDeleteRejectsBadID(t *testing.T) {
	for _, id := range []string{"", ".", "..", "foo"} {
		if err := deleteSession(Session{ID: id}); err == nil {
			t.Errorf("deleteSession(id=%q) returned nil error — should refuse", id)
		}
	}
}

// TestConfinedRemover proves the remover refuses paths outside root and never
// the root itself, then deletes a real file inside it.
func TestConfinedRemover(t *testing.T) {
	root := t.TempDir()
	rm := newConfinedRemover(root)

	// outside root: must be refused, nothing removed
	outside := filepath.Join(t.TempDir(), "victim")
	if err := os.WriteFile(outside, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	rm.remove(outside)
	if _, err := os.Stat(outside); err != nil {
		t.Error("confinedRemover removed a path outside root")
	}
	if rm.err == nil {
		t.Error("expected an error after refusing an outside path")
	}

	// root itself: must be refused
	rm2 := newConfinedRemover(root)
	rm2.remove(root)
	if _, err := os.Stat(root); err != nil {
		t.Error("confinedRemover removed root itself")
	}

	// a path .. that escapes via traversal: must be refused
	rm3 := newConfinedRemover(root)
	rm3.remove(filepath.Join(root, "..", "elsewhere"))
	if rm3.err == nil {
		t.Error("expected refusal of traversal path")
	}

	// legit path inside root: removed
	inside := filepath.Join(root, "session-env", "id")
	os.MkdirAll(inside, 0o755)
	rm4 := newConfinedRemover(root)
	rm4.remove(inside)
	if _, err := os.Stat(inside); !os.IsNotExist(err) {
		t.Error("confinedRemover failed to remove a legit inside path")
	}
	if rm4.err != nil {
		t.Errorf("unexpected error removing inside path: %v", rm4.err)
	}
}

func TestIDMatch(t *testing.T) {
	id := "ffe03f95-4631-49eb-a856-2815959dc299"
	yes := []string{id, id + ".json", id + ".jsonl", id + "-agent-1", id + "-1.json"}
	no := []string{"x" + id, id[:10], "prefix-" + id, "other"}
	for _, n := range yes {
		if !idMatch(n, id) {
			t.Errorf("idMatch(%q) = false, want true", n)
		}
	}
	for _, n := range no {
		if idMatch(n, id) {
			t.Errorf("idMatch(%q) = true, want false", n)
		}
	}
}

func TestSanitizeStripsControlAndANSI(t *testing.T) {
	cases := map[string]string{
		"plain":                   "plain",
		"a\x1b[2Jb":               "ab",       // CSI clear-screen
		"a\x1b[31mred\x1b[0mb":    "aredb",    // SGR color
		"bell\x07here":            "bellhere", // BEL
		"title\x1b]0;evil\x07end": "titleend", // OSC set-title
		"keep\nnewline\tand tab":  "keep\nnewline\tand tab",
		"世界":                      "世界",       // wide runes survive
		"line\rover":              "lineover", // CR alone (no other control) — overwrites the terminal line
		"a\x16b":                  "ab",       // SYN, in the 0x10–0x1a gap
		"a\x1cb":                  "ab",       // FS, in the 0x1c–0x1f gap
		"a\x1fb":                  "ab",       // US
	}
	for in, want := range cases {
		if got := sanitize(in); got != want {
			t.Errorf("sanitize(%q) = %q, want %q", in, got, want)
		}
	}
}

// TestParseSessionRejectsDotDot proves a jsonl literally named "...jsonl"
// (basename "..") never becomes a session.
func TestParseSessionRejectsDotDot(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "...jsonl")
	if err := os.WriteFile(p, []byte(`{"type":"user","cwd":"/x","timestamp":"2026-01-01T00:00:00Z","message":{"content":"hi"}}`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, ok := parseSession(p); ok {
		t.Error("parseSession accepted a file whose id is '..' — safety hole")
	}
}
