package update

import (
	"strings"
	"testing"
)

func TestRenderReleaseNotes(t *testing.T) {
	notes := "## Changes\n\n- **Faster** and *clearer*.\n- Use `code`.\n\n" +
		"[Details](https://github.com/feds1337/vlessia)\n\n" +
		"<script>notRendered()</script>\n\n[Invalid](javascript:alert%281%29)"
	html := renderNotes(notes)
	for _, want := range []string{"<h2>Changes</h2>", "<ul>", "<strong>Faster</strong>", "<em>clearer</em>", "<code>code</code>", "href=\"https://github.com/feds1337/vlessia\""} {
		if !strings.Contains(html, want) {
			t.Errorf("missing %q in %s", want, html)
		}
	}
	if strings.Contains(html, "<script>") || strings.Contains(html, "href=\"javascript:") {
		t.Fatalf("unsafe HTML: %s", html)
	}
	if renderNotes(" \n ") != "" {
		t.Fatal("blank release notes should be empty")
	}
}
