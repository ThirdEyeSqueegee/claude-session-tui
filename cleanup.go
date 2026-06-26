package main

import (
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

	// loosely-keyed caches: entries named for the id
	for _, sub := range []string{"paste-cache", "tasks", "todos"} {
		removeMatchingDeep(rm, filepath.Join(root, sub), s.ID)
	}
	return rm.err
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
