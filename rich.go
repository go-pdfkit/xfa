// Copyright (c) 2026, the go-pdfkit/xfa authors
// All rights reserved.
//
// SPDX-License-Identifier: BSD-3-Clause

package xfa

import (
	"strconv"
	"strings"
)

// Measuring the XHTML of an <exData contentType="text/html">.
//
// This is pdf.js's XhtmlObject[$pushGlyphs] (xhtml.js:239-329) and the two
// elements that override it: <br>, which is a line break and nothing else
// (:409-411), and <p>, which is a line break AND a paragraph, so its margins
// are charged to the height (:490-495).
//
// # What a style decides, and what it cannot
//
// A style may name a typeface, a size, a weight, a posture and a letter
// spacing, and in the regime this measures in — no font resolved, see the note
// on [emWidth] — every one of them is discarded before a glyph is made: the
// typeface is not found, so FontInfo replaces the whole of xfaFont with the
// default's (text.js:42-46). It may also name a line height, which the no-font
// branch of addString never reads (:212-219).
//
// What is left is the margins, which reach the height through addPara, and
// xfa-spacerun, which decides whether runs of spaces are collapsed and is
// applied where the text is read rather than here (see [Node.addRun]).
//
// # The defect not to copy
//
// <b> and <i> call measure.pushFont (xhtml.js:377, 467). TextMeasure has no
// such method — it has pushData — so a rich text carrying either throws a
// TypeError inside pdf.js's own measurement. No unsized leaf of the 560-form
// corpus writes one. They are measured here as the plain spans they are, which
// in this regime is what they would come to anyway, since the weight and the
// posture are discarded with the typeface.

// pushRich adds the glyphs of a rich text's markup, in the order the markup
// writes them.
func (t *textMeasure) pushRich(n *Node) {
	switch n.Kind {
	case textKind:
		t.addString(n.Text)
		return
	case "br":
		// pdf.js's Br does NOT call super, so it opens no paragraph
		// (xhtml.js:409-411).
		t.addString("\n")
		return
	}
	t.pushPara(paraOfStyle(n.Attr["style"]))
	for _, k := range n.Kids {
		t.pushRich(k)
	}
	if n.Kind == "p" {
		// A <p> ends with a line break, always — even the last one of a body,
		// where it adds no line because compute charges a line's height only
		// once, when the line after it begins (xhtml.js:490-495).
		t.addString("\n")
		t.addPara()
	}
	t.popPara()
}

// paraOfStyle reads the margins a rich text's style asks for.
//
// It reads only the margins. See the note at the top of this file for what
// else a style may say and why none of it reaches a glyph here.
func paraOfStyle(style string) paraMargin {
	var m paraMargin
	for _, decl := range strings.Split(style, ";") {
		key, value, _ := strings.Cut(decl, ":")
		switch strings.TrimSpace(key) {
		case "margin":
			// pdf.js splits the shorthand on /" \t"/ — a space followed by a
			// TAB, not a run of either — so "3pt 6pt" is one value and not
			// two, and that one value is read by a pattern that stops at the
			// first unit and keeps going (xhtml.js:267-293). This is that
			// split, because a rule that reads a form differently from the
			// reference is worse than a rule that reads it oddly.
			vals := strings.Split(value, " \t")
			switch len(vals) {
			case 1, 2:
				// One value is every side; two are vertical then horizontal.
				m.top, m.bottom = styleMeasure(vals[0]), styleMeasure(vals[0])
			case 3, 4:
				// Three are top, horizontal, bottom; four are clockwise from
				// the top. Either way the bottom is the third.
				m.top, m.bottom = styleMeasure(vals[0]), styleMeasure(vals[2])
			default:
				// pdf.js's switch has no default, so a shorthand of more than
				// four values leaves every inset unwritten and every one of
				// them is inherited (xhtml.js:269-292).
				continue
			}
			m.hasTop, m.hasBottom = true, true
		case "margin-top":
			m.top, m.hasTop = styleMeasure(value), true
		case "margin-bottom":
			m.bottom, m.hasBottom = styleMeasure(value), true
		}
	}
	return m
}

// styleMeasure reads a length written in a CSS style rather than in an XFA
// attribute, and they are not the same language.
//
// This is pdf.js's getMeasurement (utils.js:78-103) and it differs from
// [ParseMeasure] in three ways, each of them right for a style and wrong for
// an attribute: "px" IS a unit here, because a style is CSS and CSS has one; a
// unit it does not know is dropped and the number kept; and anything it cannot
// read at all is nought rather than an error, because a style pdf.js cannot
// parse does not stop it laying the form out.
func styleMeasure(s string) Measure {
	s = strings.TrimSpace(s)
	// The pattern is /([+-]?\d+\.?\d*)(.*)/: a number at the START, and
	// whatever follows is the unit.
	i := 0
	if i < len(s) && (s[i] == '+' || s[i] == '-') {
		i++
	}
	digits := i
	for i < len(s) && s[i] >= '0' && s[i] <= '9' {
		i++
	}
	if i == digits {
		return 0
	}
	if i < len(s) && s[i] == '.' {
		i++
		for i < len(s) && s[i] >= '0' && s[i] <= '9' {
			i++
		}
	}
	v, err := strconv.ParseFloat(s[:i], 64)
	if err != nil || v == 0 {
		return 0
	}
	per, known := styleUnit[strings.TrimSpace(s[i:])]
	if !known {
		return Measure(v)
	}
	return Measure(v * per)
}

// styleUnit is pdf.js's dimConverters (utils.js:19-25). A CSS pixel is one
// point there, which is not the CSS pixel of anything else; it is kept because
// the reference's numbers are what a form is judged against.
var styleUnit = map[string]float64{
	"pt": 1,
	"cm": 72 / 2.54,
	"mm": 72 / 25.4,
	"in": 72,
	"px": 1,
}
