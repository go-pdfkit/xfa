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

// TestPdfiumsOwnMeasurementTests is cxfa_measurement_unittest.cpp, run against
// this package.
//
// pdf.js agrees with the answer for the corpus's one calculated measurement
// and agrees for the wrong reason — its pattern is unanchored and never sees
// the "=" — so agreeing with IT is weak evidence here. These are the
// authority's own assertions instead, and where pdfium states a value and a
// unit that this reports in points, the points are what the same measurement
// would come to inside pdfium: TryMeasureAsFloat converts with
// ToUnit(XFA_Unit::Pt) (fxjs/xfa/cjx_object.cpp:429-436).
func TestPdfiumsOwnMeasurementTests(t *testing.T) {
	const mm = 72 / 25.4
	for _, tc := range []struct {
		in   string
		want float64
	}{
		// EqualsPrefix. L"=5" is five in a unit pdfium does not know, and a
		// length in a unit it does not know is nought points.
		{"=5", 0},
		{"=5mm", 5 * mm},
		// NoPrefix. L"5" is the same five in the same unknown unit there; here
		// a bare number is points, which is this package's own reading and the
		// one place these tests are not a port. See [calculated].
		{"5", 5},
		{"5mm", 5 * mm},
		// InvalidValues.
		{"=", 0},
		// GetUnitFromString, whose every case is exercised through the
		// calculated form, because that is where this matches exactly and
		// with the case.
		{"=1%", 0},
		{"=1em", 0},
		{"=1pt", 1},
		{"=1in", 72},
		{"=1pc", 12},
		{"=1cm", 720 / 25.4},
		{"=1mm", mm},
		{"=1mp", 0.001},
		{"=1foo", 0},
		{"=1!", 0},
		{"=1CM", 0},
		{"=1Cm", 0},
		{"=1cM", 0},
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

// TestACalculatedLengthIsReadLenientlyAndNeverRefused walks the rest of
// pdfium's rule: what its number may look like, what stops it, and what
// becomes of everything a unit table cannot name.
func TestACalculatedLengthIsReadLenientlyAndNeverRefused(t *testing.T) {
	const mm = 72 / 25.4
	for _, tc := range []struct {
		in   string
		want float64
		why  string
	}{
		{"=0mm", 0, "the one shape the corpus writes, 955 times"},
		{"=+5mm", 5 * mm, "fast_float is asked for allow_leading_plus"},
		{"=-1.5in", -108, "and a minus needs no asking"},
		{"=.5in", 36, "a number needs a digit, not one before the point"},
		{"=5.mm", 5 * mm, "nor one after it"},
		{"=  5mm", 5 * mm, "FXSYS_wcstof skips leading spaces"},
		{"=\t5mm", 0, "and skips nothing else, so a tab stops the number"},
		{"= ", 0, "spaces and then nothing is nought in no unit"},
		{"=1e2mm", 100 * mm, "an exponent counts"},
		{"=1E2mm", 100 * mm, "in either case"},
		{"=1e+2mm", 100 * mm, "signed"},
		{"=1e-2mm", 0.01 * mm, "either way"},
		{"=1emm", 0, "an e with no digits after it rolls back to the mantissa"},
		{"=5em", 0, "which is why this is five ems, a unit with no fixed size"},
		{"=50%", 0, "and a percentage is relative too: pdfium converts neither"},
		{"=96px", 0, "XFA has no px, and pdfium answers nought rather than guessing"},
		{"=1.2.3pt", 0, "the number stops at the second point, and \".3pt\" is no unit"},
		{"=5mm ", 0, "the unit is matched whole, and \"mm \" is not \"mm\""},
		{"=1e999mm", 0, "an overflow is not finite, and pdfium forces it to nought"},
		{"=-1e999mm", 0, "in either direction"},
		{"=Foo.h * 2", 0, "an expression is NOT evaluated: see [calculated]"},
	} {
		got, err := ParseMeasure(tc.in)
		if err != nil {
			t.Errorf("%q: %v — a calculated measurement is never refused (%s)", tc.in, err, tc.why)
			continue
		}
		if math.Abs(got.Points()-tc.want) > 1e-9 {
			t.Errorf("%q = %v points, want %v (%s)", tc.in, got.Points(), tc.want, tc.why)
		}
	}
}

// TestTheEqualsHasToBeTheFirstCharacter holds the one place this is stricter
// than the lenient parse behind it. pdfium tests wsMeasure.Front() on the
// string it is handed, without trimming it, so a blank before the "=" is not a
// calculation — and this package has somewhere to put that answer where
// pdfium has not.
func TestTheEqualsHasToBeTheFirstCharacter(t *testing.T) {
	if _, err := ParseMeasure(" =0mm"); err == nil {
		t.Error(`" =0mm" was read as a calculated length`)
	}
	// And the attribute reader gives all three answers for one: written, a
	// length, and nought.
	n := &Node{Attr: map[string]string{"h": "=0mm"}}
	switch m, ok, err := n.Measure("h"); {
	case err != nil || !ok || m != 0:
		t.Errorf(`Measure("h") = %v, %v, %v; want 0, true, nil`, m, ok, err)
	}
}
