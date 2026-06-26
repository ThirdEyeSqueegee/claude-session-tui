package main

import (
	"os"
	"path/filepath"
	"regexp"

	"github.com/pelletier/go-toml/v2"
)

// hexRe matches a #rgb or #rrggbb color. lipgloss.Color silently renders an
// invalid string as the terminal default, so we validate up front and warn.
var hexRe = regexp.MustCompile(`^#([0-9a-fA-F]{3}|[0-9a-fA-F]{6})$`)

// validateConfig returns human-readable warnings for values that would
// otherwise be silently ignored (bad hex colors, unknown sort). It never
// mutates the config — the loader keeps defaults for bad fields where it can.
func validateConfig(c Config) []string {
	var warns []string
	checkHex := func(label, hex string) {
		if hex != "" && !hexRe.MatchString(hex) {
			warns = append(warns, "bad color "+label+"="+hex)
		}
	}
	t := c.Theme
	checkHex("theme.accent", t.Accent)
	checkHex("theme.peach", t.Peach)
	checkHex("theme.pink", t.Pink)
	checkHex("theme.cream", t.Cream)
	checkHex("theme.dim", t.Dim)
	checkHex("theme.header", t.Header)
	checkHex("theme.selected", t.Selected)
	checkHex("theme.selected_bg", t.SelectedBg)
	checkHex("theme.danger", t.Danger)
	checkHex("theme.border", t.Border)
	checkHex("tab_color.active", c.TabColor.Active)
	checkHex("tab_color.inactive", c.TabColor.Inactive)
	checkHex("tab_color.active_fg", c.TabColor.ActiveFg)
	switch c.UI.Sort {
	case "", "recency", "project", "msgs":
	default:
		warns = append(warns, "unknown ui.sort="+c.UI.Sort+" (using recency)")
	}
	switch c.UI.DefaultScope {
	case "", "all", "cwd":
	default:
		warns = append(warns, "unknown ui.default_scope="+c.UI.DefaultScope+" (using all)")
	}
	return warns
}

// Config is the user-tunable surface, loaded from a TOML file (default
// ~/.config/cst/config.toml, override with $CST_CONFIG or -config). Every field
// has a sensible default, so a missing or partial file is fine.
type Config struct {
	// Resume controls how a picked session is launched.
	Resume ResumeConfig `toml:"resume"`
	// TabColor controls kitty tab tinting around a launched session.
	TabColor TabColorConfig `toml:"tab_color"`
	// UI controls layout and display.
	UI UIConfig `toml:"ui"`
	// Theme overrides palette colors (hex strings). Empty = built-in default.
	Theme ThemeConfig `toml:"theme"`
}

type ResumeConfig struct {
	// Command is the binary to launch (default "claude").
	Command string `toml:"command"`
	// Args are passed before the resume flag + id. Default: none. Add your own
	// flags here (e.g. ["--dangerously-skip-permissions"]). The resume id is
	// appended automatically as `<ResumeFlag> <id>`.
	Args []string `toml:"args"`
	// ResumeFlag is the flag used to pass the session id (default "--resume").
	ResumeFlag string `toml:"resume_flag"`
	// Chdir into the session's project dir before launching (default true).
	Chdir *bool `toml:"chdir"`
}

type TabColorConfig struct {
	// Enabled tints the kitty tab while a session runs (default true).
	Enabled *bool `toml:"enabled"`
	// Active / Inactive background colors (hex). Defaults to the Claude orange.
	Active   string `toml:"active"`
	Inactive string `toml:"inactive"`
	// ActiveFg is the active tab foreground (hex, default #000000).
	ActiveFg string `toml:"active_fg"`
	// PerProject derives a stable distinct hue per project (hash of the project
	// path) instead of the fixed Active color, so concurrent tabs are
	// scannable. ActiveFg still applies. Default false.
	PerProject bool `toml:"per_project"`
}

type UIConfig struct {
	// Sort is the initial sort: "recency", "project", or "msgs".
	Sort string `toml:"sort"`
	// LeftWidthPct is the left pane width as a percent of the terminal (default 42).
	LeftWidthPct int `toml:"left_width_pct"`
	// Footer shows the "made with ♥" credit line (default true).
	Footer *bool `toml:"footer"`
	// ConfirmDelete requires a y/n confirm before deleting (default true).
	ConfirmDelete *bool `toml:"confirm_delete"`
	// DefaultScope is the initial project scope: "all" or "cwd" (default "all").
	DefaultScope string `toml:"default_scope"`
}

type ThemeConfig struct {
	Accent     string `toml:"accent"`
	Peach      string `toml:"peach"`
	Pink       string `toml:"pink"`
	Cream      string `toml:"cream"`
	Dim        string `toml:"dim"`
	Header     string `toml:"header"`
	Selected   string `toml:"selected"`
	SelectedBg string `toml:"selected_bg"`
	Danger     string `toml:"danger"`
	Border     string `toml:"border"`
}

// defaultConfig returns the built-in defaults. Pointer-bool fields default true.
func defaultConfig() Config {
	t := true
	return Config{
		Resume: ResumeConfig{
			Command: "claude",
			// No extra flags by default; add your own (e.g.
			// --dangerously-skip-permissions) via the config file.
			Args:       []string{},
			ResumeFlag: "--resume",
			Chdir:      &t,
		},
		TabColor: TabColorConfig{
			Enabled:  &t,
			Active:   "#d97757",
			Inactive: "#914f39",
			ActiveFg: "#000000",
		},
		UI: UIConfig{
			Sort:          "recency",
			LeftWidthPct:  42,
			Footer:        &t,
			ConfirmDelete: &t,
			DefaultScope:  "all",
		},
	}
}

// configPath resolves the config file location: -config flag, then $CST_CONFIG,
// then $XDG_CONFIG_HOME/cst/config.toml, then ~/.config/cst/config.toml.
func configPath(override string) string {
	if override != "" {
		return override
	}
	if p := os.Getenv("CST_CONFIG"); p != "" {
		return p
	}
	base := os.Getenv("XDG_CONFIG_HOME")
	if base == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return ""
		}
		base = filepath.Join(home, ".config")
	}
	return filepath.Join(base, "cst", "config.toml")
}

// loadConfig reads the TOML file and merges it over the defaults. A missing
// file is not an error (defaults are used). A malformed file returns the
// defaults plus the parse error so the caller can warn without aborting.
//
// Unmarshals into a ZERO config (not a pre-seeded one): go-toml allocates
// fresh values for every key it sees, which would clobber pre-seeded pointer
// defaults and make an absent `confirm_delete` indistinguishable from an
// explicit `false`. Starting from zero, an absent pointer stays nil and
// mergeDefaults fills it; an explicit `false` is preserved.
func loadConfig(override string) (Config, error) {
	path := configPath(override)
	if path == "" {
		return defaultConfig(), nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return defaultConfig(), nil // no file → defaults, silently
		}
		return defaultConfig(), err
	}
	var cfg Config
	if err := toml.Unmarshal(data, &cfg); err != nil {
		return defaultConfig(), err
	}
	mergeDefaults(&cfg)
	return cfg, nil
}

// mergeDefaults fills any field the user left unset with its default. Strings
// fall back when empty; ints when <= 0; slices when nil; pointers are left as
// the user set them (nil = unset, resolved later by boolOr).
func mergeDefaults(c *Config) {
	d := defaultConfig()
	or := func(dst *string, def string) {
		if *dst == "" {
			*dst = def
		}
	}
	or(&c.Resume.Command, d.Resume.Command)
	or(&c.Resume.ResumeFlag, d.Resume.ResumeFlag)
	if c.Resume.Args == nil {
		c.Resume.Args = d.Resume.Args
	}
	or(&c.TabColor.Active, d.TabColor.Active)
	or(&c.TabColor.Inactive, d.TabColor.Inactive)
	or(&c.TabColor.ActiveFg, d.TabColor.ActiveFg)
	or(&c.UI.Sort, d.UI.Sort)
	or(&c.UI.DefaultScope, d.UI.DefaultScope)
	if c.UI.LeftWidthPct <= 0 {
		c.UI.LeftWidthPct = d.UI.LeftWidthPct
	}
	// Pointer-bool fields (Resume.Chdir, TabColor.Enabled, UI.Footer,
	// UI.ConfirmDelete) are intentionally left as parsed: nil = unset (resolved
	// to the default by boolOr at use), a non-nil pointer = the user's explicit
	// true/false. Merging them here would erase that distinction.
}

// sortModeFromString maps a config string to a sortMode (default recency).
func sortModeFromString(s string) sortMode {
	switch s {
	case "project":
		return sortProject
	case "msgs":
		return sortMsgs
	default:
		return sortRecency
	}
}

func boolOr(p *bool, def bool) bool {
	if p == nil {
		return def
	}
	return *p
}
