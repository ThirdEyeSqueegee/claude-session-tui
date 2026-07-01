# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## What this is

`cst` is a two-pane terminal UI for browsing and resuming Claude Code sessions. It reads every `*.jsonl` transcript under `~/.claude/projects`, presents them grouped by project, and on `↵` `chdir`s into the session's project dir and launches `claude --resume <id>`.

## Commands

```sh
make build   # version-stamped binary (-X main.version from git describe)
make install # build + install to ~/.local/bin
make test    # go test ./...
make race    # go test -race ./...   (CI also adds -shuffle=on)
make vet     # go vet ./...
make fmt     # gofmt -w .
```

Run a single test (one package, all files are `package main`):

```sh
go test -run TestValidID .
```

CI gates every push/PR and all must pass: `gofmt -l` must be empty, `go vet`, `go build`, `go test -race -shuffle=on` (macOS + Linux), `golangci-lint` v2 (config in `.golangci.yml`, top-level `version: "2"`), `govulncheck`, `actionlint`. Always run `make fmt vet race` before a PR.

## Dependencies and gotchas

- TUI built on Bubble Tea v2. Import paths are `charm.land/bubbletea/v2`, `charm.land/bubbles/v2`, `charm.land/lipgloss/v2` — **not** the old `github.com/charmbracelet/*` paths. Don't "fix" these.
- Requires Go 1.26+. `go.mod` is the single source of truth for the version; CI installs exactly that. Module mode is `readonly` in lint — don't expect CI to mutate `go.mod`/`go.sum`.

## Architecture

Single flat `package main`. Data flows: parse transcripts → build model → run Bubble Tea loop → on quit, the chosen session drives the launch.

- **`main.go`** — flag parsing, config load, runs the `tea.Program`, then acts on the result. `cst` does **not** exec-replace itself with `claude`; it runs `claude` as a child and _waits_, so it can tint the kitty tab before and restore after (see kitty.go). `--print`/`--output` let a wrapper own the launch instead. The `prune` subcommand (alias `p`) runs the orphan sweep and exits before the TUI: it lists what it found, then asks `y / N` before removing anything. Exit `130` = quit without choosing.
- **`session.go`** — `parseSession` folds one jsonl into a `Session` (title, project path, msg count, branch, model, first/last message). `loadSessions` globs and parses all transcripts across a worker pool (`min(NumCPU, 8)`), off the UI thread. Title precedence: customTitle > aiTitle > first user message.
- **`config.go`** — TOML config (resolved: `-config` flag → `$CST_CONFIG` → `$XDG_CONFIG_HOME/cst/config.toml` → `~/.config/cst/config.toml`). Merges over defaults; a missing file is fine, a bad value keeps the default and surfaces a warning. Unmarshals into a **zero** Config (not pre-seeded) so an absent pointer-bool stays nil and is distinguishable from an explicit `false` — `mergeDefaults` then fills nils, `boolOr` resolves them at use. Schema mirrored in `config.example.toml`.
- **`ui.go`** — the `model` and Bubble Tea state. The left pane is a flat `[]row` of `rowHeader`/`rowSession`; cursor movement skips headers. `rebuild()` is the single pipeline: filter (AND-substring over a precomputed lowercased `Haystack`) → sort (`recency`/`project`/`msgs`) → group by project path. Project scope (`.`) filters to sessions whose symlink-resolved cwd matches the launch dir.
- **`view.go`** — the `Update`/`View` halves. Four modes: `modeList`, `modeFilter`, `modeTranscript`, `modeConfirmDelete`. Two-pane layout collapses to a single full-width list below `minTwoPaneWidth`. Delete acts on the marked set, or the cursor row when nothing is marked; a row leaves the list only when its on-disk delete actually succeeds.
- **`text.go`** — width-aware (grapheme/ANSI) `truncate`/`wrapClip`/`fitLines`, and `renderTranscript` for the pager.
- **`kitty.go`** — kitty tab coloring via `kitten @ set-tab-color`. Degrades to a no-op outside kitty or when `kitten` is absent. `per_project` derives a stable warm hue from the project path.
- **`cleanup.go`** — `deleteSession` removes a transcript plus its satellite state: `session-env/<id>`, `file-history/<id>`, subagent dirs `projects/*/<id>`, `tasks/session-<id[:8]>`, the `sessions/<pid>.json` matched on its inner `sessionId` field (not the pid filename), `paste-cache`/`tasks`/`todos` matches, and the enclosing `projects/<encoded-cwd>` dir when it goes empty. Mirrors the `cce` nushell helper. Note: `tasks` and `sessions` are keyed differently from everything else — `tasks` by the 8-char short id, `sessions` by an id read from inside the file — so don't assume full-UUID-basename keying. The `tasks/session-<id[:8]>` removal is gated by `otherLiveShares`: the short id is only 32 bits, so it's skipped when another live transcript shares the prefix, to avoid reaping a live session's task state.
- **`orphans.go`** — the inverse of `deleteSession`: `findOrphans`/`sweepOrphans` scan the satellite dirs for state whose id has no live transcript (the live set = basenames of `projects/*/*.jsonl`) and reap it. Drives the `prune` subcommand, which lists orphans then confirms before removing. All removals route through `confinedRemover`. Mirrors the `ccp` / `cce-find-orphans` nushell helpers. `shell-snapshots` is deliberately not swept — its filenames carry no session id. The `projects/*` husk check skips non-directory glob matches (e.g. a stray `.DS_Store`) and spares a project dir that holds a live subagent dir (`hasLiveSubagentDir`), since a live session's subagent state can sit under a different encoded cwd than its own transcript.

## Safety invariants — preserve these when editing

- **Every destructive path is derived only from a UUID id.** `validID` (regex in session.go) gates both `parseSession` and `deleteSession`. A jsonl named `...jsonl` has basename `..`; without this check `filepath.Join` could escape and `RemoveAll` a parent. Don't loosen the id check or derive delete paths before validating.
- **`confinedRemover` (cleanup.go) refuses any path not strictly under `~/.claude`**, and refuses root itself. Defense in depth on top of the UUID check. All deletes go through it.
- **`sanitize` (session.go) is the single chokepoint** every jsonl-derived display string flows through — it strips ANSI escapes and C0 control bytes so transcript content can't retitle the terminal, clear the screen, or ring the bell. Route any new jsonl text through `contentText`/`sanitize`, never render raw transcript bytes. Message bodies go through `contentText`; the other display strings (`Branch`, `Model`, and the display `Path`/`Haystack`) are sanitized at the point they're set in `parseSession`. `PathReal` stays raw — it's the chdir/scope key, never rendered. Its fast path shares the `isStripped` predicate with the strip loop, so they can't drift out of sync and silently leak a control byte the fast path forgot to list.
- **`maxTextLen` caps every kept string** so a pasted multi-MB blob can't stall render/filter.
