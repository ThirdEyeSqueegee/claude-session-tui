package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// mkClaudeRoot builds a fake ~/.claude with one live session and assorted
// orphaned satellite state, then points the test process's HOME at it so the
// HOME-rooted code under test (deleteSession, sweepOrphans) operates on it.
func mkClaudeRoot(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	root := filepath.Join(home, ".claude")

	const (
		live   = "11111111-1111-1111-1111-111111111111"
		orphan = "99999999-9999-9999-9999-999999999999"
	)

	mkdir := func(parts ...string) string {
		p := filepath.Join(append([]string{root}, parts...)...)
		if err := os.MkdirAll(p, 0o755); err != nil {
			t.Fatal(err)
		}
		return p
	}
	write := func(path, body string) {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	// a live transcript under projects/<encoded>/
	liveProj := mkdir("projects", "-work-live")
	write(filepath.Join(liveProj, live+".jsonl"), "{}\n")

	// satellites for the LIVE session — must survive a sweep
	mkdir("session-env", live)
	mkdir("file-history", live)
	mkdir("tasks", "session-"+live[:8])
	mkdir("jobs", live[:8])
	mkdir("teams", "session-"+live[:8])
	write(filepath.Join(root, "sessions", "5784.json"), `{"sessionId":"`+live+`"}`)

	// non-id siblings in jobs/ — never id-keyed, must always survive
	mkdir("jobs", ".draft-23c45257")
	write(filepath.Join(root, "jobs", "pins.json"), "[]")

	// orphaned satellites — no live transcript backs these
	mkdir("session-env", orphan)
	mkdir("file-history", orphan)
	mkdir("tasks", "session-"+orphan[:8])
	mkdir("jobs", orphan[:8])
	mkdir("teams", "session-"+orphan[:8])
	write(filepath.Join(root, "sessions", "13774.json"), `{"sessionId":"`+orphan+`"}`)
	mkdir("projects", "-work-live", orphan) // orphan subagent dir under a live project
	mkdir("projects", "-work-dead")         // husk: project dir with no transcript

	// history.jsonl: one live line (kept), two orphan lines (stripped), one line
	// with no sessionId (kept — can't prove it's an orphan).
	write(filepath.Join(root, "history.jsonl"),
		`{"display":"live","sessionId":"`+live+`"}`+"\n"+
			`{"display":"orphan a","sessionId":"`+orphan+`"}`+"\n"+
			`{"display":"orphan b","sessionId":"`+orphan+`"}`+"\n"+
			`{"display":"no session id"}`+"\n")

	return root
}

// historyIDs returns the sessionIds present in root's history.jsonl, one per
// non-empty line, "" for a line with no/unparseable sessionId.
func historyIDs(t *testing.T, root string) []string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(root, "history.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	var ids []string
	for line := range strings.SplitSeq(strings.TrimRight(string(data), "\n"), "\n") {
		if line == "" {
			continue
		}
		var rec struct {
			SessionID string `json:"sessionId"`
		}
		if err := json.Unmarshal([]byte(line), &rec); err != nil {
			t.Fatalf("bad history line %q: %v", line, err)
		}
		ids = append(ids, rec.SessionID)
	}
	return ids
}

func exists(p string) bool { _, err := os.Lstat(p); return err == nil }

func TestSweepFindsOrphansDryRun(t *testing.T) {
	root := mkClaudeRoot(t)
	res, err := sweepOrphans(false)
	if err != nil {
		t.Fatal(err)
	}
	if res.Removed != 0 {
		t.Errorf("dry run removed %d, want 0", res.Removed)
	}
	if res.HistoryLines != 2 {
		t.Errorf("dry run counted %d orphaned history lines, want 2", res.HistoryLines)
	}
	// dry run must not rewrite history
	if got := historyIDs(t, root); len(got) != 4 {
		t.Errorf("dry run rewrote history: %d lines remain, want 4", len(got))
	}
	const orphan = "99999999-9999-9999-9999-999999999999"
	want := map[string]bool{
		filepath.Join(root, "session-env", orphan):            true,
		filepath.Join(root, "file-history", orphan):           true,
		filepath.Join(root, "tasks", "session-"+orphan[:8]):   true,
		filepath.Join(root, "jobs", orphan[:8]):               true,
		filepath.Join(root, "teams", "session-"+orphan[:8]):   true,
		filepath.Join(root, "sessions", "13774.json"):         true,
		filepath.Join(root, "projects", "-work-live", orphan): true,
		filepath.Join(root, "projects", "-work-dead"):         true,
	}
	got := map[string]bool{}
	for _, p := range res.Orphans {
		got[p] = true
	}
	for p := range want {
		if !got[p] {
			t.Errorf("orphan not found: %s", p)
		}
	}
	for p := range got {
		if !want[p] {
			t.Errorf("unexpected orphan (live state swept?): %s", p)
		}
	}
	// dry run must not touch the filesystem
	if !exists(filepath.Join(root, "session-env", orphan)) {
		t.Error("dry run deleted an orphan")
	}
}

func TestSweepApplyRemovesOnlyOrphans(t *testing.T) {
	root := mkClaudeRoot(t)
	res, err := sweepOrphans(true)
	if err != nil {
		t.Fatal(err)
	}
	if res.Err != nil {
		t.Fatalf("apply error: %v", res.Err)
	}
	if res.HistoryLines != 2 {
		t.Errorf("apply stripped %d history lines, want 2", res.HistoryLines)
	}
	const (
		live   = "11111111-1111-1111-1111-111111111111"
		orphan = "99999999-9999-9999-9999-999999999999"
	)
	// history keeps the live line and the no-id line, drops both orphan lines.
	if got := historyIDs(t, root); len(got) != 2 || got[0] != live || got[1] != "" {
		t.Errorf("history after sweep = %v, want [%s ]", got, live)
	}
	// every orphan gone
	for _, p := range []string{
		filepath.Join(root, "session-env", orphan),
		filepath.Join(root, "file-history", orphan),
		filepath.Join(root, "tasks", "session-"+orphan[:8]),
		filepath.Join(root, "jobs", orphan[:8]),
		filepath.Join(root, "teams", "session-"+orphan[:8]),
		filepath.Join(root, "sessions", "13774.json"),
		filepath.Join(root, "projects", "-work-dead"),
	} {
		if exists(p) {
			t.Errorf("orphan survived apply: %s", p)
		}
	}
	// every live satellite survives, plus jobs/ non-id siblings
	for _, p := range []string{
		filepath.Join(root, "session-env", live),
		filepath.Join(root, "file-history", live),
		filepath.Join(root, "tasks", "session-"+live[:8]),
		filepath.Join(root, "jobs", live[:8]),
		filepath.Join(root, "teams", "session-"+live[:8]),
		filepath.Join(root, "jobs", ".draft-23c45257"),
		filepath.Join(root, "jobs", "pins.json"),
		filepath.Join(root, "sessions", "5784.json"),
		filepath.Join(root, "projects", "-work-live", live+".jsonl"),
	} {
		if !exists(p) {
			t.Errorf("live state wrongly swept: %s", p)
		}
	}
}

// TestDeleteSessionRemovesSatellites checks the extended deleteSession reaches
// the tasks short-id dir, the sessions/*.json keyed by inner sessionId, and the
// now-empty project dir.
func TestDeleteSessionRemovesSatellites(t *testing.T) {
	root := mkClaudeRoot(t)
	const live = "11111111-1111-1111-1111-111111111111"
	projDir := filepath.Join(root, "projects", "-work-live")

	// drop the orphan subagent dir first so the project dir can go empty when
	// the live transcript is deleted.
	os.RemoveAll(filepath.Join(projDir, "99999999-9999-9999-9999-999999999999"))

	s := Session{
		ID:        live,
		JsonlPath: filepath.Join(projDir, live+".jsonl"),
	}
	if err := deleteSession(s); err != nil {
		t.Fatalf("deleteSession: %v", err)
	}
	for _, p := range []string{
		s.JsonlPath,
		filepath.Join(root, "session-env", live),
		filepath.Join(root, "file-history", live),
		filepath.Join(root, "tasks", "session-"+live[:8]),
		filepath.Join(root, "jobs", live[:8]),
		filepath.Join(root, "teams", "session-"+live[:8]),
		filepath.Join(root, "sessions", "5784.json"),
		projDir, // empty husk removed
	} {
		if exists(p) {
			t.Errorf("deleteSession left %s behind", p)
		}
	}
	// the live session's history line is stripped; other sessions' lines and the
	// no-id line survive (delete only targets s.ID).
	for _, id := range historyIDs(t, root) {
		if id == live {
			t.Error("deleteSession left the session's history line behind")
		}
	}
}

// TestDeleteKeepsNonEmptyProjectDir proves the husk cleanup only fires when the
// project dir is actually empty — a sibling session must keep its dir.
func TestDeleteKeepsNonEmptyProjectDir(t *testing.T) {
	root := mkClaudeRoot(t)
	const live = "11111111-1111-1111-1111-111111111111"
	projDir := filepath.Join(root, "projects", "-work-live")
	// a sibling transcript in the same project dir
	sibling := filepath.Join(projDir, "22222222-2222-2222-2222-222222222222.jsonl")
	if err := os.WriteFile(sibling, []byte("{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	s := Session{ID: live, JsonlPath: filepath.Join(projDir, live+".jsonl")}
	if err := deleteSession(s); err != nil {
		t.Fatal(err)
	}
	if !exists(projDir) {
		t.Error("project dir removed while a sibling session remained")
	}
	if !exists(sibling) {
		t.Error("sibling transcript wrongly removed")
	}
}

func TestSessionJSONID(t *testing.T) {
	dir := t.TempDir()
	good := filepath.Join(dir, "5784.json")
	if err := os.WriteFile(good, []byte(`{"pid":5784,"sessionId":"abc-123"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := sessionJSONID(good); got != "abc-123" {
		t.Errorf("sessionJSONID = %q, want abc-123", got)
	}
	if got := sessionJSONID(filepath.Join(dir, "nope.json")); got != "" {
		t.Errorf("missing file = %q, want empty", got)
	}
}
