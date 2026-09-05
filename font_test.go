// Copyright (c) 2026, the go-pdfkit/xfa authors
// All rights reserved.
//
// SPDX-License-Identifier: BSD-3-Clause

package xfa

import (
	"math"
	"strings"
	"testing"
)

// A stub is a Face a test can state in one line: every rune the same advance,
// except the ones it names.
type stub struct {
	adv     float64
	per     map[rune]float64
	metrics FaceMetrics
}

func (s stub) Advance(r rune) float64 {
	if w, ok := s.per[r]; ok {
		return w
	}
	return s.adv
}

func (s stub) Metrics() FaceMetrics { return s.metrics }

// oneEm is a face whose every glyph is a whole em, so a measurement made with
// it can be read off the character count.
func oneEm() stub {
	return stub{adv: 1000, metrics: FaceMetrics{LineHeight: 1.2, HasLineGap: true, LineGap: 0.2}}
}

func TestAFamilyWithNoRegularFaceTakesOneFromWhateverItHas(t *testing.T) {
	// `pdfFont.regular ||= pdfFont.italic || pdfFont.bold || pdfFont.bolditalic`
	// (fonts.js:32-34), in that order.
	italic, bold, boldItalic := stub{adv: 1}, stub{adv: 2}, stub{adv: 3}
	for _, tc := range []struct {
		what string
		in   Typeface
		want float64
	}{
		{"the italic comes first", Typeface{Italic: italic, Bold: bold, BoldItalic: boldItalic}, 1},
		{"then the bold", Typeface{Bold: bold, BoldItalic: boldItalic}, 2},
		{"then the bold italic", Typeface{BoldItalic: boldItalic}, 3},
	} {
		t.Run(tc.what, func(t *testing.T) {
			s := NewFontSet()
			s.Add("F", tc.in)
			got, _ := s.find("F")
			if got.Regular.(stub).adv != tc.want {
				t.Errorf("the regular face is %v, want %v", got.Regular.(stub).adv, tc.want)
			}
		})
	}
	t.Run("and a family with nothing at all keeps nothing", func(t *testing.T) {
		s := NewFontSet()
		s.Add("F", Typeface{})
		if got, _ := s.find("F"); got.Regular != nil {
			t.Error("a family of no faces was given one")
		}
	})
}

func TestTheFirstFamilyAddedIsTheDefaultAndAddingTwiceReplaces(t *testing.T) {
	s := NewFontSet()
	s.Add("First", Typeface{Regular: stub{adv: 1}})
	s.Add("Second", Typeface{Regular: stub{adv: 2}})
	def, name := s.getDefault()
	if name != "First" || def.Regular.(stub).adv != 1 {
		t.Errorf("the default is %q, want First", name)
	}
	// Replacing a family must not add it to the order a second time, or the
	// prefix passes would answer with a font that is no longer there.
	s.Add("First", Typeface{Regular: stub{adv: 9}})
	if len(s.order) != 2 {
		t.Errorf("the order holds %d families, want 2", len(s.order))
	}
	if got, _ := s.find("First"); got.Regular.(stub).adv != 9 {
		t.Error("adding a family twice did not replace it")
	}
}

func TestANilFontSetFindsNothing(t *testing.T) {
	var s *FontSet
	if got, _ := s.find("Helvetica"); got != nil {
		t.Error("a nil set answered")
	}
	if got, name := s.getDefault(); got != nil || name != "" {
		t.Error("a nil set has a default")
	}
}

func TestFindTakesThePassesInOrder(t *testing.T) {
	// One set, exercising every pass of fonts.js:90-155 in turn.
	s := NewFontSet()
	s.Add("Helvetica", Typeface{Regular: stub{adv: 1}, Name: "Helvetica"})
	s.Add("Times New Roman", Typeface{Regular: stub{adv: 2}, Name: "TimesNewRomanPSMT"})
	s.Add("Zapf Chancery", Typeface{Regular: stub{adv: 3}, Name: "ZapfChanceryMT"})
	s.Add("ArialNarrow", Typeface{Regular: stub{adv: 4}, Name: "ArialNarrow"})
	for _, tc := range []struct {
		what, ask, want string
	}{
		{"the name exactly", "Helvetica", "Helvetica"},
		{"the name with its noise stripped, exactly", "Arial-Narrow", "ArialNarrow"},
		{"the name with its noise stripped, then as a prefix", "Times-New-Roman", "Times New Roman"},
		{"a prefix of a family, lower cased", "helveticaneue", ""},
		{"a family that has what was asked as a PREFIX", "Times", "Times New Roman"},
		{"the regular face's own name", "TimesNewRomanPSMT", "Times New Roman"},
		{"the same name once the psmt is stripped too", "ZapfChanceryPSMT", "Zapf Chancery"},
		{"and nothing where nothing answers", "Wingdings", ""},
	} {
		t.Run(tc.what, func(t *testing.T) {
			_, got := s.find(tc.ask)
			if got != tc.want {
				t.Errorf("%q reached %q, want %q", tc.ask, got, tc.want)
			}
			// Every answer, including a miss, must come back the same way the
			// second time: find caches what it resolved (fonts.js:99, 152).
			if _, again := s.find(tc.ask); again != got {
				t.Errorf("asked twice, %q reached %q then %q", tc.ask, got, again)
			}
		})
	}
}

func TestStripFontNameIsAnOrderedAlternationAndNotAChainOfReplacements(t *testing.T) {
	// The order of /[,\-_ ]|bolditalic|bold|italic|regular|it/gi decides the
	// answer: "bolditalic" is tried before "bold", and "it" last of all.
	for _, tc := range []struct{ in, want string }{
		{"Arial-Bold_Italic, ", "Arial"},
		{"ArialBoldItalic", "Arial"},
		{"Regular", ""},
		{"Italic", ""},
		{"Wingdings", "Wingdings"},
		{"Ω-Ω", "ΩΩ"},
	} {
		if got := stripFontName(tc.in); got != tc.want {
			t.Errorf("%q stripped to %q, want %q", tc.in, got, tc.want)
		}
	}
	if got := stripPSMT("TimesPSMTNewMT"); got != "TimesNew" {
		t.Errorf("the psmt pattern gave %q", got)
	}
}

func TestSelectFaceReadsThePostureFirstAndTheWeightInsideIt(t *testing.T) {
	full := &Typeface{
		Regular: stub{adv: 1}, Bold: stub{adv: 2},
		Italic: stub{adv: 3}, BoldItalic: stub{adv: 4},
	}
	for _, tc := range []struct {
		posture, weight string
		want            float64
	}{
		{"normal", "normal", 1},
		{"normal", "bold", 2},
		{"italic", "normal", 3},
		{"italic", "bold", 4},
	} {
		got := selectFace(xfaFont{posture: tc.posture, weight: tc.weight}, full)
		if got.(stub).adv != tc.want {
			t.Errorf("%s %s chose %v, want %v", tc.posture, tc.weight, got.(stub).adv, tc.want)
		}
	}
	// A family that has the face asked for at one weight and not the other
	// answers nothing, and the caller falls back (text.js:49-51).
	if f := selectFace(xfaFont{posture: "italic"}, &Typeface{Regular: stub{}}); f != nil {
		t.Error("a family with no italic answered with one")
	}
}

func TestTheDefaultFontChainIsHelveticaThenMyriadThenArialThenTheFirstAdded(t *testing.T) {
	for _, tc := range []struct {
		what, family, want string
	}{
		{"Helvetica first", "Helvetica", "Helvetica"},
		{"then Myriad Pro", "Myriad Pro", "Myriad Pro"},
		{"then Arial", "Arial", "Arial"},
		{"then whatever was added first", "Bodoni", "Bodoni"},
	} {
		t.Run(tc.what, func(t *testing.T) {
			s := NewFontSet()
			s.Add(tc.family, Typeface{Regular: stub{adv: 500}})
			face, f := defaultFace(s)
			if face == nil || f.typeface != tc.want || f.size != defaultSize {
				t.Errorf("the default is %+v with face %v", f, face)
			}
		})
	}
	t.Run("and with nothing to be had it is Courier at ten points, with no face", func(t *testing.T) {
		s := NewFontSet()
		s.Add("Empty", Typeface{})
		face, f := defaultFace(s)
		if face != nil || f != defaultXfaFont {
			t.Errorf("the last resort is %+v with face %v", f, face)
		}
	})
}

// withFace measures a string with one face of the given metrics, at the given
// size, and returns the width and the height.
func withFace(text string, face Face, f xfaFont, lineHeight, maxWidth Measure) (Measure, Measure) {
	s := NewFontSet()
	s.Add("F", Typeface{Regular: face})
	f.typeface = "F"
	m := newTextMeasure(s, &f, paraMargin{}, lineHeight)
	m.addString(text)
	w, h, _ := m.compute(maxWidth)
	return w, h
}

func TestAFaceGivesTheAdvancesAndTheTwoLineHeights(t *testing.T) {
	// One em per glyph at eight points is eight points a glyph; the line
	// height is max(1.2, faceLineHeight) * size and the first line is
	// (faceLineHeight - lineGap) * size, never under one em (text.js:175-180).
	face := stub{adv: 1000, metrics: FaceMetrics{LineHeight: 1.5, LineGap: 0.3, HasLineGap: true}}
	_, h := withFace("abcd", face, xfaFont{size: 8}, 0, 1000)
	if h != 1.2*8 {
		t.Errorf("one line came to %v, want %v", h, 1.2*8)
	}
	_, h = withFace("ab\ncd", face, xfaFont{size: 8}, 0, 1000)
	// first line (1.5-0.3)*8 = 9.6, then one line of 1.5*8 = 12.
	if h != 9.6+12 {
		t.Errorf("two lines came to %v, want 21.6", h)
	}
	t.Run("a face with no line height of its own takes 1.2", func(t *testing.T) {
		bare := stub{adv: 1000, metrics: FaceMetrics{HasLineGap: true, LineGap: 0.2}}
		_, h := withFace("a\nb", bare, xfaFont{size: 10}, 0, 1000)
		if h != 10+12 {
			t.Errorf("two lines came to %v, want 22", h)
		}
	})
	t.Run("and one that says nothing about its gap takes 0.2", func(t *testing.T) {
		bare := stub{adv: 1000, metrics: FaceMetrics{LineHeight: 2}}
		_, h := withFace("a\nb", bare, xfaFont{size: 10}, 0, 1000)
		// first line max(1, 2-0.2)*10 = 18, then 2*10 = 20.
		if h != 18+20 {
			t.Errorf("two lines came to %v, want 38", h)
		}
	})
	t.Run("a paragraph's line height overrides the face's", func(t *testing.T) {
		_, h := withFace("a\nb", oneEm(), xfaFont{size: 10}, 30, 1000)
		// The FIRST line is still the face's; only the others take the para's.
		if h != 10+30 {
			t.Errorf("two lines came to %v, want 40", h)
		}
	})
}

func TestAGlyphTheFaceHasNoAdvanceForTakesTheFallback(t *testing.T) {
	// `glyph.width || fallbackWidth`, and the fallback is the face's default
	// width or, failing that, its space (text.js:179, 190).
	missing := stub{adv: 0, per: map[rune]float64{' ': 700}, metrics: FaceMetrics{HasLineGap: true}}
	w, _ := withFace("ab", missing, xfaFont{size: 10}, 0, 10000)
	if want := Measure(widthFactor * 2 * 7); math.Abs(float64(w-want)) > 1e-9 {
		t.Errorf("two unknown glyphs came to %v, want %v", w, want)
	}
	stated := stub{adv: 0, per: map[rune]float64{' ': 700}, metrics: FaceMetrics{DefaultWidth: 100, HasLineGap: true}}
	w, _ = withFace("ab", stated, xfaFont{size: 10}, 0, 10000)
	if want := Measure(widthFactor * 2 * 1); math.Abs(float64(w-want)) > 1e-9 {
		t.Errorf("a stated default width gave %v, want %v", w, want)
	}
}

func TestLetterSpacingIsAddedToEveryAdvance(t *testing.T) {
	w, _ := withFace("abc", oneEm(), xfaFont{size: 10, letterSpacing: 2}, 0, 10000)
	if want := Measure(widthFactor * 3 * 12); math.Abs(float64(w-want)) > 1e-9 {
		t.Errorf("three glyphs and a spacing of 2 came to %v, want %v", w, want)
	}
}

func TestASpanInheritsEveryValueItLeavesFalsy(t *testing.T) {
	// `xfaFont[name] ||= lastFont.xfaFont[name]` (text.js:103-111) is a FALSY
	// test, so a span writing a size of nought inherits rather than overriding.
	s := NewFontSet()
	s.Add("F", Typeface{Regular: oneEm(), Bold: oneEm()})
	m := newTextMeasure(s, &xfaFont{typeface: "F", size: 20, letterSpacing: 3, weight: "bold"}, paraMargin{}, 0)
	m.pushData(xfaFont{}, paraMargin{}, 0)
	got := m.top().font
	if got.size != 20 || got.letterSpacing != 3 || got.weight != "bold" || got.typeface != "F" {
		t.Errorf("the span inherited %+v", got)
	}
	m.popFont()
	if len(m.stack) != 1 {
		t.Error("popping did not close the span")
	}
}

func TestAWeightAFamilyHasNoFaceForFallsBackWholesale(t *testing.T) {
	// selectFont answers nothing for a family with no bold face, and FontInfo
	// then replaces the WHOLE of xfaFont with the default's (text.js:49-51) —
	// so asking for bold at twenty points in a family that has only a regular
	// face is measured at ten, upright, in whatever the default chain finds.
	s := NewFontSet()
	s.Add("F", Typeface{Regular: oneEm()})
	m := newTextMeasure(s, &xfaFont{typeface: "F", size: 20, weight: "bold"}, paraMargin{}, 0)
	if got := m.top().font.size; got != defaultSize {
		t.Errorf("the size survived at %v, want the default's", got)
	}
}

func TestASpanNamingAMissingTypefaceKeepsTheFaceOutsideIt(t *testing.T) {
	// `fontInfo.pdfFont ||= lastFont.pdfFont` (text.js:124): the five values
	// are replaced by the default's and the FACE is not.
	s := NewFontSet()
	s.Add("F", Typeface{Regular: oneEm()})
	m := newTextMeasure(s, &xfaFont{typeface: "F", size: 20}, paraMargin{}, 0)
	m.pushData(xfaFont{typeface: "NoSuchFont"}, paraMargin{}, 0)
	top := m.top()
	if top.face == nil {
		t.Fatal("the span lost the face outside it")
	}
	if top.font.size != defaultSize {
		t.Errorf("the span kept a size of %v, want the default's", top.font.size)
	}
}

func TestASpanInheritsALineHeightItDoesNotWrite(t *testing.T) {
	s := NewFontSet()
	s.Add("F", Typeface{Regular: oneEm()})
	m := newTextMeasure(s, &xfaFont{typeface: "F", size: 10}, paraMargin{}, 40)
	m.pushData(xfaFont{}, paraMargin{}, 0)
	if got := m.top().lineHeight; got != 40 {
		t.Errorf("the span's line height is %v, want 40", got)
	}
}

func TestAFontElementsDefaultsAreTheReferences(t *testing.T) {
	root := parse(t, `<template><subform name="f"><draw name="A"><font/></draw></subform></template>`)
	draw := root.Child("subform").Child("draw")
	f := templateFont(draw)
	if *f != defaultXfaFont {
		t.Errorf("an empty <font> read as %+v", *f)
	}
	if templateFont(root.Child("subform")) != nil {
		t.Error("an element with no <font> was given one")
	}
}

func TestAFontsTwoLengthsAreReadLeniently(t *testing.T) {
	// getMeasurement drops a unit it does not know and keeps the number
	// (utils.js:78-102), which is how letterSpacing="-0.002em" is a length.
	// 24 draws of the corpus write exactly that.
	root := parse(t, `<template><subform name="f"><draw name="A">`+
		`<font typeface="X" posture="italic" weight="bold" size="fourteen" letterSpacing="-0.002em"/>`+
		`</draw></subform></template>`)
	f := templateFont(root.Child("subform").Child("draw"))
	if f.typeface != "X" || f.posture != "italic" || f.weight != "bold" {
		t.Errorf("the three strings read as %+v", *f)
	}
	if f.size != defaultSize {
		t.Errorf("an unreadable size read as %v, want the default", f.size)
	}
	if f.letterSpacing != -0.002 {
		t.Errorf("the letter spacing read as %v, want -0.002", f.letterSpacing)
	}
}

func TestAnUnrecognisedPostureOrWeightIsTheFirstOfItsList(t *testing.T) {
	root := parse(t, `<template><subform name="f"><draw name="A">`+
		`<font posture="oblique" weight="heavy"/></draw></subform></template>`)
	f := templateFont(root.Child("subform").Child("draw"))
	if f.posture != "normal" || f.weight != "normal" {
		t.Errorf("read as %+v", *f)
	}
}

func TestAFontIsInheritedFromTheNearestElementAboveIt(t *testing.T) {
	root := parse(t, `<template><subform name="f"><font typeface="Outer"/>`+
		`<subform name="g"><font typeface="Middle"/><draw name="A"/></subform>`+
		`<draw name="B"/></subform></template>`)
	fs := fontSource{up: templateParents(root), root: root}
	outer := root.Child("subform")
	middle := outer.Children("subform")[0]
	for _, tc := range []struct {
		what string
		node *Node
		want string
	}{
		{"the nearest wins", middle.Child("draw"), "Middle"},
		{"and the walk goes on past an element with none", outer.Children("draw")[0], "Outer"},
	} {
		t.Run(tc.what, func(t *testing.T) {
			if got := fs.fontOf(tc.node); got == nil || got.typeface != tc.want {
				t.Errorf("resolved to %v, want %q", got, tc.want)
			}
		})
	}
	t.Run("the template root's own font is out of reach", func(t *testing.T) {
		// pdf.js stops AT the root (html_utils.js:234), so a <font> written on
		// <template> itself is never inherited.
		bare := parse(t, `<template><font typeface="Root"/><subform name="f"><draw name="A"/></subform></template>`)
		fs := fontSource{up: templateParents(bare), root: bare}
		if got := fs.fontOf(bare.Child("subform").Child("draw")); got != nil {
			t.Errorf("the root's font was inherited as %v", got)
		}
	})
	t.Run("and an element with no links above it has none", func(t *testing.T) {
		var fs fontSource
		if got := fs.fontOf(outer.Children("draw")[0]); got != nil {
			t.Errorf("resolved to %v with no links", got)
		}
	})
}

func TestAParagraphsLineHeightIsRead(t *testing.T) {
	root := parse(t, `<template><subform name="f"><draw name="A">`+
		`<para lineHeight="13pt"/></draw></subform></template>`)
	_, lineHeight, why := paraOf(root.Child("subform").Child("draw"))
	if why != "" || lineHeight != 13 {
		t.Errorf("read as %v (%s), want 13", lineHeight, why)
	}
	bad := parse(t, `<template><subform name="f"><draw name="A">`+
		`<para lineHeight="13furlongs"/></draw></subform></template>`)
	if _, _, why := paraOf(bad.Child("subform").Child("draw")); why == "" {
		t.Error("a line height that is not a length was accepted")
	}
}

func TestStripQuotesIsTheReferencesAndNotACorrectOne(t *testing.T) {
	// A leading quote of either kind means the LAST character goes too,
	// whether or not it is one (utils.js:28-33).
	for _, tc := range []struct{ in, want string }{
		{`"Myriad Pro"`, "Myriad Pro"},
		{`'Arial'`, "Arial"},
		{`"Arial`, "Aria"},
		{`Arial`, "Arial"},
		{`'`, ""},
		{``, ""},
	} {
		if got := stripQuotes(tc.in); got != tc.want {
			t.Errorf("%q stripped to %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestAStyleSaysAllSixThingsAFontIs(t *testing.T) {
	f, m, lineHeight := styleFont(
		`font-family:'Arial';font-size:8pt;font-weight:bold;font-style:italic;` +
			`letter-spacing:1pt;line-height:9pt;margin-top:2pt`)
	if f.typeface != "Arial" || f.size != 8 || f.weight != "bold" || f.posture != "italic" ||
		f.letterSpacing != 1 {
		t.Errorf("the style read as %+v", f)
	}
	if lineHeight != 9 || m.top != 2 || !m.hasTop {
		t.Errorf("the line height is %v and the margin %+v", lineHeight, m)
	}
}

func TestALengthIsFoundWhereverItFirstAppears(t *testing.T) {
	// /([+-]?\d+\.?\d*)(.*)/ is unanchored, and a sign counts only where a
	// digit follows it.
	for _, tc := range []struct {
		in   string
		def  Measure
		want Measure
	}{
		{"8pt", 0, 8},
		{"=0mm", 0, 0},
		{"-2.5in", 0, -180},
		{"+3", 0, 3},
		{"a4pt", 0, 4},
		{"+", 7, 7},
		{"", 7, 7},
		{"none", 7, 7},
		{"0furlongs", 7, 0},
		{"5furlongs", 7, 5},
		{"8 pt", 0, 8},
		{strings.Repeat("9", 400) + "pt", 7, 7},
	} {
		if got := getMeasurement(tc.in, tc.def); got != tc.want {
			t.Errorf("%q read as %v, want %v", tc.in, got, tc.want)
		}
	}
}

func TestPlaceWithFontsMeasuresALeafWithThem(t *testing.T) {
	// One draw of eight characters in a box 40 points wide. With no fonts each
	// character is ten points and it takes two lines; with a face of half an em
	// at eight points each is four, and it takes one.
	src := `<template><subform name="f" layout="tb">
	  <pageSet><pageArea name="P"><contentArea x="0pt" y="0pt" w="200pt" h="400pt"/>
	    <medium short="200pt" long="400pt"/></pageArea></pageSet>
	  <draw name="A" w="40pt"><font typeface="F" size="8pt"/>
	    <value><text>abcdefgh</text></value></draw>
	</subform></template>`
	bare := Place(Expand(parse(t, src), nil))
	half := stub{adv: 500, metrics: FaceMetrics{LineHeight: 1.2, LineGap: 0.2, HasLineGap: true}}
	s := NewFontSet()
	s.Add("F", Typeface{Regular: half})
	withFonts := PlaceWithFonts(Expand(parse(t, src), nil), s)
	if len(bare.Pages) != 1 || len(withFonts.Pages) != 1 {
		t.Fatal("the form did not lay out")
	}
	gotBare := bare.Pages[0].Boxes[0].H
	gotFonts := withFonts.Pages[0].Boxes[0].H
	if gotBare != 10+12 {
		t.Errorf("with no fonts the draw is %v tall, want two lines of the no-font regime", gotBare)
	}
	if gotFonts != 8 {
		t.Errorf("with a face the draw is %v tall, want one line of eight points", gotFonts)
	}
}

func TestPlaceWithNoFontsIsPlace(t *testing.T) {
	if l := PlaceWithFonts(nil, nil); len(l.Pages) != 0 {
		t.Error("a nil form laid something out")
	}
}
