package main

import (
	"os"
	"path/filepath"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/fsnotify/fsnotify"
)

// Live reload watches ~/.claude/projects for transcript changes. fsnotify is
// the primary path (near-instant, event-driven); the time-based poll in ui.go
// (watchTickMsg) is the fallback when a watcher can't be built — e.g. Linux
// inotify limits, or a filesystem fsnotify doesn't support.
//
// fsnotify is NOT recursive and transcripts live two levels down
// (projects/<encoded-cwd>/<id>.jsonl), so we watch projects/ itself (to catch a
// new project dir) plus every existing projects/* subdir (to catch jsonl
// create/write/remove). When a new subdir appears we Add it on the fly.
//
// A raw fs event is only a hint: many carry no net change (chmod, editor temp
// files, a write that doesn't alter the id set/size/mtime). Every reload is
// still gated by projectsFingerprint, and a burst is debounced into one reload
// via a pending flag, so an actively-appending session doesn't reload per line.

// watchDebounce coalesces a burst of fs events into a single reload.
const watchDebounce = 300 * time.Millisecond

// watchStartedMsg carries the freshly built watcher (or an error → poll
// fallback) back into the update loop, where it's stored on the model.
type watchStartedMsg struct {
	w   *fsnotify.Watcher
	err error
}

// fsEventMsg is one delivered fsnotify event (or a watcher error). The update
// loop reacts, then re-arms waitFSEvent to read the next one.
type fsEventMsg struct {
	event fsnotify.Event
	err   error
	ok    bool // false when the Events channel closed (watcher dead)
}

// fsReloadMsg fires watchDebounce after a burst began; the handler reloads only
// if the fingerprint actually moved.
type fsReloadMsg struct{}

// projectsDir is ~/.claude/projects, or "" if HOME can't be resolved.
func projectsDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".claude", "projects")
}

// startWatchCmd builds a watcher over projects/ and its subdirs. On any failure
// it returns the error so the caller falls back to polling.
func startWatchCmd() tea.Msg {
	root := projectsDir()
	if root == "" {
		return watchStartedMsg{err: os.ErrInvalid}
	}
	w, err := fsnotify.NewWatcher()
	if err != nil {
		return watchStartedMsg{err: err}
	}
	// Watch the root (new project dirs land here) plus each existing subdir
	// (transcripts land there). A missing root is fatal to the fs path.
	if err := w.Add(root); err != nil {
		w.Close()
		return watchStartedMsg{err: err}
	}
	for _, e := range dirEntries(root) {
		if e.IsDir() {
			// A subdir we can't watch (perms, races) isn't fatal — the root
			// watch still catches its creation, and the poll fallback would
			// catch the rest. Best-effort.
			_ = w.Add(filepath.Join(root, e.Name()))
		}
	}
	return watchStartedMsg{w: w}
}

// waitFSEvent blocks until the next fs event or error, as a tea.Cmd. Returns
// ok=false when the Events channel closes so the loop stops re-arming.
func waitFSEvent(w *fsnotify.Watcher) tea.Cmd {
	return func() tea.Msg {
		select {
		case event, ok := <-w.Events:
			return fsEventMsg{event: event, ok: ok}
		case err, ok := <-w.Errors:
			return fsEventMsg{err: err, ok: ok}
		}
	}
}

// onFSEvent handles one delivered event: keeps the watch set current (a newly
// created project dir is added so its transcripts are seen) and decides whether
// to arm a debounced reload. Returns the command to run next (re-arm the read,
// possibly batched with a debounce timer), or nil if the watcher died.
func (m *model) onFSEvent(msg fsEventMsg) tea.Cmd {
	if !msg.ok {
		// Events/Errors channel closed: watcher is gone. Fall back to polling so
		// live reload still works, just slower.
		m.fsw = nil
		return m.pollCmd()
	}
	if msg.err != nil {
		// A watcher error (e.g. event overflow) isn't fatal on its own — keep
		// reading. The fingerprint gate still catches whatever changed.
		return waitFSEvent(m.fsw)
	}
	// A new directory under projects/ is a new project dir — watch it too, or
	// its transcripts would be invisible until the next full reload.
	if msg.event.Has(fsnotify.Create) {
		if fi, err := os.Stat(msg.event.Name); err == nil && fi.IsDir() {
			_ = m.fsw.Add(msg.event.Name)
		}
	}
	cmds := []tea.Cmd{waitFSEvent(m.fsw)}
	if !m.fsReloadPending {
		m.fsReloadPending = true
		cmds = append(cmds, tea.Tick(watchDebounce, func(time.Time) tea.Msg { return fsReloadMsg{} }))
	}
	return tea.Batch(cmds...)
}

// pollCmd is the time-based fallback tick (used when fsnotify is unavailable).
func (m model) pollCmd() tea.Cmd {
	if !m.watch {
		return nil
	}
	return tea.Tick(m.watchInterval, func(time.Time) tea.Msg { return watchTickMsg{} })
}
