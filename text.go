// Copyright (c) 2026, the go-pdfkit/xfa authors
// All rights reserved.
//
// SPDX-License-Identifier: BSD-3-Clause

package xfa

// Measuring a leaf's text, so that a stack above it can begin.
//
// # Why there is no font here
//
// pdf.js measures text with the fonts of the PDF the XFA package came in, and
// falls back when it has none. The fallback is not a degradation: it is
// written down, it is exact, and it is what pdf.js itself runs on a form whose
// fonts it cannot resolve.
//
//   - TextMeasure.addString, with no font: "When we have no font in the pdf,
//     just use the font size as default width" — one em per character, a line
//     1.2 ems tall, and a first line one em tall (text.js:211-219).
//   - getMetrics, with no font: lineHeight 12, lineGap 2, lineNoGap 10
//     (fonts.js:173-179).
//
// This package implements that regime and nothing else, which is why it still
// has no dependencies. Real per-glyph advances are a matter of filling
// [glyph].w from a face rather than from [emWidth]; the line breaking, the
// line counting and the arithmetic above them do not change. When that day
// comes the metrics belong in go-opentype/opentype, which already exposes
// glyph advances and units per em, and NOT here.
//
// # The size is not the template's, and that is pdf.js's doing
//
// It is tempting to read the <font size="14pt"> a draw writes and measure at
// fourteen points. pdf.js does not, and cannot: FontInfo asks the font finder
// for the typeface (text.js:42) and, when it does not have it, REPLACES the
// whole of xfaFont with the default's (text.js:43-46, 55-83) — typeface
// Courier, posture normal, weight normal, size 10, letterSpacing 0. The size
// written beside a typeface nobody has is discarded along with it. So in this
// regime every glyph is ten points wide and every line twelve points tall,
// whatever the template asks for, and a measurement that honoured the written
// size would disagree with the reference on nearly every leaf.

// emWidth is the width of one character, and the size every measurement here
// is made at. See the note above for why the template cannot change it.
const emWidth Measure = 10

// lineFactor is how tall a line is, in ems: pdf.js pushes 1.2 * fontSize for
// every glyph's line height when it has no font (text.js:214).
const lineFactor = 1.2

// widthFactor is the fudge pdf.js multiplies a measured width by before
// returning it (WIDTH_FACTOR, text.js:18, applied at :296). It is two per cent
// and it is applied to the width only, never to the height.
const widthFactor = 1.02

// A block of n lines comes to 10 + 12(n-1) points rather than 12n: the first
// line is one em, every line after it 1.2 ems. pdf.js carries both numbers on
// every glyph and picks between them with isFirstLine (text.js:236).
// A glyph is one character as the measurer sees it.
//
// pdf.js pushes a five-element array per character — width, line height, first
// line height, the character, and whether it ends a line (text.js:196-205) —
// and compute reads them back positionally. The character itself is kept only
// to ask whether it is a space, so that is what this carries.
type glyph struct {
	w, lineH, firstLineH Measure
	space, eol           bool
}

// A textMeasure accumulates the glyphs of a leaf's text and then breaks them
// into lines. It is pdf.js's TextMeasure (text.js:145-297) with the font stack
// reduced to the one thing that survives having no font.
//
// FontInfo replaces the typeface, the posture, the weight, the SIZE and the
// letterSpacing with the default's the moment the typeface is not found
// (text.js:42-46), so a <span style="font-size:14pt"> measures at ten points
// like everything else. What it does keep is the paragraph margins, and those
// reach the height through addPara — which pdf.js calls for a <p> of a rich
// text (xhtml.js:490-495) and for nothing else.
type textMeasure struct {
	glyphs []glyph
	// paras is the stack of paragraph margins, innermost last, and is never
	// empty: the leaf's own <para> is at the bottom of it.
	paras []paraMargin
	// extraHeight is what addPara has added.
	extraHeight Measure
}

// A paraMargin is the space a paragraph asks for above and below itself.
//
// pdf.js carries four insets and adds two of them to the height
// (text.js:165-168). The left and the right reach nothing at all: the width a
// line is broken at is the container's, and a paragraph's own left and right
// margins are never taken off it.
type paraMargin struct {
	top, bottom Measure
	// hasTop and hasBottom say the style wrote them. pdf.js writes NaN for an
	// inset a style says nothing about and fills it in from the paragraph
	// outside (text.js:116-120), which is a third state a Measure has no room
	// for.
	hasTop, hasBottom bool
}

// newTextMeasure starts a measurement whose outermost paragraph margin is the
// leaf's own <para>: pdf.js reads spaceAbove and spaceBelow into it
// (html_utils.js:222-229). A leaf holding a plain string is not affected by
// its own <para> at all, because nothing then calls addPara.
func newTextMeasure(outer paraMargin) *textMeasure {
	return &textMeasure{paras: []paraMargin{outer}}
}

// pushPara opens a nested paragraph, inheriting each inset the style did not
// write from the paragraph outside it (text.js:104-131).
func (t *textMeasure) pushPara(m paraMargin) {
	outer := t.paras[len(t.paras)-1]
	if !m.hasTop {
		m.top = outer.top
	}
	if !m.hasBottom {
		m.bottom = outer.bottom
	}
	t.paras = append(t.paras, m)
}

// popPara closes one.
func (t *textMeasure) popPara() { t.paras = t.paras[:len(t.paras)-1] }

// addPara charges the paragraph in hand to the block's height
// (text.js:165-168).
func (t *textMeasure) addPara() {
	m := t.paras[len(t.paras)-1]
	t.extraHeight += m.top + m.bottom
}

// addString adds one run of text.
//
// pdf.js splits on newline and on U+2029, the Unicode paragraph separator,
// pushes an end-of-line glyph after each piece and then pops the last one
// (text.js:212-219). This interleaves them instead, which is the same list.
//
// The characters are counted as UTF-16 code units, not as runes, because
// pdf.js's line.split("") does: a character outside the basic plane is two
// glyphs there and so is two here. No corpus form writes one, and the day one
// does this will agree with the reference rather than with the Unicode
// standard, which is the point.
func (t *textMeasure) addString(s string) {
	if s == "" {
		return
	}
	for i, line := range splitLines(s) {
		if i > 0 {
			t.glyphs = append(t.glyphs, glyph{eol: true})
		}
		for _, r := range line {
			n := 1
			if r > 0xFFFF {
				n = 2
			}
			for range n {
				t.glyphs = append(t.glyphs, glyph{
					w:          emWidth,
					lineH:      lineFactor * emWidth,
					firstLineH: emWidth,
					space:      r == ' ',
				})
			}
		}
	}
}

// isLineSeparator says which characters pdf.js's split on a newline and
// U+2029, the Unicode paragraph separator, breaks at (text.js:212). A
// carriage return is NOT one of them, so text written with CRLF endings
// keeps its returns as ordinary characters, one em wide each.
func isLineSeparator(r rune) bool { return r == '\n' || r == '\u2029' }

// splitLines is that split, keeping the empty pieces a FieldsFunc would drop:
// two newlines in a row are a blank line and a blank line is a line's worth of
// height.
func splitLines(s string) []string {
	out := []string{}
	start := 0
	for i, r := range s {
		if isLineSeparator(r) {
			out = append(out, s[start:i])
			start = i + len(string(r))
		}
	}
	return append(out, s[start:])
}

// compute breaks the glyphs into lines no wider than maxWidth and returns how
// wide and how tall the block comes out, and whether a line had to be broken.
//
// It is a port of TextMeasure.compute (text.js:222-297), branch for branch,
// including the two ways it breaks: at the last space seen, by rewinding the
// cursor to it, or in the middle of a word when there was no space on the
// line. The rewind terminates because each one goes to a position strictly
// later than the last one it rewound to.
func (t *textMeasure) compute(maxWidth Measure) (w, h Measure, broken bool) {
	lastSpacePos := -1
	var lastSpaceWidth, width, height, lineW, lineH Measure
	first := true
	for i := 0; i < len(t.glyphs); i++ {
		g := t.glyphs[i]
		gh := g.lineH
		if first {
			gh = g.firstLineH
		}
		switch {
		case g.eol:
			width = max(width, lineW)
			lineW = 0
			height += lineH
			lineH = gh
			lastSpacePos, lastSpaceWidth = -1, 0
			first = false
		case g.space && lineW+g.w > maxWidth:
			// A break may be taken here and the space is not carried onto the
			// next line, so it costs nothing.
			width = max(width, lineW)
			lineW = 0
			height += lineH
			lineH = gh
			lastSpacePos, lastSpaceWidth = -1, 0
			broken, first = true, false
		case g.space:
			lineH = max(gh, lineH)
			lastSpaceWidth = lineW
			lineW += g.w
			lastSpacePos = i
		case lineW+g.w > maxWidth:
			height += lineH
			lineH = gh
			if lastSpacePos != -1 {
				// Back to the last space and start the line again from just
				// after it. The loop's own increment does the "just after".
				i = lastSpacePos
				width = max(width, lastSpaceWidth)
				lineW = 0
				lastSpacePos, lastSpaceWidth = -1, 0
			} else {
				width = max(width, lineW)
				lineW = g.w
			}
			broken, first = true, false
		default:
			lineW += g.w
			lineH = max(gh, lineH)
		}
	}
	width = max(width, lineW)
	height += lineH + t.extraHeight
	return widthFactor * width, height, broken
}
