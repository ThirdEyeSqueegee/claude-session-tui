package main

import (
	"bufio"
	"os"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	jsonv2 "github.com/go-json-experiment/json"
)

// truncate cuts s to a display width of w, appending an ellipsis when clipped.
// Grapheme/ANSI-aware: wide runes don't overflow and escape codes aren't split.
func truncate(s string, w int) string {
	s = strings.ReplaceAll(s, "\n", " ")
	s = strings.ReplaceAll(s, "\t", " ")
	if w <= 0 {
		return ""
	}
	return ansi.Truncate(s, w, "…")
}

// wrapClip word-wraps s to width w across at most maxLines lines, clipping the
// last line with an ellipsis if there's more text. Tokens longer than w are
// hard-broken so no produced line ever exceeds w cells.
func wrapClip(s string, w, maxLines int) string {
	s = strings.Join(strings.Fields(strings.ReplaceAll(s, "\n", " ")), " ")
	if s == "" {
		return styDetailDim.Render("—")
	}
	if w <= 0 {
		return ""
	}
	lines := strings.Split(ansi.Wrap(s, w, ""), "\n")
	clipped := false
	if len(lines) > maxLines {
		lines = lines[:maxLines]
		clipped = true
	}
	if clipped {
		lines[maxLines-1] = truncate(lines[maxLines-1]+" …", w)
	}
	return stySessText.Render(strings.Join(lines, "\n"))
}

// displayWidth is the terminal cell width of s (grapheme/ANSI aware).
func displayWidth(s string) int { return ansi.StringWidth(s) }

// fitLines forces s to be exactly n lines: clip overflow, pad short with blanks.
// Both panes are fit to the same n so their borders line up after JoinHorizontal
// (lipgloss Height is only a minimum, so equal-height content is the reliable fix).
func fitLines(s string, n int) string {
	if n <= 0 {
		return ""
	}
	lines := strings.Split(s, "\n")
	if len(lines) > n {
		return strings.Join(lines[:n], "\n")
	}
	for len(lines) < n {
		lines = append(lines, "")
	}
	return strings.Join(lines, "\n")
}

// renderTranscript builds a readable, role-tagged dump of a session jsonl for
// the pager view. Tool noise is collapsed; only human + assistant text shows.
func renderTranscript(jsonlPath string, w int) string {
	f, err := os.Open(jsonlPath)
	if err != nil {
		return "could not open transcript: " + err.Error()
	}
	defer f.Close()

	var b strings.Builder
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 1024*1024), 16*1024*1024)
	for sc.Scan() {
		line := sc.Bytes()
		if len(line) == 0 {
			continue
		}
		var r record
		if jsonv2.Unmarshal(line, &r) != nil {
			continue
		}
		switch r.Type {
		case "user":
			if r.IsSidechain || r.IsMeta || r.Message == nil {
				continue
			}
			txt := stripPrompt(contentText(r.Message.Content))
			if txt == "" {
				continue
			}
			b.WriteString(lipgloss.NewStyle().Foreground(cPink).Bold(true).Render("▌ you"))
			b.WriteString("\n")
			b.WriteString(indentWrap(txt, w))
			b.WriteString("\n\n")
		case "assistant":
			if r.Message == nil {
				continue
			}
			txt := contentText(r.Message.Content)
			if txt == "" {
				continue
			}
			b.WriteString(lipgloss.NewStyle().Foreground(cAccent).Bold(true).Render("✦ claude"))
			b.WriteString("\n")
			b.WriteString(indentWrap(txt, w))
			b.WriteString("\n\n")
		}
	}
	if sc.Err() != nil {
		b.WriteString(styDetailDim.Render("\n… transcript truncated (line exceeded buffer)"))
	}
	out := b.String()
	if strings.TrimSpace(out) == "" {
		return styDetailDim.Render("(no readable messages)")
	}
	return out
}

// indentWrap word-wraps text to width w for the pager, hard-breaking
// over-width tokens and preserving blank lines between paragraphs.
func indentWrap(s string, w int) string {
	if w < 10 {
		w = 10
	}
	s = strings.ReplaceAll(s, "\t", "    ")
	var out []string
	for para := range strings.SplitSeq(s, "\n") {
		para = strings.TrimRight(para, " ")
		if para == "" {
			out = append(out, "")
			continue
		}
		out = append(out, ansi.Wrap(para, w, ""))
	}
	return stySessText.Render(strings.Join(out, "\n"))
}
