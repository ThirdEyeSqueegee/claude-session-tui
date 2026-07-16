package main

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/fsnotify/fsnotify"
)

// waitEvent drains the watcher until it sees an event whose path satisfies want,
// or the deadline passes. Returns true on a match.
func waitEvent(t *testing.T, w *fsnotify.Watcher, want func(fsnotify.Event) bool) bool {
	t.Helper()
	deadline := time.After(3 * time.Second)
	for {
		select {
		case e, ok := <-w.Events:
			if !ok {
				return false
			}
			if want(e) {
				return true
			}
		case <-w.Errors:
			// keep waiting; a stray error isn't the assertion
		case <-deadline:
			return false
		}
	}
}

// TestStartWatchBuildsWatcher checks the fs watcher builds over a real
// projects/ tree and reports no error.
func TestStartWatchBuildsWatcher(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	if err := os.MkdirAll(filepath.Join(home, ".claude", "projects", "-work"), 0o755); err != nil {
		t.Fatal(err)
	}
	msg, ok := startWatchCmd().(watchStartedMsg)
	if !ok {
		t.Fatal("startWatchCmd did not return watchStartedMsg")
	}
	if msg.err != nil || msg.w == nil {
		t.Fatalf("watcher build failed: err=%v w=%v", msg.err, msg.w)
	}
	msg.w.Close()
}

// TestStartWatchMissingRoot falls back (error) when projects/ doesn't exist, so
// the caller polls instead.
func TestStartWatchMissingRoot(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home) // no .claude/projects created
	msg := startWatchCmd().(watchStartedMsg)
	if msg.err == nil {
		t.Error("expected an error (→ poll fallback) when projects/ is absent")
		if msg.w != nil {
			msg.w.Close()
		}
	}
}

// TestWatchDetectsNewTranscript proves a jsonl created in an already-watched
// subdir surfaces an event.
func TestWatchDetectsNewTranscript(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	sub := filepath.Join(home, ".claude", "projects", "-work")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	msg := startWatchCmd().(watchStartedMsg)
	if msg.err != nil {
		t.Fatalf("build: %v", msg.err)
	}
	defer msg.w.Close()

	f := filepath.Join(sub, "11111111-1111-1111-1111-111111111111.jsonl")
	if err := os.WriteFile(f, []byte("{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if !waitEvent(t, msg.w, func(e fsnotify.Event) bool { return e.Name == f }) {
		t.Error("no fs event for a new transcript in a watched subdir")
	}
}

// TestOnFSEventWatchesNewSubdir proves a newly-created project dir gets added to
// the watch set, so transcripts written into it afterward are seen — the
// two-level watch that a flat Add(root) would miss.
func TestOnFSEventWatchesNewSubdir(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	root := filepath.Join(home, ".claude", "projects")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	msg := startWatchCmd().(watchStartedMsg)
	if msg.err != nil {
		t.Fatalf("build: %v", msg.err)
	}
	m := model{fsw: msg.w, watch: true, watchInterval: time.Second}
	defer msg.w.Close()

	// a new project dir appears
	newDir := filepath.Join(root, "-new-project")
	if err := os.Mkdir(newDir, 0o755); err != nil {
		t.Fatal(err)
	}
	// feed the create event through onFSEvent so it Adds the subdir
	if cmd := m.onFSEvent(fsEventMsg{event: fsnotify.Event{Name: newDir, Op: fsnotify.Create}, ok: true}); cmd == nil {
		t.Fatal("onFSEvent returned nil for a live watcher")
	}
	// drain any pending events from the mkdir itself so they don't mask the write
	drain(msg.w, 200*time.Millisecond)

	// a transcript written into the new dir must now surface
	f := filepath.Join(newDir, "22222222-2222-2222-2222-222222222222.jsonl")
	if err := os.WriteFile(f, []byte("{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if !waitEvent(t, msg.w, func(e fsnotify.Event) bool { return e.Name == f }) {
		t.Error("new subdir was not added to the watch set (transcript event missed)")
	}
}

// TestFSReloadTriggersLoadWhenChanged drives the debounced-reload handler
// through Update: when the fingerprint moved, fsReloadMsg must dispatch a
// command that reloads (yields sessionsLoadedMsg); when it hasn't, it must not.
func TestFSReloadTriggersLoadWhenChanged(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	sub := filepath.Join(home, ".claude", "projects", "-work")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}

	m := model{watch: true, watchInterval: time.Second, gitStatus: false}
	m.fingerprint = projectsFingerprint() // baseline: empty projects tree
	m.fsReloadPending = true

	// no on-disk change → reload handler should NOT dispatch a load
	tm, cmd := m.Update(fsReloadMsg{})
	m = tm.(model)
	if m.fsReloadPending {
		t.Error("fsReloadPending not cleared")
	}
	if yieldsLoad(cmd) {
		t.Error("reload dispatched with no fingerprint change")
	}

	// now add a transcript so the fingerprint moves
	if err := os.WriteFile(filepath.Join(sub, "11111111-1111-1111-1111-111111111111.jsonl"), []byte("{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	m.fsReloadPending = true
	_, cmd = m.Update(fsReloadMsg{})
	if !yieldsLoad(cmd) {
		t.Error("reload did not dispatch a load after the projects tree changed")
	}
}

// yieldsLoad reports whether invoking cmd (or any command in a Batch) produces a
// sessionsLoadedMsg — i.e. a reload was scheduled.
func yieldsLoad(cmd tea.Cmd) bool {
	if cmd == nil {
		return false
	}
	switch msg := cmd().(type) {
	case sessionsLoadedMsg:
		return true
	case tea.BatchMsg:
		return slices.ContainsFunc(msg, yieldsLoad)
	}
	return false
}

func drain(w *fsnotify.Watcher, d time.Duration) {
	deadline := time.After(d)
	for {
		select {
		case <-w.Events:
		case <-w.Errors:
		case <-deadline:
			return
		}
	}
}

// TestOnFSEventChannelClosedFallsBack proves a closed Events channel drops the
// watcher and returns the poll fallback command (non-nil when watch is on).
func TestOnFSEventChannelClosedFallsBack(t *testing.T) {
	m := model{watch: true, watchInterval: time.Second, fsw: &fsnotify.Watcher{}}
	cmd := m.onFSEvent(fsEventMsg{ok: false})
	if m.fsw != nil {
		t.Error("closed channel did not clear the watcher")
	}
	if cmd == nil {
		t.Error("expected poll-fallback command after watcher death")
	}
}
