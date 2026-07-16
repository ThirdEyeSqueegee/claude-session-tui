package main

import "testing"

func TestHumanSize(t *testing.T) {
	cases := map[int64]string{
		0:         "0 B",
		900:       "900 B",
		1024:      "1.0 KB",
		5300:      "5.2 KB",
		4_600_000: "4.4 MB",
	}
	for in, want := range cases {
		if got := humanSize(in); got != want {
			t.Errorf("humanSize(%d) = %q, want %q", in, got, want)
		}
	}
}

func TestHumanCount(t *testing.T) {
	cases := map[int64]string{
		0:         "0",
		999:       "999",
		1200:      "1.2k",
		1_600_000: "1.6M",
	}
	for in, want := range cases {
		if got := humanCount(in); got != want {
			t.Errorf("humanCount(%d) = %q, want %q", in, got, want)
		}
	}
}

func TestPriceForLongestPrefix(t *testing.T) {
	// opus-4-8 must match the "claude-opus-4" tier, not a shorter/other one.
	p, ok := priceFor("claude-opus-4-8")
	if !ok || p.in != 5 || p.out != 25 {
		t.Errorf("opus-4-8 price = %+v ok=%v, want {5 25}", p, ok)
	}
	if p, ok := priceFor("claude-haiku-3-5-20241022"); !ok || p.in != 0.8 {
		t.Errorf("haiku-3-5 longest-prefix = %+v ok=%v, want in=0.8", p, ok)
	}
	if _, ok := priceFor("some-unknown-model"); ok {
		t.Error("unknown model should not match a price")
	}
}

// TestSessionCost checks the cache-multiplier math against a hand-computed value.
func TestSessionCost(t *testing.T) {
	s := Session{
		Model:       "claude-opus-4-8",
		InTok:       1_000_000, // $5.00 at $5/M
		OutTok:      1_000_000, // $25.00 at $25/M
		CacheReadT:  1_000_000, // $0.50 at 0.1×$5
		CacheWriteT: 1_000_000, // $6.25 at 1.25×$5
	}
	p, _ := priceFor(s.Model)
	got := sessionCost(s, p)
	want := 5.0 + 25.0 + 0.5 + 6.25
	if got != want {
		t.Errorf("sessionCost = %v, want %v", got, want)
	}
}

func TestTokenLineEmptyWhenNoUsage(t *testing.T) {
	if got := tokenLine(Session{}); got != "" {
		t.Errorf("tokenLine with no usage = %q, want empty", got)
	}
	// with usage but an unknown model: tokens shown, no dollar figure
	s := Session{Model: "mystery", InTok: 2000, OutTok: 500}
	got := tokenLine(s)
	if got == "" || containsDollar(got) {
		t.Errorf("tokenLine(unknown model) = %q, want tokens without a cost", got)
	}
}

func containsDollar(s string) bool {
	for _, r := range s {
		if r == '$' {
			return true
		}
	}
	return false
}
