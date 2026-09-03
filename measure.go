// Copyright (c) 2026, the go-pdfkit/xfa authors
// All rights reserved.
//
// SPDX-License-Identifier: BSD-3-Clause

package xfa

import (
	"fmt"
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
func ParseMeasure(s string) (Measure, error) {
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
