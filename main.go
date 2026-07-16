package main

import (
	"bufio"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/colorprofile"
	"github.com/pelletier/go-toml/v2"
)

// printResolvedConfig dumps the effective config (defaults merged with the
// file) as TOML, plus any validation warnings, for debugging knobs.
func printResolvedConfig(cfg Config, warns []string) {
	out, err := toml.Marshal(cfg)
	if err != nil {
		fmt.Fprintln(os.Stderr, "could not marshal config:", err)
		os.Exit(1)
	}
	fmt.Print(string(out))
	for _, w := range warns {
		fmt.Fprintln(os.Stderr, "warning:", w)
	}
}

// version is stamped at build time via -ldflags "-X main.version=...".
var version = "dev"

// claude-session-tui: a two-pane picker for Claude Code sessions.
//
// By default, picking a session launches it. cst stays alive as the parent of
// the `claude` process (it runs it and waits) rather than exec-replacing
// itself, so it can tint the kitty tab before the session and restore it after.
// It chdir's into the session's project dir first — Claude keys its session
// store off the encoded cwd, so resume only finds the conversation there.
//
// Opt-outs for callers that want to own the launch:
//
//	--output, -o <file>  write the chosen id to a file and exit (yazi --cwd-file trick)
//	--print,  -p         print the chosen id to stdout and exit
func main() {
	// `prune` (alias `p`) is a subcommand, not a flag: sweep orphaned session
	// state and exit before the TUI. It must be the first argument.
	if len(os.Args) > 1 && (os.Args[1] == "prune" || os.Args[1] == "p") {
		os.Exit(runPrune(os.Args[2:]))
	}

	var (
		outFile  string
		printID  bool
		configF  string
		printCfg bool
		showVer  bool
	)
	// Each flag has a long and a short name bound to the same variable.
	flag.StringVar(&outFile, "output", "", "write chosen conversation_id to this file and exit (don't launch)")
	flag.StringVar(&outFile, "o", "", "shorthand for --output")
	flag.BoolVar(&printID, "print", false, "print chosen conversation_id to stdout and exit (don't launch)")
	flag.BoolVar(&printID, "p", false, "shorthand for --print")
	flag.StringVar(&configF, "config", "", "path to config TOML (default ~/.config/cst/config.toml)")
	flag.StringVar(&configF, "c", "", "shorthand for --config")
	flag.BoolVar(&printCfg, "print-config", false, "print the resolved effective config and exit")
	flag.BoolVar(&printCfg, "C", false, "shorthand for --print-config")
	flag.BoolVar(&showVer, "version", false, "print version and exit")
	flag.BoolVar(&showVer, "v", false, "shorthand for --version")
	flag.Usage = usage
	flag.Parse()

	if showVer {
		fmt.Println("cst", version)
		return
	}

	cfg, cfgErr := loadConfig(configF)
	if cfgErr != nil {
		fmt.Fprintln(os.Stderr, "warning: config:", cfgErr, "(using defaults)")
	}
	warns := validateConfig(cfg)

	if printCfg {
		printResolvedConfig(cfg, warns)
		return
	}

	cwd, _ := os.Getwd()
	final, err := tea.NewProgram(initialModel(time.Now(), cfg, warns, cwd)).Run()
	if err != nil {
		fmt.Fprintln(os.Stderr, "tui error:", err)
		os.Exit(1)
	}

	fm, ok := final.(model)
	// Release the fs watcher before we hand the terminal to claude — cst stays
	// alive as claude's parent for the whole session, so a leaked watcher FD and
	// its blocked read goroutine would linger that entire time.
	if ok && fm.fsw != nil {
		fm.fsw.Close()
	}
	if !ok || fm.chosen == nil {
		// quit without choosing → exit 130 so any wrapper knows not to resume
		os.Exit(130)
	}
	chosen := fm.chosen

	if outFile != "" {
		if err := os.WriteFile(outFile, []byte(chosen.ID+"\n"), 0o644); err != nil {
			fmt.Fprintln(os.Stderr, "could not write out file:", err)
			os.Exit(1)
		}
		return
	}
	if printID {
		fmt.Println(chosen.ID)
		return
	}

	os.Exit(launchClaude(chosen, cfg))
}

// runPrune performs the orphan sweep behind an interactive confirm and prints a
// themed report. It first scans (dry) and lists what it found, then asks before
// removing anything. Returns a process exit code. Mirrors the `ccp` helper.
// Output is themed like usage() — colors auto-strip for pipes / NO_COLOR.
func runPrune(args []string) int {
	if len(args) > 0 && (args[0] == "-h" || args[0] == "--help") {
		usage()
		return 0
	}

	w := colorprofile.NewWriter(os.Stdout, os.Environ())
	title := styLogo.Render(logoMark) + " " + styTitleBar.Render("cst prune") +
		styDetailDim.Render(" — sweep orphaned session state under ~/.claude")
	fmt.Fprintf(w, "\n  %s\n", title)

	res, err := sweepOrphans(false)
	if err != nil {
		fmt.Fprintln(os.Stderr, "prune failed:", err)
		return 1
	}
	if len(res.Orphans) == 0 && res.HistoryLines == 0 {
		fmt.Fprintf(w, "\n  %s\n\n", styDetailDim.Render("no orphans found — nothing to sweep"))
		return 0
	}

	if len(res.Orphans) > 0 {
		fmt.Fprintf(w, "\n  %s\n", styDetailLbl.Render(fmt.Sprintf("%d orphan(s)", len(res.Orphans))))
		for _, p := range res.Orphans {
			fmt.Fprintf(w, "    %s %s\n", styDanger.Render("✗"), styDetailDim.Render(collapseHome(p)))
		}
	}
	if res.HistoryLines > 0 {
		fmt.Fprintf(w, "\n  %s\n", styDetailLbl.Render(fmt.Sprintf("%d orphaned history line(s)", res.HistoryLines)))
		fmt.Fprintf(w, "    %s %s\n", styDanger.Render("✗"), styDetailDim.Render("~/.claude/"+historyFile))
	}

	fmt.Fprintf(w, "\n  %s ", styDanger.Render(promptSummary(res)))
	if !confirm(os.Stdin) {
		fmt.Fprintf(w, "\n  %s\n\n", styDetailDim.Render("aborted — nothing removed"))
		return 0
	}

	res, err = sweepOrphans(true)
	if err != nil {
		fmt.Fprintln(os.Stderr, "prune failed:", err)
		return 1
	}
	fmt.Fprintf(w, "\n  %s\n\n", styCount.Render(removedSummary(res)))
	if res.Err != nil {
		fmt.Fprintln(os.Stderr, "some removals failed:", res.Err)
		return 1
	}
	return 0
}

// promptSummary is the confirm-line text: names orphan paths and orphaned
// history lines together so the y/N covers everything the sweep will touch.
func promptSummary(res SweepResult) string {
	switch {
	case len(res.Orphans) > 0 && res.HistoryLines > 0:
		return fmt.Sprintf("remove %d orphan(s) + %d history line(s)? y / N", len(res.Orphans), res.HistoryLines)
	case res.HistoryLines > 0:
		return fmt.Sprintf("strip %d orphaned history line(s)? y / N", res.HistoryLines)
	default:
		return fmt.Sprintf("remove %d orphan(s)? y / N", len(res.Orphans))
	}
}

// removedSummary is the post-apply report, mirroring promptSummary.
func removedSummary(res SweepResult) string {
	switch {
	case res.Removed > 0 && res.HistoryLines > 0:
		return fmt.Sprintf("removed %d orphan(s) + %d history line(s)", res.Removed, res.HistoryLines)
	case res.HistoryLines > 0:
		return fmt.Sprintf("stripped %d orphaned history line(s)", res.HistoryLines)
	default:
		return fmt.Sprintf("removed %d orphan(s)", res.Removed)
	}
}

// confirm reads one line and reports whether it's an affirmative (y / yes,
// case-insensitive). Anything else — including a bare enter or EOF — is a no,
// so the destructive path is never taken by default.
func confirm(r io.Reader) bool {
	line, _ := bufio.NewReader(r).ReadString('\n')
	switch strings.ToLower(strings.TrimSpace(line)) {
	case "y", "yes":
		return true
	default:
		return false
	}
}

// launchClaude runs the configured resume command for the chosen session,
// inheriting this terminal, and waits for it. It tints the kitty tab around the
// session and restores it after. Returns the child's exit code (or a non-zero
// code on a launch failure, after printing the id so the pick is never lost).
func launchClaude(s *Session, cfg Config) int {
	bin, err := exec.LookPath(cfg.Resume.Command)
	if err != nil {
		fmt.Fprintln(os.Stderr, cfg.Resume.Command+" not found on PATH; resume manually:")
		fmt.Println(s.ID)
		return 1
	}

	argv := append([]string{}, cfg.Resume.Args...)
	argv = append(argv, cfg.Resume.ResumeFlag, s.ID)
	cmd := exec.Command(bin, argv...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	if boolOr(cfg.Resume.Chdir, true) && s.PathReal != "" {
		if fi, err := os.Stat(s.PathReal); err == nil && fi.IsDir() {
			cmd.Dir = s.PathReal
		}
	}

	tabs := newTabColorer(cfg.TabColor, s.PathReal)
	tabs.set()
	defer tabs.reset()

	// Bridge the gap between the TUI closing and Claude drawing its own UI.
	// Colored through a colorprofile writer so it auto-strips for pipes / NO_COLOR.
	loading := styDetailDim.Render("Loading session ") +
		styTitleBar.Render("“"+resumeLabel(s)+"”") +
		styDetailDim.Render("…")
	lw := colorprofile.NewWriter(os.Stderr, os.Environ())
	fmt.Fprintln(lw, loading)

	if err := cmd.Run(); err != nil {
		// A non-zero child exit arrives here too; surface its code if we can.
		if ee, ok := err.(*exec.ExitError); ok {
			return ee.ExitCode()
		}
		fmt.Fprintln(os.Stderr, "could not launch "+cfg.Resume.Command+":", err)
		fmt.Println(s.ID)
		return 1
	}
	return 0
}

// usage prints a themed, hand-laid help screen. Flags are shown with the
// double-dash spelling (Go's flag package accepts both -x and --x). Output goes
// through a colorprofile writer so colors auto-downsample (or strip) for pipes,
// dumb terminals, and NO_COLOR.
func usage() {
	w := colorprofile.NewWriter(flag.CommandLine.Output(), os.Environ())
	var b strings.Builder

	title := styLogo.Render(logoMark) + " " + styTitleBar.Render("cst") +
		styDetailDim.Render(" — Claude session picker")
	fmt.Fprintf(&b, "\n  %s\n", title)
	fmt.Fprintf(&b, "%s\n", styDetailDim.Render("  a two-pane TUI for browsing and resuming Claude Code sessions"))

	section := func(s string) { fmt.Fprintf(&b, "\n  %s\n", styDetailLbl.Render(s)) }
	row := func(left, right string) {
		fmt.Fprintf(&b, "    %s  %s\n", styTitleBar.Render(padRight(left, 22)), styDetailDim.Render(right))
	}

	section("usage")
	fmt.Fprintf(&b, "    %s\n", styCount.Render("cst [flags]"))
	fmt.Fprintf(&b, "    %s\n", styCount.Render("cst prune"))

	section("commands")
	row("p, prune", "sweep orphaned session state (asks before removing)")

	section("flags")
	row("-p, --print", "print the chosen session id to stdout and exit")
	row("-o, --output <file>", "write the chosen id to <file> and exit")
	row("-c, --config <path>", "use a specific config TOML")
	row("-C, --print-config", "print the resolved effective config and exit")
	row("-v, --version", "print the build version and exit")
	row("-h, --help", "show this help")

	section("keys")
	row("↵", "resume the selected session")
	row("j / k, ↑ / ↓", "move (skips group headers)")
	row("/", "fuzzy filter")
	row("s", "cycle sort: recency / project / msgs / size")
	row("S", "cycle group: project / date / branch")
	row("y / Y", "copy session id / project path to clipboard")
	row("p", "preview the transcript (/ to search, n / N to step)")
	row("d", "delete the session")
	row("q / esc", "quit")

	section("config")
	fmt.Fprintf(&b, "%s\n\n", styDetailDim.Render("    ~/.config/cst/config.toml — see config.example.toml"))

	_, _ = w.WriteString(b.String())
}

// padRight pads s with spaces to at least n display cells.
func padRight(s string, n int) string {
	if d := n - displayWidth(s); d > 0 {
		return s + strings.Repeat(" ", d)
	}
	return s
}

// resumeLabel is what we name the session in the "Loading session …" line:
// its title when it has a real one, otherwise the short id.
func resumeLabel(s *Session) string {
	t := firstLine(s.Title)
	if t == "" || t == "(untitled)" {
		if len(s.ID) >= 8 {
			return s.ID[:8]
		}
		return s.ID
	}
	return t
}
