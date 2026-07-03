package main

import (
	"bufio"
	"encoding/json"
	"github.com/charmbracelet/x/ansi"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// maxTextLen caps every jsonl-derived string we keep, in runes. The render and
// filter paths must never see a multi-KB blob (a pasted minified file or URL as
// a first message), which would stall the TUI. Previews only ever show a few
// hundred cells, so this is lossless for display.
const maxTextLen = 600

// idRe is a Claude conversation id: a UUID. We refuse to derive destructive
// paths from anything else (see deleteSession) — a jsonl literally named
// "...jsonl" yields a basename of ".." which would otherwise escape the
// session dir on delete.
var idRe = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

func validID(id string) bool { return idRe.MatchString(id) }

// shortIDRe matches the 8-hex-char short id form Claude Code uses to key
// tasks/session-<id[:8]>, jobs/<id[:8]>, and teams/session-<id[:8]>. It gates
// orphan matching so non-id siblings (e.g. jobs/.draft-<hex>, jobs/pins.json)
// are never treated as reapable short-id state.
var shortIDRe = regexp.MustCompile(`^[0-9a-fA-F]{8}$`)

func validShortID(id string) bool { return shortIDRe.MatchString(id) }

// Session is one Claude Code conversation, parsed from a single jsonl file.
type Session struct {
	ID        string // conversation_id (jsonl basename, sans extension); always a valid UUID
	Path      string // cwd, home-collapsed (~/...)
	PathReal  string // cwd, absolute
	Title     string // customTitle > aiTitle > first user message preview
	FirstMsg  string // first real user message text (capped, sanitized)
	LastMsg   string // last assistant reply (empty if the session has none)
	Msgs      int    // count of real user messages (no sidechain/meta)
	Branch    string // git branch at last activity
	Model     string // last model id seen
	Updated   time.Time
	JsonlPath string
	Haystack  string // precomputed lowercased Title+Path+FirstMsg for fuzzyMatch
	Truncated bool   // a line exceeded the scanner buffer; data may be incomplete
}

// clip bounds a string to maxTextLen runes (cheap byte fast-path first).
func clip(s string) string {
	if len(s) <= maxTextLen { // bytes >= runes, so this is always safe
		return s
	}
	r := []rune(s)
	if len(r) <= maxTextLen {
		return s
	}
	return string(r[:maxTextLen])
}

// sanitize strips ANSI escape sequences and control bytes from untrusted jsonl
// text so message content can't clear the screen, recolor, ring the bell, or
// retitle the terminal when rendered. ansi.Strip removes ESC-introduced
// sequences (CSI/OSC/charset); a second pass drops any remaining C0 control +
// DEL. Newlines and tabs are kept (callers wrap their own); C1 (U+0080–U+009F)
// is left intact as valid decoded Unicode.
func sanitize(s string) string {
	if s == "" {
		return s
	}
	s = ansi.Strip(s)
	// Fast path: if no rune trips the strip predicate, the string is already
	// clean. Sharing isStripped with the loop keeps the two in lockstep — a
	// hand-listed byte set silently leaks any control byte left off the list
	// (e.g. CR, which overwrites the current terminal line).
	if strings.IndexFunc(s, isStripped) < 0 {
		return s
	}
	var b strings.Builder
	b.Grow(len(s))
	for _, c := range s {
		if !isStripped(c) {
			b.WriteRune(c)
		}
	}
	return b.String()
}

// isStripped reports whether sanitize drops a rune: any C0 control byte except
// newline and tab, plus DEL. C1 (U+0080–U+009F) is kept as valid decoded
// Unicode; callers wrap their own newlines/tabs, so those two stay.
func isStripped(c rune) bool {
	if c == '\n' || c == '\t' {
		return false
	}
	return c < 0x20 || c == 0x7f
}

// jsonl record. Only the fields we read are declared; the rest are ignored.
type record struct {
	Type        string        `json:"type"`
	Cwd         string        `json:"cwd"`
	GitBranch   string        `json:"gitBranch"`
	IsSidechain bool          `json:"isSidechain"`
	IsMeta      bool          `json:"isMeta"`
	Timestamp   string        `json:"timestamp"`
	Message     *messageField `json:"message"`
	CustomTitle string        `json:"customTitle"`
	AiTitle     string        `json:"aiTitle"`
}

type messageField struct {
	Model   string          `json:"model"`
	Content json.RawMessage `json:"content"`
}

// contentText pulls plain text out of a message content field, which is either
// a bare string (user) or a list of typed blocks (assistant / structured user).
// All returned text is sanitized here — this is the single chokepoint every
// jsonl-derived message body flows through (parse and transcript render alike),
// so control bytes / ANSI escapes can never reach the terminal.
func contentText(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	if raw[0] == '"' {
		var s string
		if json.Unmarshal(raw, &s) == nil {
			return sanitize(s)
		}
		return ""
	}
	var blocks []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if json.Unmarshal(raw, &blocks) != nil {
		return ""
	}
	var parts []string
	for _, b := range blocks {
		if b.Type == "text" && b.Text != "" {
			parts = append(parts, b.Text)
		}
	}
	return sanitize(strings.Join(parts, " "))
}

// stripPrompt removes Claude Code command wrapper tags from a user message so
// the preview shows the human intent, not the slash-command plumbing.
func stripPrompt(s string) string {
	for _, tag := range []string{"command-message", "command-args", "local-command-caveat"} {
		open, close := "<"+tag+">", "</"+tag+">"
		for {
			i := strings.Index(s, open)
			if i < 0 {
				break
			}
			j := strings.Index(s, close)
			if j < 0 || j < i {
				break
			}
			s = s[:i] + s[j+len(close):]
		}
	}
	s = strings.ReplaceAll(s, "<command-name>", "")
	s = strings.ReplaceAll(s, "</command-name>", "")
	return strings.TrimSpace(s)
}

// collapseHome rewrites an absolute path under $HOME to a ~-prefixed one.
func collapseHome(p string) string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return p
	}
	if p == home {
		return "~"
	}
	if strings.HasPrefix(p, home+string(os.PathSeparator)) {
		return "~" + p[len(home):]
	}
	return p
}

// parseSession reads one jsonl file end-to-end and folds it into a Session.
// Returns ok=false for files with no cwd, no timestamp, or a malformed id
// (not real, safe-to-act-on sessions).
func parseSession(jsonlPath string) (Session, bool) {
	id := strings.TrimSuffix(filepath.Base(jsonlPath), ".jsonl")
	if !validID(id) {
		return Session{}, false // refuse ids we won't derive delete paths from
	}

	f, err := os.Open(jsonlPath)
	if err != nil {
		return Session{}, false
	}
	defer f.Close()

	s := Session{
		ID:        id,
		JsonlPath: jsonlPath,
	}
	var customTitle, aiTitle, lastTs string
	var firstUserSet bool

	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 1024*1024), 16*1024*1024)
	for sc.Scan() {
		line := sc.Bytes()
		if len(line) == 0 {
			continue
		}
		var r record
		if json.Unmarshal(line, &r) != nil {
			continue
		}
		if r.Cwd != "" && s.PathReal == "" {
			s.PathReal = r.Cwd
		}
		if r.GitBranch != "" {
			s.Branch = clip(sanitize(r.GitBranch))
		}
		if r.Timestamp != "" {
			lastTs = r.Timestamp
		}
		switch r.Type {
		case "user":
			if r.IsSidechain || r.IsMeta || r.Message == nil {
				continue
			}
			s.Msgs++
			if !firstUserSet {
				txt := stripPrompt(contentText(r.Message.Content)) // already sanitized
				if txt != "" {
					s.FirstMsg = clip(txt)
					firstUserSet = true
				}
			}
		case "assistant":
			if r.Message == nil {
				continue
			}
			if r.Message.Model != "" {
				s.Model = clip(sanitize(r.Message.Model))
			}
			if txt := contentText(r.Message.Content); txt != "" {
				s.LastMsg = clip(txt) // contentText already sanitized
			}
		case "custom-title":
			if r.CustomTitle != "" {
				customTitle = r.CustomTitle
			}
		case "ai-title":
			if r.AiTitle != "" {
				aiTitle = r.AiTitle
			}
		}
	}
	// A line longer than the 16MB buffer stops the scan early; flag it so the
	// UI can warn the session may be incomplete rather than silently lying.
	if sc.Err() != nil {
		s.Truncated = true
	}
	if s.PathReal == "" || lastTs == "" {
		return Session{}, false
	}
	if t, err := time.Parse(time.RFC3339, lastTs); err == nil {
		s.Updated = t
	} else if t, err := time.Parse(time.RFC3339Nano, lastTs); err == nil {
		s.Updated = t
	}
	// Path is display-only (headers, detail, title) and feeds Haystack, so it
	// must be sanitized — a cwd can legally hold ESC/control bytes. PathReal
	// stays raw: it's the chdir target and the scope-match key.
	s.Path = sanitize(collapseHome(s.PathReal))

	switch {
	case customTitle != "":
		s.Title = clip(sanitize(customTitle))
	case aiTitle != "":
		s.Title = clip(sanitize(aiTitle))
	case s.FirstMsg != "":
		s.Title = firstLine(s.FirstMsg)
	default:
		s.Title = "(untitled)"
	}
	s.Haystack = strings.ToLower(s.Title + " " + s.Path + " " + s.FirstMsg)
	return s, true
}

func firstLine(s string) string {
	if before, _, ok := strings.Cut(s, "\n"); ok {
		return strings.TrimSpace(before)
	}
	return s
}

// LoadResult is what a load produces: the parsed sessions plus a count of files
// that were skipped (unreadable / not a real session) so the UI can hint that
// the list may be incomplete rather than appearing to silently lose data.
type LoadResult struct {
	Sessions  []Session
	Skipped   int // jsonl files that failed to parse into a session
	Truncated int // sessions whose transcript hit the scanner buffer cap
}

// loadSessions globs every project jsonl and parses them in parallel.
func loadSessions() (LoadResult, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return LoadResult{}, err
	}
	root := filepath.Join(home, ".claude", "projects")
	paths, err := filepath.Glob(filepath.Join(root, "*", "*.jsonl"))
	if err != nil {
		return LoadResult{}, err
	}

	out := make([]Session, len(paths))
	ok := make([]bool, len(paths))

	workers := min(runtime.NumCPU(), 8)
	jobs := make(chan int)
	var wg sync.WaitGroup
	for range workers {
		wg.Go(func() {
			for i := range jobs {
				if s, good := parseSession(paths[i]); good {
					out[i] = s
					ok[i] = true
				}
			}
		})
	}
	for i := range paths {
		jobs <- i
	}
	close(jobs)
	wg.Wait()

	res := LoadResult{Sessions: make([]Session, 0, len(paths))}
	for i, good := range ok {
		if !good {
			res.Skipped++
			continue
		}
		res.Sessions = append(res.Sessions, out[i])
		if out[i].Truncated {
			res.Truncated++
		}
	}
	sort.Slice(res.Sessions, func(i, j int) bool {
		return res.Sessions[i].Updated.After(res.Sessions[j].Updated)
	})
	return res, nil
}

// agoString renders a duration since t as a compact relative label. Fresh and
// day-boundary cases get friendly words ("just now", "yesterday"); everything
// else stays a tidy unit so rows line up.
func agoString(t time.Time, now time.Time) string {
	d := now.Sub(t)
	switch {
	case d < 90*time.Second:
		return "just now"
	case d < time.Hour:
		return strconv.Itoa(int(d.Minutes())) + "m"
	case d < 24*time.Hour:
		return strconv.Itoa(int(d.Hours())) + "h"
	case d < 48*time.Hour:
		return "yesterday"
	case d < 7*24*time.Hour:
		return strconv.Itoa(int(d.Hours()/24)) + "d"
	default:
		return strconv.Itoa(int(d.Hours()/(24*7))) + "w"
	}
}
