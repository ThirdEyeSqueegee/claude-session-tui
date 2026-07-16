package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func parseFixture(t *testing.T, name string) (Session, bool) {
	t.Helper()
	return parseSession(filepath.Join("testdata", name+".jsonl"))
}

func TestParseSessionFixtures(t *testing.T) {
	const (
		u1 = "11111111-1111-1111-1111-111111111111"
		u2 = "22222222-2222-2222-2222-222222222222"
		u3 = "33333333-3333-3333-3333-333333333333"
		u4 = "44444444-4444-4444-4444-444444444444"
		u5 = "55555555-5555-5555-5555-555555555555"
	)

	t.Run("custom title wins, last-wins branch/model, msg count", func(t *testing.T) {
		s, ok := parseFixture(t, u1)
		if !ok {
			t.Fatal("expected ok")
		}
		if s.Title != "My Custom Title" {
			t.Errorf("Title = %q, want custom title", s.Title)
		}
		if s.Branch != "feature" { // last gitBranch wins
			t.Errorf("Branch = %q, want feature", s.Branch)
		}
		if s.Model != "claude-sonnet-4-6" { // last model wins
			t.Errorf("Model = %q, want last model", s.Model)
		}
		if s.LastMsg != "last answer" {
			t.Errorf("LastMsg = %q", s.LastMsg)
		}
		if s.Msgs != 2 {
			t.Errorf("Msgs = %d, want 2", s.Msgs)
		}
		if s.FirstMsg != "first question" {
			t.Errorf("FirstMsg = %q", s.FirstMsg)
		}
		if s.Haystack == "" {
			t.Error("Haystack not precomputed")
		}
	})

	t.Run("ai-title used when no custom", func(t *testing.T) {
		s, _ := parseFixture(t, u2)
		if s.Title != "Auto Named Session" {
			t.Errorf("Title = %q, want ai title", s.Title)
		}
	})

	t.Run("first-message title; sidechain+meta excluded", func(t *testing.T) {
		s, _ := parseFixture(t, u3)
		if s.Title != "the real first line" { // firstLine of FirstMsg
			t.Errorf("Title = %q", s.Title)
		}
		if s.Msgs != 2 { // 2 real users, sidechain+meta dropped
			t.Errorf("Msgs = %d, want 2", s.Msgs)
		}
	})

	t.Run("no assistant reply leaves LastMsg empty", func(t *testing.T) {
		s, ok := parseFixture(t, u4)
		if !ok {
			t.Fatal("expected ok")
		}
		if s.LastMsg != "" {
			t.Errorf("LastMsg = %q, want empty", s.LastMsg)
		}
		if s.Title != "lonely question" {
			t.Errorf("Title = %q", s.Title)
		}
	})

	t.Run("unparseable timestamp -> zero Updated but still parsed", func(t *testing.T) {
		s, ok := parseFixture(t, u5)
		if !ok {
			t.Fatal("expected ok (has cwd + a timestamp field)")
		}
		if !s.Updated.IsZero() {
			t.Errorf("Updated = %v, want zero for bad timestamp", s.Updated)
		}
	})
}

// TestParseSessionUsageAndSize proves per-turn usage is summed across assistant
// messages and the transcript's on-disk size is captured.
func TestParseSessionUsageAndSize(t *testing.T) {
	dir := t.TempDir()
	id := "33333333-3333-3333-3333-333333333333"
	p := filepath.Join(dir, id+".jsonl")
	body := `{"type":"user","cwd":"/x","timestamp":"2026-01-01T00:00:00Z","message":{"content":"hi"}}` + "\n" +
		`{"type":"assistant","timestamp":"2026-01-01T00:00:01Z","message":{"model":"claude-opus-4-8","content":[{"type":"text","text":"a"}],"usage":{"input_tokens":100,"output_tokens":10,"cache_read_input_tokens":50,"cache_creation_input_tokens":20}}}` + "\n" +
		`{"type":"assistant","timestamp":"2026-01-01T00:00:02Z","message":{"model":"claude-opus-4-8","content":[{"type":"text","text":"b"}],"usage":{"input_tokens":200,"output_tokens":30,"cache_read_input_tokens":5,"cache_creation_input_tokens":0}}}` + "\n"
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	s, ok := parseSession(p)
	if !ok {
		t.Fatal("expected ok")
	}
	if s.InTok != 300 || s.OutTok != 40 || s.CacheReadT != 55 || s.CacheWriteT != 20 {
		t.Errorf("usage sums = in%d out%d cr%d cw%d, want 300/40/55/20",
			s.InTok, s.OutTok, s.CacheReadT, s.CacheWriteT)
	}
	if s.Size != int64(len(body)) {
		t.Errorf("Size = %d, want %d (file length)", s.Size, len(body))
	}
}

// TestParseSessionSanitizesBranchModel proves gitBranch and model flow through
// sanitize like message bodies do — a branch or model carrying ANSI/control
// bytes (git allows ESC in ref names) must not reach the detail pane raw, where
// it could retitle the terminal or clear the screen. \\u001b / \\u0007 are
// literal JSON unicode escapes (decode to ESC / BEL), as a real transcript
// stores them; raw control bytes are not legal inside a JSON string.
func TestParseSessionSanitizesBranchModel(t *testing.T) {
	dir := t.TempDir()
	id := "11111111-1111-1111-1111-111111111111"
	p := filepath.Join(dir, id+".jsonl")
	body := `{"type":"user","cwd":"/x","gitBranch":"main\u001b]0;PWNED\u0007","timestamp":"2026-01-01T00:00:00Z","message":{"content":"hi"}}` + "\n" +
		`{"type":"assistant","timestamp":"2026-01-01T00:00:01Z","message":{"model":"claude\u001b[2J-evil","content":[{"type":"text","text":"ok"}]}}` + "\n"
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	s, ok := parseSession(p)
	if !ok {
		t.Fatal("expected ok")
	}
	if strings.ContainsAny(s.Branch, "\x1b\x07") {
		t.Errorf("Branch not sanitized: %q", s.Branch)
	}
	if strings.Contains(s.Model, "\x1b") {
		t.Errorf("Model not sanitized: %q", s.Model)
	}
}

// TestParseSessionSanitizesPath proves the display Path (and the Haystack built
// from it) are sanitized: a cwd may legally contain ESC/control bytes, but Path
// renders in headers, detail, and the title bar. PathReal stays raw — it is
// the chdir target and scope key, never rendered.
func TestParseSessionSanitizesPath(t *testing.T) {
	dir := t.TempDir()
	id := "22222222-2222-2222-2222-222222222222"
	p := filepath.Join(dir, id+".jsonl")
	body := `{"type":"user","cwd":"/x\u001b]0;PWNED\u0007/y","timestamp":"2026-01-01T00:00:00Z","message":{"content":"hi"}}` + "\n"
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	s, ok := parseSession(p)
	if !ok {
		t.Fatal("expected ok")
	}
	if strings.ContainsAny(s.Path, "\x1b\x07") {
		t.Errorf("display Path not sanitized: %q", s.Path)
	}
	if strings.ContainsAny(s.Haystack, "\x1b\x07") {
		t.Errorf("Haystack not sanitized: %q", s.Haystack)
	}
	if s.PathReal == "" || !strings.Contains(s.PathReal, "\x1b") {
		t.Errorf("PathReal should stay raw for chdir/scope, got %q", s.PathReal)
	}
}
