package main

import (
	"fmt"
	"strings"
)

// modelPrice is per-1M-token USD pricing for a model: input, output, and the
// cache read/write multipliers off the input rate. Sourced from the claude-api
// skill (2026-06 cache). Used only for the detail-pane cost estimate — an
// unknown model shows tokens without a dollar figure rather than guessing.
type modelPrice struct {
	in, out float64 // $ per 1M tokens
}

// cache multipliers off the input rate (uniform across current models):
// reads ~0.1x, 5-minute writes ~1.25x.
const (
	cacheReadMult  = 0.10
	cacheWriteMult = 1.25
)

// prices is keyed by the model-id prefix we match (shortModel-style), longest
// prefix wins. Kept small: the models a Claude Code transcript actually records.
var prices = map[string]modelPrice{
	"claude-fable-5":   {in: 10, out: 50},
	"claude-mythos-5":  {in: 10, out: 50},
	"claude-opus-4":    {in: 5, out: 25}, // 4.8 / 4.7 / 4.6 / 4.5 all $5/$25
	"claude-opus-3":    {in: 15, out: 75},
	"claude-sonnet":    {in: 3, out: 15}, // sonnet 5 / 4.6 / 4.5
	"claude-haiku-4":   {in: 1, out: 5},
	"claude-haiku-3-5": {in: 0.8, out: 4},
	"claude-haiku-3":   {in: 0.25, out: 1.25},
}

// priceFor returns the pricing for a model id by longest-prefix match, and
// whether a match was found.
func priceFor(model string) (modelPrice, bool) {
	best := ""
	for k := range prices {
		if strings.HasPrefix(model, k) && len(k) > len(best) {
			best = k
		}
	}
	if best == "" {
		return modelPrice{}, false
	}
	return prices[best], true
}

// sessionCost estimates the USD cost of a session from its summed token usage
// and per-model pricing. cache reads bill at ~0.1x input, writes at ~1.25x.
func sessionCost(s Session, p modelPrice) float64 {
	perM := func(tokens int64, rate float64) float64 {
		return float64(tokens) / 1_000_000 * rate
	}
	return perM(s.InTok, p.in) +
		perM(s.OutTok, p.out) +
		perM(s.CacheReadT, p.in*cacheReadMult) +
		perM(s.CacheWriteT, p.in*cacheWriteMult)
}

// tokenLine renders the detail-pane token/cost summary, e.g.
// "1.6M in · 12k out · $2.14". Empty when the session recorded no usage.
func tokenLine(s Session) string {
	total := s.InTok + s.OutTok + s.CacheReadT + s.CacheWriteT
	if total == 0 {
		return ""
	}
	// "in" here is all input the model saw: fresh + cache read + cache write.
	inAll := s.InTok + s.CacheReadT + s.CacheWriteT
	parts := []string{humanCount(inAll) + " in", humanCount(s.OutTok) + " out"}
	if p, ok := priceFor(s.Model); ok {
		parts = append(parts, humanCost(sessionCost(s, p)))
	}
	return strings.Join(parts, " · ")
}

// humanCount abbreviates a token count: 1234 → "1.2k", 1_600_000 → "1.6M".
func humanCount(n int64) string {
	switch {
	case n >= 1_000_000:
		return fmt.Sprintf("%.1fM", float64(n)/1_000_000)
	case n >= 1_000:
		return fmt.Sprintf("%.1fk", float64(n)/1_000)
	default:
		return fmt.Sprintf("%d", n)
	}
}

// humanCost formats a USD figure: sub-cent as "<$0.01", else "$X.XX".
func humanCost(c float64) string {
	if c > 0 && c < 0.01 {
		return "<$0.01"
	}
	return fmt.Sprintf("$%.2f", c)
}

// humanSize abbreviates a byte count: 900 → "900 B", 5300 → "5.2 KB",
// 4_600_000 → "4.4 MB".
func humanSize(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for m := n / unit; m >= unit; m /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(n)/float64(div), "KMGTPE"[exp])
}
