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
		{"5000mp", 5},
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
	for _, in := range []string{"", "   ", "mm", "wide", "1.2.3pt", "10furlongs", "5em", "50%", "96px"} {
		if _, err := ParseMeasure(in); err == nil {
			t.Errorf("%q was read as a length", in)
		}
	}
}

func TestAnAttributeIsAbsentOrALengthOrWrong(t *testing.T) {
	// The three answers are the reason this exists. A layout that cannot tell
	// "no width" from "width nought" draws the second when it met the first.
	n := &Node{Attr: map[string]string{"w": "1in", "h": "", "x": "nonsense"}}
	for _, tc := range []struct {
		name    string
		want    Measure
		wantOK  bool
		wantErr bool
	}{
		{"w", 72, true, false},
		{"h", 0, false, false}, // written empty, which XFA means as absent
		{"y", 0, false, false}, // not written at all
		{"x", 0, true, true},   // written, and not a length
	} {
		m, ok, err := n.Measure(tc.name)
		if m != tc.want || ok != tc.wantOK || (err != nil) != tc.wantErr {
			t.Errorf("Measure(%q) = %v, %v, %v", tc.name, m, ok, err)
		}
	}
}
