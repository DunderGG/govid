// url_input.go — Faster ways to fill the URL field.
//
// Responsibilities:
//   - parseURLList, looksLikeURL, shortcutURL, mergeURLs: the pure helpers
//     that read URLs from a text list or an internet shortcut and add them
//     to the field without duplicates.
//   - UIManager.showLoadURLFile / loadURLList: the "Load from file…" button,
//     which reads a .txt list (one URL per line; blank lines and # comments
//     are skipped).
//   - UIManager.pasteURLs: the paste button, which takes the clipboard only
//     when every line of it is a URL.
//   - UIManager.handleDrop: files dropped onto the window. A .txt file is
//     loaded as a list, and a .url (Windows) or .desktop (Linux) internet
//     shortcut adds its URL.
//
// Links dragged straight from a browser do not arrive on Windows: GLFW only
// accepts dropped files there (WM_DROPFILES), and browsers offer a link as
// text. Dragging the link to the desktop first makes a .url shortcut, which
// can then be dropped.
package main

import (
	"bufio"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/storage"
)

// maxURLListBytes bounds how much of a URL list file is read, so picking a
// large file by mistake cannot exhaust memory.
const maxURLListBytes = 4 << 20

// maxListedLines is how many line numbers of skipped lines the log names.
const maxListedLines = 5

// urlList is what parseURLList found in a text list of URLs.
type urlList struct {
	urls         []string // the URLs, in order, without repeats
	comments     int      // lines starting with #
	invalidLines []int    // 1-based numbers of lines that are not URLs
	repeats      int      // URLs listed more than once
}

// isURLComment reports whether a trimmed line of a URL list is a comment.
// collectURLs skips the same lines in batch mode.
func isURLComment(line string) bool {
	return strings.HasPrefix(line, "#")
}

// looksLikeURL reports whether text is an http(s) URL with a host.
func looksLikeURL(text string) bool {
	parsed, err := url.Parse(text)
	if err != nil {
		return false
	}
	return (parsed.Scheme == "http" || parsed.Scheme == "https") && parsed.Host != ""
}

// parseURLList reads a list of URLs, one per line. Blank lines and comments
// are skipped, and so is any other line that is not a URL.
func parseURLList(text string) urlList {
	var list urlList
	for i, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		switch {
		case line == "":
		case isURLComment(line):
			list.comments++
		case !looksLikeURL(line):
			list.invalidLines = append(list.invalidLines, i+1)
		case slices.Contains(list.urls, line):
			list.repeats++
		default:
			list.urls = append(list.urls, line)
		}
	}
	return list
}

// skipSummary describes the lines parseURLList skipped, e.g. "2 comment
// lines, 1 line that is not a URL (line 7)", or "" when it skipped none.
func (list urlList) skipSummary() string {
	var parts []string
	if list.comments > 0 {
		parts = append(parts, plural(list.comments, "comment line", "comment lines"))
	}
	if n := len(list.invalidLines); n > 0 {
		var numbers []string
		for _, line := range list.invalidLines[:min(n, maxListedLines)] {
			numbers = append(numbers, strconv.Itoa(line))
		}
		if n > maxListedLines {
			numbers = append(numbers, "…")
		}
		noun := "line"
		if n > 1 {
			noun = "lines"
		}
		parts = append(parts, fmt.Sprintf("%s (%s %s)",
			plural(n, "line that is not a URL", "lines that are not URLs"), noun, strings.Join(numbers, ", ")))
	}
	if list.repeats > 0 {
		parts = append(parts, plural(list.repeats, "repeated URL", "repeated URLs"))
	}
	return strings.Join(parts, ", ")
}

// plural formats n with the singular or plural noun, e.g. "1 file", "2 files".
func plural(n int, singular, pluralForm string) string {
	if n == 1 {
		return fmt.Sprintf("%d %s", n, singular)
	}
	return fmt.Sprintf("%d %s", n, pluralForm)
}

// shortcutURL returns the URL an internet shortcut points at: the URL= line
// of a Windows .url file or a Linux .desktop link.
func shortcutURL(text string) (string, bool) {
	scanner := bufio.NewScanner(strings.NewReader(text))
	for scanner.Scan() {
		key, value, found := strings.Cut(strings.TrimSpace(scanner.Text()), "=")
		if found && strings.EqualFold(strings.TrimSpace(key), "URL") {
			value = strings.TrimSpace(value)
			return value, looksLikeURL(value)
		}
	}
	return "", false
}

// mergeURLs appends urls to the URL field's text, one per line, leaving out
// those already in it. It returns the new text and how many were added.
func mergeURLs(fieldText string, urls []string) (text string, added int) {
	var lines []string
	for _, line := range strings.Split(fieldText, "\n") {
		if line = strings.TrimSpace(line); line != "" {
			lines = append(lines, line)
		}
	}
	for _, u := range urls {
		if !slices.Contains(lines, u) {
			lines = append(lines, u)
			added++
		}
	}
	return strings.Join(lines, "\n"), added
}

// addURLs puts urls into the URL field without repeating any already there,
// and returns how many were added. Batch mode is switched on when the field
// would then hold more than one URL. Must be called on the UI thread.
func (manager *UIManager) addURLs(urls []string) int {
	download := manager.ui.download
	current := download.entry.Text
	if !download.batchMode.Checked {
		current = strings.TrimSpace(current)
	}
	text, added := mergeURLs(current, urls)
	if strings.Contains(text, "\n") && !download.batchMode.Checked {
		download.batchMode.SetChecked(true)
	}
	download.entry.SetText(text)
	return added
}

// showLoadURLFile opens the "Load from file" picker for a .txt list of URLs.
func (manager *UIManager) showLoadURLFile() {
	picker := dialog.NewFileOpen(func(reader fyne.URIReadCloser, err error) {
		if err != nil {
			dialog.ShowError(err, manager.mainWindow)
			return
		}
		if reader == nil {
			return // cancelled
		}
		defer reader.Close()
		manager.loadURLList(reader.URI().Name(), reader)
	}, manager.mainWindow)
	picker.SetFilter(storage.NewExtensionFileFilter([]string{".txt"}))
	picker.Show()
}

// loadURLList adds the URLs of a text list read from r to the URL field and
// logs what was added and skipped. name is the list's file name, for the
// log. Must be called on the UI thread.
func (manager *UIManager) loadURLList(name string, r io.Reader) {
	data, err := io.ReadAll(io.LimitReader(r, maxURLListBytes))
	if err != nil {
		dialog.ShowError(fmt.Errorf("could not read %s: %w", name, err), manager.mainWindow)
		return
	}
	list := parseURLList(string(data))
	added := manager.addURLs(list.urls)

	message := fmt.Sprintf("[SYSTEM] %s: added %s", name, plural(added, "URL", "URLs"))
	if already := len(list.urls) - added; already > 0 {
		message += fmt.Sprintf(" (%d already in the list)", already)
	}
	if skipped := list.skipSummary(); skipped != "" {
		message += "; skipped " + skipped
	}
	col := colSystem
	if len(list.invalidLines) > 0 {
		col = colWarning
	}
	manager.onLog(message+".", col)
}

// pasteURLs adds the clipboard's URLs to the URL field. It refuses text in
// which any non-blank line is not a URL, so a stray paste of something else
// does not land in the queue. Must be called on the UI thread.
func (manager *UIManager) pasteURLs() {
	content := fyne.CurrentApp().Clipboard().Content()
	var urls []string
	for _, line := range strings.Split(content, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if !looksLikeURL(line) {
			dialog.ShowInformation("Paste URLs", "The clipboard does not hold a URL. Copy one or more http(s) links, one per line, and try again.", manager.mainWindow)
			return
		}
		urls = append(urls, line)
	}
	if len(urls) == 0 {
		dialog.ShowInformation("Paste URLs", "The clipboard is empty.", manager.mainWindow)
		return
	}
	manager.addURLs(urls)
}

// handleDrop adds the URLs in files dropped onto the main window: a .txt
// list, a .url or .desktop internet shortcut, or an http(s) link some
// platforms deliver as a file path. Anything else is logged and ignored.
// Must be called on the UI thread.
func (manager *UIManager) handleDrop(items []fyne.URI) {
	for _, item := range items {
		path := item.Path()
		if looksLikeURL(path) {
			manager.addURLs([]string{path})
			continue
		}
		switch strings.ToLower(filepath.Ext(path)) {
		case ".txt":
			manager.loadDroppedList(path)
		case ".url", ".desktop":
			manager.addShortcut(path)
		default:
			manager.onLog(fmt.Sprintf("[SYSTEM] Ignored %s: drop a .txt list of URLs or an internet shortcut (.url).", item.Name()), colWarning)
		}
	}
}

// loadDroppedList loads a dropped .txt file as a list of URLs.
func (manager *UIManager) loadDroppedList(path string) {
	file, err := os.Open(path)
	if err != nil {
		manager.onLog(fmt.Sprintf("[SYSTEM] Could not read %s: %v", filepath.Base(path), err), colWarning)
		return
	}
	defer file.Close()
	manager.loadURLList(filepath.Base(path), file)
}

// addShortcut adds the URL of a dropped internet shortcut.
func (manager *UIManager) addShortcut(path string) {
	data, err := os.ReadFile(path)
	if err != nil {
		manager.onLog(fmt.Sprintf("[SYSTEM] Could not read %s: %v", filepath.Base(path), err), colWarning)
		return
	}
	link, ok := shortcutURL(string(data))
	if !ok {
		manager.onLog(fmt.Sprintf("[SYSTEM] Ignored %s: it does not point at an http(s) URL.", filepath.Base(path)), colWarning)
		return
	}
	manager.addURLs([]string{link})
}
