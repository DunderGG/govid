package main

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/storage"
)

func TestLooksLikeURL(t *testing.T) {
	tests := []struct {
		text string
		want bool
	}{
		{"https://www.youtube.com/watch?v=abc", true},
		{"http://example.com", true},
		{"ftp://example.com/file", false},
		{"www.youtube.com/watch?v=abc", false},
		{"https://", false},
		{"just some text", false},
		{"", false},
	}
	for _, tt := range tests {
		if got := looksLikeURL(tt.text); got != tt.want {
			t.Errorf("looksLikeURL(%q) = %v, want %v", tt.text, got, tt.want)
		}
	}
}

// fiftyLineList returns a 50-line URL list with blank lines, comments, a
// repeat, and two lines that are not URLs, plus the URLs it holds.
func fiftyLineList() (text string, urls []string) {
	var lines []string
	for n := 1; len(lines) < 50; n++ {
		switch {
		case n%10 == 0:
			lines = append(lines, "")
		case n%10 == 5:
			lines = append(lines, "# section "+fmt.Sprint(n))
		case n == 13:
			lines = append(lines, "not a url")
		case n == 27:
			lines = append(lines, "  "+urls[0]+"  ") // repeat
		case n == 33:
			lines = append(lines, "www.example.com/no-scheme")
		default:
			url := fmt.Sprintf("https://example.com/v/%d", n)
			urls = append(urls, url)
			lines = append(lines, "  "+url+"\r") // CRLF file, stray spaces
		}
	}
	return strings.Join(lines, "\n"), urls
}

func TestParseURLList(t *testing.T) {
	text, wantURLs := fiftyLineList()

	list := parseURLList(text)

	if !slices.Equal(list.urls, wantURLs) {
		t.Errorf("urls = %q\nwant %q", list.urls, wantURLs)
	}
	if list.comments != 5 || list.repeats != 1 || !slices.Equal(list.invalidLines, []int{13, 33}) {
		t.Errorf("skipped = %d comments, %d repeats, invalid lines %v; want 5, 1, [13 33]", list.comments, list.repeats, list.invalidLines)
	}
	if got, want := list.skipSummary(), "5 comment lines, 2 lines that are not URLs (lines 13, 33), 1 repeated URL"; got != want {
		t.Errorf("skipSummary() = %q, want %q", got, want)
	}
}

func TestShortcutURL(t *testing.T) {
	tests := []struct {
		name   string
		text   string
		want   string
		wantOK bool
	}{
		{"windows .url", "[{000214A0-0000-0000-C000-000000000046}]\r\nProp3=19,11\r\n[InternetShortcut]\r\nIDList=\r\nURL=https://www.youtube.com/watch?v=abc\r\n", "https://www.youtube.com/watch?v=abc", true},
		{"linux .desktop", "[Desktop Entry]\nType=Link\nName=Video\nURL=https://vimeo.com/123\n", "https://vimeo.com/123", true},
		{"not http", "[InternetShortcut]\nURL=file:///C:/video.mp4\n", "file:///C:/video.mp4", false},
		{"no URL line", "[InternetShortcut]\nIDList=\n", "", false},
	}
	for _, tt := range tests {
		got, ok := shortcutURL(tt.text)
		if got != tt.want || ok != tt.wantOK {
			t.Errorf("%s: shortcutURL() = %q, %v; want %q, %v", tt.name, got, ok, tt.want, tt.wantOK)
		}
	}
}

func TestMergeURLs(t *testing.T) {
	text, added := mergeURLs("https://a\n\n  https://b \n", []string{"https://b", "https://c"})
	if text != "https://a\nhttps://b\nhttps://c" || added != 1 {
		t.Errorf("mergeURLs() = %q, %d; want the new URL appended once", text, added)
	}
}

func TestLoadURLListFillsBatchField(t *testing.T) {
	h := newDownloadHarness(t, "ytdlp-download")
	h.app.ui.download.entry.SetText("https://example.com/v/1") // already in the list
	text, urls := fiftyLineList()

	h.app.uiManager.loadURLList("list.txt", strings.NewReader(text))

	if !h.app.ui.download.batchMode.Checked {
		t.Error("batch mode not switched on")
	}
	if got := strings.Split(h.app.ui.download.entry.Text, "\n"); !slices.Equal(got, urls) {
		t.Errorf("field lines = %q\nwant %q", got, urls)
	}
	want := fmt.Sprintf("[SYSTEM] list.txt: added %d URLs (1 already in the list); skipped 5 comment lines, 2 lines that are not URLs (lines 13, 33), 1 repeated URL.", len(urls)-1)
	if !strings.Contains(h.joinedLogs(), want) {
		t.Errorf("log missing %q:\n%s", want, h.joinedLogs())
	}
}

func TestCollectURLsSkipsCommentLines(t *testing.T) {
	got, err := collectURLs("# my list\nhttps://a\n  # another\nhttps://b", true)
	if err != nil || !slices.Equal(got, []string{"https://a", "https://b"}) {
		t.Errorf("collectURLs() = %q, %v; want the two URLs", got, err)
	}
}

func TestPasteURLs(t *testing.T) {
	tests := []struct {
		name      string
		field     string
		clipboard string
		wantField string
		wantBatch bool
	}{
		{"one URL into an empty field", "", " https://a \n", "https://a", false},
		{"several URLs switch on batch mode", "", "https://a\r\nhttps://b\r\n", "https://a\nhttps://b", true},
		{"appends to a URL already there", "https://a", "https://b", "https://a\nhttps://b", true},
		{"refuses text that is not a URL", "https://a", "https://b\nhello", "https://a", false},
		{"refuses plain text", "", "hello world", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := newDownloadHarness(t, "ytdlp-download")
			h.app.ui.download.entry.SetText(tt.field)
			fyne.CurrentApp().Clipboard().SetContent(tt.clipboard)

			h.app.uiManager.pasteURLs()

			if got := h.app.ui.download.entry.Text; got != tt.wantField {
				t.Errorf("field = %q, want %q", got, tt.wantField)
			}
			if got := h.app.ui.download.batchMode.Checked; got != tt.wantBatch {
				t.Errorf("batch mode = %v, want %v", got, tt.wantBatch)
			}
			refused := tt.field == tt.wantField
			if shown := h.window.Canvas().Overlays().Top() != nil; shown != refused {
				t.Errorf("message shown = %v, want %v", shown, refused)
			}
		})
	}
}

func TestDropAddsListsAndShortcuts(t *testing.T) {
	h := newDownloadHarness(t, "ytdlp-download")
	dir := t.TempDir()
	write := func(name, content string) fyne.URI {
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
		return storage.NewFileURI(path)
	}

	h.app.uiManager.handleDrop([]fyne.URI{
		write("list.txt", "https://example.com/a\n# comment\nhttps://example.com/b\n"),
		write("Video.url", "[InternetShortcut]\r\nURL=https://example.com/c\r\n"),
		write("video.mp4", "not a list"),
	})

	if got, want := h.app.ui.download.entry.Text, "https://example.com/a\nhttps://example.com/b\nhttps://example.com/c"; got != want {
		t.Errorf("field = %q, want %q", got, want)
	}
	if !h.app.ui.download.batchMode.Checked {
		t.Error("batch mode not switched on")
	}
	if !strings.Contains(h.joinedLogs(), "Ignored video.mp4") {
		t.Errorf("log does not mention the ignored file:\n%s", h.joinedLogs())
	}
}
