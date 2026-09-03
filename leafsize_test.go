// Copyright (c) 2026, the go-pdfkit/xfa authors
// All rights reserved.
//
// SPDX-License-Identifier: BSD-3-Clause

package xfa

import (
	"strings"
	"testing"
)

func TestADrawIsAsTallAsItsTextComesTo(t *testing.T) {
	// The content area is 500 points wide; the draw is 65, so seven characters
	// come to two lines and the draw to twenty points. Its width comes back
	// from the measurement too, 1.02 times the longest line.
	same(t, "the sheet", laid(laidOut(t, page(`w="500pt" h="500pt"`, `
	  <draw name="A" w="65pt"><value><text>abcdefg</text></value></draw>
	  <draw name="B" w="1pt" h="3pt"/>`))),
		[]string{"draw f.A 0,0 65x20", "draw f.B 0,20 1x3"})
}

func TestADrawWithNoWidthTakesTheOneItsTextComesTo(t *testing.T) {
	// Neither dimension is written, so both come from the text: five
	// characters at ten points, times pdf.js's WIDTH_FACTOR.
	same(t, "the sheet", laid(laidOut(t, page(`w="500pt" h="500pt"`, `
	  <draw name="A"><value><text>hello</text></value></draw>`))),
		[]string{"draw f.A 0,0 51x10"})
}

func TestADrawIsBrokenAtTheRoomItHasWhereItWritesNoWidth(t *testing.T) {
	// The content area is 45 points wide, so a six-character word is two
	// lines: four characters then two. The room comes down the chain from the
	// content area, less the container's own insets.
	same(t, "the sheet", laid(laidOut(t, page(`w="45pt" h="500pt"`, `
	  <draw name="A"><value><text>abcdef</text></value></draw>`))),
		[]string{"draw f.A 0,0 40.8x22"})
}

func TestTheMarginOfALeafIsTakenOffItsWidthAndAddedToItsHeight(t *testing.T) {
	// pdf.js takes the left and right insets off the width its text is broken
	// at and adds the top and bottom ones to the height that comes out
	// (html_utils.js:216-217, 244, 279-285).
	same(t, "the sheet", laid(laidOut(t, page(`w="500pt" h="500pt"`, `
	  <draw name="A" w="85pt"><margin leftInset="10pt" rightInset="10pt"
	     topInset="3pt" bottomInset="4pt"/>
	   <value><text>abcdefg</text></value></draw>`))),
		[]string{"draw f.A 0,0 85x27"})
}

func TestWhatAMeasurementRefuses(t *testing.T) {
	for _, tc := range []struct {
		what string
		body string
		want []string
	}{
		{"a width that is not a length",
			`<draw name="A" w="wide"><value><text>hi</text></value></draw>`,
			[]string{`f.A: its size is written as w="wide" h="", which is not a size`}},
		{"a margin that is not in lengths",
			`<draw name="A" w="50pt"><margin topInset="=0mm"/>
			 <value><text>hi</text></value></draw>`,
			[]string{"f.A: " + marginNotLengths}},
		{"a paragraph whose space above is not a length",
			`<draw name="A" w="50pt"><para spaceAbove="=0mm"/>
			 <value><text>hi</text></value></draw>`,
			[]string{`f.A: its paragraph is written as spaceAbove="=0mm", which is not a length`}},
		{"a paragraph whose space below is not a length",
			`<draw name="A" w="50pt"><para spaceBelow="=0mm"/>
			 <value><text>hi</text></value></draw>`,
			[]string{`f.A: its paragraph is written as spaceBelow="=0mm", which is not a length`}},
		{"a check box whose size is not a length",
			`<field name="A" w="50pt"><ui><checkButton size="=0mm"/></ui></field>`,
			[]string{`f.A: its check box is written as size="=0mm", which is not a length`}},
		{"a caption whose reserve is not a length",
			`<field name="A" w="50pt"><caption reserve="=0mm"><value><text>hi</text></value></caption></field>`,
			[]string{`f.A: its caption is written as reserve="=0mm", which is not a length`}},
		{"a caption whose margin is not in lengths",
			`<field name="A" w="50pt"><caption><margin topInset="=0mm"/>
			 <value><text>hi</text></value></caption></field>`,
			[]string{"f.A: " + marginNotLengths}},
		{"an edge of the widget's border that is not a length",
			`<field name="A" w="50pt"><ui><textEdit><border><edge thickness="=0mm"/></border></textEdit></ui></field>`,
			[]string{`f.A: an edge of its border is written as thickness="=0mm", which is not a length`}},
		{"a margin on the widget's border that is not in lengths",
			`<field name="A" w="50pt"><ui><textEdit><border><margin topInset="=0mm"/></border></textEdit></ui></field>`,
			[]string{"f.A: " + marginNotLengths}},
		{"a smallest height that is not a length",
			`<field name="A" w="50pt" minH="=0mm"><value><text>hi</text></value></field>`,
			[]string{`f.A: its smallest size is written as minH="=0mm", which is not a length`}},
		{"a largest height that is not a length",
			`<field name="A" w="50pt" maxH="=0mm"><value><text>hi</text></value></field>`,
			[]string{`f.A: its largest size is written as maxH="=0mm", which is not a length`}},
		{"a smallest width that is not a length",
			`<field name="A" h="50pt" minW="=0mm"><value><text>hi</text></value></field>`,
			[]string{`f.A: its smallest size is written as minW="=0mm", which is not a length`}},
		{"a largest width that is not a length",
			`<field name="A" h="50pt" maxW="=0mm"><value><text>hi</text></value></field>`,
			[]string{`f.A: its largest size is written as maxW="=0mm", which is not a length`}},
	} {
		if got := notLaid(laidOut(t, page(`w="500pt" h="500pt"`, tc.body))); strings.Join(got, "\n") != strings.Join(tc.want, "\n") {
			t.Errorf("%s:\n  got  %v\n  want %v", tc.what, got, tc.want)
		}
	}
}

func TestWhatARefusalSaysWhenTheHeightIsTheOneAtFault(t *testing.T) {
	// The width is written, so only the height has to be measured, and the
	// reasons name the height's own attributes.
	for _, tc := range []struct {
		body string
		want string
	}{
		{`<field name="A" w="50pt" maxH="=0mm"><value><text>hi</text></value></field>`,
			`f.A: its largest size is written as maxH="=0mm", which is not a length`},
		{`<draw name="A" maxH="=0mm"/>`,
			`f.A: its largest size is written as maxH="=0mm", which is not a length`},
		{`<draw name="A" minH="=0mm"/>`,
			`f.A: its smallest size is written as minH="=0mm", which is not a length`},
	} {
		if got := notLaid(laidOut(t, page(`w="500pt" h="500pt"`, tc.body))); len(got) != 1 || got[0] != tc.want {
			t.Errorf("%v, want [%s]", got, tc.want)
		}
	}
}

func TestAFieldWithNothingToMeasureIsOneLineTall(t *testing.T) {
	// pdf.js's getMetrics answers { lineHeight: 12, lineGap: 2, lineNoGap: 10 }
	// where it resolves no font (fonts.js:173-179) and a field with no text
	// takes lineNoGap for its widget (template.js:2821). It is also the line
	// pdf.js cannot reach: selectFont dereferences the typeface before the
	// test that would have returned those constants, which is where it dies on
	// 70 of the 560 corpus forms.
	same(t, "the sheet", laid(laidOut(t, page(`w="500pt" h="500pt"`, `
	  <field name="A" w="20pt"><font typeface="Nothing Has This"/></field>`))),
		[]string{"field f.A 0,0 20x10"})
}

func TestACheckBoxIsAsTallAsItsSide(t *testing.T) {
	same(t, "the sheet", laid(laidOut(t, page(`w="500pt" h="500pt"`, `
	  <field name="A"><ui><checkButton/></ui></field>
	  <field name="B"><ui><checkButton size="17pt"/></ui></field>`))),
		[]string{"field f.A 0,0 10x10", "field f.B 0,10 17x17"})
}

func TestTheBorderOfTheWidgetIsAddedToTheField(t *testing.T) {
	// getBorderDims (template.js:155-178) crosses the two: what it adds to the
	// HEIGHT is edges one and three with the right and left insets. Four
	// half-point edges and no margin add one point each way, and a border with
	// no edges at all is four of the default half-point one.
	same(t, "the sheet", laid(laidOut(t, page(`w="500pt" h="500pt"`, `
	  <field name="A" w="30pt"><ui><textEdit><border/></textEdit></ui></field>
	  <field name="B" w="30pt"><ui><textEdit><border><edge thickness="2pt"/></border></textEdit></ui></field>
	  <field name="C" w="30pt"><ui><textEdit/></ui></field>
	  <field name="D" w="30pt"><ui/></field>
	  <field name="E" w="30pt"><ui><extras/><picture/><textEdit/></ui></field>`))),
		[]string{
			"field f.A 0,0 30x11", "field f.B 0,11 30x14",
			"field f.C 0,25 30x10", "field f.D 0,35 30x10", "field f.E 0,45 30x10"})
}

func TestACaptionBesideTheWidgetReplacesItsHeight(t *testing.T) {
	// pdf.js assigns the caption's measurement over the widget's whole
	// (template.js:2838-2839) and adds the widget back into the one dimension
	// the placement calls for. Beside, the field is as tall as the CAPTION;
	// above or below, it is the two added up.
	same(t, "the sheet", laid(laidOut(t, page(`w="500pt" h="500pt"`, `
	  <field name="A" w="30pt"><ui><checkButton size="25pt"/></ui>
	    <caption><value><text>hi</text></value></caption></field>
	  <field name="B" w="30pt"><ui><checkButton size="25pt"/></ui>
	    <caption placement="top"><value><text>hi</text></value></caption></field>`))),
		[]string{"field f.A 0,0 30x10", "field f.B 0,10 30x35"})
}

func TestACaptionWithNoTextLeavesTheFieldWithNoHeightBesideIt(t *testing.T) {
	// The caption's measurement gives null, and only the dimension the
	// placement adds the widget's back into survives. Beside the widget that
	// is the width, so the height falls back on what computeBbox writes.
	same(t, "the sheet", laid(laidOut(t, page(`w="500pt" h="500pt"`, `
	  <field name="A" w="30pt" minH="7pt"><ui><checkButton size="25pt"/></ui>
	    <caption/></field>`))),
		[]string{"field f.A 0,0 30x7"})
}

func TestACaptionClaimsTheRoomItReserves(t *testing.T) {
	// A caption beside the widget is measured against its reserve rather than
	// against the room it has, and the reserve is rounded UP to a whole point
	// (template.js:1161). Seven characters in 25 points is four lines of two:
	// ten points then three of twelve.
	same(t, "the sheet", laid(laidOut(t, page(`w="500pt" h="500pt"`, `
	  <field name="A" w="300pt"><caption reserve="24.1pt"><value><text>abcdefg</text></value></caption></field>`))),
		[]string{"field f.A 0,0 300x46"})
}

func TestASizeIsHeldBetweenTheSmallestAndTheLargest(t *testing.T) {
	// pdf.js: min(maxH <= 0 ? Infinity : maxH, minH + 1 < height ? height : minH)
	// (template.js:2867-2870). A measurement within a point of the minimum IS
	// the minimum.
	same(t, "the sheet", laid(laidOut(t, page(`w="500pt" h="500pt"`, `
	  <field name="A" w="30pt" minH="40pt"><value><text>hi</text></value></field>
	  <field name="B" w="30pt" maxH="6pt"><value><text>hi</text></value></field>
	  <field name="C" w="30pt" minH="9.5pt"><value><text>hi</text></value></field>`))),
		[]string{"field f.A 0,0 30x40", "field f.B 0,40 30x6", "field f.C 0,46 30x9.5"})
}

func TestALeafWithNothingToMeasureIsAsTallAsItsSmallest(t *testing.T) {
	// pdf.js's computeBbox does not leave the height unwritten: it fills it in
	// from minH (html_utils.js:310-319), and the stack above adds THAT.
	same(t, "the sheet", laid(laidOut(t, page(`w="500pt" h="500pt"`, `
	  <draw name="A" w="10pt" minH="6pt"/>
	  <draw name="B" w="10pt" h="3pt"/>`))),
		[]string{"draw f.A 0,0 10x6", "draw f.B 0,6 10x3"})
}

func TestUnderAPositionedParentWithASizeItIsNoughtInstead(t *testing.T) {
	// computeBbox: parent.layout === "position" && parent.h !== "" ? 0 : minH
	// (html_utils.js:312-314).
	same(t, "the sheet", laid(laidOut(t, page(`w="500pt" h="500pt"`, `
	  <subform name="S" h="80pt" w="80pt"><draw name="A" minH="6pt" minW="5pt"/></subform>`))),
		[]string{"draw f.S.A 0,0 0x0"})
}

func TestACellOfARowWithNoColumnsHasNoWidthToBreakItsTextAt(t *testing.T) {
	// pdf.js reads the columns off the container above the row and throws when
	// there are none (layout.js:184-189, template.js:5095-5101). A cell whose
	// own width IS written is unaffected, because pdf.js takes that first.
	same(t, "what was left off", notLaid(laidOut(t, page(`w="500pt" h="500pt"`, `
	  <subform name="T" layout="tb">
	    <subform name="R" layout="row"><draw name="A"><value><text>hi</text></value></draw></subform>
	    <draw name="Z" w="1pt" h="1pt"/>
	  </subform>`))),
		[]string{
			"f.T.R.A: a row cuts its cells from the columnWidths of the container above it, which writes none this reads",
			"f.T.Z: a tb layout stacks its children, and the height of the one above it is not computed: " + noWidthToBreakAt})
}

func TestATableWithNoWidthIsAsWideAsItsColumns(t *testing.T) {
	// fixDimensions: node.w = sum(columnWidths) for a table that writes none
	// (html_utils.js:352-356). The table is thirty points wide, so a
	// four-character draw inside it is two lines.
	same(t, "the sheet", laid(laidOut(t, page(`w="500pt" h="500pt"`, `
	  <subform name="T" layout="table" columnWidths="10pt 20pt">
	    <draw name="A"><value><text>abcd</text></value></draw></subform>`))),
		[]string{"draw f.T.A 0,0 30.6x20"})
}

func TestACellIsMeasuredAgainstEveryColumnLeftInTheRow(t *testing.T) {
	// A draw has its own width replaced by the columns it spans BEFORE its
	// text is measured (fixDimensions at template.js:1901, layoutNode at
	// :1908); a FIELD has it replaced after (:2876 against :2816), so a field
	// is broken at every column left in the row and a draw at its own.
	// The same five characters in the same first column of twenty points: the
	// draw comes to three lines, the field to one, because the field was
	// measured against the whole eighty points of the row.
	same(t, "the sheet", laid(laidOut(t, page(`w="500pt" h="500pt"`, `
	  <subform name="T" layout="table" columnWidths="20pt 60pt">
	    <subform name="R1" layout="row">
	      <draw name="A"><value><text>abcde</text></value></draw>
	      <draw name="Z" w="1pt" h="1pt"/></subform>
	    <subform name="R2" layout="row">
	      <field name="B"><value><text>abcde</text></value></field>
	      <draw name="Y" w="1pt" h="1pt"/></subform></subform>`))),
		[]string{
			"draw f.T.R1.A 0,0 20x34", "draw f.T.R1.Z 20,0 60x34",
			"field f.T.R2.B 0,34 20x10", "draw f.T.R2.Y 20,34 60x10"})
}

func TestAFieldDrawsTheDataItIsBoundTo(t *testing.T) {
	// pdf.js's binder replaces the value outright (bind.js:88-100), so a bound
	// field is as tall as its DATA rather than as the template's default. Five
	// characters in a twenty-point field are three lines.
	tmpl := parse(t, page(`w="500pt" h="500pt"`, `
	  <field name="A" w="20pt"><value><text>x</text></value>
	    <bind match="dataRef" ref="$.A"/></field>`))
	d, err := ParseDatasets(strings.NewReader(wrap(`<form1><A>abcde</A></form1>`)))
	if err != nil {
		t.Fatal(err)
	}
	same(t, "the sheet", laid(Place(Expand(tmpl, d))), []string{"field f.A 0,0 20x34"})
}

func TestAWidthNobodyCanReadLeavesNoWidthToBreakAt(t *testing.T) {
	// A container whose own width is not a length gives what it holds no width
	// at all, rather than a width of nought or the one it was given.
	same(t, "what was left off", notLaid(laidOut(t, page(`w="500pt" h="500pt"`, `
	  <subform name="S" w="=0mm" layout="tb">
	    <draw name="A"><value><text>hi</text></value></draw></subform>`))),
		[]string{"f.S.A: " + noWidthToBreakAt})
}

func TestARowWithNothingAboveItHasNoColumns(t *testing.T) {
	// The outermost subform's parent is the template root, which writes no
	// columnWidths. pdf.js reads them with no guard (template.js:5096).
	src := `<template><subform name="f" layout="row">
	  <pageSet><pageArea name="P"><contentArea w="500pt" h="500pt"/></pageArea></pageSet>
	  <draw name="A" w="1pt" h="1pt"/></subform></template>`
	same(t, "what was left off", notLaid(laidOut(t, src)),
		[]string{"f.A: a row cuts its cells from the columnWidths of the container above it, which writes none this reads"})
}

func TestAValueHoldingNoTextIsMeasuredAsNothing(t *testing.T) {
	// A <value> that is there and says nothing is not text: pdf.js's
	// `if (text)` (html_utils.js:264) never runs, so computeBbox fills the
	// height in from minH.
	same(t, "the sheet", laid(laidOut(t, page(`w="500pt" h="500pt"`, `
	  <draw name="A" w="10pt" minH="6pt"><value><text></text></value></draw>`))),
		[]string{"draw f.A 0,0 10x6"})
}

func TestWhatAFieldsOwnMeasurementRefuses(t *testing.T) {
	// The same refusals as a draw's, reached through the widget rather than
	// through the draw, and through the caption rather than through the field.
	// Each is stated as the reason the STACK gives, because that is where the
	// measurement is asked for; own is the reason the leaf itself is refused
	// with, where the placement gets to it first.
	for _, tc := range []struct {
		what, body, own, want string
	}{
		{"a field whose own margin is not in lengths",
			`<field name="A" w="50pt"><margin topInset="=0mm"/><value><text>hi</text></value></field>`,
			"", marginNotLengths},
		{"a field whose width is not a length",
			`<field name="A" w="wide"><value><text>hi</text></value></field>`,
			`its size is written as w="wide" h="", which is not a size`,
			`its width is written as w="wide", which is not a length, and its text has to be broken at a width`},
		{"a field whose paragraph is not in lengths",
			`<field name="A" w="50pt"><para spaceAbove="=0mm"/><value><text>hi</text></value></field>`,
			"", `its paragraph is written as spaceAbove="=0mm", which is not a length`},
		{"a field holding rich text written as escaped markup",
			`<field name="A" w="50pt"><value><exData contentType="text/html">&lt;body&gt;hi&lt;/body&gt;</exData></value></field>`,
			"", notMeasurable},
		{"a caption holding rich text written as escaped markup",
			`<field name="A" w="50pt"><caption><value><exData contentType="text/html">&lt;body&gt;hi&lt;/body&gt;</exData></value></caption></field>`,
			"", notMeasurable},
		{"a caption whose paragraph is not in lengths",
			`<field name="A" w="50pt"><caption><para spaceBelow="=0mm"/><value><text>hi</text></value></caption></field>`,
			"", `its paragraph is written as spaceBelow="=0mm", which is not a length`},
	} {
		got := notLaid(laidOut(t, page(`w="500pt" h="500pt"`, tc.body+`<draw name="Z" w="1pt" h="1pt"/>`)))
		own := tc.own
		if own == "" {
			own = tc.want
		}
		want := []string{"f.A: " + own,
			"f.Z: a tb layout stacks its children, and the height of the one above it is not computed: " + tc.want}
		if strings.Join(got, "\n") != strings.Join(want, "\n") {
			t.Errorf("%s:\n  got  %v\n  want %v", tc.what, got, want)
		}
	}
}

func TestACellOfARowWithNoColumnsRefusesAFieldAndItsCaptionToo(t *testing.T) {
	same(t, "what was left off", notLaid(laidOut(t, page(`w="500pt" h="500pt"`, `
	  <subform name="T" layout="tb">
	    <subform name="R" layout="row">
	      <field name="A"><value><text>hi</text></value></field></subform>
	    <draw name="Z" w="1pt" h="1pt"/></subform>`))),
		[]string{
			"f.T.R.A: a row cuts its cells from the columnWidths of the container above it, which writes none this reads",
			"f.T.Z: a tb layout stacks its children, and the height of the one above it is not computed: " + noWidthToBreakAt})
	same(t, "what was left off", notLaid(laidOut(t, page(`w="500pt" h="500pt"`, `
	  <subform name="T" layout="tb">
	    <subform name="R" layout="row">
	      <field name="A"><caption><value><text>hi</text></value></caption></field></subform>
	    <draw name="Z" w="1pt" h="1pt"/></subform>`))),
		[]string{
			"f.T.R.A: a row cuts its cells from the columnWidths of the container above it, which writes none this reads",
			"f.T.Z: a tb layout stacks its children, and the height of the one above it is not computed: " + noWidthToBreakAt})
}

func TestAnEdgeThatWritesNoThicknessIsHalfAPoint(t *testing.T) {
	// getMeasurement(attributes.thickness, "0.5pt") (template.js:2025), and a
	// border writing fewer than four edges repeats its last (:906-914).
	same(t, "the sheet", laid(laidOut(t, page(`w="500pt" h="500pt"`, `
	  <field name="A" w="30pt"><ui><textEdit><border><edge/></border></textEdit></ui></field>`))),
		[]string{"field f.A 0,0 30x11"})
}
