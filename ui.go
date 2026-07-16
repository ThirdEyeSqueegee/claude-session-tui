package main

import (
	"image/color"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"charm.land/bubbles/v2/spinner"
	"charm.land/bubbles/v2/textinput"
	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/fsnotify/fsnotify"
)

// ── sort modes ───────────────────────────────────────────────────────────────

type sortMode int

const (
	sortRecency sortMode = iota
	sortProject
	sortMsgs
	sortSize
)

// sortModeCount is the number of sort modes, for the cycle key.
const sortModeCount = 4

func (s sortMode) label() string {
	switch s {
	case sortProject:
		return "project"
	case sortMsgs:
		return "msgs"
	case sortSize:
		return "size"
	default:
		return "recency"
	}
}

// groupMode is how the flat row list is bucketed under headers.
type groupMode int

const (
	groupProject groupMode = iota
	groupDate
	groupBranch
)

const groupModeCount = 3

func (g groupMode) label() string {
	switch g {
	case groupDate:
		return "date"
	case groupBranch:
		return "branch"
	default:
		return "project"
	}
}

// ── flattened rows ─────────────────────────────────────────────────────────--
//
// The left pane is a single scrollable column of rows. A row is either a
// project header or a session under it. Headers are skipped by cursor movement.

type rowKind int

const (
	rowHeader rowKind = iota
	rowSession
)

type row struct {
	kind    rowKind
	header  string // path, for rowHeader
	count   int    // session count, for rowHeader
	session *Session
}

// ── colors / styles ──────────────────────────────────────────────────────────

// Warm peach palette: the claude orange stays the star, soft peach / pink /
// cream warm the edges so the whole thing reads cozy rather than clinical.
var (
	cAccent = lipgloss.Color("#d97757") // claude orange — primary accent
	cPeach  = lipgloss.Color("#e8a87c") // soft peach — sparkle, detail labels
	cPink   = lipgloss.Color("#d98e9a") // dusty pink — badges, gentle pop
	cCream  = lipgloss.Color("#ecd9c6") // warm cream — body text
	cDim    = lipgloss.Color("#8a7a70") // warm grey — secondary text
	cHeader = lipgloss.Color("#c9a892") // muted peach — group headers
	cSel    = lipgloss.Color("#fff3e8") // near-white warm — selected text
	cSelBg  = lipgloss.Color("#43302a") // cocoa — selected row background
	cDanger = lipgloss.Color("#e0788a") // soft red-pink — danger (still clear)
	cBorder = lipgloss.Color("#5a4a42") // warm border
)

// logoMark is the Anthropic starburst, the mark Claude Code shows at startup.
const logoMark = "✻"

var (
	styTitleBar  lipgloss.Style
	styLogo      lipgloss.Style
	styCount     lipgloss.Style
	styHeaderRow lipgloss.Style
	styBadge     lipgloss.Style
	styAgo       lipgloss.Style
	stySessText  lipgloss.Style
	stySelRow    lipgloss.Style
	styHelp      lipgloss.Style
	styDetailLbl lipgloss.Style
	styDetailDim lipgloss.Style
	styDanger    lipgloss.Style
	styFooter    lipgloss.Style
)

func init() { buildStyles() }

// buildStyles (re)derives every style from the current color vars. Called once
// at init and again after a theme override is applied.
func buildStyles() {
	styTitleBar = lipgloss.NewStyle().Bold(true).Foreground(cAccent)
	styLogo = lipgloss.NewStyle().Bold(true).Foreground(cAccent)
	styCount = lipgloss.NewStyle().Foreground(cDim)
	styHeaderRow = lipgloss.NewStyle().Foreground(cHeader).Bold(true)
	styBadge = lipgloss.NewStyle().Foreground(cPink)
	styAgo = lipgloss.NewStyle().Foreground(cDim)
	stySessText = lipgloss.NewStyle().Foreground(cCream)
	stySelRow = lipgloss.NewStyle().Foreground(cSel).Background(cSelBg).Bold(true)
	styHelp = lipgloss.NewStyle().Foreground(cDim)
	styDetailLbl = lipgloss.NewStyle().Foreground(cPeach).Bold(true)
	styDetailDim = lipgloss.NewStyle().Foreground(cDim)
	styDanger = lipgloss.NewStyle().Foreground(cDanger).Bold(true)
	styFooter = lipgloss.NewStyle().Foreground(cDim)
}

// applyTheme overrides palette colors from config (any empty or invalid field
// is left at its default — validateConfig surfaces the warning) and rebuilds
// the styles.
func applyTheme(t ThemeConfig) {
	set := func(dst *color.Color, hex string) {
		if hexRe.MatchString(hex) {
			*dst = lipgloss.Color(hex)
		}
	}
	set(&cAccent, t.Accent)
	set(&cPeach, t.Peach)
	set(&cPink, t.Pink)
	set(&cCream, t.Cream)
	set(&cDim, t.Dim)
	set(&cHeader, t.Header)
	set(&cSel, t.Selected)
	set(&cSelBg, t.SelectedBg)
	set(&cDanger, t.Danger)
	set(&cBorder, t.Border)
	buildStyles()
}

// ── model ─────────────────────────────────────────────────────────────────────

type uiMode int

const (
	modeList uiMode = iota
	modeFilter
	modeTranscript
	modeConfirmDelete
)

type model struct {
	all       []Session // every session, recency-sorted at load
	rows      []row     // current flattened view (post-filter, post-sort)
	cursor    int       // index into rows (always points at a rowSession)
	top       int       // first visible row (scroll offset)
	sort      sortMode
	group     groupMode
	filter    textinput.Model
	mode      uiMode
	vp        viewport.Model // transcript pager
	spinner   spinner.Model
	width     int
	height    int
	now       time.Time
	loading   bool     // true until the async load completes
	loadErr   error    // non-nil if the load itself failed
	skipped   int      // unreadable jsonl files (advisory)
	truncated int      // sessions that hit the scanner buffer cap (advisory)
	deleteErr error    // last failed delete, shown in the help bar
	notice    string   // transient confirmation (e.g. "copied id"), shown in the help bar
	chosen    *Session // session to resume; set on enter, drives quit (nil = quit without picking)

	// config-derived UI knobs
	leftPct       int  // left pane width percent
	footer        bool // show the made-with credit line
	confirmDelete bool // require y/n before deleting
	gitStatus     bool // flag gone project dirs / branches
	watch         bool // reload when ~/.claude/projects changes
	watchInterval time.Duration
	cfgWarnings   []string // non-fatal config warnings, shown in the title bar

	// transcript pager in-view search (modeTranscript)
	searching       bool   // search field is focused
	searchQuery     string // last applied transcript search
	searchInput     textinput.Model
	transcriptLines []string // ANSI-stripped transcript lines, aligned to the viewport, for search

	// project scope: when cwdScope is on, only sessions whose project dir
	// matches cwd are shown. cwd is symlink-resolved at startup.
	cwd      string
	cwdScope bool

	// multi-select: ids marked for bulk delete (keyed by conversation_id so
	// filter/sort rebuilds never corrupt the set).
	marked map[string]bool

	// watch: fingerprint of projects/ at last load, to detect on-disk changes.
	fingerprint string
	// fsw is the fsnotify watcher when the event-driven path is active; nil when
	// falling back to the time-based poll. fsReloadPending debounces a burst of
	// events into a single reload.
	fsw             *fsnotify.Watcher
	fsReloadPending bool
}

// sessionsLoadedMsg is delivered when the background load finishes.
type sessionsLoadedMsg struct {
	res LoadResult
	err error
}

func initialModel(now time.Time, cfg Config, warnings []string, cwd string) model {
	applyTheme(cfg.Theme) // palette overrides before any style is used

	ti := textinput.New()
	ti.Placeholder = "filter…"
	ti.Prompt = "/ "

	si := textinput.New()
	si.Placeholder = "search transcript…"
	si.Prompt = "/ "

	// a spinning asterisk, echoing the Claude Code starburst mark
	sp := spinner.New(spinner.WithSpinner(spinner.Spinner{
		Frames: []string{"✶", "✸", "✺", "✸"},
		FPS:    time.Second / 6,
	}))
	sp.Style = lipgloss.NewStyle().Foreground(cAccent)

	leftPct := cfg.UI.LeftWidthPct
	if leftPct < 20 || leftPct > 80 {
		leftPct = 42 // keep both panes usable
	}

	return model{
		sort:          sortModeFromString(cfg.UI.Sort),
		group:         groupModeFromString(cfg.UI.Group),
		filter:        ti,
		searchInput:   si,
		mode:          modeList,
		spinner:       sp,
		now:           now,
		loading:       true,
		leftPct:       leftPct,
		footer:        boolOr(cfg.UI.Footer, true),
		confirmDelete: boolOr(cfg.UI.ConfirmDelete, true),
		gitStatus:     boolOr(cfg.UI.GitStatus, true),
		watch:         boolOr(cfg.UI.Watch, false),
		watchInterval: time.Duration(cfg.UI.WatchIntervalSecs) * time.Second,
		cfgWarnings:   warnings,
		cwd:           resolveCwd(cwd),
		cwdScope:      cfg.UI.DefaultScope == "cwd",
		marked:        map[string]bool{},
	}
}

// scopeName is the short label for the scoped repo: the cwd's basename.
func scopeName(cwd string) string {
	if cwd == "" {
		return "cwd"
	}
	return filepath.Base(cwd)
}

// resolveCwd symlink-resolves a path for stable comparison against session
// project dirs (jj/symlinked checkouts differ from the logical path Claude
// records). Falls back to the raw path on error.
func resolveCwd(p string) string {
	if p == "" {
		return ""
	}
	if r, err := filepath.EvalSymlinks(p); err == nil {
		return r
	}
	return p
}

// loadCmd parses all sessions off the UI thread so a heavy ~/.claude/projects
// doesn't stall on a blank terminal at startup. gitStatus is threaded through so
// a reload keeps the same annotation behavior as the initial load.
func loadCmd(gitStatus bool) tea.Cmd {
	return func() tea.Msg {
		res, err := loadSessions(gitStatus)
		return sessionsLoadedMsg{res: res, err: err}
	}
}

// watchTickMsg fires on the watch poll interval (the fallback path); the
// handler reloads if ~/.claude/projects changed since the last load.
type watchTickMsg struct{}

// watchCmd starts live reload when enabled: try the event-driven fsnotify path
// first (startWatchCmd), which falls back to polling if a watcher can't be
// built. Returns nil when watch is off.
func (m model) watchCmd() tea.Cmd {
	if !m.watch {
		return nil
	}
	return startWatchCmd
}

func (m model) Init() tea.Cmd {
	return tea.Batch(loadCmd(m.gitStatus), m.spinner.Tick, m.watchCmd())
}

// ── rebuild: filter + sort + group into rows ───────────────────────────────────

func (m *model) rebuild() {
	q := strings.ToLower(strings.TrimSpace(m.filter.Value()))

	tokens := strings.Fields(q)
	var matched []Session
	for _, s := range m.all {
		if m.cwdScope && !m.inScope(s) {
			continue
		}
		if fuzzyMatch(tokens, s) {
			matched = append(matched, s)
		}
	}

	switch m.sort {
	case sortRecency:
		sort.SliceStable(matched, func(i, j int) bool {
			return matched[i].Updated.After(matched[j].Updated)
		})
	case sortMsgs:
		sort.SliceStable(matched, func(i, j int) bool {
			return matched[i].Msgs > matched[j].Msgs
		})
	case sortSize:
		sort.SliceStable(matched, func(i, j int) bool {
			return matched[i].Size > matched[j].Size
		})
	case sortProject:
		sort.SliceStable(matched, func(i, j int) bool {
			if matched[i].Path == matched[j].Path {
				return matched[i].Updated.After(matched[j].Updated)
			}
			return matched[i].Path < matched[j].Path
		})
	}

	// group by the current group key, preserving the order each group first
	// appears in the sorted slice. For recency that orders groups by their
	// newest session.
	order := []string{}
	groups := map[string][]Session{}
	for _, s := range matched {
		key := m.groupKey(s)
		if _, ok := groups[key]; !ok {
			order = append(order, key)
		}
		groups[key] = append(groups[key], s)
	}

	rows := make([]row, 0, len(matched)+len(order))
	for _, path := range order {
		g := groups[path]
		rows = append(rows, row{kind: rowHeader, header: path, count: len(g)})
		for i := range g {
			s := g[i]
			rows = append(rows, row{kind: rowSession, session: &s})
		}
	}
	m.rows = rows

	// rows were just rebuilt; the old index may be stale or land on a header
	if m.cursor >= len(rows) {
		m.cursor = len(rows) - 1
	}
	if m.cursor < 0 {
		m.cursor = 0
	}
	m.snapCursorToSession(+1)
	m.ensureVisible()
}

// groupKey is the header a session is bucketed under, per the current group
// mode. Project is the raw display path; date is a coarse recency bucket; branch
// is the git branch (or a placeholder when the session has none).
func (m *model) groupKey(s Session) string {
	switch m.group {
	case groupDate:
		return dateBucket(s.Updated, m.now)
	case groupBranch:
		if s.Branch == "" {
			return "(no branch)"
		}
		return s.Branch
	default:
		return s.Path
	}
}

// dateBucket labels t by recency relative to now, coarser than agoString so
// sessions collapse into a handful of readable headers.
func dateBucket(t, now time.Time) string {
	d := now.Sub(t)
	switch {
	case d < 24*time.Hour:
		return "today"
	case d < 48*time.Hour:
		return "yesterday"
	case d < 7*24*time.Hour:
		return "this week"
	case d < 30*24*time.Hour:
		return "this month"
	case d < 365*24*time.Hour:
		return "this year"
	default:
		return "older"
	}
}

// fuzzyMatch tests query tokens against the session's precomputed lowercased
// haystack (title+path+first-msg). Every token must appear as a substring
// (AND), order-independent. Empty token list matches everything.
func fuzzyMatch(tokens []string, s Session) bool {
	for _, tok := range tokens {
		if !strings.Contains(s.Haystack, tok) {
			return false
		}
	}
	return true
}

// inScope reports whether a session's project dir matches the launch cwd. Both
// sides are symlink-resolved (jj/symlinked checkouts make the logical path
// Claude records differ from the real cwd, which would silently empty the list).
func (m *model) inScope(s Session) bool {
	if m.cwd == "" {
		return true // no cwd to scope to → show everything
	}
	return resolveCwd(s.PathReal) == m.cwd
}

// ── cursor helpers ─────────────────────────────────────────────────────────---

func (m *model) snapCursorToSession(dir int) {
	if len(m.rows) == 0 {
		m.cursor = 0
		return
	}
	for m.cursor >= 0 && m.cursor < len(m.rows) && m.rows[m.cursor].kind == rowHeader {
		m.cursor += dir
	}
	// if we ran off an edge, search the other way
	if m.cursor < 0 || m.cursor >= len(m.rows) {
		m.cursor = clamp(m.cursor, 0, len(m.rows)-1)
		for m.cursor >= 0 && m.cursor < len(m.rows) && m.rows[m.cursor].kind == rowHeader {
			m.cursor -= dir
		}
	}
	m.cursor = clamp(m.cursor, 0, len(m.rows)-1)
}

func (m *model) moveCursor(delta int) {
	if len(m.rows) == 0 {
		return
	}
	i := m.cursor
	step := 1
	if delta < 0 {
		step = -1
	}
	remaining := delta
	if remaining < 0 {
		remaining = -remaining
	}
	for remaining > 0 {
		next := i + step
		// skip headers
		for next >= 0 && next < len(m.rows) && m.rows[next].kind == rowHeader {
			next += step
		}
		if next < 0 || next >= len(m.rows) {
			break // hit list edge
		}
		i = next
		remaining--
	}
	m.cursor = i
	m.ensureVisible()
}

func (m *model) selected() *Session {
	if m.cursor >= 0 && m.cursor < len(m.rows) && m.rows[m.cursor].kind == rowSession {
		return m.rows[m.cursor].session
	}
	return nil
}

// listHeight is the number of body rows the left pane can show.
// padX is the horizontal breathing room inside each pane, per side, so text
// never hugs the border.
const padX = 2

func (m *model) listHeight() int {
	// chrome: title bar (1) + help bar (1) + framing blank above & below (2) +
	// pane border top/bottom (2) = 6, plus 1 for the footer row when shown.
	chrome := 6
	if m.footer {
		chrome++
	}
	return max(m.height-chrome, 1)
}

// visualRows is how many terminal lines renderList emits for rows[from..to]
// inclusive: one per row plus a blank separator before each group header that
// isn't the first row in the window. ensureVisible budgets against this — a
// plain (to-from+1) row count ignores the separators and lets the cursor row
// scroll under the fold where fitLines then clips it.
func (m *model) visualRows(from, to int) int {
	n := 0
	for i := from; i <= to && i < len(m.rows); i++ {
		n++
		if m.rows[i].kind == rowHeader && i > from {
			n++
		}
	}
	return n
}

func (m *model) ensureVisible() {
	h := m.listHeight()
	if m.cursor < m.top {
		m.top = m.cursor
	}
	// Scroll down until the cursor row fits within h visual lines of the top,
	// counting the blank line renderList emits before each interior header.
	for m.top < m.cursor && m.visualRows(m.top, m.cursor) > h {
		m.top++
	}
	if m.top < 0 {
		m.top = 0
	}
}

func clamp(v, lo, hi int) int {
	if hi < lo {
		return lo
	}
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}
