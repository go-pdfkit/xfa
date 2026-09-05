// Copyright (c) 2026, the go-pdfkit/xfa authors
// All rights reserved.
//
// SPDX-License-Identifier: BSD-3-Clause

package xfa

import (
	"fmt"
	"math"
	"strings"
	"testing"
)

// measured is a string broken at a width, as "w h broken", so a test can state
// a whole measurement in one line.
func measured(text string, maxWidth Measure) string {
	t := newTextMeasure(nil, nil, paraMargin{}, 0)
	t.addString(text)
	w, h, broken := t.compute(maxWidth)
	return fmt.Sprintf("%g %g %v", w.Points(), h.Points(), broken)
}

func TestOneLineIsOneEmTallAndEveryLineAfterItIsMore(t *testing.T) {
	// The whole of the no-font regime is in these numbers: a character is ten
	// points wide, the first line ten points tall, every line after it twelve,
	// and the width comes back multiplied by 1.02 (WIDTH_FACTOR, text.js:18).
	for _, tc := range []struct {
		what  string
		text  string
		wide  Measure
		want  string
		lines int
	}{
		{"nothing at all", "", 100, "0 0 false", 0},
		{"one character", "A", 100, "10.2 10 false", 1},
		{"a word that fits", "hello", 100, "51 10 false", 1},
		{"two lines written as two", "ab\ncd", 100, "20.4 22 false", 2},
		// A blank line takes NO room, which is the reference's answer rather
		// than a rounding of it: the end-of-line glyph carries a line height
		// of nought (text.js:205, 217), and what compute charges for a line is
		// the height of the glyph that ENDED it (:240-241).
		{"a blank line between them takes no room", "ab\n\ncd", 100, "20.4 22 false", 2},
		{"U+2029 breaks a line as a newline does", "ab cd", 100, "20.4 22 false", 2},
		{"a carriage return is an ordinary character", "ab\rcd", 100, "51 10 false", 1},
	} {
		if got := measured(tc.text, tc.wide); got != tc.want {
			t.Errorf("%s: %q at %v came to %s, want %s", tc.what, tc.text, tc.wide, got, tc.want)
		}
	}
}

func TestALineIsBrokenAtTheLastSpaceThatFits(t *testing.T) {
	// Six characters fit in sixty-five points and seven do not.
	for _, tc := range []struct {
		what string
		text string
		want string
	}{
		{"at the space, which is not carried onto the next line",
			// "abc def": the space at index 3 is where the break is taken, and
			// the space itself costs nothing. Two lines: 10 then 12.
			"abc def", "30.6 22 true"},
		{"in the middle of a word, where the line holds no space",
			// Seven characters, none of them a space: the seventh begins a
			// line of its own. The second line is TEN points tall rather than
			// twelve, because pdf.js reads the glyph's height before it clears
			// isFirstLine (text.js:236, 284) — so the line a first break opens
			// is charged at the first line's height.
			"abcdefg", "61.2 20 true"},
		{"back to the last space, where the word after it is too long",
			// "ab cdefghij": the break goes back to the space at index 2, so
			// the first line is "ab" and the rest is broken again mid-word.
			"ab cdefghij", "61.2 34 true"},
		{"a space that would itself overflow ends the line",
			// Six characters then a space: the space is at the width, so the
			// line ends there and the space is not carried over.
			"abcdef gh", "61.2 22 true"},
	} {
		if got := measured(tc.text, 65); got != tc.want {
			t.Errorf("%s: %q came to %s, want %s", tc.what, tc.text, got, tc.want)
		}
	}
}

func TestNothingIsBrokenWhereThereIsNoBoundOnTheWidth(t *testing.T) {
	// A content area that writes no width bounds nothing, and a line then runs
	// as long as its text.
	if got := measured("a b c d e f g h", Measure(math.Inf(1))); got != "153 10 false" {
		t.Errorf("unbounded came to %s", got)
	}
}

func TestACharacterOutsideTheBasicPlaneCountsAsTwo(t *testing.T) {
	// pdf.js's line.split("") is UTF-16 code units, so an astral character is
	// two glyphs there and is two here. This agrees with the reference rather
	// than with the Unicode standard, on purpose.
	if got := measured("\U0001F600", 100); got != "20.4 10 false" {
		t.Errorf("an astral character came to %s, want two glyphs", got)
	}
}

func TestAParagraphChargesItsMarginsOnceEach(t *testing.T) {
	// addPara adds the paragraph's top and bottom to the height ONCE per <p>
	// (text.js:165-168), whatever the number of lines it comes to.
	m := newTextMeasure(nil, nil, paraMargin{top: 3, bottom: 2, hasTop: true, hasBottom: true}, 0)
	m.addPara()
	m.addString("ab\ncd")
	_, h, _ := m.compute(100)
	if h != 22+5 {
		t.Errorf("two lines and one paragraph came to %v, want 27", h)
	}
}

func TestAParagraphInheritsWhatItsStyleDoesNotWrite(t *testing.T) {
	// pdf.js writes NaN for an inset a style says nothing about and fills it in
	// from the paragraph outside (text.js:116-120).
	m := newTextMeasure(nil, nil, paraMargin{top: 3, bottom: 2, hasTop: true, hasBottom: true}, 0)
	m.pushData(xfaFont{}, paraMargin{top: 7, hasTop: true}, 0)
	m.addPara()
	m.popFont()
	m.addPara()
	if m.extraHeight != 7+2+3+2 {
		t.Errorf("the inherited margins came to %v, want 14", m.extraHeight)
	}
}

func TestSplittingKeepsTheEmptyPieces(t *testing.T) {
	// A blank line is a line's worth of height, so the split cannot drop it.
	if got := splitLines("a\n\nb"); strings.Join(got, "|") != "a||b" {
		t.Errorf("the split came out %q", got)
	}
	if !isLineSeparator(' ') || isLineSeparator('\r') {
		t.Error("the separators are a newline and U+2029, and nothing else")
	}
}
