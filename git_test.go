package main

import (
	"os/exec"
	"testing"
)

// TestAnnotateGitPathGone flags a session whose project dir doesn't exist and
// leaves an existing dir alone.
func TestAnnotateGitPathGone(t *testing.T) {
	real := t.TempDir()
	ss := []Session{
		{PathReal: real},
		{PathReal: "/no/such/dir/really-gone"},
	}
	annotateGit(ss)
	if ss[0].PathGone {
		t.Error("existing dir marked PathGone")
	}
	if !ss[1].PathGone {
		t.Error("missing dir not marked PathGone")
	}
}

// TestAnnotateGitBranchGone builds a real repo, then checks a recorded branch
// that exists is kept and one that doesn't is flagged.
func TestAnnotateGitBranchGone(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not on PATH")
	}
	dir := t.TempDir()
	run := func(args ...string) {
		cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run("init", "-q", "-b", "main")
	run("config", "user.email", "t@example.com")
	run("config", "user.name", "t")
	run("commit", "-q", "--allow-empty", "-m", "init")

	ss := []Session{
		{PathReal: dir, Branch: "main"},         // exists
		{PathReal: dir, Branch: "feature-gone"}, // never created
		{PathReal: dir, Branch: ""},             // no branch recorded — never flagged
	}
	annotateGit(ss)
	if ss[0].BranchGone {
		t.Error("live branch 'main' wrongly marked gone")
	}
	if !ss[1].BranchGone {
		t.Error("missing branch 'feature-gone' not marked")
	}
	if ss[2].BranchGone {
		t.Error("empty-branch session marked gone")
	}
}

// TestAnnotateGitNonRepoLeavesBranch checks a real dir that isn't a git repo
// doesn't get BranchGone (can't prove the branch is gone).
func TestAnnotateGitNonRepoLeavesBranch(t *testing.T) {
	dir := t.TempDir()
	ss := []Session{{PathReal: dir, Branch: "main"}}
	annotateGit(ss)
	if ss[0].PathGone {
		t.Error("existing non-repo dir marked PathGone")
	}
	if ss[0].BranchGone {
		t.Error("non-repo dir wrongly marked BranchGone")
	}
}
