// Copyright (c) 2026, the go-pdfkit/xfa authors
// All rights reserved.
//
// SPDX-License-Identifier: BSD-3-Clause

package xfa

import (
	"fmt"
	"math"
	"strconv"
	"strings"
)

// A Measure is a length, in points.
//
// XFA writes lengths as a number and a unit — "62mm", "9pt", "0.25in" — and
// mixes them freely within one form: the corpus's simplest template gives its
// page in points, its content area's origin in inches and its fields' widths
// in millimetres, all in the same subform.
type Measure float64

// Points converts to the unit a PDF is drawn in.
func (m Measure) Points() float64 { return float64(m) }

// perUnit is how many points one of each unit is.
//
// A pica is twelve points, a millipoint is a thousandth of one, and a
// millimetre is a seventy-second of an inch times 25.4 — all exact. "em" and
// "%" are relative and cannot be resolved here, because the thing they are
// relative to is decided by where in the tree the measurement sits.
//
// # There is no "px"
//
// This list is Foxit's, read from pdfium's CXFA_Measurement, which is the
// implementation of XFA closest to Adobe's own: mm, pt, in, cm, pc, mp, em
// and % (xfa/fxfa/parser/cxfa_measurement.cpp, GetUnitFromString). "px" is
// not among them, and neither is any other.
//
// The two free implementations disagreed about it and neither cited anything.
// pdf.js reads "px" as one point (utils.js:24); this package used to read it
// as 72/96 of one, the CSS pixel. Both were HTML habits carried into a format
// that does not have the unit at all. Rather than choose between two
// inventions, a length in "px" is now refused like a length in furlongs, and
// the element carrying it is reported as unplaced rather than drawn a third of
// its width out. No x, y, w or h in the 560-template corpus is written in it.
//
// "mp" is the other half of that reading: a real XFA unit that pdfium knows,
// that pdf.js does not, and that this did not have either.
var perUnit = map[string]float64{
	"pt": 1,
	"in": 72,
	"mm": 72 / 25.4,
	"cm": 720 / 25.4,
	"pc": 12,
	"mp": 0.001,
}

// ParseMeasure reads a length. A bare number is points, which is what the
// specification says and what the corpus writes for a page's height.
//
// A length may also be written as a CALCULATION, with a leading "=", and that
// is read by [calculated] rather than here.
func ParseMeasure(s string) (Measure, error) {
	// The "=" has to be the first character, as it is in pdfium: SetString
	// tests wsMeasure.Front() on the string it is handed and does not trim it
	// first. A blank before it is therefore not a calculation, and falls
	// through to be reported as no length at all rather than read as one.
	if calc, is := strings.CutPrefix(s, "="); is {
		return calculated(calc), nil
	}
	t := strings.TrimSpace(s)
	if t == "" {
		return 0, fmt.Errorf("xfa: an empty string is not a length")
	}
	// The unit is the trailing letters. Splitting on the last digit rather
	// than on a fixed width lets "0.25in" and "-1.5mm" read the same way.
	i := len(t)
	for i > 0 && (t[i-1] < '0' || t[i-1] > '9') && t[i-1] != '.' {
		i--
	}
	num, unit := strings.TrimSpace(t[:i]), strings.ToLower(strings.TrimSpace(t[i:]))
	v, err := strconv.ParseFloat(num, 64)
	if err != nil {
		return 0, fmt.Errorf("xfa: %q is not a length", s)
	}
	if unit == "" {
		return Measure(v), nil
	}
	per, ok := perUnit[unit]
	if !ok {
		return 0, fmt.Errorf("xfa: %q is a length in no unit this reads", s)
	}
	return Measure(v * per), nil
}

// Measure reads one of a node's attributes as a length.
//
// It answers three ways, not two, and the third is the point of it. ok is
// false when the attribute is not there at all; err is non-nil when it is
// there and is not a length. XFA's "unspecified" and its "zero" are different
// things — a field with no width has no width until its text is measured,
// which is not the same as a field nought wide — and a layout that collapses
// them draws the second when it meant to report the first.
//
// pdf.js keeps the distinction as the empty string and then loses it in
// measureToString (html_utils.js:35-41), which turns any string, meant or
// mistaken, into "0px".
func (n *Node) Measure(name string) (m Measure, ok bool, err error) {
	s, there := n.Attr[name]
	if !there || strings.TrimSpace(s) == "" {
		return 0, false, nil
	}
	m, err = ParseMeasure(s)
	if err != nil {
		return 0, true, err
	}
	return m, true, nil
}

// calculated reads a measurement written as a calculation — anything after a
// leading "=" — and answers in points.
//
// # The "=" is stripped deliberately, and by the authority rather than by luck
//
// pdfium's CXFA_Measurement is Foxit's, the implementation of XFA closest to
// Adobe's own, and its SetString takes a leading "=" off before it parses
// anything (xfa/fxfa/parser/cxfa_measurement.cpp). Its own unit tests say so
// outright: L"=5" is five and L"=5mm" is five millimetres
// (cxfa_measurement_unittest.cpp, EqualsPrefix), and L"=" alone is nought in
// no unit (InvalidValues).
//
// pdf.js reaches the same answer for the corpus's h="=0mm" and does not mean
// to. getMeasurement's pattern /([+-]?\d+\.?\d*)(.*)/ is unanchored, so it
// finds the "0mm" INSIDE the string having never noticed the "=" at all
// (utils.js:83-87). That is a property of a regular expression, not a
// statement about XFA — the same shape as the "px" this package refused in
// its first slice — so the two implementations agree here and only one of
// them agrees for a reason.
//
// # An expression is NOT evaluated, and that is the rule and not a gap
//
// ="Foo.h * 2" is not a calculation to perform. Under pdfium's rule nothing
// numeric begins it, so the value is nought and the unit is one pdfium does
// not know, and a width or a height in a unit it does not know is nought
// points (see below). Nought is therefore the ANSWER the reference gives for
// an expression, not a shortfall of this package standing in for one, and
// putting a script engine behind this function would make it disagree with
// pdfium on every form that writes one. Nobody should "fix" this.
//
// # What the rest of the string means
//
// Everything after the "=" is read the way pdfium reads any measurement,
// which is leniently and without ever failing:
//
//   - blanks before the number are skipped, and only spaces are — FXSYS_wcstof
//     skips ' ' and nothing else (core/fxcrt/fx_extension.cpp:41-45), so a tab
//     stops the number before it starts;
//   - the value is the longest floating-point number BEGINNING what is left,
//     and whatever fails to parse simply is not part of it;
//   - a value that is not finite is nought, which is pdfium's isfinite() test;
//   - the unit is the whole of the rest, matched exactly.
//
// Failure has nowhere to go: SetString returns void, so a string with no
// length in it is not refused but recorded as nought in a unit named Unknown.
// This function answers the same way — in points, so a caller cannot tell
// Unknown from nought millimetres — and so a calculated measurement always
// reads, which is why [ParseMeasure] returns no error for one.
//
// # Where this parts company with the reading above it
//
// A calculated measurement is read by the reference from end to end, and
// [ParseMeasure] is not, so two of these answers differ from the ones a plain
// measurement gets. A calculated number with NO unit is nought — ="5" is not
// five points — because pdfium's Unknown is not a unit it converts but a
// conversion it declines: ToUnitInternal has no arm for it and returns false,
// and ToUnit turns that into nought. And the unit is matched with its case,
// so ="5MM" is nought where "5MM" is nine tenths of an inch.
//
// Neither is a rule this package invents, and no corpus measurement is
// written either way: the 560 forms write one calculated shape, h="=0mm", 955
// times over 101 of them, and nothing else with a leading "=" at all.
func calculated(s string) Measure {
	i := 0
	for i < len(s) && s[i] == ' ' {
		i++
	}
	num, unit := numericPrefix(s[i:])
	// The only error ParseFloat can return over a prefix scanned to its own
	// grammar is ErrRange, and the value it returns alongside it is ±Inf or
	// nought — which is precisely what pdfium's isfinite() is there to catch.
	// An empty prefix gives nought, which is what pdfium's failed wcstof
	// gives.
	v, _ := strconv.ParseFloat(num, 64)
	if math.IsInf(v, 0) {
		v = 0
	}
	per, known := perUnit[unit]
	if !known {
		// pdfium keeps "em" and "%" apart from a unit it has never heard of,
		// and then loses the distinction at the one place a layout asks:
		// ToUnitInternal has an arm for neither, so converting any of the
		// three to points gives nought (cxfa_measurement.cpp). A relative
		// length cannot be resolved without the thing it is relative to, and
		// this is the reference's answer for one, not an omission here.
		//
		// The match is exact, and so is pdfium's: GetUnitFromString compares
		// with EqualsASCII, and its unit tests assert that "CM", "Cm" and
		// "cM" are all units it does not know.
		return 0
	}
	return Measure(v * per)
}

// numericPrefix splits a string into the longest floating-point number
// beginning it and everything after that number.
//
// It is fast_float's "general" format with a leading plus allowed, which is
// what pdfium hands FXSYS_wcstof: an optional sign, digits about an optional
// point, and an optional exponent. Two details of it are load-bearing here.
//
// A number needs a digit but not one before the point, so ".5mm" is half a
// millimetre. And an "e" with no digits after it is NOT an error: general is
// fixed|scientific, and fast_float rolls the exponent back to the mantissa
// rather than refusing the whole string when the fixed format would have
// accepted it. That is why "5em" is five in ems — a unit — rather than a
// number with a broken exponent on the end of it.
//
// There is no case for "inf" or for "nan", which fast_float does read and
// this does not, because the answer cannot differ: pdfium forces a non-finite
// value to nought, so the reading contributes nought points whatever unit
// follows it, and refusing to read it leaves nought in a unit nothing knows,
// which is nought points as well.
func numericPrefix(s string) (num, rest string) {
	i, digits := 0, 0
	if i < len(s) && (s[i] == '+' || s[i] == '-') {
		i++
	}
	for i < len(s) && isDigit(s[i]) {
		i, digits = i+1, digits+1
	}
	if i < len(s) && s[i] == '.' {
		i++
		for i < len(s) && isDigit(s[i]) {
			i, digits = i+1, digits+1
		}
	}
	if digits == 0 {
		return "", s
	}
	if j := i; j < len(s) && (s[j] == 'e' || s[j] == 'E') {
		j++
		if j < len(s) && (s[j] == '+' || s[j] == '-') {
			j++
		}
		k := j
		for j < len(s) && isDigit(s[j]) {
			j++
		}
		if j > k {
			i = j
		}
	}
	return s[:i], s[i:]
}

func isDigit(c byte) bool { return c >= '0' && c <= '9' }
