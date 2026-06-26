package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestLaunchForkWait verifies the launch path runs the configured command with
// the right argv and chdir's into the session's project dir. Uses a fake
// `claude` on PATH that records its argv + cwd.
func TestLaunchForkWait(t *testing.T) {
	fake := t.TempDir()
	out := filepath.Join(fake, "out.txt")
	script := "#!/bin/sh\necho \"$@ pwd=$(pwd)\" > " + out + "\n"
	if err := os.WriteFile(filepath.Join(fake, "claude"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", fake+":"+os.Getenv("PATH"))

	proj := t.TempDir()
	s := &Session{ID: "11111111-1111-1111-1111-111111111111", PathReal: proj}
	cfg := defaultConfig()
	off := false
	cfg.TabColor.Enabled = &off // no kitty in tests

	if code := launchClaude(s, cfg); code != 0 {
		t.Fatalf("exit code = %d", code)
	}
	b, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("fake claude did not run: %v", err)
	}
	got := string(b)
	// default config passes no extra flags — just `--resume <id>`
	if !strings.HasPrefix(got, "--resume "+s.ID+" pwd=") {
		t.Errorf("wrong argv (want bare --resume): %s", got)
	}
	if !strings.Contains(got, "pwd="+proj) {
		t.Errorf("did not chdir to project dir %s: %s", proj, got)
	}
}

func TestResumeLabel(t *testing.T) {
	cases := []struct {
		s    Session
		want string
	}{
		{Session{ID: "11111111-1111-1111-1111-111111111111", Title: "Fix auth"}, "Fix auth"},
		{Session{ID: "22222222-2222-2222-2222-222222222222", Title: "first\nsecond"}, "first"},
		{Session{ID: "33333333-3333-3333-3333-333333333333", Title: "(untitled)"}, "33333333"},
		{Session{ID: "44444444-4444-4444-4444-444444444444", Title: ""}, "44444444"},
	}
	for _, c := range cases {
		if got := resumeLabel(&c.s); got != c.want {
			t.Errorf("resumeLabel(%q/%q) = %q, want %q", c.s.ID, c.s.Title, got, c.want)
		}
	}
}

// TestLaunchCustomCommand verifies the resume command + args are configurable.
func TestLaunchCustomCommand(t *testing.T) {
	fake := t.TempDir()
	out := filepath.Join(fake, "out.txt")
	if err := os.WriteFile(filepath.Join(fake, "myclaude"),
		[]byte("#!/bin/sh\necho \"$@\" > "+out+"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", fake+":"+os.Getenv("PATH"))

	s := &Session{ID: "22222222-2222-2222-2222-222222222222"}
	cfg := defaultConfig()
	off := false
	cfg.TabColor.Enabled = &off
	cfg.Resume.Command = "myclaude"
	cfg.Resume.Args = []string{"--safe"}
	cfg.Resume.ResumeFlag = "-r"

	if code := launchClaude(s, cfg); code != 0 {
		t.Fatalf("exit = %d", code)
	}
	b, _ := os.ReadFile(out)
	got := strings.TrimSpace(string(b))
	if got != "--safe -r "+s.ID {
		t.Errorf("custom argv = %q, want %q", got, "--safe -r "+s.ID)
	}
}
