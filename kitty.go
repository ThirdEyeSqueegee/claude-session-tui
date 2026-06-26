package main

import (
	"hash/fnv"
	"os"
	"os/exec"

	"github.com/lucasb-eyer/go-colorful"
)

// Kitty tab coloring.
//
// When cst runs inside kitty and launches a Claude session, it tints the
// terminal tab (`kitten @ set-tab-color --self ...`) and restores it when the
// session exits. Restoring requires cst to outlive the child, so the launch
// path waits on the child (see runClaude).
//
// Everything here degrades silently: not kitty, no `kitten` on PATH, or remote
// control disabled → the calls no-op and the picker still works.

// inKitty reports whether we're running inside a kitty terminal.
func inKitty() bool {
	return os.Getenv("KITTY_WINDOW_ID") != "" || os.Getenv("TERM") == "xterm-kitty"
}

// kittenPath finds the `kitten` binary, or "" if unavailable.
func kittenPath() string {
	if p, err := exec.LookPath("kitten"); err == nil {
		return p
	}
	// kitty ships kitten next to itself; the MacOS bundle path is common.
	for _, p := range []string{"/Applications/kitty.app/Contents/MacOS/kitten"} {
		if fi, err := os.Stat(p); err == nil && !fi.IsDir() {
			return p
		}
	}
	return ""
}

// tabColorer applies and restores kitty tab colors for a single launch. A nil
// or disabled colorer is safe to call — methods no-op.
type tabColorer struct {
	kitten   string // path to kitten, "" if unavailable/disabled
	active   string // active_bg color
	inact    string // inactive_bg color
	activeFg string // active_fg color
}

// newTabColorer builds a colorer from config for a given project path. Returns
// a no-op colorer when tab coloring is off, we're not in kitty, or kitten can't
// be found. When cfg.PerProject is set, the active color is a stable hue
// derived from projectPath so each repo gets a distinct, scannable tab.
func newTabColorer(cfg TabColorConfig, projectPath string) tabColorer {
	if !boolOr(cfg.Enabled, true) || !inKitty() {
		return tabColorer{}
	}
	fg := cfg.ActiveFg
	if fg == "" {
		fg = "#000000"
	}
	active, inact := cfg.Active, cfg.Inactive
	if cfg.PerProject && projectPath != "" {
		active, inact = projectHue(projectPath)
	}
	return tabColorer{kitten: kittenPath(), active: active, inact: inact, activeFg: fg}
}

// projectHue maps a path to a stable (active, inactive) hex pair. The hue is
// warm-banded (avoids cold blues/greens) to stay on-theme; the inactive shade
// is the same hue, darker. Deterministic: same path → same colors every run.
func projectHue(path string) (active, inactive string) {
	h := fnv.New32a()
	_, _ = h.Write([]byte(path))
	// Warm band: hues 20°–55° (orange/amber/gold) wrapping a little into pink.
	// Map the hash onto a 0–360 wheel but bias toward the warm arc.
	deg := float64(h.Sum32()%360) / 360.0
	hue := 18.0 + 320.0*warmBias(deg) // mostly 18°–55°, occasional rosy excursion
	return colorful.Hsl(hue, 0.55, 0.55).Hex(), colorful.Hsl(hue, 0.40, 0.32).Hex()
}

// warmBias squashes a uniform [0,1) toward the low end so most hues land in the
// warm arc, with a long thin tail for variety.
func warmBias(x float64) float64 { return x * x * 0.12 }

func (t tabColorer) enabled() bool { return t.kitten != "" }

// set tints the current tab. No-op if disabled.
func (t tabColorer) set() {
	if !t.enabled() {
		return
	}
	args := []string{"@", "set-tab-color", "--self"}
	if t.active != "" {
		args = append(args, "active_fg="+t.activeFg, "active_bg="+t.active)
	}
	if t.inact != "" {
		args = append(args, "inactive_bg="+t.inact)
	}
	t.run(args)
}

// reset clears the tab color overrides. No-op if disabled.
func (t tabColorer) reset() {
	if !t.enabled() {
		return
	}
	t.run([]string{"@", "set-tab-color", "--self", "active_fg=NONE", "active_bg=NONE", "inactive_bg=NONE"})
}

func (t tabColorer) run(args []string) {
	cmd := exec.Command(t.kitten, args...)
	// kitten @ talks to kitty over the controlling tty; give it ours and
	// swallow output. Failure (remote control off, etc.) is non-fatal.
	cmd.Stdin = os.Stdin
	cmd.Stdout = nil
	cmd.Stderr = nil
	_ = cmd.Run()
}
