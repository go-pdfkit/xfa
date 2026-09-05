// Copyright (c) 2026, the go-pdfkit/xfa authors
// All rights reserved.
//
// SPDX-License-Identifier: BSD-3-Clause

package xfa

import "strings"

// The fonts a measurement is made with, and how a template's typeface name
// finds one of them.
//
// # Why they come from outside
//
// A template names typefaces and says nothing about them. The fonts themselves
// are in the PDF the XFA package travelled in, which this package does not
// read, and on the machine, which this package must not depend on. So the font
// set belongs to the caller, and this file is what the caller fills in: an
// interface of two methods, a family of four faces, and an ordered set of
// families with pdf.js's own fuzzy lookup over it.
//
// That is also how the reference does it. FontFinder's constructor takes the
// document's fonts as an argument (fonts.js:21-25) and never looks for one
// itself; [FontSet] is that map, and [FontSet.find] is its `find`, pass for
// pass.
//
// Nothing here reads a font file. Glyph advances, units per em and the
// vertical metrics belong to a font parser — go-opentype/opentype has all
// three — and a caller wires one to [Face] in a dozen lines. Writing a second
// font stack inside a layout package would be the wrong place for it and would
// cost this package the property that it has no dependencies.

// A Face is one concrete typeface at one weight and one posture: the whole of
// what a measurement asks of a font.
//
// The unit is the font's, not the page's. pdf.js measures with a PDF font,
// whose advances are in PDF glyph space — thousandths of an em — and scales
// them by the size divided by a thousand (text.js:181, 192). A face reading a
// TrueType file gets there by dividing the glyph's advance by the head table's
// units per em and multiplying by a thousand.
type Face interface {
	// Advance is how far the pen moves for r, in thousandths of an em.
	//
	// Nought means the face has no advance for it, and
	// [FaceMetrics.DefaultWidth] stands in — which is pdf.js's
	// `glyph.width || fallbackWidth` (text.js:190), a test that cannot tell a
	// missing glyph from one of zero width and does not need to.
	Advance(r rune) float64
	// Metrics are the face's vertical metrics and its fallback advance. It is
	// read once per run of text, so it may be computed rather than stored.
	Metrics() FaceMetrics
}

// FaceMetrics is what a face says about itself beyond its advances.
//
// The two vertical numbers are in EMS, and pdf.js says where a real font's
// come from: the line gap is hhea's, divided by the head table's units per em,
// and the line height is the ascent less the descent plus that gap, each of
// them scaled the same way (fonts.js:3005-3017). A face over an AFM metric
// file, which has no hhea, leaves both unwritten and takes the constants
// below.
type FaceMetrics struct {
	// LineHeight is how tall a line is, in ems. Nought means the face gives
	// none and 1.2 stands in (text.js:174).
	LineHeight float64
	// LineGap is the space between lines, in ems.
	LineGap float64
	// HasLineGap says the face gives one. A gap of NOUGHT and a gap the face
	// does not give are different: pdf.js tests `lineGap === undefined` and
	// falls back to 0.2 only for the second (text.js:176), so a face that
	// really has no gap must say so with HasLineGap and a LineGap of nought.
	HasLineGap bool
	// DefaultWidth is what stands in for a rune the face gives no advance
	// for, in thousandths of an em. Nought means the face's own space stands
	// in instead: `pdfFont.defaultWidth || pdfFont.charsToGlyphs(" ")[0].width`
	// (text.js:179).
	DefaultWidth float64
}

// A Typeface is one family's four faces.
//
// pdf.js builds exactly this shape, keyed by CSS font family, and fills the
// four properties by reading each PDF font's weight and italic angle
// (fonts.js:45-73). A family missing a face is not a defect: [selectFace]
// answers nil for one and the measurement falls back to the default font, the
// way FontInfo does (text.js:49-51).
type Typeface struct {
	// Regular is the upright face of normal weight. Where a family has none,
	// [FontSet.Add] fills it in from whichever of the other three the caller
	// gave — `pdfFont.regular ||= pdfFont.italic || pdfFont.bold ||
	// pdfFont.bolditalic` (fonts.js:32-34) — because everything downstream
	// asks for it by name.
	Regular Face
	// Bold, Italic and BoldItalic are the other three, each of them optional.
	Bold, Italic, BoldItalic Face
	// Name is the regular face's OWN name, as against the family it was filed
	// under: the PostScript name of the file, "ArialMT" for the family
	// "Arial". [FontSet.find]'s third and fifth passes match against it
	// (fonts.js:104-116, 130-141), which is how a template naming
	// "TimesNewRomanPSMT" reaches a family called "Times New Roman". Empty
	// where the caller has no such name, and then those passes simply do not
	// match.
	Name string
}

// A FontSet is the fonts a form may be measured with, in the order they were
// added.
//
// The order is not decoration. find's prefix passes push every family whose
// name starts with what was asked for and then take the FIRST of them
// (fonts.js:143-149), so which font a template naming "Helvetica" gets is
// decided by insertion order where a set holds two that would answer. pdf.js
// iterates a Map, which is insertion ordered; this keeps the order explicitly
// because Go's map does not have one.
//
// The zero value is not usable; see [NewFontSet]. A nil *FontSet is, and is
// what [Place] measures with: it finds nothing, so every leaf falls to the
// regime described at [defaultSize].
type FontSet struct {
	// order is the family names in the order they were added, and byFamily
	// their entries.
	order    []string
	byFamily map[string]*Typeface
	// cache is find's, memoising the fuzzy passes for a name it has already
	// been asked (fonts.js:97, 152).
	cache       map[string]*Typeface
	cacheFamily map[string]string
	// def is the first family added: FontFinder's `this.defaultFont ??= font`
	// (fonts.js:52), which is the last resort of the default-font chain.
	def *Typeface
	// defName is the family name of def, which the replacement xfaFont
	// carries.
	defName string
}

// NewFontSet is an empty set, ready to be filled with [FontSet.Add].
func NewFontSet() *FontSet {
	return &FontSet{
		byFamily:    map[string]*Typeface{},
		cache:       map[string]*Typeface{},
		cacheFamily: map[string]string{},
	}
}

// Add registers a family under the name a template would call it by.
//
// The first family added becomes the set's default, and a family whose regular
// face the caller did not give takes one from its other three. Adding a family
// twice replaces it. Add before measuring: it clears find's cache, but a
// [Layout] already computed is not recomputed.
func (s *FontSet) Add(family string, t Typeface) {
	if t.Regular == nil {
		switch {
		case t.Italic != nil:
			t.Regular = t.Italic
		case t.Bold != nil:
			t.Regular = t.Bold
		default:
			t.Regular = t.BoldItalic
		}
	}
	if _, seen := s.byFamily[family]; !seen {
		s.order = append(s.order, family)
	}
	s.byFamily[family] = &t
	if s.def == nil {
		s.def, s.defName = &t, family
	}
	clear(s.cache)
	clear(s.cacheFamily)
}

// find is the typeface a template's name asks for, or nil.
//
// It is FontFinder.find (fonts.js:90-155) and it is deliberately fuzzy,
// because the name in a template is the name of a font on the machine that
// authored it and the name in a PDF is the name of the file that was embedded.
// The passes, in order:
//
//  1. the name exactly, or a name already answered;
//  2. the name with [stripFontName]'s pattern removed, exactly;
//  3. lower case, as a PREFIX of a family name stripped the same way;
//  4. as a prefix of the regular face's OWN name, stripped the same way;
//  5. and 3 and 4 again with "psmt" and "mt" also removed from what was asked.
//
// Where more than one family answers a prefix, the first in insertion order
// wins and pdf.js warns; this does not warn, because a layout has nowhere to
// put the warning and the caller chose the set.
func (s *FontSet) find(name string) (*Typeface, string) {
	if s == nil {
		return nil, ""
	}
	if t, ok := s.byFamily[name]; ok {
		return t, name
	}
	if t, ok := s.cache[name]; ok {
		return t, s.cacheFamily[name]
	}
	stripped := stripFontName(name)
	if t, ok := s.byFamily[stripped]; ok {
		s.remember(name, t, stripped)
		return t, stripped
	}
	lower := strings.ToLower(stripped)
	t, family := s.byPrefix(lower)
	if t == nil {
		// pdf.js strips "psmt" and "mt" from what was ASKED for and never
		// from the families, so "TimesNewRomanPSMT" becomes "timesnewroman"
		// and reaches "Times New Roman" (fonts.js:118-120).
		t, family = s.byPrefix(stripPSMT(lower))
	}
	if t != nil {
		s.remember(name, t, family)
	}
	return t, family
}

// remember is find's cache, which pdf.js writes on every pass that answers
// (fonts.js:99, 152). The family is kept beside the typeface because the
// default-font replacement carries the name it was filed under, and a nested
// span inherits that name and asks for it again.
func (s *FontSet) remember(name string, t *Typeface, family string) {
	s.cache[name] = t
	s.cacheFamily[name] = family
}

// byPrefix is find's two prefix passes: over the family names first, and over
// the regular faces' own names where no family answered.
func (s *FontSet) byPrefix(name string) (*Typeface, string) {
	for _, family := range s.order {
		if strings.HasPrefix(strings.ToLower(stripFontName(family)), name) {
			return s.byFamily[family], family
		}
	}
	for _, family := range s.order {
		t := s.byFamily[family]
		if t.Name != "" && strings.HasPrefix(strings.ToLower(stripFontName(t.Name)), name) {
			return t, family
		}
	}
	return nil, ""
}

// getDefault is FontFinder.getDefault (fonts.js:86-88): the family added
// first, and the name it was added under.
func (s *FontSet) getDefault() (*Typeface, string) {
	if s == nil {
		return nil, ""
	}
	return s.def, s.defName
}

// fontNameNoise is what [stripFontName] removes, in the order pdf.js's
// alternation tries them: the separators first, then the four style words,
// then "it".
//
// The order decides the answer. "bolditalic" is tried before "bold", so
// "ArialBoldItalic" loses the whole word rather than leaving "italic" behind;
// and "it" is LAST, which is why "italic" is not eaten two letters at a time.
var fontNameNoise = []string{"bolditalic", "bold", "italic", "regular", "it"}

// stripFontName removes what a font name says about its own style, so that a
// family can be recognised through it.
//
// This is /[,\-_ ]|bolditalic|bold|italic|regular|it/gi (fonts.js:95), and it
// is a scanner rather than a chain of replacements because a JavaScript
// alternation is ordered and leftmost-first: at each position it tries the
// separators, then each word in turn, and copies the character where none
// matches. Replacing the words one after another would not be the same
// function — "bold" removed first would leave the "italic" of "bolditalic".
//
// It is not a good rule and it is not meant to be one. It exists because the
// names do not match, and a rule that reads a form differently from the
// reference is worse than a rule that reads it oddly.
func stripFontName(s string) string {
	return stripWords(s, fontNameNoise, true)
}

// psmtNoise is /psmt|mt/gi (fonts.js:118), the suffix a PostScript name of the
// Monotype foundry carries: "TimesNewRomanPSMT", "ArialMT".
var psmtNoise = []string{"psmt", "mt"}

// stripPSMT removes that suffix, wherever it falls.
func stripPSMT(s string) string { return stripWords(s, psmtNoise, false) }

// stripWords is the scanner both patterns are: at each position, drop a
// separator where the pattern has one, else drop the first word that matches
// there ignoring case, else keep the byte.
//
// seps says whether the pattern's separator class is part of it. Both patterns
// are ASCII, so scanning by byte is safe: a multi-byte rune's continuation
// bytes are all above 0x7F and match neither a separator nor a word.
func stripWords(s string, words []string, seps bool) string {
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); {
		if seps && strings.IndexByte(",-_ ", s[i]) >= 0 {
			i++
			continue
		}
		cut := 0
		for _, w := range words {
			if len(s)-i >= len(w) && strings.EqualFold(s[i:i+len(w)], w) {
				cut = len(w)
				break
			}
		}
		if cut > 0 {
			i += cut
			continue
		}
		b.WriteByte(s[i])
		i++
	}
	return b.String()
}

// selectFace is which of a family's four faces a <font> asks for, and it is
// pdf.js's selectFont (fonts.js:157-165): the posture decides first, the
// weight inside it.
//
// It may answer nil, for a family that has the face it was asked for and not
// the one it was asked for at that weight. The caller falls back to the
// default font, which is what FontInfo does (text.js:49-51).
func selectFace(f xfaFont, t *Typeface) Face {
	if f.posture == "italic" {
		if f.weight == "bold" {
			return t.BoldItalic
		}
		return t.Italic
	}
	if f.weight == "bold" {
		return t.Bold
	}
	return t.Regular
}
