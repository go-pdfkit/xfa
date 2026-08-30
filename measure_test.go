// Copyright (c) 2026, the go-pdfkit/xfa authors
// All rights reserved.
//
// SPDX-License-Identifier: BSD-3-Clause

package xfa

import (
	"math"
	"testing"
)

func TestALengthIsReadInWhateverUnitItIsWritten(t *testing.T) {
	// One template gives its page in points, its content area's origin in
	// inches and its fields' widths in millimetres, all in the same subform.
	for _, tc := range []struct {
		in   string
		want float64
	}{
		{"756pt", 756},
		{"0.25in", 18},
		{"62mm", 62 * 72 / 25.4},
		{"1cm", 720 / 25.4},
		{"6pc", 72},
		{"96px", 72},
		{"612", 612}, // a bare number is points
		{"-1.5mm", -1.5 * 72 / 25.4},
		{"  9mm  ", 9 * 72 / 25.4},
		{"9MM", 9 * 72 / 25.4}, // a unit is read whatever its case
	} {
		got, err := ParseMeasure(tc.in)
		if err != nil {
			t.Errorf("%q: %v", tc.in, err)
			continue
		}
		if math.Abs(got.Points()-tc.want) > 1e-9 {
			t.Errorf("%q = %v points, want %v", tc.in, got.Points(), tc.want)
		}
	}
}

func TestWhatIsNotALength(t *testing.T) {
	for _, in := range []string{"", "   ", "mm", "wide", "1.2.3pt", "10furlongs", "5em", "50%"} {
		if _, err := ParseMeasure(in); err == nil {
			t.Errorf("%q was read as a length", in)
		}
	}
}

func TestALengthThatCannotBeReadFallsBack(t *testing.T) {
	// A template writes a great many optional lengths, and one written wrongly
	// is not a reason to refuse the form: a field with an unreadable width is
	// laid out at its default, which is what a reader does with it.
	for _, tc := range []struct {
		in   string
		want Measure
	}{
		{"", 42},
		{"nonsense", 42},
		{"1in", 72},
	} {
		if got := measureOr(tc.in, 42); got != tc.want {
			t.Errorf("measureOr(%q, 42) = %v, want %v", tc.in, got, tc.want)
		}
	}
}
