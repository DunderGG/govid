// guide_html.go — The GoVid Guide as a web page, for "Open in browser".
//
// Responsibilities:
//   - guideHTML: the guide's topics (helpItems) as one standalone HTML page,
//     with a list of contents and a link to the full user guide. A browser
//     gives the guide what Fyne's RichText cannot: selecting and copying
//     text, and its own find.
//   - UIManager.openGuideInBrowser: writes the page to the temp folder and
//     opens it in the default browser.
package main

import (
	"bytes"
	"fmt"
	"html"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"unicode"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/dialog"
	"github.com/yuin/goldmark"
)

// userGuideURL is the full user guide, docs/user-guide.md, on GitHub.
const userGuideURL = "https://github.com/" + govidOwner + "/" + govidRepo + "/blob/main/docs/user-guide.md"

// guideIntro opens the guide, in the window and in the browser: what it
// covers, and where the rest is.
const guideIntro = "This guide explains each of GoVid's settings and windows. The [full user guide](" + userGuideURL + ") also covers getting started, troubleshooting, the command line, and reporting bugs."

// guidePageStyle is the page's CSS, light or dark as the system is.
const guidePageStyle = `
:root { --bg: #ffffff; --fg: #1f2328; --muted: #59636e; --accent: #0b7fa3; --code-bg: #eff2f5; --rule: #d1d9e0; }
@media (prefers-color-scheme: dark) {
  :root { --bg: #121218; --fg: #e6e6f0; --muted: #9a9aae; --accent: #1c9bbe; --code-bg: #1e1e28; --rule: #2d2d3c; }
}
body { margin: 0; background: var(--bg); color: var(--fg); font: 16px/1.6 system-ui, -apple-system, "Segoe UI", sans-serif; }
main { max-width: 760px; margin: 0 auto; padding: 32px 16px 64px; }
h1 { margin: 0 0 4px; font-size: 28px; }
.version { margin: 0 0 16px; color: var(--muted); }
h2 { margin: 40px 0 8px; padding-top: 16px; border-top: 1px solid var(--rule); font-size: 21px; }
a { color: var(--accent); }
strong { color: var(--accent); }
code { padding: 1px 5px; border-radius: 4px; background: var(--code-bg); font: 14px ui-monospace, Consolas, monospace; }
nav ol { columns: 2; column-gap: 32px; padding-left: 20px; }
nav li { break-inside: avoid; }
@media (max-width: 560px) { nav ol { columns: 1; } }
`

// guideHTML returns items as one HTML page, headed with GoVid's version,
// guideIntro, and a list of contents linking to each topic.
func guideHTML(items []helpItem, version string) ([]byte, error) {
	markdown := goldmark.New()
	var page, contents, topics bytes.Buffer
	ids := map[string]int{}
	for _, item := range items {
		id := topicID(item.label, ids)
		label := html.EscapeString(item.label)
		fmt.Fprintf(&contents, "<li><a href=\"#%s\">%s</a></li>\n", id, label)
		fmt.Fprintf(&topics, "<h2 id=\"%s\">%s</h2>\n", id, label)
		if err := markdown.Convert([]byte(item.desc), &topics); err != nil {
			return nil, fmt.Errorf("rendering %q: %w", item.label, err)
		}
	}
	var intro bytes.Buffer
	if err := markdown.Convert([]byte(guideIntro), &intro); err != nil {
		return nil, fmt.Errorf("rendering the introduction: %w", err)
	}

	fmt.Fprintf(&page, "<!doctype html>\n<html lang=\"en\">\n<head>\n<meta charset=\"utf-8\">\n"+
		"<meta name=\"viewport\" content=\"width=device-width, initial-scale=1\">\n"+
		"<title>GoVid Guide</title>\n<style>%s</style>\n</head>\n<body>\n<main>\n"+
		"<h1>GoVid Guide</h1>\n<p class=\"version\">GoVid %s</p>\n%s"+
		"<nav>\n<ol>\n%s</ol>\n</nav>\n%s</main>\n</body>\n</html>\n",
		guidePageStyle, html.EscapeString(version), intro.String(), contents.String(), topics.String())
	return page.Bytes(), nil
}

// topicID returns an HTML id for a topic's label, such as "trim-start-trim-end",
// unique among the ids seen so far (which it records in seen).
func topicID(label string, seen map[string]int) string {
	id := strings.Join(strings.FieldsFunc(strings.ToLower(label), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	}), "-")
	if id == "" {
		id = "topic"
	}
	seen[id]++
	if n := seen[id]; n > 1 {
		id = fmt.Sprintf("%s-%d", id, n)
	}
	return id
}

// openGuideInBrowser writes the guide as a web page to the temp folder,
// replacing the one written before, and opens it in the default browser.
// Errors are shown over the guide window.
func (manager *UIManager) openGuideInBrowser() {
	path := filepath.Join(os.TempDir(), "GoVid_Guide.html")
	page, err := guideHTML(helpItems, version)
	if err == nil {
		err = os.WriteFile(path, page, 0o644)
	}
	if err == nil {
		err = fyne.CurrentApp().OpenURL(fileURL(path))
	}
	if err != nil {
		dialog.ShowError(fmt.Errorf("could not open the guide in the browser: %w", err), manager.helpWindow)
	}
}

// fileURL returns the file:// URL of the absolute path, e.g.
// file:///C:/Users/me/GoVid_Guide.html.
func fileURL(path string) *url.URL {
	slashed := filepath.ToSlash(path)
	if !strings.HasPrefix(slashed, "/") {
		slashed = "/" + slashed
	}
	return &url.URL{Scheme: "file", Path: slashed}
}
