package main

import (
	"strings"
	"testing"
)

// A pushed snapshot can come from anyone holding a leaked API key, so raw
// HTML in it must never reach the page as markup.
func TestRenderMarkdownCodeBlockIsInert(t *testing.T) {
	in := "Before.\n\n```html\n<script>alert(1)</script>\n<img src=x onerror=alert(2)>\n```\n"
	out := string(renderMarkdown(in))

	if strings.Contains(out, "<script") || strings.Contains(out, "<img") {
		t.Fatalf("raw HTML survived as markup:\n%s", out)
	}
	if !strings.Contains(out, "&lt;script&gt;alert(1)&lt;/script&gt;") {
		t.Fatalf("code block should show the script as readable text:\n%s", out)
	}
	if strings.Contains(out, "&amp;lt;") {
		t.Fatalf("code block is double-escaped:\n%s", out)
	}
	if !strings.Contains(out, "<pre><code") {
		t.Fatalf("code block lost its <pre><code> wrapper:\n%s", out)
	}
}

func TestRenderMarkdownStripsActiveContent(t *testing.T) {
	for _, in := range []string{
		"<script>alert(1)</script>",
		"[x](javascript:alert(1))",
		"<a href=\"https://example.com\" onclick=\"alert(1)\">x</a>",
		"<iframe src=\"https://example.com\"></iframe>",
	} {
		out := strings.ToLower(string(renderMarkdown(in)) + string(renderInline(in)))
		for _, bad := range []string{"<script", "javascript:", "onclick", "<iframe"} {
			if strings.Contains(out, bad) {
				t.Errorf("%q rendered with %q:\n%s", in, bad, out)
			}
		}
	}
}
