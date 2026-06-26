package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
)

// deleteSession removes a session's jsonl plus all the satellite state Claude
// Code keeps for it: session-env, file-history, subagent project dirs, and the
// paste-cache / tasks / todos entries keyed by the session id. Mirrors the
// cleanup in the `cce` nushell helper.
//
// Safety: the id is validated as a UUID before any path is derived from it (a
// bare "" or ".." would otherwise make filepath.Join escape the session dir and
// RemoveAll a parent — catastrophic). Every join is also re-confirmed to live
// strictly under ~/.claude before removal as defense in depth. Per-path errors
// are collected but don't abort the rest of the delete; the first is returned
// so the caller can avoid dropping the row on a failed delete.
func deleteSession(s Session) error {
	if !validID(s.ID) { // never derive a destructive path from a non-UUID id
		return os.ErrInvalid
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	root := filepath.Join(home, ".claude")
	rm := newConfinedRemover(root)

	// the conversation transcript itself (path came from the glob, but confirm)
	rm.remove(s.JsonlPath)

	// per-session state dirs keyed exactly by id
	rm.remove(filepath.Join(root, "session-env", s.ID))
	rm.remove(filepath.Join(root, "file-history", s.ID))

	// subagent transcript dirs live under projects/*/<id>
	matches, _ := filepath.Glob(filepath.Join(root, "projects", "*", s.ID))
	for _, m := range matches {
		rm.remove(m)
	}

	// tasks are keyed by a truncated id: tasks/session-<id[:8]>. The short id is
	// only 32 bits, so two sessions can collide on it; removing the dir would
	// then reap a still-live session's task state. Only remove it when no other
	// live transcript shares the prefix.
	if !otherLiveShares(root, s.ID) {
		rm.remove(filepath.Join(root, "tasks", "session-"+shortID(s.ID)))
	}

	// session metadata files: sessions/<pid>.json, keyed by the UUID in each
	// file's inner "sessionId" field rather than by its (pid) filename.
	for _, p := range sessionJSONFilesFor(root, s.ID) {
		rm.remove(p)
	}

	// loosely-keyed caches: entries named for the id
	for _, sub := range []string{"paste-cache", "tasks", "todos"} {
		removeMatchingDeep(rm, filepath.Join(root, sub), s.ID)
	}

	// once the transcript is gone, drop the enclosing projects/<encoded-cwd>
	// dir if it's now empty so a deleted repo doesn't leave a husk behind.
	removeIfEmptyDir(rm, filepath.Dir(s.JsonlPath))
	return rm.err
}

// shortID is the first 8 chars of a UUID — the form Claude Code names some
// satellite state with (e.g. tasks/session-<id[:8]>).
func shortID(id string) string {
	if len(id) >= 8 {
		return id[:8]
	}
	return id
}

// otherLiveShares reports whether a live transcript other than id shares id's
// 8-char short form. Guards the short-id-keyed delete (tasks/session-<id[:8]>)
// against reaping state that a still-live, prefix-colliding session relies on.
func otherLiveShares(root, id string) bool {
	short := shortID(id)
	paths, _ := filepath.Glob(filepath.Join(root, "projects", "*", "*.jsonl"))
	for _, p := range paths {
		other := strings.TrimSuffix(filepath.Base(p), ".jsonl")
		if other != id && validID(other) && shortID(other) == short {
			return true
		}
	}
	return false
}

// sessionJSONID reads the "sessionId" field from a sessions/<pid>.json file.
// Returns "" if the file is unreadable or has no such field.
func sessionJSONID(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	var rec struct {
		SessionID string `json:"sessionId"`
	}
	if json.Unmarshal(data, &rec) != nil {
		return ""
	}
	return rec.SessionID
}

// sessionJSONFilesFor returns the sessions/*.json files whose inner sessionId
// equals id. These files are named by pid (e.g. 5784.json), not by the
// conversation id, so each must be read to find the match.
func sessionJSONFilesFor(root, id string) []string {
	matches, _ := filepath.Glob(filepath.Join(root, "sessions", "*.json"))
	var out []string
	for _, p := range matches {
		if sessionJSONID(p) == id {
			out = append(out, p)
		}
	}
	return out
}

// removeIfEmptyDir removes dir only when it has no entries. Used to clean a
// projects/<encoded-cwd> directory after its last transcript is deleted.
func removeIfEmptyDir(rm *confinedRemover, dir string) {
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) > 0 {
		return
	}
	rm.remove(dir)
}

// confinedRemover refuses to remove any path that doesn't resolve to something
// strictly inside root (and never root itself). It records the first error.
type confinedRemover struct {
	root string
	err  error
}

func newConfinedRemover(root string) *confinedRemover {
	clean, _ := filepath.Abs(root)
	return &confinedRemover{root: filepath.Clean(clean)}
}

func (r *confinedRemover) confined(path string) bool {
	abs, err := filepath.Abs(path)
	if err != nil {
		return false
	}
	abs = filepath.Clean(abs)
	return abs != r.root && strings.HasPrefix(abs, r.root+string(os.PathSeparator))
}

func (r *confinedRemover) remove(path string) {
	if !r.confined(path) {
		if r.err == nil {
			r.err = os.ErrPermission
		}
		return
	}
	if err := os.RemoveAll(path); err != nil && r.err == nil {
		r.err = err
	}
}

// removeMatchingDeep walks base and removes entries whose name is the id or is
// the id with a "." or "-" suffix (e.g. "<id>.json", "<id>-agent-1"). The match
// is structured, not a bare substring, so one id can't sweep unrelated state.
func removeMatchingDeep(rm *confinedRemover, base, id string) {
	if _, err := os.Stat(base); err != nil {
		return
	}
	_ = filepath.Walk(base, func(path string, info os.FileInfo, err error) error {
		if err != nil || path == base || info == nil {
			return nil
		}
		if idMatch(filepath.Base(path), id) {
			rm.remove(path)
			if info.IsDir() {
				return filepath.SkipDir
			}
		}
		return nil
	})
}

// idMatch reports whether a filename belongs to the session id: an exact match,
// or the id followed by a "." (extension) or "-" (suffix) delimiter.
func idMatch(name, id string) bool {
	if name == id {
		return true
	}
	return strings.HasPrefix(name, id+".") || strings.HasPrefix(name, id+"-")
}
