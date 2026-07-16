package main

import (
	"os"
	"os/exec"
	"strings"
)

// annotateGit flags sessions whose project dir is gone (PathGone) or whose
// recorded git branch no longer exists in that repo (BranchGone). Both are
// advisory UI signals for spotting stale sessions.
//
// Work is deduped per unique project dir: os.Stat once for existence, and one
// `git for-each-ref` to list the repo's branches (rather than a subprocess per
// session). A dir that isn't a git repo, or a git that isn't on PATH, just
// leaves BranchGone false — absence of proof isn't proof of absence.
func annotateGit(sessions []Session) {
	gitOK := gitAvailable()
	dirExists := map[string]bool{}
	branchSets := map[string]map[string]bool{} // dir → set of local branch names

	for i := range sessions {
		dir := sessions[i].PathReal
		if dir == "" {
			continue
		}
		exists, seen := dirExists[dir]
		if !seen {
			fi, err := os.Stat(dir)
			exists = err == nil && fi.IsDir()
			dirExists[dir] = exists
		}
		if !exists {
			sessions[i].PathGone = true
			continue // a gone dir has no branches to check
		}
		if !gitOK || sessions[i].Branch == "" {
			continue
		}
		set, ok := branchSets[dir]
		if !ok {
			set = gitBranches(dir)
			branchSets[dir] = set
		}
		// set == nil means "not a git repo / couldn't list" — don't flag, since
		// we can't prove the branch is gone.
		if set != nil && !set[sessions[i].Branch] {
			sessions[i].BranchGone = true
		}
	}
}

func gitAvailable() bool {
	_, err := exec.LookPath("git")
	return err == nil
}

// gitBranches returns the set of local branch names in the repo at dir, or nil
// if dir isn't a git work tree or git failed.
func gitBranches(dir string) map[string]bool {
	cmd := exec.Command("git", "-C", dir, "for-each-ref", "--format=%(refname:short)", "refs/heads")
	out, err := cmd.Output()
	if err != nil {
		return nil
	}
	set := map[string]bool{}
	for line := range strings.SplitSeq(strings.TrimSpace(string(out)), "\n") {
		if line != "" {
			set[line] = true
		}
	}
	return set
}
