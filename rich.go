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
// # What a style decides
//
// A style may name a typeface, a size, a weight, a posture, a letter spacing
// and a line height, and [textMeasure.pushData] reads all six
// (xhtml.js:249-307). Where the measurement has no fonts none of them reaches
// a glyph — the typeface is not found, so FontInfo replaces the whole of
// xfaFont with the default's (text.js:43-46), and the no-font branch of
// addString never reads a line height (:208-219). Where it has fonts, all six
// decide the run's advances and its two line heights.
//
// The margins reach the height through addPara either way, and xfa-spacerun,
// which decides whether runs of spaces are collapsed, is applied where the
// text is read rather than here (see [Node.addRun]).
//
// # One deviation, measured before it was kept
//
// pdf.js splits the style on ";" and does NOT trim what comes back, so a
// declaration written as "font-size:8pt; margin-top:2pt" has its second half
// ignored entirely: the key is " margin-top" and no case matches it. This
// trims, because a rule that reads a form differently from the reference is
// worse than a rule that reads it oddly and this one cannot be seen: not one
// of the corpus's 124 319 style attributes writes a space after a semicolon.
//
// # The defect not to copy
//
// <b> and <i> call measure.pushFont (xhtml.js:377, 467). TextMeasure has no
// such method — it has pushData — so a rich text carrying either throws a
// TypeError inside pdf.js's own measurement. No unsized leaf of the 560-form
// corpus writes one. They are measured here as the plain spans they are, which
// with no fonts is what they would come to anyway; with fonts they keep the
// face outside them rather than turning bold or italic, which is a shortfall
// against a reference that cannot run at all.

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
	f, m, lineHeight := styleFont(n.Attr["style"])
	t.pushData(f, m, lineHeight)
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
	t.popFont()
}

// styleFont reads everything a span's style says that a measurement reads: the
// five font values, the paragraph margins, and the line height.
//
// It is one pass over the declarations because pdf.js's is (xhtml.js:247-307),
// and the three answers it fills in are the three arguments of pushData.
func styleFont(style string) (f xfaFont, m paraMargin, lineHeight Measure) {
	for _, decl := range strings.Split(style, ";") {
		key, value, _ := strings.Cut(decl, ":")
		switch strings.TrimSpace(key) {
		case "font-family":
			f.typeface = stripQuotes(strings.TrimSpace(value))
		case "font-size":
			f.size = styleMeasure(value)
		case "font-weight":
			// Taken as written, whatever it says. pdf.js does not validate a
			// span's weight or posture against the two values a <font>
			// element's are held to (xhtml.js:255-260), so "700" is neither
			// bold nor normal and selects the regular face by falling through
			// [selectFace].
			f.weight = strings.TrimSpace(value)
		case "font-style":
			f.posture = strings.TrimSpace(value)
		case "letter-spacing":
			f.letterSpacing = styleMeasure(value)
		case "line-height":
			lineHeight = styleMeasure(value)
		}
	}
	m = paraOfStyle(style)
	return f, m, lineHeight
}

// stripQuotes takes the quotes off a CSS font family, which is pdf.js's
// (utils.js:28-33): a leading quote of either kind means the LAST character is
// dropped too, whether or not it is one.
func stripQuotes(s string) string {
	if len(s) > 0 && (s[0] == '\'' || s[0] == '"') {
		if len(s) < 2 {
			// slice(1, -1) on a string of one character is the empty string
			// in JavaScript and a panic in Go, which is the one place the two
			// languages part. A style of a single quote is not a font family
			// and never was; it is here because a panic is not a reading.
			return ""
		}
		return s[1 : len(s)-1]
	}
	return s
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

// styleMeasure reads a length written in a CSS style, which is
// [getMeasurement] with nought for a value it cannot read at all.
func styleMeasure(s string) Measure { return getMeasurement(s, 0) }

// getMeasurement reads a length the way pdf.js reads every one of them
// (utils.js:78-102), and it is not the same language as [ParseMeasure].
//
// It differs in four ways, each of them right for the reference and wrong for
// an XFA attribute read strictly: "px" IS a unit here, because pdf.js's table
// has one; a unit it does not know is DROPPED and the number kept, which is
// what makes letterSpacing="-0.002em" a length of -0.002 rather than an error;
// the pattern is unanchored, so a number is found wherever it first appears
// and "=0mm" is nought; and a string holding no number at all is the default
// rather than a refusal.
//
// This package reads x, y, w and h strictly, following pdfium, and reports the
// element rather than drawing it a third of its width out. It reads a FONT's
// two lengths leniently, following pdf.js, because a size or a letter spacing
// nobody can read does not stop pdfium laying the form out either — and
// because the corpus writes one: 24 draws of ca-cra__t1206 and t1207 set
// letterSpacing in "em", a unit relative to the very size being resolved.
// Refusing those cost four forms their whole table.
func getMeasurement(s string, def Measure) Measure {
	value, unit, ok := firstNumber(strings.TrimSpace(s))
	if !ok {
		return def
	}
	if value == 0 {
		// pdf.js returns before it looks the unit up (utils.js:93-95), so
		// nought in a unit it does not know is still nought.
		return 0
	}
	if per, known := styleUnit[unit]; known {
		return Measure(value * per)
	}
	return Measure(value)
}

// firstNumber is /([+-]?\d+\.?\d*)(.*)/ applied to a string: the first number
// in it, whatever follows that number, and whether there was one.
//
// The pattern is unanchored, so it is a scan rather than a parse of the head.
// A sign counts only where a digit follows it, because the engine backtracks
// over an optional group rather than failing on it.
func firstNumber(s string) (value float64, unit string, ok bool) {
	for i := 0; i < len(s); i++ {
		j := i
		if s[j] == '+' || s[j] == '-' {
			j++
		}
		if j >= len(s) || s[j] < '0' || s[j] > '9' {
			continue
		}
		for j < len(s) && s[j] >= '0' && s[j] <= '9' {
			j++
		}
		if j < len(s) && s[j] == '.' {
			j++
			for j < len(s) && s[j] >= '0' && s[j] <= '9' {
				j++
			}
		}
		v, err := strconv.ParseFloat(s[i:j], 64)
		if err != nil {
			// A run of digits too long to be a float64. parseFloat answers
			// Infinity there and pdf.js carries it; this treats it as no
			// number, which is the only place the two part and which no
			// template reaches.
			return 0, "", false
		}
		// The unit is whatever follows, NOT trimmed: pdf.js trims the whole
		// string once and then looks the rest up as it stands, so "8 pt" is
		// eight of no unit at all.
		return v, s[j:], true
	}
	return 0, "", false
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
