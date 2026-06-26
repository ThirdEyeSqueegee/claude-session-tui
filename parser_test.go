package main

import (
	"path/filepath"
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
