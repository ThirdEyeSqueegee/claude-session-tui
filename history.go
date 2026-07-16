package main

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
)

// historyFile is the global prompt-history log Claude Code appends to: one JSON
// object per line, each carrying the "sessionId" that produced the prompt.
// Unlike every other satellite it isn't a path per session — all sessions share
// this one file — so cleanup strips matching lines rather than removing a path.
const historyFile = "history.jsonl"

// rewriteHistory rewrites ~/.claude/history.jsonl keeping only the lines whose
// sessionId satisfies keep, and returns how many lines were dropped. A line
// whose sessionId can't be parsed is always kept: we can't prove it's orphaned
// (mirrors the sessions/*.json policy).
//
// With apply=false it only counts (no write). With apply=true and at least one
// line to drop, it writes the survivors to a temp file in the same dir and
// renames over the original, so a crash mid-write can't truncate history. A
// missing file is fine (0 dropped, nil error). A scan error aborts the rewrite
// untouched — we never write back from a partial read.
func rewriteHistory(root string, keep func(sessionID string) bool, apply bool) (dropped int, err error) {
	path := filepath.Join(root, historyFile)
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, err
	}
	defer f.Close()

	var kept [][]byte
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 1024*1024), 16*1024*1024)
	for sc.Scan() {
		line := sc.Bytes()
		if len(line) == 0 {
			continue
		}
		var rec struct {
			SessionID string `json:"sessionId"`
		}
		id := ""
		if json.Unmarshal(line, &rec) == nil {
			id = rec.SessionID
		}
		if keep(id) {
			// Scanner reuses its buffer across Scan calls, so copy the line.
			kept = append(kept, append([]byte(nil), line...))
			continue
		}
		dropped++
	}
	if err := sc.Err(); err != nil {
		return 0, err // partial read: leave history intact
	}
	if !apply || dropped == 0 {
		return dropped, nil
	}
	return dropped, writeHistoryLines(path, kept)
}

// writeHistoryLines atomically replaces path with the given lines (each newline
// terminated) via a same-dir temp file + rename, preserving the original mode.
func writeHistoryLines(path string, lines [][]byte) error {
	mode := os.FileMode(0o600) // history.jsonl is private; default to that
	if fi, err := os.Stat(path); err == nil {
		mode = fi.Mode().Perm()
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".history-*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	w := bufio.NewWriter(tmp)
	for _, line := range lines {
		// bufio.Writer errors are sticky; the Flush below surfaces them.
		_, _ = w.Write(line)
		_ = w.WriteByte('\n')
	}
	if err := w.Flush(); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return err
	}
	if err := os.Chmod(tmpName, mode); err != nil {
		os.Remove(tmpName)
		return err
	}
	if err := os.Rename(tmpName, path); err != nil {
		os.Remove(tmpName)
		return err
	}
	return nil
}
