// guide_search.go — Find in the GoVid Guide (help_window.go).
//
// Responsibilities:
//   - foldedText: the text a RichText's segments show, in lower case, for
//     searching.
//   - findMatches: every place a query appears in the guide, ignoring case.
//   - highlightMatches: a RichText's segments with the matches coloured and
//     a matchMark just before the current one.
//   - matchMark: an empty segment that RichText lays out where the current
//     match starts, so the guide knows where to scroll.
package main

import (
	"image/color"
	"slices"
	"unicode"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/widget"
)

// guideMatch is one match of the guide's search: the block it is in (an
// index into the guide's RichTexts) and where, in runes of the block's
// foldedText.
type guideMatch struct {
	block      int
	start, end int
}

// walkText calls visit for every TextSegment in segs, in reading order,
// with its offset in the block's foldedText, and puts what visit returns in
// its place. Paragraphs and list items are walked into; a line break after
// a segment, paragraph, or list item counts as one rune of the text.
func walkText(segs []widget.RichTextSegment, offset *int, visit func(seg *widget.TextSegment, start int) []widget.RichTextSegment) []widget.RichTextSegment {
	var out []widget.RichTextSegment
	for _, seg := range segs {
		switch s := seg.(type) {
		case *widget.TextSegment:
			start := *offset
			*offset += len([]rune(s.Text))
			if !s.Inline() {
				*offset++
			}
			out = append(out, visit(s, start)...)
		case *widget.ParagraphSegment:
			s.Texts = walkText(s.Texts, offset, visit)
			*offset++
			out = append(out, s)
		case *widget.ListSegment:
			s.Items = walkText(s.Items, offset, visit)
			out = append(out, s)
		default:
			out = append(out, seg)
		}
	}
	return out
}

// foldedText returns the text segs show, in lower case, with a newline at
// each line break walkText counts.
func foldedText(segs []widget.RichTextSegment) []rune {
	var text []rune
	offset := 0
	walkText(segs, &offset, func(seg *widget.TextSegment, start int) []widget.RichTextSegment {
		for len(text) < start {
			text = append(text, '\n')
		}
		text = append(text, foldRunes(seg.Text)...)
		return []widget.RichTextSegment{seg}
	})
	return text
}

// foldRunes returns s in lower case, one rune for each rune of s, so
// offsets into it are offsets into s.
func foldRunes(s string) []rune {
	runes := []rune(s)
	for i, r := range runes {
		runes[i] = unicode.ToLower(r)
	}
	return runes
}

// findMatches returns every place query appears in texts (foldedText of
// each block), ignoring case, in order; matches do not overlap. An empty
// query has none.
func findMatches(texts [][]rune, query string) []guideMatch {
	q := foldRunes(query)
	if len(q) == 0 {
		return nil
	}
	var matches []guideMatch
	for block, text := range texts {
		for i := 0; i+len(q) <= len(text); {
			if slices.Equal(text[i:i+len(q)], q) {
				matches = append(matches, guideMatch{block: block, start: i, end: i + len(q)})
				i += len(q)
			} else {
				i++
			}
		}
	}
	return matches
}

// highlightMatches returns segs with the text of each of matches (all in
// the block segs make up) in the search colour, the one at index
// current in the current-match colour with mark just before it. segs is
// changed; pass freshly parsed segments.
func highlightMatches(segs []widget.RichTextSegment, matches []guideMatch, current int, mark widget.RichTextSegment) []widget.RichTextSegment {
	offset := 0
	return walkText(segs, &offset, func(seg *widget.TextSegment, start int) []widget.RichTextSegment {
		return splitAtMatches(seg, start, matches, current, mark)
	})
}

// splitAtMatches splits seg, which starts at rune start of its block's
// text, into pieces at the matches that overlap it, styled as
// highlightMatches says. Only the last piece ends the line when seg does.
func splitAtMatches(seg *widget.TextSegment, start int, matches []guideMatch, current int, mark widget.RichTextSegment) []widget.RichTextSegment {
	runes := []rune(seg.Text)
	end := start + len(runes)
	var out []widget.RichTextSegment
	pos := start
	addPiece := func(from, to int, colorName fyne.ThemeColorName) {
		if from >= to {
			return
		}
		piece := *seg
		piece.Text = string(runes[from-start : to-start])
		piece.Style.Inline = true
		if colorName != "" {
			piece.Style.ColorName = colorName
		}
		out = append(out, &piece)
	}

	for i, match := range matches {
		if match.end <= start || match.start >= end {
			continue
		}
		from, to := max(match.start, start), min(match.end, end)
		addPiece(pos, from, "")
		colorName := colorNameSearchMatch
		if i == current {
			colorName = colorNameSearchCurrent
			if from == match.start {
				out = append(out, mark)
			}
		}
		addPiece(from, to, colorName)
		pos = to
	}
	if out == nil {
		return []widget.RichTextSegment{seg}
	}
	addPiece(pos, end, "")
	if last, ok := out[len(out)-1].(*widget.TextSegment); ok {
		last.Style.Inline = seg.Style.Inline
	}
	return out
}

// matchMark is an empty inline segment put just before the current match.
// RichText lays its visual, a rectangle of no size, out where the match
// starts, which tells the guide where to scroll.
type matchMark struct {
	visual *canvas.Rectangle
}

func newMatchMark() *matchMark {
	return &matchMark{visual: canvas.NewRectangle(color.Transparent)}
}

// Inline keeps the mark on the match's line.
func (mark *matchMark) Inline() bool { return true }

// Textual is empty: the mark adds no text.
func (mark *matchMark) Textual() string { return "" }

// Update does nothing: the visual never changes.
func (mark *matchMark) Update(fyne.CanvasObject) {}

// Visual returns the rectangle whose position is the match's.
func (mark *matchMark) Visual() fyne.CanvasObject { return mark.visual }

// Select does nothing: the mark has no text to select.
func (mark *matchMark) Select(_, _ fyne.Position) {}

// SelectedText is empty: the mark has no text to select.
func (mark *matchMark) SelectedText() string { return "" }

// Unselect does nothing: the mark has no text to select.
func (mark *matchMark) Unselect() {}
