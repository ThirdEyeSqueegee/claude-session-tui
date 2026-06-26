package main

import (
	"os"
	"path/filepath"
)

// Orphan sweep: find and remove satellite state under ~/.claude that no longer
// has a live transcript. The inverse of deleteSession — instead of cleaning up
// after a session we delete, it reaps state left behind by sessions that are
// already gone (crashes, manual deletes, jsonl moved away). Mirrors the
// cce-find-orphans + ccp nushell helpers.
//
// "Live" is defined by the set of transcript ids: the basenames of every
// projects/*/*.jsonl. Any id-keyed satellite whose id isn't in that set is an
// orphan. Every removal still goes through confinedRemover, so a sweep can
// never escape ~/.claude.

// SweepResult is the outcome of a sweep: the paths that are (or would be)
// removed, plus the count actually removed when apply is set.
type SweepResult struct {
	Orphans []string // paths identified as orphaned satellite state
	Removed int      // paths actually removed (0 on a dry run)
	Err     error    // first removal error, if any
}

// liveIDs returns the set of transcript ids (jsonl basenames) under
// projects/*/*.jsonl, plus the set of their 8-char short forms.
func liveIDs(root string) (full map[string]bool, short map[string]bool) {
	full = map[string]bool{}
	short = map[string]bool{}
	paths, _ := filepath.Glob(filepath.Join(root, "projects", "*", "*.jsonl"))
	for _, p := range paths {
		id := trimJsonl(filepath.Base(p))
		if !validID(id) {
			continue
		}
		full[id] = true
		short[shortID(id)] = true
	}
	return full, short
}

func trimJsonl(name string) string {
	if ext := filepath.Ext(name); ext == ".jsonl" {
		return name[:len(name)-len(ext)]
	}
	return name
}

// findOrphans collects every id-keyed satellite path whose id has no live
// transcript, plus empty projects/<encoded-cwd> husks. It reads only; removal
// is the caller's job (see sweepOrphans).
func findOrphans(root string) []string {
	live, liveShort := liveIDs(root)
	var orphans []string

	// session-env/<id> and file-history/<id>: dirs named by the full UUID.
	for _, sub := range []string{"session-env", "file-history"} {
		for _, e := range dirEntries(filepath.Join(root, sub)) {
			if validID(e.Name()) && !live[e.Name()] {
				orphans = append(orphans, filepath.Join(root, sub, e.Name()))
			}
		}
	}

	// projects/<encoded>/<id>: subagent transcript dirs named by the parent
	// session's UUID. Orphan when that parent id has no live transcript.
	projDirs, _ := filepath.Glob(filepath.Join(root, "projects", "*"))
	for _, pd := range projDirs {
		// The glob also matches stray files (e.g. a macOS .DS_Store at the
		// projects/ root); only encoded-cwd directories are project dirs.
		if fi, err := os.Stat(pd); err != nil || !fi.IsDir() {
			continue
		}
		for _, e := range dirEntries(pd) {
			if e.IsDir() && validID(e.Name()) && !live[e.Name()] {
				orphans = append(orphans, filepath.Join(pd, e.Name()))
			}
		}
		// husk: a project dir holding no transcript and no live subagent dir.
		// A live session's subagent dir can sit under a different encoded cwd
		// than its own transcript, so a no-transcript dir is not necessarily
		// dead — reaping it as a husk would take that live state with it.
		if !hasTranscript(pd) && !hasLiveSubagentDir(pd, live) {
			orphans = append(orphans, pd)
		}
	}

	// tasks/session-<id[:8]>: keyed by the short id.
	for _, e := range dirEntries(filepath.Join(root, "tasks")) {
		name := e.Name()
		if s, ok := cutPrefix(name, "session-"); ok && !liveShort[s] {
			orphans = append(orphans, filepath.Join(root, "tasks", name))
		}
	}

	// sessions/<pid>.json: keyed by the inner sessionId field, not the filename.
	// An unreadable / id-less file is left alone (can't prove it's an orphan).
	sessFiles, _ := filepath.Glob(filepath.Join(root, "sessions", "*.json"))
	for _, p := range sessFiles {
		if id := sessionJSONID(p); id != "" && !live[id] {
			orphans = append(orphans, p)
		}
	}

	return orphans
}

// hasTranscript reports whether a project dir holds at least one *.jsonl.
func hasTranscript(projDir string) bool {
	for _, e := range dirEntries(projDir) {
		if !e.IsDir() && filepath.Ext(e.Name()) == ".jsonl" {
			return true
		}
	}
	return false
}

// hasLiveSubagentDir reports whether a project dir holds a subagent dir named
// for a live session id. Such a dir keeps a live session's state even though
// the parent transcript lives under a different encoded cwd, so the project
// dir is not a husk and must not be reaped.
func hasLiveSubagentDir(projDir string, live map[string]bool) bool {
	for _, e := range dirEntries(projDir) {
		if e.IsDir() && live[e.Name()] {
			return true
		}
	}
	return false
}

func dirEntries(dir string) []os.DirEntry {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	return entries
}

// cutPrefix is strings.CutPrefix (kept local to avoid widening imports here).
func cutPrefix(s, prefix string) (after string, found bool) {
	if len(s) >= len(prefix) && s[:len(prefix)] == prefix {
		return s[len(prefix):], true
	}
	return s, false
}

// sweepOrphans finds orphaned satellite state under ~/.claude. With apply set
// it removes each (confined to ~/.claude); otherwise it's a dry run that only
// reports what would go. A path is counted removed when it no longer exists
// after the attempt — robust against confinedRemover keeping only its first
// error, and against a parent husk's RemoveAll having already taken a child.
func sweepOrphans(apply bool) (SweepResult, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return SweepResult{}, err
	}
	root := filepath.Join(home, ".claude")
	orphans := findOrphans(root)

	res := SweepResult{Orphans: orphans}
	if !apply {
		return res, nil
	}
	rm := newConfinedRemover(root)
	for _, p := range orphans {
		rm.remove(p)
		if _, err := os.Lstat(p); os.IsNotExist(err) {
			res.Removed++
		}
	}
	res.Err = rm.err
	return res, nil
}
