// Copyright (c) 2026, the go-pdfkit/xfa authors
// All rights reserved.
//
// SPDX-License-Identifier: BSD-3-Clause

package xfa

// Measuring a leaf's text, so that a stack above it can begin.
//
// # Two regimes, and the reference has both
//
// pdf.js measures text with the fonts of the PDF the XFA package came in, and
// falls back when it has none. Both branches are here, chosen the same way it
// chooses: by whether the font finder answered.
//
//   - With a face: per-glyph advances in thousandths of an em, scaled by the
//     size the template wrote, and a line height and a first line height from
//     the face's own vertical metrics (text.js:175-206).
//   - Without one: "When we have no font in the pdf, just use the font size as
//     default width" — one em per character, a line 1.2 ems tall, and a first
//     line one em tall (text.js:208-219) — at the DEFAULT size of ten points,
//     because the size written beside a typeface nobody has is discarded along
//     with it (see [fontInfo]).
//
// [textMeasure.compute] is the same function for both. Only [glyph] is filled
// in differently, which is why real metrics reach the layout by changing one
// method and the stack that feeds it, and not the line breaking or the
// arithmetic above it.
//
// # Where the fonts come from
//
// From the caller. See [FontSet]: a template carries no fonts, a PDF does, and
// this package reads neither. [Place] passes none and every leaf falls to the
// second regime, which is what this package did at v0.18.0 and before.

// defaultSize is the size a measurement is made at where no face was found: the
// size of pdf.js's default xfaFont (text.js:69, 76), ten points, which stands
// in for whatever the template wrote.
const defaultSize Measure = 10

// lineFactor is how tall a line is, in ems, where there is no face: pdf.js
// pushes 1.2 * fontSize for every glyph's line height (text.js:214). It is
// also the floor a face's own line height is held above (text.js:174).
const lineFactor = 1.2

// defaultLineGap is the gap pdf.js assumes for a face that gives none:
// `pdfFont.lineGap === undefined ? 0.2 : pdfFont.lineGap` (text.js:176).
const defaultLineGap = 0.2

// widthFactor is the fudge pdf.js multiplies a measured width by before
// returning it (WIDTH_FACTOR, text.js:18, applied at :296). It is two per cent
// and it is applied to the width only, never to the height.
const widthFactor = 1.02

// A block of n lines comes to one first line plus n-1 ordinary ones rather
// than n of either: pdf.js carries both numbers on every glyph and picks
// between them with isFirstLine (text.js:236). Without a face those are one em
// and 1.2 ems; with one they are the face's, and the first is the SHORTER
// because it is the line height less the gap.
//
// A glyph is one character as the measurer sees it.
//
// pdf.js pushes a five-element array per character — width, line height, first
// line height, the character, and whether it ends a line (text.js:183-190,
// 196-205) — and compute reads them back positionally. The character itself is
// kept only to ask whether it is a space, so that is what this carries.
type glyph struct {
	w, lineH, firstLineH Measure
	space, eol           bool
}

// An xfaFont is what a <font> element says that a measurement reads.
//
// pdf.js copies exactly these five out of the template's Font and carries them
// through the stack (text.js:35-41). The rest of the element — the fill, the
// underline, the horizontal scale — decides how a glyph is DRAWN and never how
// wide it is.
//
// The zero value of each field means "not written here", which is what a
// nested span inherits on: pdf.js's `xfaFont[name] ||= lastFont.xfaFont[name]`
// (text.js:103-111) is a falsy test, so a span writing a size of nought or an
// empty typeface inherits rather than overriding. That is why size and
// letterSpacing are plain measures and not measure-and-written pairs: the
// reference cannot tell the two apart either.
type xfaFont struct {
	typeface string
	// posture is "normal" or "italic", weight "normal" or "bold": the two
	// values each is validated against (template.js:3241, 3253).
	posture, weight string
	size            Measure
	letterSpacing   Measure
}

// defaultXfaFont is what pdf.js measures with when it has no font at all:
// Courier at ten points, upright and of normal weight (text.js:73-80). No face
// is ever found for it — the whole point is that there is none — so the
// typeface names nothing that is looked up.
var defaultXfaFont = xfaFont{
	typeface: "Courier",
	posture:  "normal",
	weight:   "normal",
	size:     defaultSize,
}

// templateFont is a <font> element read as the five values a measurement wants,
// with the defaults the reference gives each of them (template.js:3218-3253):
// typeface "Courier", size ten points, posture and weight normal, letter
// spacing nought.
//
// The two lengths are read LENIENTLY, by [getMeasurement], and not by this
// package's strict [ParseMeasure]. That is a deliberate exception and the
// corpus forced it: 24 draws of ca-cra__t1206 and t1207 write
// letterSpacing="-0.002em", a unit relative to the size being resolved, and
// pdf.js drops the unit and keeps the number. Refusing it would leave four
// forms with no table at all, over a quantity that moves a glyph by two
// thousandths of a point.
func templateFont(n *Node) *xfaFont {
	f := n.Child("font")
	if f == nil {
		return nil
	}
	out := defaultXfaFont
	if t := f.Get("typeface"); t != "" {
		out.typeface = t
	}
	if p := f.Get("posture"); p == "italic" {
		// getStringOption falls back to the FIRST of the list for anything
		// unrecognised, and the first is "normal" (template.js:3241).
		out.posture = p
	}
	if w := f.Get("weight"); w == "bold" {
		out.weight = w
	}
	out.size = getMeasurement(f.Get("size"), defaultSize)
	out.letterSpacing = getMeasurement(f.Get("letterSpacing"), 0)
	return &out
}

// A fontInfo is one level of the measurement's font stack: the face its text is
// measured with, the five values that chose it, the paragraph margins in scope
// and the line height the paragraph asks for.
//
// It is pdf.js's FontInfo (text.js:20-82), and the thing worth reading twice is
// what happens when the typeface is NOT found: the whole of xfaFont is replaced
// by the default's — typeface, posture, weight, SIZE and letterSpacing
// (text.js:43-46, 55-83). A <draw font="Wingdings" size="14pt"> in a document
// holding no Wingdings is measured at ten points and not at fourteen. That is
// not a shortfall standing in for something better: it is the reference's
// answer, and a measurement that honoured the written size would disagree with
// it on every leaf whose typeface is missing.
type fontInfo struct {
	// face is the font the glyphs come from, or nil for the no-font regime.
	face Face
	// font is the five values AFTER the replacement above.
	font xfaFont
	// lineHeight is what the paragraph asks for, or nought for none. pdf.js
	// reads it from <para lineHeight> and lets it override the face's
	// (text.js:172-173); the no-font branch never looks at it.
	lineHeight Measure
	// para is the paragraph margins in scope, which reach the height through
	// [textMeasure.addPara] and nowhere else.
	para paraMargin
}

// newFontInfo resolves a level of the stack.
func newFontInfo(fonts *FontSet, f *xfaFont, m paraMargin, lineHeight Measure) fontInfo {
	fi := fontInfo{para: m, lineHeight: lineHeight}
	if f == nil {
		fi.face, fi.font = defaultFace(fonts)
		return fi
	}
	fi.font = *f
	t, _ := fonts.find(f.typeface)
	if t == nil {
		fi.face, fi.font = defaultFace(fonts)
		return fi
	}
	// The typeface was found; the WRITTEN values stand, including the size.
	// pdf.js does not write the family it landed on back into xfaFont.
	fi.face = selectFace(fi.font, t)
	if fi.face == nil {
		fi.face, fi.font = defaultFace(fonts)
	}
	return fi
}

// defaultFace is the font a measurement falls back on, and the five values that
// go with it: FontInfo.defaultFont (text.js:54-81).
//
// The chain is Helvetica, then Myriad Pro, then Arial, then whichever family
// the caller added FIRST — and each of those three names goes through the fuzzy
// [FontSet.find], so a set holding only "Arial" answers the first of them.
// Where the set has nothing at all, there is no face and the five values are
// Courier at ten points, which is the regime this package ran in entirely
// before there were any fonts to hand it.
//
// The typeface the replacement carries is the family name the set filed the
// font under. pdf.js takes it from the chosen font's own cssFontInfo
// (text.js:65) — the same name for a set built the way FontFinder builds one,
// where the key IS that family.
func defaultFace(fonts *FontSet) (Face, xfaFont) {
	t, family := fonts.find("Helvetica")
	if t == nil {
		t, family = fonts.find("Myriad Pro")
	}
	if t == nil {
		t, family = fonts.find("Arial")
	}
	if t == nil {
		t, family = fonts.getDefault()
	}
	if t != nil && t.Regular != nil {
		return t.Regular, xfaFont{
			typeface: family,
			posture:  "normal",
			weight:   "normal",
			size:     defaultSize,
		}
	}
	return nil, defaultXfaFont
}

// A textMeasure accumulates the glyphs of a leaf's text and then breaks them
// into lines. It is pdf.js's TextMeasure (text.js:136-297) with its
// FontSelector folded in, because the stack is three fields and a push.
type textMeasure struct {
	glyphs []glyph
	fonts  *FontSet
	// stack is the font stack, innermost last, and is never empty: the leaf's
	// own font and <para> are at the bottom of it.
	stack []fontInfo
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
	// outside (text.js:113-117), which is a third state a Measure has no room
	// for.
	hasTop, hasBottom bool
}

// newTextMeasure starts a measurement.
//
// f is the leaf's font — its own <font> or the nearest one above it, which is
// what layoutNode resolves before it measures anything (html_utils.js:232-241)
// — and nil where the template writes none anywhere up the chain. outer is the
// leaf's own <para>, whose spaceAbove and spaceBelow pdf.js reads into the
// outermost FontInfo (html_utils.js:222-229), where nothing but a <p> of a rich
// text ever reads them again.
func newTextMeasure(fonts *FontSet, f *xfaFont, outer paraMargin, lineHeight Measure) *textMeasure {
	return &textMeasure{
		fonts: fonts,
		stack: []fontInfo{newFontInfo(fonts, f, outer, lineHeight)},
	}
}

// top is the level in scope.
func (t *textMeasure) top() fontInfo { return t.stack[len(t.stack)-1] }

// pushData opens a nested span, inheriting from the span outside it every one
// of the five font values and every margin inset this one does not write, and
// its line height where this one writes none (text.js:102-127).
func (t *textMeasure) pushData(f xfaFont, m paraMargin, lineHeight Measure) {
	outer := t.top()
	if f.typeface == "" {
		f.typeface = outer.font.typeface
	}
	if f.posture == "" {
		f.posture = outer.font.posture
	}
	if f.weight == "" {
		f.weight = outer.font.weight
	}
	if f.size == 0 {
		f.size = outer.font.size
	}
	if f.letterSpacing == 0 {
		f.letterSpacing = outer.font.letterSpacing
	}
	if !m.hasTop {
		m.top = outer.para.top
	}
	if !m.hasBottom {
		m.bottom = outer.para.bottom
	}
	if lineHeight == 0 {
		lineHeight = outer.lineHeight
	}
	fi := newFontInfo(t.fonts, &f, m, lineHeight)
	// `fontInfo.pdfFont ||= lastFont.pdfFont` (text.js:124): a span naming a
	// typeface the document does not hold keeps DRAWING in the face outside
	// it, even though its five values were replaced by the default's. The two
	// halves disagree and both are the reference's.
	if fi.face == nil {
		fi.face = outer.face
	}
	t.stack = append(t.stack, fi)
}

// popFont closes one.
func (t *textMeasure) popFont() { t.stack = t.stack[:len(t.stack)-1] }

// addPara charges the paragraph in hand to the block's height
// (text.js:159-162).
func (t *textMeasure) addPara() {
	m := t.top().para
	t.extraHeight += m.top + m.bottom
}

// addString adds one run of text, in whichever regime the level in hand is in.
//
// pdf.js splits on newline and on U+2029, the Unicode paragraph separator,
// pushes an end-of-line glyph after each piece and then pops the last one
// (text.js:186-205, 212-219). This interleaves them instead, which is the same
// list.
func (t *textMeasure) addString(s string) {
	if s == "" {
		return
	}
	f := t.top()
	if f.face != nil {
		t.addWithFace(s, f)
		return
	}
	t.addWithoutFace(s, f.font.size)
}

// addWithFace is the branch that has a font (text.js:175-206).
//
// The four heights are arrived at once for the whole run, because pdf.js does:
// the face's line height floors at 1.2 unless the paragraph named one, the
// first line is that height less the gap and never less than one em, and both
// are in ems until the size multiplies them.
func (t *textMeasure) addWithFace(s string, f fontInfo) {
	size := f.font.size
	m := f.face.Metrics()
	faceLineH := m.LineHeight
	if faceLineH == 0 {
		faceLineH = lineFactor
	}
	lineH := f.lineHeight
	if lineH == 0 {
		lineH = Measure(max(lineFactor, faceLineH)) * size
	}
	gap := defaultLineGap
	if m.HasLineGap {
		gap = m.LineGap
	}
	firstLineH := Measure(max(1, faceLineH-gap)) * size
	// The advances are in thousandths of an em, which is PDF glyph space and
	// what a face is asked for (text.js:181).
	scale := size / 1000
	fallback := m.DefaultWidth
	if fallback == 0 {
		// `pdfFont.charsToGlyphs(" ")[0].width` (text.js:179). A face with no
		// space either measures every unknown rune at nought, which is what
		// pdf.js would do with it too.
		fallback = f.face.Advance(' ')
	}
	for i, line := range splitLines(s) {
		if i > 0 {
			t.glyphs = append(t.glyphs, glyph{eol: true})
		}
		// One glyph per RUNE here, not per UTF-16 code unit: pdf.js encodes
		// the string and asks the font for its glyphs (text.js:186-187), and a
		// character outside the basic plane is one glyph there. The no-font
		// branch counts differently on purpose; see [textMeasure.addWithoutFace].
		for _, r := range line {
			w := f.face.Advance(r)
			if w == 0 {
				w = fallback
			}
			t.glyphs = append(t.glyphs, glyph{
				w:          Measure(w)*scale + f.font.letterSpacing,
				lineH:      lineH,
				firstLineH: firstLineH,
				space:      r == ' ',
			})
		}
	}
}

// addWithoutFace is the branch that has none (text.js:208-219): every glyph one
// em wide, every line 1.2 ems tall and the first line one em.
//
// The characters are counted as UTF-16 code units, not as runes, because
// pdf.js's line.split("") does: a character outside the basic plane is two
// glyphs there and so is two here. No corpus form writes one, and the day one
// does this will agree with the reference rather than with the Unicode
// standard, which is the point.
func (t *textMeasure) addWithoutFace(s string, size Measure) {
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
					w:          size,
					lineH:      lineFactor * size,
					firstLineH: size,
					space:      r == ' ',
				})
			}
		}
	}
}

// isLineSeparator says which characters pdf.js's split on a newline and
// U+2029, the Unicode paragraph separator, breaks at (text.js:186, 212). A
// carriage return is NOT one of them, so text written with CRLF endings
// keeps its returns as ordinary characters, one glyph each.
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
//
// It is the same function in both regimes. Only the glyphs differ.
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
