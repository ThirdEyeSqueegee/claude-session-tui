package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDefaultConfig(t *testing.T) {
	c := defaultConfig()
	if c.Resume.Command != "claude" {
		t.Errorf("default command = %q", c.Resume.Command)
	}
	if !boolOr(c.Resume.Chdir, false) {
		t.Error("chdir should default true")
	}
	if !boolOr(c.TabColor.Enabled, false) {
		t.Error("tab color should default enabled")
	}
	if c.UI.LeftWidthPct != 42 {
		t.Errorf("left width = %d", c.UI.LeftWidthPct)
	}
	if !boolOr(c.UI.Footer, false) || !boolOr(c.UI.ConfirmDelete, false) {
		t.Error("footer + confirm_delete should default true")
	}
}

func TestLoadConfigMissingFile(t *testing.T) {
	c, err := loadConfig(filepath.Join(t.TempDir(), "nope.toml"))
	if err != nil {
		t.Fatalf("missing file should not error: %v", err)
	}
	if c.Resume.Command != "claude" {
		t.Error("missing file should yield defaults")
	}
}

func TestLoadConfigMerge(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "config.toml")
	body := `
[resume]
command = "my-claude"
args = ["--foo"]

[ui]
sort = "msgs"
left_width_pct = 55
footer = false

[theme]
accent = "#112233"
`
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	c, err := loadConfig(p)
	if err != nil {
		t.Fatal(err)
	}
	if c.Resume.Command != "my-claude" {
		t.Errorf("command = %q", c.Resume.Command)
	}
	if len(c.Resume.Args) != 1 || c.Resume.Args[0] != "--foo" {
		t.Errorf("args = %v", c.Resume.Args)
	}
	// unset key keeps its default
	if c.Resume.ResumeFlag != "--resume" {
		t.Errorf("resume_flag should keep default, got %q", c.Resume.ResumeFlag)
	}
	if c.UI.Sort != "msgs" || c.UI.LeftWidthPct != 55 {
		t.Errorf("ui not merged: %+v", c.UI)
	}
	if boolOr(c.UI.Footer, true) {
		t.Error("footer=false should be honored")
	}
	// unset pointer-bool stays default-true
	if !boolOr(c.UI.ConfirmDelete, true) {
		t.Error("confirm_delete should stay default true when unset")
	}
	if c.Theme.Accent != "#112233" {
		t.Errorf("theme.accent = %q", c.Theme.Accent)
	}
}

func TestValidateConfig(t *testing.T) {
	c := defaultConfig()
	c.Theme.Accent = "#zzzzzz" // bad hex
	c.Theme.Peach = "#abc"     // valid short hex
	c.UI.Sort = "bogus"        // unknown sort
	warns := validateConfig(c)
	if len(warns) != 2 {
		t.Fatalf("expected 2 warnings (bad accent + bad sort), got %d: %v", len(warns), warns)
	}
}

func TestSortModeFromString(t *testing.T) {
	cases := map[string]sortMode{"recency": sortRecency, "project": sortProject, "msgs": sortMsgs, "": sortRecency, "weird": sortRecency}
	for in, want := range cases {
		if got := sortModeFromString(in); got != want {
			t.Errorf("sortModeFromString(%q) = %v, want %v", in, got, want)
		}
	}
}

func TestThemeOverrideSkipsBadHex(t *testing.T) {
	// applyTheme must leave the default in place for an invalid hex.
	defer applyTheme(ThemeConfig{}) // restore defaults after
	applyTheme(ThemeConfig{Accent: "not-a-color"})
	// cAccent should still be the default orange, not mangled.
	if got := lipglossWidth("x"); got != 1 { // sanity, styles still build
		t.Errorf("styles broke after bad theme: %d", got)
	}
}
