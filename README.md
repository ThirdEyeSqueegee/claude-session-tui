<div align="center">

# cst

**A TUI picker for your Claude Code sessions.**

[![CI](https://github.com/ThirdEyeSqueegee/claude-session-tui/actions/workflows/ci.yml/badge.svg)](https://github.com/ThirdEyeSqueegee/claude-session-tui/actions/workflows/ci.yml)
[![Release](https://img.shields.io/github/v/release/ThirdEyeSqueegee/claude-session-tui?sort=semver)](https://github.com/ThirdEyeSqueegee/claude-session-tui/releases)
[![Go Reference](https://pkg.go.dev/badge/github.com/ThirdEyeSqueegee/claude-session-tui.svg)](https://pkg.go.dev/github.com/ThirdEyeSqueegee/claude-session-tui)
[![Go 1.26](https://img.shields.io/badge/go-1.26-00ADD8?logo=go&logoColor=white)](go.mod)
[![License: MIT](https://img.shields.io/badge/license-MIT-blue.svg)](LICENSE)

</div>

```
 ✻ Claude Sessions                                    3 sessions · sort:recency

╭───────────────────────────────╮───────────────────────────────────────────╮
│  ❯ Fix auth middleware  18 2h │  ✻ Fix auth middleware                    │
│    Add rate limiting    7 yest│  ────────────────────────────────────     │
│                               │                                           │
│  ~/Desktop/misc/tui           │  path   ~/work/api-service                │
│    Picker design       31 5m  │  18 msgs · 2h ago · main · opus-4-8       │
│                               │                                           │
│                               │  ✎ first                                  │
│                               │  auth expiry uses < not <=                │
│                               │                                           │
│                               │  ✦ last                                   │
│                               │  fixed, all tests pass                    │
╰───────────────────────────────╯───────────────────────────────────────────╯

 ↵ resume · / filter · space mark · d delete · . scope · q quit
                                       made with ♥ by ThirdEyeSqueegee and Claude
```

## Features

- 🗂️ **Grouped by project** — every session under `~/.claude/projects`, organized by repo
- 🔍 **Fuzzy filter** — match across title, path, and first message (space = AND)
- 👀 **Live detail pane** — first/last message, branch, model, message count, relative age
- ⚡ **Instant resume** — `chdir`s into the project and relaunches Claude where you left off
- 🎯 **Project scope** — one key to show only the current repo's sessions
- 🧹 **Safe bulk delete** — multi-select, UUID-validated, path-confined to `~/.claude`
- 🎨 **Warm theme + kitty tab coloring** — fully configurable via TOML
- 🚀 **Fast cold start** — transcripts parsed in parallel, off the UI thread

## Install

### mise

```sh
mise use -g github:ThirdEyeSqueegee/claude-session-tui
```

### Prebuilt binary

Grab the archive for your platform from the
[latest release](https://github.com/ThirdEyeSqueegee/claude-session-tui/releases),
verify it against `*_checksums.txt`, and drop `cst` on your `PATH`.

### From source

```sh
go install github.com/ThirdEyeSqueegee/claude-session-tui@latest
```

Or clone and use the Makefile (builds a version-stamped binary into `~/.local/bin`):

```sh
git clone https://github.com/ThirdEyeSqueegee/claude-session-tui
cd claude-session-tui
make install
```

> **Requirements:** Go 1.26+ to build. Runs on macOS and Linux.

## Usage

Just run it:

```sh
cst
```

Pick a session with `↵` and `cst` `chdir`s into that session's project directory,
then launches the resume command (default: a plain `claude --resume <id>`),
waits for it, and restores your terminal afterward.

### Keybindings

| Key                  | Action                                      |
| -------------------- | ------------------------------------------- |
| `↵`                  | Resume the selected session                 |
| `j` / `k`, `↑` / `↓` | Move (skips group headers)                  |
| `ctrl-d` / `ctrl-u`  | Half-page jump                              |
| `g` / `G`            | Jump to top / bottom                        |
| `/`                  | Fuzzy filter (title + path + first message) |
| `space`              | Mark / unmark a row for bulk delete         |
| `A`                  | Clear all marks                             |
| `.`                  | Toggle scope to the launch repo only        |
| `s`                  | Cycle sort: recency → project → msgs        |
| `p`                  | Preview the full transcript                 |
| `d`                  | Delete (marked rows, or the cursor row)     |
| `q` / `esc`          | Quit                                        |

### Flags

| Flag                    | Behavior                                     |
| ----------------------- | -------------------------------------------- |
| `-p`, `--print`         | Print the chosen session id and exit         |
| `-o`, `--output <file>` | Write the chosen id to `<file>` and exit     |
| `-c`, `--config <path>` | Use a specific config file                   |
| `-C`, `--print-config`  | Print the resolved effective config and exit |
| `-v`, `--version`       | Print the build version                      |
| `-h`, `--help`          | Show help                                    |

`--print` / `--output` let a wrapper own the launch instead of `cst`. Exit code
`130` means you quit without choosing.

## Configuration

`cst` reads an optional TOML file, resolved in this order:

1. `--config <path>`
2. `$CST_CONFIG`
3. `$XDG_CONFIG_HOME/cst/config.toml`
4. `~/.config/cst/config.toml`

Every key is optional — a missing file means all defaults. A bad value (malformed
hex, an unknown sort) keeps the default and shows a `config ⚠` note in the title
bar; run `cst --print-config` to dump the resolved config. The full annotated
schema lives in [`config.example.toml`](config.example.toml).

```toml
[resume]
command = "claude"   # the binary to launch
args = []            # extra flags, e.g. ["--dangerously-skip-permissions"]

[tab_color]
enabled = true
per_project = true   # distinct kitty tab hue per repo

[ui]
sort = "recency"     # "recency" | "project" | "msgs"
default_scope = "all" # "all" | "cwd"
```

### Project scope

Press `.` to toggle a view of just the sessions whose project directory matches
the directory `cst` was launched from — the inline equivalent of a per-repo
resume picker. Both paths are symlink-resolved, so jj worktrees and symlinked
checkouts still match. Start scoped with `default_scope = "cwd"`.

### Bulk delete

`space` marks rows (shown with a `✓` in the gutter); `d` then deletes all marked
sessions behind a single `delete N chats forever?` confirm, or just the cursor
row when nothing is marked.

Deleting removes the transcript plus its satellite state (`session-env`,
`file-history`, subagent project dirs, and `paste-cache` / `tasks` / `todos`
entries keyed by the session id). Every id is validated as a UUID and every
removed path is confirmed to live strictly under `~/.claude` before deletion, so
a malformed id can never escape that tree. A row is dropped from the list only
when its on-disk delete actually succeeds; failures stay marked and are reported
in the help bar.

### kitty tab coloring

Inside kitty, `cst` tints the terminal tab while a session runs and restores it
on exit (via `kitten @ set-tab-color`). That's why `cst` waits on the child
rather than replacing itself with it. Set `per_project = true` under
`[tab_color]` to derive a stable, distinct hue per repo so a wall of concurrent
tabs is scannable at a glance. Outside kitty — or if remote control is off — it
silently no-ops.

</details>

## How it works

Each session is parsed straight from its `.jsonl` transcript:

- **Title** — custom title, else AI-generated title, else the first user message
- **Project path** — the session's cwd, home-collapsed
- **Message count** — real user messages only (sidechain / meta excluded)
- **Detail** — branch, model, first message, last assistant reply

Sessions load asynchronously behind a spinner, so a heavy `~/.claude/projects`
never stalls startup. Unreadable or truncated transcripts surface as an advisory
count in the title bar instead of silently vanishing. On a terminal too narrow
for two panes, the layout collapses to a single full-width list.

Built with [Bubble Tea](https://github.com/charmbracelet/bubbletea),
[Bubbles](https://github.com/charmbracelet/bubbles), and
[Lip Gloss](https://github.com/charmbracelet/lipgloss).

## Development

```sh
make build   # version-stamped binary
make test    # go test ./...
make race    # go test -race ./...
make vet     # go vet ./...
make fmt     # gofmt -w .
```

CI runs on every push and PR: a `go test -race` matrix on macOS + Linux,
`golangci-lint`, `govulncheck`, `actionlint`, and a `gofmt` gate — each under
[harden-runner](https://github.com/step-security/harden-runner) with a
least-privilege token. Actions are pinned to major versions and bumped by
Dependabot.

### Releasing

Releases are cut by [GoReleaser](https://goreleaser.com) on a pushed semver tag,
producing cross-compiled `darwin`/`linux` × `amd64`/`arm64` archives plus a
checksums file:

```sh
git tag -a v0.1.0 -m v0.1.0
git push origin v0.1.0
```

## Contributing

Issues and PRs welcome. Before opening a PR, please run `make fmt`, `make vet`,
and `make race` — CI enforces all three.

## License

[MIT](LICENSE) © ThirdEyeSqueegee
