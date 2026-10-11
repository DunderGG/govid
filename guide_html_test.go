package main

import (
	"runtime"
	"strings"
	"testing"
)

func TestGuideHTML(t *testing.T) {
	items := []helpItem{
		{"Trim Start / Trim End", "Use **Trim Start**:\n  * `HH:MM:SS`\n  * `<script>`"},
		{"Cookies", "Plain text."},
		{"Cookies", "A second topic of the same name."},
	}
	page, err := guideHTML(items, "1.2.3 <dev>")
	if err != nil {
		t.Fatal(err)
	}
	text := string(page)
	for _, want := range []string{
		"<title>GoVid Guide</title>",
		"GoVid 1.2.3 &lt;dev&gt;",
		`<a href="` + userGuideURL + `">full user guide</a>`,
		`<li><a href="#trim-start-trim-end">Trim Start / Trim End</a></li>`,
		`<h2 id="trim-start-trim-end">Trim Start / Trim End</h2>`,
		`<h2 id="cookies">Cookies</h2>`,
		`<h2 id="cookies-2">Cookies</h2>`,
		"<strong>Trim Start</strong>",
		"<li><code>HH:MM:SS</code></li>",
		"<code>&lt;script&gt;</code>",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("page lacks %q", want)
		}
	}
}

func TestGuideHTMLRendersEveryTopic(t *testing.T) {
	page, err := guideHTML(helpItems, "dev")
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Count(string(page), "<h2 id="); got != len(helpItems) {
		t.Errorf("page has %d topics, want %d", got, len(helpItems))
	}
}

func TestFileURL(t *testing.T) {
	cases := map[string]string{
		`C:\Users\me\AppData\Local\Temp\GoVid_Guide.html`: "file:///C:/Users/me/AppData/Local/Temp/GoVid_Guide.html",
		"/tmp/GoVid Guide.html":                           "file:///tmp/GoVid%20Guide.html",
	}
	for path, want := range cases {
		if strings.HasPrefix(path, `C:\`) && runtime.GOOS != "windows" {
			continue // filepath.ToSlash only converts backslashes on Windows
		}
		if got := fileURL(path).String(); got != want {
			t.Errorf("fileURL(%q) = %q, want %q", path, got, want)
		}
	}
}
