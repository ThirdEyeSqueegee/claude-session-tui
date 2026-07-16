package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// benchTranscript writes a realistic transcript: a mix of small metadata lines
// and fat assistant turns (some text, some tool-only) so the benchmark reflects
// the real cost of parseSession, not a toy.
func benchTranscript(tb testing.TB) string {
	tb.Helper()
	dir := tb.TempDir()
	id := "11111111-1111-1111-1111-111111111111"
	p := filepath.Join(dir, id+".jsonl")

	var b strings.Builder
	bigText := strings.Repeat("lorem ipsum dolor sit amet ", 80)    // ~2KB text block
	bigTool := strings.Repeat(`{"k":"vvvvvvvvvvvvvvvvvvvv"},`, 200) // ~5KB tool-only content
	for i := range 120 {
		ts := fmt.Sprintf("2026-07-10T12:00:%02dZ", i%60)
		switch i % 4 {
		case 0: // user
			fmt.Fprintf(&b, `{"type":"user","cwd":"/work/demo","gitBranch":"main","timestamp":%q,"message":{"content":"question %d"}}`+"\n", ts, i)
		case 1: // assistant with text
			fmt.Fprintf(&b, `{"type":"assistant","cwd":"/work/demo","gitBranch":"main","timestamp":%q,"message":{"model":"claude-opus-4-8","content":[{"type":"text","text":%q}],"usage":{"input_tokens":100,"output_tokens":50,"cache_read_input_tokens":10,"cache_creation_input_tokens":5}}}`+"\n", ts, bigText)
		case 2: // assistant tool-only (no text block — prefilter should skip)
			fmt.Fprintf(&b, `{"type":"assistant","cwd":"/work/demo","gitBranch":"main","timestamp":%q,"message":{"model":"claude-opus-4-8","content":[{"type":"tool_use","id":"t","name":"Bash","input":[`+bigTool+`null]}],"usage":{"input_tokens":200,"output_tokens":5}}}`+"\n", ts)
		default: // small metadata line
			fmt.Fprintf(&b, `{"type":"mode","cwd":"/work/demo","timestamp":%q,"mode":"default"}`+"\n", ts)
		}
	}
	if err := os.WriteFile(p, []byte(b.String()), 0o644); err != nil {
		tb.Fatal(err)
	}
	return p
}

func BenchmarkParseSession(b *testing.B) {
	p := benchTranscript(b)
	b.ReportAllocs()
	for b.Loop() {
		if _, ok := parseSession(p); !ok {
			b.Fatal("parse failed")
		}
	}
}
