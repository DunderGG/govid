package main

import (
	"slices"
	"strconv"
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/widget"
)

func TestFindMatches(t *testing.T) {
	texts := [][]rune{foldRunes("Batch Mode and batch"), foldRunes("aaaa"), foldRunes("BATCH")}
	cases := []struct {
		query string
		want  []guideMatch
	}{
		{"", nil},
		{"batch", []guideMatch{{0, 0, 5}, {0, 15, 20}, {2, 0, 5}}},
		{"MODE A", []guideMatch{{0, 6, 12}}},
		{"aa", []guideMatch{{1, 0, 2}, {1, 2, 4}}}, // matches do not overlap
		{"zzz", nil},
	}
	for _, tc := range cases {
		if got := findMatches(texts, tc.query); !slices.Equal(got, tc.want) {
			t.Errorf("findMatches(%q) = %v, want %v", tc.query, got, tc.want)
		}
	}
}

func TestFoldedText(t *testing.T) {
	segs := helpBodySegments("Use **Batch Mode** now.\n\n  * one `X`\n  * two")
	if got, want := string(foldedText(segs)), "use batch mode now.\none x\ntwo"; got != want {
		t.Errorf("foldedText = %q, want %q", got, want)
	}
}

// textSegments returns the TextSegments of segs, in order, inside
// paragraphs and lists too.
func textSegments(segs []widget.RichTextSegment) []*widget.TextSegment {
	var out []*widget.TextSegment
	offset := 0
	walkText(segs, &offset, func(seg *widget.TextSegment, _ int) []widget.RichTextSegment {
		out = append(out, seg)
		return []widget.RichTextSegment{seg}
	})
	return out
}

func TestHighlightMatchesSplitsAcrossSegments(t *testing.T) {
	markdown := "Use **Batch Mode** now. Mode."
	folded := foldedText(helpBodySegments(markdown))
	matches := findMatches([][]rune{folded}, "mode")
	if len(matches) != 2 {
		t.Fatalf("got %d matches, want 2", len(matches))
	}
	mark := newMatchMark()

	segs := highlightMatches(helpBodySegments(markdown), matches, 1, mark)
	if got := string(foldedText(segs)); got != string(folded) {
		t.Errorf("highlighting changed the text: %q, want %q", got, string(folded))
	}
	markAt := slices.Index(segs, widget.RichTextSegment(mark))
	if markAt < 0 {
		t.Fatal("no mark before the current match")
	}
	if next := segs[markAt+1].(*widget.TextSegment); next.Text != "Mode" || next.Style.ColorName != colorNameSearchCurrent {
		t.Errorf("after the mark: %q in %q, want the current match", next.Text, next.Style.ColorName)
	}

	var highlighted []string
	for _, seg := range textSegments(segs) {
		if isHighlighted(seg) {
			highlighted = append(highlighted, seg.Text)
		}
	}
	if want := []string{"Mode", "Mode"}; !slices.Equal(highlighted, want) {
		t.Errorf("highlighted %q, want %q", highlighted, want)
	}

	// A match across a bold and a plain segment colours both pieces.
	segs = highlightMatches(helpBodySegments(markdown), findMatches([][]rune{folded}, "mode no"), 0, newMatchMark())
	highlighted = nil
	for _, seg := range textSegments(segs) {
		if seg.Style.ColorName == colorNameSearchCurrent {
			highlighted = append(highlighted, seg.Text)
		}
	}
	if want := []string{"Mode", " no"}; !slices.Equal(highlighted, want) {
		t.Errorf("highlighted %q, want %q", highlighted, want)
	}
}

func TestHighlightMatchesKeepsLineEnds(t *testing.T) {
	// A segment that ends its line, such as a code block; only its last
	// piece may. (Since Fyne 2.8 a heading is inline text and an empty
	// segment that ends the line.)
	segs := []widget.RichTextSegment{&widget.TextSegment{Style: widget.RichTextStyleCodeBlock, Text: "Video URL"}}
	matches := findMatches([][]rune{foldedText(segs)}, "video")
	segs = highlightMatches(segs, matches, 0, newMatchMark())
	pieces := textSegments(segs)
	if len(pieces) != 2 || pieces[0].Text != "Video" || pieces[1].Text != " URL" {
		t.Fatalf("pieces = %v", pieces)
	}
	if !pieces[0].Inline() || pieces[1].Inline() {
		t.Errorf("inline = %v, %v; want only the last piece to end the line", pieces[0].Inline(), pieces[1].Inline())
	}
}

func TestGuidePanelFind(t *testing.T) {
	_ = test.NewApp()
	panel := newGuidePanel(func() {}, func() {})
	if panel.countLabel.Text != "" || !panel.nextBtn.Disabled() {
		t.Fatalf("before a search: count %q, next disabled %v", panel.countLabel.Text, panel.nextBtn.Disabled())
	}

	test.Type(panel.search, "cookie")
	total := len(panel.matches)
	if total < 2 {
		t.Fatalf("found %d matches of cookie, want several", total)
	}
	if want := "1 of " + strconv.Itoa(total); panel.countLabel.Text != want {
		t.Errorf("count = %q, want %q", panel.countLabel.Text, want)
	}

	panel.search.TypedKey(&fyne.KeyEvent{Name: fyne.KeyReturn})
	if want := "2 of " + strconv.Itoa(total); panel.countLabel.Text != want {
		t.Errorf("after Enter: count = %q, want %q", panel.countLabel.Text, want)
	}
	shift := &fyne.KeyEvent{Name: desktop.KeyShiftLeft}
	panel.search.KeyDown(shift)
	panel.search.TypedKey(&fyne.KeyEvent{Name: fyne.KeyReturn})
	panel.search.TypedKey(&fyne.KeyEvent{Name: fyne.KeyReturn})
	panel.search.KeyUp(shift)
	if want := strconv.Itoa(total) + " of " + strconv.Itoa(total); panel.countLabel.Text != want {
		t.Errorf("after Shift+Enter twice: count = %q, want %q (wrapped around)", panel.countLabel.Text, want)
	}

	// Only the current match's block has the mark, and every match is
	// highlighted.
	marks, highlighted := 0, 0
	for _, block := range panel.blocks {
		marks += countMarks(block.text.Segments)
		for _, seg := range textSegments(block.text.Segments) {
			if isHighlighted(seg) {
				highlighted++
			}
		}
	}
	if marks != 1 {
		t.Errorf("%d marks, want 1", marks)
	}
	if highlighted < total {
		t.Errorf("%d highlighted pieces for %d matches", highlighted, total)
	}

	panel.search.SetText("no such words anywhere")
	if panel.countLabel.Text != "No matches" || !panel.prevBtn.Disabled() {
		t.Errorf("no match: count %q, previous disabled %v", panel.countLabel.Text, panel.prevBtn.Disabled())
	}
	for i, block := range panel.blocks {
		if countMarks(block.text.Segments) != 0 {
			t.Errorf("block %d still has a mark", i)
		}
		for _, seg := range textSegments(block.text.Segments) {
			if isHighlighted(seg) {
				t.Fatalf("block %d still highlights %q", i, seg.Text)
			}
		}
	}
}

// isHighlighted reports whether seg is coloured as a search match.
func isHighlighted(seg *widget.TextSegment) bool {
	return seg.Style.ColorName == colorNameSearchMatch || seg.Style.ColorName == colorNameSearchCurrent
}

// countMarks returns how many matchMarks segs holds, inside paragraphs and
// lists too.
func countMarks(segs []widget.RichTextSegment) int {
	marks := 0
	for _, seg := range segs {
		switch s := seg.(type) {
		case *matchMark:
			marks++
		case *widget.ParagraphSegment:
			marks += countMarks(s.Texts)
		case *widget.ListSegment:
			marks += countMarks(s.Items)
		}
	}
	return marks
}

func TestGuidePanelScrollsToMatch(t *testing.T) {
	_ = test.NewApp()
	panel := newGuidePanel(func() {}, func() {})
	window := test.NewWindow(panel.content)
	defer window.Close()
	window.Resize(fyne.NewSize(550, 500))

	// upscaleVideo appears only in the last topic.
	test.Type(panel.search, "upscaleVideo")
	if len(panel.matches) != 1 {
		t.Fatalf("got %d matches, want 1", len(panel.matches))
	}
	if panel.scroll.Offset.Y == 0 {
		t.Error("did not scroll to the last topic")
	}
	assertMatchInView(t, panel)

	// Stepping through matches all over the guide, forward and back, keeps
	// each in view.
	panel.search.SetText("cookie")
	for range len(panel.matches) + 1 {
		panel.step(1)
		assertMatchInView(t, panel)
	}
	for range 3 {
		panel.step(-1)
		assertMatchInView(t, panel)
	}
}

// assertMatchInView fails t when the current match is outside the visible
// part of the guide. It asks the test driver where the mark is, which
// (unlike the real driver) can place objects it has not drawn.
func assertMatchInView(t *testing.T, panel *guidePanel) {
	t.Helper()
	driver := fyne.CurrentApp().Driver()
	y := driver.AbsolutePositionForObject(panel.mark.visual).Y - driver.AbsolutePositionForObject(panel.list).Y
	offset, view := panel.scroll.Offset.Y, panel.scroll.Size().Height
	if y < offset || y > offset+view {
		t.Errorf("match %d at %v is not in view: offset %v, height %v", panel.current+1, y, offset, view)
	}
}

func TestGuideSearchEscape(t *testing.T) {
	_ = test.NewApp()
	closed := false
	panel := newGuidePanel(func() { closed = true }, func() {})
	escape := &fyne.KeyEvent{Name: fyne.KeyEscape}

	panel.search.SetText("cookies")
	panel.search.TypedKey(escape)
	if panel.search.Text != "" || closed {
		t.Fatalf("first Esc: text %q, closed %v; want cleared and still open", panel.search.Text, closed)
	}
	if len(panel.matches) != 0 {
		t.Errorf("clearing the search left %d matches", len(panel.matches))
	}

	panel.search.TypedKey(escape)
	if !closed {
		t.Error("Esc in an empty search field should close the guide")
	}
}
