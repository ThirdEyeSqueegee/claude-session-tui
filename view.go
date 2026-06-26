package main

import (
	"fmt"
	"strconv"
	"strings"

	"charm.land/bubbles/v2/spinner"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

// ── update ─────────────────────────────────────────────────────────────────---

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case sessionsLoadedMsg:
		m.loading = false
		if msg.err != nil {
			m.loadErr = msg.err
			return m, nil
		}
		m.all = msg.res.Sessions
		m.skipped = msg.res.Skipped
		m.truncated = msg.res.Truncated
		m.rebuild()
		return m, nil

	case spinner.TickMsg:
		if m.loading {
			var cmd tea.Cmd
			m.spinner, cmd = m.spinner.Update(msg)
			return m, cmd
		}
		return m, nil

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.vp.SetWidth(msg.Width - 2)
		m.vp.SetHeight(msg.Height - 4)
		m.ensureVisible()
		return m, nil

	case tea.KeyPressMsg:
		if m.loading {
			if s := msg.String(); s == "q" || s == "ctrl+c" || s == "esc" {
				return m, tea.Quit
			}
			return m, nil
		}
		switch m.mode {
		case modeFilter:
			return m.updateFilter(msg)
		case modeTranscript:
			return m.updateTranscript(msg)
		case modeConfirmDelete:
			return m.updateConfirm(msg)
		default:
			return m.updateList(msg)
		}
	}
	return m, nil
}

func (m model) updateList(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	m.deleteErr = nil // any key dismisses a stale delete-error notice
	switch msg.String() {
	case "q", "ctrl+c", "esc":
		return m, tea.Quit
	case "enter":
		if s := m.selected(); s != nil {
			m.chosen = s
			return m, tea.Quit
		}
	case "j", "down":
		m.moveCursor(1)
	case "k", "up":
		m.moveCursor(-1)
	case "ctrl+d", "pgdown":
		m.moveCursor(m.listHeight() / 2)
	case "ctrl+u", "pgup":
		m.moveCursor(-m.listHeight() / 2)
	case "g", "home":
		m.cursor = 0
		m.snapCursorToSession(1)
		m.ensureVisible()
	case "G", "end":
		m.cursor = len(m.rows) - 1
		m.snapCursorToSession(-1)
		m.ensureVisible()
	case "s":
		m.sort = (m.sort + 1) % 3
		m.rebuild()
	case "/":
		m.mode = modeFilter
		return m, m.filter.Focus()
	case "p":
		if s := m.selected(); s != nil {
			m.openTranscript(s)
		}
	case ".":
		// toggle project scope; keep cursor sane after the list changes
		m.cwdScope = !m.cwdScope
		m.rebuild()
	case " ", "space":
		if s := m.selected(); s != nil {
			if m.marked[s.ID] {
				delete(m.marked, s.ID)
			} else {
				m.marked[s.ID] = true
			}
		}
	case "A":
		clear(m.marked)
	case "d":
		// bulk-delete the marked set if any, else the cursor row
		if len(m.deleteTargets()) == 0 {
			return m, nil
		}
		if m.confirmDelete {
			m.mode = modeConfirmDelete
		} else {
			m.doDelete()
		}
	}
	return m, nil
}

// deleteTargets is the set of sessions a delete would act on: the marked ones,
// or the cursor row when nothing is marked.
func (m *model) deleteTargets() []*Session {
	if len(m.marked) > 0 {
		var out []*Session
		for i := range m.rows {
			if r := m.rows[i]; r.kind == rowSession && m.marked[r.session.ID] {
				out = append(out, r.session)
			}
		}
		return out
	}
	if s := m.selected(); s != nil {
		return []*Session{s}
	}
	return nil
}

// doDelete removes every delete target on disk. A row is dropped from the list
// only if its on-disk delete actually succeeded; ids that failed stay marked so
// a failed delete never ghost-vanishes a session that reappears next launch.
func (m *model) doDelete() {
	targets := m.deleteTargets()
	if len(targets) == 0 {
		return
	}
	deleted := map[string]bool{}
	var failed int
	for _, s := range targets {
		if err := deleteSession(*s); err == nil {
			deleted[s.ID] = true
			delete(m.marked, s.ID)
		} else {
			failed++
			m.deleteErr = err
		}
	}
	if len(deleted) > 0 {
		m.removeByID(deleted)
		m.rebuild()
	}
	if failed > 0 && len(deleted) > 0 {
		m.deleteErr = errPartialDelete(len(deleted), failed)
	}
}

func (m model) updateFilter(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.filter.SetValue("")
		m.filter.Blur()
		m.mode = modeList
		m.rebuild()
		return m, nil
	case "enter":
		m.filter.Blur()
		m.mode = modeList
		return m, nil
	}
	var cmd tea.Cmd
	m.filter, cmd = m.filter.Update(msg)
	m.rebuild()
	return m, cmd
}

func (m model) updateTranscript(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "q", "esc", "p":
		m.mode = modeList
		return m, nil
	case "enter":
		if s := m.selected(); s != nil {
			m.chosen = s
			return m, tea.Quit
		}
	}
	var cmd tea.Cmd
	m.vp, cmd = m.vp.Update(msg)
	return m, cmd
}

func (m model) updateConfirm(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "y", "Y":
		m.doDelete()
		m.mode = modeList
		return m, nil
	default:
		m.mode = modeList
		return m, nil
	}
}

// removeByID drops every session whose id is in the set from m.all.
func (m *model) removeByID(ids map[string]bool) {
	out := m.all[:0]
	for _, x := range m.all {
		if !ids[x.ID] {
			out = append(out, x)
		}
	}
	m.all = out
}

// errPartialDelete formats a "deleted N, M failed" notice for the help bar.
func errPartialDelete(deleted, failed int) error {
	return fmt.Errorf("deleted %d, %d failed", deleted, failed)
}

func (m *model) openTranscript(s *Session) {
	m.mode = modeTranscript
	m.vp.SetWidth(m.width - 2)
	m.vp.SetHeight(m.height - 4)
	m.vp.SetContent(renderTranscript(s.JsonlPath, m.width-4))
	m.vp.GotoTop()
}

// ── view ────────────────────────────────────────────────────────────────────--

// minTwoPaneWidth is the narrowest terminal that fits both panes side by side:
// left (content 30 + 2 border + 2*padX) + right (content 18 + 2 border + 2*padX)
// + the shared seam already counted. Below this we stack to a single pane.
const minTwoPaneWidth = (30 + 2 + 2*padX) + (18 + 2 + 2*padX) + 1

func (m model) View() tea.View {
	v := tea.NewView("")
	v.AltScreen = true
	if m.width == 0 || m.height == 0 {
		v.Content = "loading…"
		return v
	}
	switch {
	case m.loading:
		v.Content = m.viewLoading()
	case m.loadErr != nil:
		v.Content = styDanger.Render(" failed to load sessions: ") + m.loadErr.Error()
	case len(m.all) == 0:
		empty := styLogo.Render("  "+logoMark+" ") + styDetailDim.Render("no chats yet — go make some with Claude")
		v.Content = strings.Join([]string{m.titleBar(), "", empty, "", m.helpBar()}, "\n")
	case m.mode == modeTranscript:
		v.Content = m.viewTranscript()
	default:
		v.Content = m.viewMain()
	}
	return v
}

func (m model) viewLoading() string {
	logo := styLogo.Render(logoMark) + "  " + styTitleBar.Render("Claude Sessions")
	line := m.spinner.View() + styDetailDim.Render(" gathering your chats…")
	card := lipgloss.JoinVertical(lipgloss.Center, logo, "", line)
	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, card)
}

func (m model) viewMain() string {
	bodyH := m.listHeight()
	// A pane's Width() is the OUTER width (border + padding + content). The
	// content we render must be that minus the 2 border cols and 2*padX padding,
	// or it wraps and breaks the layout.
	innerOf := func(paneW int) int { return paneW - 2 - 2*padX }

	pane := func(width int, content string, left bool) string {
		sides := lipgloss.RoundedBorder()
		return lipgloss.NewStyle().
			Width(width).
			Padding(0, padX).
			Border(sides, true, true, true, left).
			BorderForeground(cBorder).
			Render(content)
	}

	// Narrow terminal: a single full-width list pane, no detail.
	if m.width < minTwoPaneWidth {
		left := fitLines(m.renderList(innerOf(m.width), bodyH), bodyH)
		return m.frameBody(pane(m.width, left, true))
	}

	// Two panes, both derived from m.width so the total never exceeds it.
	// Chrome between/around: left 2 borders, right 1 (shared seam) + 1 = 3 cols.
	leftW := max(30, m.width*m.leftPct/100)
	rightW := m.width - leftW - 3
	minRight := 18 + 2 + 2*padX
	if rightW < minRight {
		rightW = minRight
		leftW = m.width - rightW - 3
	}

	left := fitLines(m.renderList(innerOf(leftW), bodyH), bodyH)
	right := fitLines(m.renderDetail(innerOf(rightW)), bodyH)

	body := lipgloss.JoinHorizontal(lipgloss.Top, pane(leftW, left, true), pane(rightW, right, false))
	return m.frameBody(body)
}

// frameBody stacks the title bar, body, help bar, and (when enabled) the credit
// footer. When there's vertical room it adds a blank line above and below the
// body for breathing room; on a cramped terminal it drops those so the whole
// frame still fits within the terminal height.
func (m model) frameBody(body string) string {
	// Non-body chrome: title (1) + help (1) + body border (2) + footer row (0/1).
	// listHeight() already subtracted those plus 2 framing blanks, so if it
	// landed above its floor the blanks fit; otherwise drop them to stay in
	// bounds on a short terminal.
	footRows := 0
	if m.footerLine() != "" {
		footRows = 1
	}
	fixed := 1 + 1 + 2 + footRows
	roomy := m.height >= m.listHeight()+fixed+2

	parts := []string{m.titleBar()}
	if roomy {
		parts = append(parts, "", body, "", m.helpBar())
	} else {
		parts = append(parts, body, m.helpBar())
	}
	if footRows == 1 {
		parts = append(parts, m.footerLine())
	}
	return strings.Join(parts, "\n")
}

func (m model) titleBar() string {
	left := styLogo.Render(" "+logoMark+" ") + styTitleBar.Render("Claude Sessions")
	if m.mode == modeFilter || m.filter.Value() != "" {
		left += "  " + m.filter.View()
	}
	status := strconv.Itoa(len(m.all)) + " sessions · sort:" + m.sort.label()
	if m.cwdScope {
		status += " · scope:" + scopeName(m.cwd)
	}
	if n := len(m.marked); n > 0 {
		status += " · " + strconv.Itoa(n) + " marked"
	}
	if m.skipped > 0 {
		status += " · " + strconv.Itoa(m.skipped) + " unreadable"
	}
	if m.truncated > 0 {
		status += " · " + strconv.Itoa(m.truncated) + " truncated"
	}
	if n := len(m.cfgWarnings); n > 0 {
		status += " · " + strconv.Itoa(n) + " config ⚠"
	}
	right := styCount.Render(status + " ")
	gap := m.width - lipgloss.Width(left) - lipgloss.Width(right)
	if gap < 1 {
		// no room for both: just clamp the left side to width
		return truncate(left, m.width)
	}
	return left + strings.Repeat(" ", gap) + right
}

// footerText is the credit line, shown bottom-right when it fits and is enabled.
const footerText = "made with ♥ by ThirdEyeSqueegee and Claude"

func (m model) helpBar() string {
	if m.deleteErr != nil && m.mode == modeList {
		return styHelp.Render(truncate(" delete failed: "+m.deleteErr.Error(), m.width))
	}
	if m.mode == modeConfirmDelete {
		n := len(m.deleteTargets())
		prompt := " delete this chat forever? y / n"
		if n > 1 {
			prompt = " delete " + strconv.Itoa(n) + " chats forever? y / n"
		}
		return styDanger.Render(truncate(prompt, m.width))
	}
	var keys string
	switch m.mode {
	case modeFilter:
		keys = "type to filter · ↵ apply · esc clear"
	default:
		// progressively shorter hints so the bar never exceeds the width
		del := "d delete"
		if len(m.marked) > 0 {
			del = "d delete " + strconv.Itoa(len(m.marked)) + " · A unmark"
		}
		keys = "↵ resume · / filter · space mark · " + del + " · . scope · s sort · p preview · q quit"
		if m.width > 0 && displayWidth(" "+keys) > m.width {
			keys = "↵ resume · / filter · space mark · " + del + " · . scope · q quit"
		}
		if m.width > 0 && displayWidth(" "+keys) > m.width {
			keys = "↵ resume · / filter · " + del + " · q quit"
		}
		if m.width > 0 && displayWidth(" "+keys) > m.width {
			keys = "↵ resume · q quit"
		}
	}
	return styHelp.Render(truncate(" "+keys, m.width))
}

// footerLine is the right-aligned credit, rendered on its own bottom row when
// enabled and the terminal is wide enough to hold it. Empty string = no footer
// row (frameBody then omits it).
func (m model) footerLine() string {
	// Need width for the text and at least the minimal frame (title+body+help)
	// plus the footer row itself; drop it on a cramped terminal.
	if !m.footer || displayWidth(footerText)+2 > m.width || m.height < 6 {
		return ""
	}
	foot := styFooter.Render(footerText)
	gap := max(m.width-displayWidth(foot)-1, 0)
	return strings.Repeat(" ", gap) + foot
}

// renderList draws the visible rows. w is the inner content width (the caller
// has already accounted for border + padding).
func (m model) renderList(w, h int) string {
	if len(m.rows) == 0 {
		if m.cwdScope && m.filter.Value() == "" {
			return styDetailDim.Render("no chats in this repo — . to show all")
		}
		return styDetailDim.Render("nothing matches — try fewer letters")
	}
	var b strings.Builder
	end := min(m.top+h, len(m.rows))
	for i := m.top; i < end; i++ {
		r := m.rows[i]
		// a blank line before each group header (except the very top of the
		// viewport) so projects read as distinct, airier blocks
		if r.kind == rowHeader && i > m.top {
			b.WriteByte('\n')
		}
		var line string
		if r.kind == rowHeader {
			badge := styBadge.Render(strconv.Itoa(r.count))
			line = styHeaderRow.Render(truncate(r.header, w-len(strconv.Itoa(r.count))-1)) + " " + badge
		} else {
			line = m.renderSessionRow(r.session, w, i == m.cursor)
		}
		b.WriteString(line)
		if i < end-1 {
			b.WriteByte('\n')
		}
	}
	return b.String()
}

func (m model) renderSessionRow(s *Session, w int, sel bool) string {
	ago := agoString(s.Updated, m.now)
	msgs := strconv.Itoa(s.Msgs)
	// 2-col gutter: [mark][cursor]. mark = ✓ when selected for bulk delete,
	// cursor = ❯ on the focused row.
	mark, curs := " ", " "
	if m.marked[s.ID] {
		mark = "✓"
	}
	if sel {
		curs = "❯"
	}
	gutter := mark + curs + " "
	if sel || m.marked[s.ID] {
		gutter = styTitleBar.Render(mark+curs) + " "
	}
	meta := msgs + " " + ago
	titleW := max(w-lipgloss.Width(gutter)-lipgloss.Width(meta)-1, 4)
	title := truncate(firstLine(s.Title), titleW)
	pad := max(titleW-lipgloss.Width(title), 0)
	body := gutter + title + strings.Repeat(" ", pad) + " " +
		styBadge.Render(msgs) + " " + styAgo.Render(ago)
	if sel {
		return stySelRow.Render(strings.TrimRight(body, " "))
	}
	return stySessText.Render(body)
}

func (m model) renderDetail(w int) string {
	s := m.selected()
	if s == nil {
		return styDetailDim.Render("pick a chat on the left")
	}
	var b strings.Builder

	b.WriteString(styLogo.Render(logoMark))
	b.WriteString(" ")
	b.WriteString(styTitleBar.Render(truncate(firstLine(s.Title), w-2)))
	b.WriteString("\n")
	b.WriteString(styDetailDim.Render(strings.Repeat("─", min(w, 40))))
	b.WriteString("\n\n")

	fmt.Fprintf(&b, "%s   %s\n", styDetailLbl.Render("path"), truncate(s.Path, w-7))
	line2 := strconv.Itoa(s.Msgs) + " msgs · " + agoString(s.Updated, m.now)
	if s.Branch != "" {
		line2 += " · " + s.Branch
	}
	if s.Model != "" {
		line2 += " · " + shortModel(s.Model)
	}
	fmt.Fprintf(&b, "%s\n\n", styDetailDim.Render(truncate(line2, w)))

	fmt.Fprintf(&b, "%s\n\n", styDetailLbl.Render("✎ first"))
	fmt.Fprintf(&b, "%s\n\n", wrapClip(s.FirstMsg, w, 4))
	fmt.Fprintf(&b, "%s\n\n", styDetailLbl.Render("✦ last"))
	last := s.LastMsg
	if last == "" {
		fmt.Fprintf(&b, "%s\n", styDetailDim.Render("no reply yet"))
	} else {
		fmt.Fprintf(&b, "%s\n", wrapClip(last, w, 6))
	}

	fmt.Fprintf(&b, "\n%s", styDetailDim.Render(truncate("· "+s.ID, w)))
	return b.String()
}

// ── transcript view ─────────────────────────────────────────────────────────-

func (m model) viewTranscript() string {
	s := m.selected()
	title := "transcript"
	if s != nil {
		title = firstLine(s.Title)
	}
	bar := styLogo.Render(" "+logoMark+" ") + styTitleBar.Render(truncate(title, m.width-3))
	help := styHelp.Render(" ↑/↓ scroll · ↵ resume this · q/esc back")
	return strings.Join([]string{bar, m.vp.View(), help}, "\n")
}

func shortModel(s string) string {
	s = strings.TrimPrefix(s, "claude-")
	if i := strings.Index(s, "-2"); i > 0 { // strip date suffix like -20251001
		s = s[:i]
	}
	return s
}
