package main

import "testing"

// TestContentTextPrefilter covers the byte-prefilter fast path in contentText:
// tool-only blocks skip the decode, real text blocks extract, and a false
// positive (the substring "text" living outside a text block) still decodes
// correctly rather than returning wrong data.
func TestContentTextPrefilter(t *testing.T) {
	cases := []struct {
		name string
		raw  string
		want string
	}{
		{"bare string", `"hello world"`, "hello world"},
		{"text block", `[{"type":"text","text":"a"},{"type":"text","text":"b"}]`, "a b"},
		{"tool only, no text key, prefilter skips", `[{"type":"tool_use","name":"Bash","input":{"cmd":"ls"}}]`, ""},
		{"tool_result only", `[{"type":"tool_result","content":"stdout here"}]`, ""},
		{
			// "text" appears inside another key/value, but there is no real text
			// block: prefilter lets it through, decode returns "".
			"false positive: text substring in tool input",
			`[{"type":"tool_use","name":"edit","input":{"note":"add text","other":"context"}}]`,
			"",
		},
		{
			// mixed: a tool block plus a genuine text block; only the text extracts.
			"mixed tool + text",
			`[{"type":"tool_use","name":"Bash","input":{"c":"x"}},{"type":"text","text":"done"}]`,
			"done",
		},
		{"empty", ``, ""},
		{"empty array", `[]`, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := contentText([]byte(c.raw)); got != c.want {
				t.Errorf("contentText(%s) = %q, want %q", c.raw, got, c.want)
			}
		})
	}
}

// TestContentTextSanitizesViaPrefilterPath proves the prefilter doesn't bypass
// sanitize: a text block carrying control/ANSI bytes still gets stripped. The
// JSON stores the ESC via a unicode escape (raw control bytes aren't legal
// in a JSON string); it decodes to a CSI clear-screen that must be stripped.
func TestContentTextSanitizesViaPrefilterPath(t *testing.T) {
	// The \\u001b in this Go interpreted string is passed to the JSON decoder
	// as a unicode escape, not a raw ESC byte; contentText must strip the ESC.
	raw := "[{\"type\":\"text\",\"text\":\"a\\u001b[2Jb\"}]"
	if got := contentText([]byte(raw)); got != "ab" {
		t.Errorf("contentText = %q, want %q (must sanitize)", got, "ab")
	}
}
