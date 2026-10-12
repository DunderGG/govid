# GoVid — Fyne 2.8 Upgrade

> **Status:** on hold. GoVid stays on Fyne 2.7.3. A trial upgrade to 2.8.1 on 2026-10-12 works, but a bug in 2.8.1's text measuring makes the test suite race, crash, and hang. Retry when a Fyne release fixes it (see [§3](#3-the-blocker-walkstring-in-281)). The checklist is in the [roadmap](../roadmap.md#fyne-upgrade).
> **Audience:** contributors and maintainers.

---

## 1. What the upgrade brings

Fyne 2.8.1 fixes RichText's word wrap breaking a word into one letter per line. In 2.7.3, when a bold word, code span, or link starts with less than a letter's width left on its line, every letter of it gets a line of its own. The guide's topics show it at 230 of the widths from 300 to 700 px (Video URL, at 437 px, grows from 290 to 2573 px tall). 2.7.3's `lineBounds` never resets the line width after its one-letter fallback; 2.8.1's `wrapWordLines` moves the segment to a new line instead, and the same check finds no such widths. The bug is listed under Known Limitations in the roadmap, with its workarounds.

Nothing else in 2.8 is needed by GoVid, so the upgrade waits rather than carrying a patched Fyne (§3).

## 2. What the trial found

`go get fyne.io/fyne/v2@v2.8.1` and `go mod tidy` also move:

- goldmark from 1.7.8 to 1.8.2, which renders the guide's web page (`guide_html.go`);
- GLFW from 3.3 to 3.4 (`github.com/go-gl/glfw/v3.4/glfw`), which handles windows, input, monitors, and drag and drop;
- go-text/typesetting from 0.3.3 to 0.3.4, and other indirect dependencies.

The upgrade builds, passes `go vet`, and `staticcheck -checks SA1019` finds no deprecated calls.

### Tests that 2.8.1 broke, already fixed

These fixes work on 2.7.3 and 2.8.1 alike, so they are in the code now:

- **Shortcut tests** panicked: they cast the test window's canvas to `fyne.Shortcutable`. 2.8.1's test canvas wraps a software canvas in an interface field, which hides its `TypedShortcut`. `shortcutTarget` in `shortcuts_test.go` uses the canvas itself when it takes shortcuts (2.7.3) and otherwise the wrapped canvas (2.8.1). `TestCtrlTTogglesTheTerminal` in `view_test.go` uses it too.
- **`TestHighlightMatchesKeepsLineEnds`** built its input from `### Video URL`. In 2.7.3 a heading is one segment that ends its line; in 2.8.1 it is inline text followed by an empty segment that ends the line (`###` is still bold body text). A code block is still one segment that ends its line, in both versions, so the test now uses one.

### Changes in 2.8 that touch GoVid

From Fyne's changelogs for 2.8.0 and 2.8.1, to check by hand after the upgrade:

- **Text layout was rewritten** (wrapping and truncation): the log view, Queue rows, the History list, the Info… window, dialogs, the guide window while resizing it, and the main window at small sizes. "Fix inconsistent vertical positioning of mixed-font text lines" also lands in 2.8.1.
- **Markdown in RichText** now keeps inline styles in headings and supports nested lists, tables, checkboxes, and strikethrough. The guide's find bar (`guide_search.go`) walks the segments; it relies on RichText laying out a zero-size inline segment (`matchMark`) where the match starts, and on positions set by layout to scroll there (`scrollToMark` in `help_window.go`).
- **`canvas.Circle`** is now always a circle, centred in its space: the status dot (`statusDot` in `types.go`).
- **Popups have no padding** by default: the presets menu (`preset_ui.go`).
- **The `fyne.Do` migration warning**: apps that have not declared the migration print a warning on launch. The warning reads `Migrations["fyneDo"]` from the app's metadata (`FyneApp.toml`), which GoVid does not have, although it uses `fyne.Do` throughout. Do not build with the `migrated_fynedo` tag: it turns Fyne's internal locks, `runBufferMut` among them, into no-ops.
- **GLFW 3.4**: the Fyne 2.7 behaviours behind [DD-42](design_decisions.md#dd-42-dropping-links-from-a-browser) (dropped links), [DD-43](design_decisions.md#dd-43-f1-and-esc-outside-text-fields) (F1 and Esc), and [DD-45](design_decisions.md#dd-45-fyne-dialog-and-scrolling-quirks) (dialog and scrolling quirks) need testing again, as does whether RichText can now be selected (the guide-selection item in the roadmap).

## 3. The blocker: `walkString` in 2.8.1

On 2.8.1, `go test -race ./...` reported 184 races, all in `go-text/typesetting` (harfbuzz), then panicked with an index out of range inside harfbuzz. Without `-race` the suite hung until the 10-minute timeout. On 2.7.3 the same suite passes with no races.

**Cause.** 2.7.3 created a new `HarfbuzzShaper` for each text measurement. 2.8.1 shares one package-level `shaper` in `internal/painter/font.go`, guarded by `runBufferMut`, but `walkString` looks up a font face (`faces.ResolveFace(' ')`) and shapes a space (`shaper.Shape(in)`) before it takes the lock. Two goroutines measuring text at once corrupt the shaper's buffer and the fonts' glyph caches. The panic that follows happens while the lock is held, and `walkString` does not unlock with `defer`, so every later text measurement waits forever: that is the hang.

**Why the tests meet it and the app does not.** The test driver runs `fyne.Do` on the calling goroutine ([DD-48](design_decisions.md#dd-48-the-fyne-test-driver-runs-fynedo-inline)), so download sessions, notices, and the status throttle's timer measure text at the same time as the test goroutine. Every one of the 184 races came through `fyne.Do` or from the test goroutine; none touched GoVid's own state. In the app, the GLFW driver runs all `fyne.Do` work on the UI thread, so measurements never overlap. But CI runs `go test -race`, so the upgrade cannot go in while the bug is there.

**How to tell a release has fixed it.** Read `walkString` in the release's `internal/painter/font.go`. Fixed means the lock (or whatever replaces it) is taken before the first `ResolveFace` and `shaper.Shape`, and released with `defer`, or that the shaper is no longer shared. Then run `go test -race -count=2 ./...` on the upgrade: it must report no races in `go-text/typesetting`. Two runs, because the race depends on timing.

### The patch, if we ever carry it

The trial confirmed this fix: with it, `go test -race -count=2 ./...` passed with no races. Carrying it means building against a patched copy of Fyne through a `replace` in `go.mod`, either a fork or a copy in the repo (12 MB, 761 files without Fyne's tests and `cmd/`). That cost is why GoVid waits instead (2026-10-12).

```diff
--- a/internal/painter/font.go
+++ b/internal/painter/font.go
@@ -287,6 +287,16 @@
 func walkString(faces shaping.Fontmap, s string, textSize fixed.Int26_6, style fyne.TextStyle, advance *float32, scale float32,
 	cb func(run shaping.Output, x, y float32),
 ) (size fyne.Size, base float32) {
+	// The shared shaper and the font faces' glyph caches are not safe for
+	// concurrent use, so hold the lock before the first face lookup and
+	// Shape call, and release it on a panic too.
+	runBufferMut.Lock()
+	defer func() {
+		clear(runBuffer)
+		runBuffer = runBuffer[:0]
+		runBufferMut.Unlock()
+	}()
+
 	s = strings.ReplaceAll(s, "\r", "")
 
 	runes := []rune(s)
@@ -320,7 +330,6 @@
 	}
 
 	ins := splitEmojiSequences(in, faces, segmenter)
-	runBufferMut.Lock()
 	for _, in := range ins {
 		inEnd := in.RunEnd
 
@@ -348,9 +357,6 @@
 	for _, run := range runBuffer {
 		cb(run.out, run.x, y)
 	}
-	clear(runBuffer)
-	runBuffer = runBuffer[:0]
-	runBufferMut.Unlock()
 
 	*advance = x
 	return fyne.NewSize(*advance, fixed266ToFloat32(out.LineBounds.LineThickness())),
```

Taking the lock only before the first `shaper.Shape` is not enough: the trial still saw races in the glyph cache through the `ResolveFace` call above it.
