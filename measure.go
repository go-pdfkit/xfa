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
// A pica is twelve points and a millimetre is a seventy-second of an inch
// times 25.4 — both exact. "em" and "%" are relative and cannot be resolved
// here, because the thing they are relative to is decided by where in the tree
// the measurement sits.
var perUnit = map[string]float64{
	"pt": 1,
	"in": 72,
	"mm": 72 / 25.4,
	"cm": 720 / 25.4,
	"pc": 12,
	"px": 72.0 / 96.0, // a CSS pixel, which XFA inherits from HTML
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

// measureOr reads a length, or returns the fallback when there is none to read
// and when what is there cannot be read.
//
// A template writes a great many optional lengths, and one written wrongly is
// not a reason to refuse the form: a field with an unreadable width is laid out
// at its default width, which is what a reader does with it.
func measureOr(s string, fallback Measure) Measure {
	if s == "" {
		return fallback
	}
	m, err := ParseMeasure(s)
	if err != nil {
		return fallback
	}
	return m
}
