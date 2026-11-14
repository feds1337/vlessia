package update

import (
	"bytes"
	"strings"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/extension"
)

var releaseMarkdown = goldmark.New(goldmark.WithExtensions(extension.GFM))

// renderNotes converts GitHub release Markdown with raw HTML disabled.
func renderNotes(markdown string) string {
	markdown = strings.TrimSpace(markdown)
	if markdown == "" {
		return ""
	}
	var html bytes.Buffer
	if err := releaseMarkdown.Convert([]byte(markdown), &html); err != nil {
		return ""
	}
	return html.String()
}
