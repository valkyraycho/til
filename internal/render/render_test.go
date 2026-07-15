package render

import (
	"strings"
	"testing"
)

func TestMarkdown(t *testing.T) {
	tests := []struct {
		name        string
		in          string
		wantContain string
		wantAbsent  string
	}{
		{"heading", "# Title", "<h1>Title</h1>", ""},
		{"code block", "```go\nfmt.Println(1)\n```", "<code class=\"language-go\"", ""},
		{"link", "[docs](https://example.com)", `<a href="https://example.com"`, ""},
		{"gfm table", "| a | b |\n|---|---|\n| 1 | 2 |", "<table>", ""},
		{"raw html stripped", "<script>alert(1)</script>", "", "<script>"},
		{"javascript url neutralized", "[x](javascript:alert(1))", "", "javascript:alert(1)"},
		{"img onerror stripped", `<img src=x onerror=alert(1)>`, "", "onerror"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Markdown(tt.in)
			if err != nil {
				t.Fatalf("Markdown: %v", err)
			}
			html := string(got)
			if tt.wantContain != "" && !strings.Contains(html, tt.wantContain) {
				t.Errorf("output %q missing %q", html, tt.wantContain)
			}
			if tt.wantAbsent != "" && strings.Contains(html, tt.wantAbsent) {
				t.Errorf("output %q must not contain %q", html, tt.wantAbsent)
			}
		})
	}
}
