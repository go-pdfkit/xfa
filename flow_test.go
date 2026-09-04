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

// page wraps a body in the smallest form that has somewhere to put it: a sheet
// with one page area, one content area at the origin, and room enough that
// nothing in these tests reaches the bottom of it unless it means to.
func page(area, body string) string {
	return `<template><subform name="f" layout="tb">
	  <pageSet><pageArea name="P"><medium long="1000pt" short="1000pt"/>
	    <contentArea ` + area + `/></pageArea></pageSet>` + body + `</subform></template>`
}

func TestAStackPutsEachChildWhereTheOneAboveItEnds(t *testing.T) {
	// Worked by hand. The content area begins at the page's own origin, so the
	// first child sits at 0,0; the second at the first one's height; the third
	// at the sum of the two above it.
	l := laidOut(t, page(`w="500pt" h="500pt"`, `
	  <draw name="A" x="900pt" y="900pt" w="10pt" h="7pt"/>
	  <draw name="B" w="10pt" h="11pt"/>
	  <draw name="C" w="10pt" h="13pt"/>`))
	same(t, "the page", laid(l), []string{
		// A's own x and y are thrown away: a flow layout does not read them.
		"draw f.A 0,0 10x7",
		"draw f.B 0,7 10x11",
		"draw f.C 0,18 10x13",
	})
}

func TestAContainerIsAsTallAsWhatItHolds(t *testing.T) {
	for _, tc := range []struct {
		what string
		body string
		want []string
	}{
		{"a positioned container reaches as far as its furthest child",
			`<subform name="S"><draw name="A" y="30pt" w="1pt" h="5pt"/>
			   <draw name="B" y="10pt" w="1pt" h="2pt"/></subform>
			 <draw name="After" w="1pt" h="1pt"/>`,
			// S reaches 30+5 = 35, not 10+2 and not the sum.
			[]string{"draw f.S.A 0,30 1x5", "draw f.S.B 0,10 1x2", "draw f.After 0,35 1x1"}},
		{"a stacked container is the sum of its children",
			`<subform name="S" layout="tb"><draw name="A" w="1pt" h="5pt"/>
			   <draw name="B" w="1pt" h="2pt"/></subform>
			 <draw name="After" w="1pt" h="1pt"/>`,
			[]string{"draw f.S.A 0,0 1x5", "draw f.S.B 0,5 1x2", "draw f.After 0,7 1x1"}},
		{"a container is no shorter than the height it writes",
			`<subform name="S" layout="tb" h="50pt"><draw name="A" w="1pt" h="5pt"/></subform>
			 <draw name="After" w="1pt" h="1pt"/>`,
			[]string{"draw f.S.A 0,0 1x5", "draw f.After 0,50 1x1"}},
		{"and no shorter than what it holds either",
			// pdf.js: Math.max(extra.height + marginV, this.h || 0). A written
			// height that is too small does not shrink what is inside it.
			`<subform name="S" layout="tb" h="3pt"><draw name="A" w="1pt" h="5pt"/></subform>
			 <draw name="After" w="1pt" h="1pt"/>`,
			[]string{"draw f.S.A 0,0 1x5", "draw f.After 0,5 1x1"}},
		{"a container's margin is outside what it holds",
			// The insets move the children in and add to the height reported
			// upwards: 4 + 5 + 6 = 15.
			`<subform name="S" layout="tb"><margin topInset="4pt" bottomInset="6pt" leftInset="3pt"/>
			   <draw name="A" w="1pt" h="5pt"/></subform>
			 <draw name="After" w="1pt" h="1pt"/>`,
			[]string{"draw f.S.A 3,4 1x5", "draw f.After 0,15 1x1"}},
		{"a hidden child takes no room at all",
			`<draw name="A" w="1pt" h="5pt" presence="hidden"/>
			 <draw name="B" w="1pt" h="7pt"/>`,
			[]string{"draw f.A 0,0 1x5", "draw f.B 0,0 1x7"}},
		{"nor does an inactive one",
			`<draw name="A" w="1pt" h="5pt" presence="inactive"/>
			 <draw name="B" w="1pt" h="7pt"/>`,
			[]string{"draw f.A 0,0 1x5", "draw f.B 0,0 1x7"}},
		{"but an invisible one does",
			// pdf.js gives "invisible" CSS visibility:hidden, which keeps its
			// room, and "hidden" display:none, which does not.
			`<draw name="A" w="1pt" h="5pt" presence="invisible"/>
			 <draw name="B" w="1pt" h="7pt"/>`,
			[]string{"draw f.A 0,0 1x5", "draw f.B 0,5 1x7"}},
		{"a hidden child of a positioned container reaches nowhere",
			`<subform name="S"><draw name="A" y="900pt" w="1pt" h="5pt" presence="hidden"/>
			   <draw name="B" y="10pt" w="1pt" h="2pt"/></subform>
			 <draw name="After" w="1pt" h="1pt"/>`,
			[]string{"draw f.S.A 0,900 1x5", "draw f.S.B 0,10 1x2", "draw f.After 0,12 1x1"}},
		{"a subformSet is not a container of its own",
			// Its children are the parent's, so they stack among them.
			`<subformSet name="Set"><draw name="A" w="1pt" h="5pt"/></subformSet>
			 <draw name="After" w="1pt" h="1pt"/>`,
			[]string{"draw f.Set.A 0,0 1x5", "draw f.After 0,5 1x1"}},
		{"an exclGroup is a container like any other",
			`<exclGroup name="G" layout="tb"><field name="A" w="1pt" h="5pt"/>
			   <field name="B" w="1pt" h="2pt"/></exclGroup>
			 <draw name="After" w="1pt" h="1pt"/>`,
			[]string{"field f.G.A 0,0 1x5", "field f.G.B 0,5 1x2", "draw f.After 0,7 1x1"}},
		{"an area is a positioned container with no height of its own",
			`<area name="Ar"><draw name="A" y="8pt" w="1pt" h="5pt"/></area>
			 <draw name="After" w="1pt" h="1pt"/>`,
			[]string{"draw f.Ar.A 0,8 1x5", "draw f.After 0,13 1x1"}},
		{"a table stacks its children as a tb does",
			`<subform name="T" layout="table" columnWidths="10pt">
			   <subform name="R1" layout="row"><draw name="A" w="99pt" h="5pt"/></subform>
			   <subform name="R2" layout="row"><draw name="B" w="99pt" h="2pt"/></subform>
			 </subform><draw name="After" w="1pt" h="1pt"/>`,
			// A cell's width comes from the table's columnWidths, not its own.
			[]string{"draw f.T.R1.A 0,0 10x5", "draw f.T.R2.B 0,5 10x2", "draw f.After 0,7 1x1"}},
	} {
		if got := laid(laidOut(t, page(`w="500pt" h="500pt"`, tc.body))); strings.Join(got, "\n") != strings.Join(tc.want, "\n") {
			t.Errorf("%s:\n  got  %v\n  want %v", tc.what, got, tc.want)
		}
	}
}

func TestARowCutsItsCellsFromTheTablesColumns(t *testing.T) {
	// Worked by hand against pdf.js's dimensions converter
	// (html_utils.js:81-106): a cell is as wide as the columns it spans, and
	// the next cell begins where it ends. Columns are 10, 20 and 30 wide.
	l := laidOut(t, page(`w="500pt" h="500pt"`, `
	  <subform name="T" layout="table" columnWidths="10pt 20pt 30pt">
	    <subform name="R" layout="row">
	      <draw name="A" w="99pt" h="4pt"/>
	      <draw name="B" w="99pt" h="9pt" colSpan="2"/>
	      <draw name="C" w="99pt" h="4pt"/>
	    </subform>
	  </subform>`))
	same(t, "the page", laid(l), []string{
		// Every cell is stretched to the tallest of them, which is B at 9.
		"draw f.T.R.A 0,0 10x9",
		"draw f.T.R.B 10,0 50x9",
		// The fourth column does not exist, so the count begins again at the
		// first: pdf.js takes it modulo the number of columns.
		"draw f.T.R.C 60,0 10x9",
	})
}

func TestWhatARowDoesWithTheAwkwardCases(t *testing.T) {
	for _, tc := range []struct {
		what string
		body string
		want []string
	}{
		{"a cell spanning every column left",
			`<subform name="T" layout="table" columnWidths="10pt 20pt 30pt">
			   <subform name="R" layout="row"><draw name="A" w="1pt" h="4pt"/>
			     <draw name="B" w="1pt" h="4pt" colSpan="-1"/>
			     <draw name="C" w="1pt" h="4pt"/></subform></subform>`,
			// B takes columns two and three, and the count begins again.
			[]string{"draw f.T.R.A 0,0 10x4", "draw f.T.R.B 10,0 50x4", "draw f.T.R.C 60,0 10x4"}},
		{"a colSpan that is not a span",
			`<subform name="T" layout="table" columnWidths="10pt 20pt">
			   <subform name="R" layout="row"><draw name="A" w="1pt" h="4pt" colSpan="0"/>
			     <draw name="B" w="1pt" h="4pt" colSpan="what"/></subform></subform>`,
			// pdf.js validates colSpan as n >= 1 or n == -1 and falls back to 1.
			[]string{"draw f.T.R.A 0,0 10x4", "draw f.T.R.B 10,0 20x4"}},
		{"a hidden cell takes no column",
			`<subform name="T" layout="table" columnWidths="10pt 20pt">
			   <subform name="R" layout="row"><draw name="A" w="3pt" h="4pt" presence="hidden"/>
			     <draw name="B" w="1pt" h="4pt"/></subform></subform>`,
			// A keeps its own width, being drawn nowhere, and B takes the
			// first column rather than the second.
			[]string{"draw f.T.R.A 0,0 3x4", "draw f.T.R.B 0,0 10x4"}},
		{"a cell whose height nobody can measure leaves the row unstretched",
			`<subform name="T" layout="table" columnWidths="10pt 20pt">
			   <subform name="R" layout="row"><draw name="A" w="1pt" h="4pt"/>
			     <draw name="B" w="1pt" maxH="9pt"/></subform></subform>`,
			// A keeps its own four points rather than being stretched to a
			// row height nobody can arrive at.
			[]string{"draw f.T.R.A 0,0 10x4"}},
		{"a cell with no height and nothing to measure is as tall as its minH",
			`<subform name="T" layout="table" columnWidths="10pt 20pt">
			   <subform name="R" layout="row"><draw name="A" w="1pt" h="4pt"/>
			     <draw name="B" w="1pt" minH="7pt"/></subform></subform>`,
			// pdf.js's computeBbox fills the height in from minH
			// (html_utils.js:310-319), so the row is seven points tall and
			// both cells are stretched to it.
			[]string{"draw f.T.R.A 0,0 10x7", "draw f.T.R.B 10,0 20x7"}},
		{"a cell that is itself a container",
			`<subform name="T" layout="table" columnWidths="10pt">
			   <subform name="R" layout="row"><subform name="Cell">
			     <draw name="A" x="1pt" y="2pt" w="3pt" h="4pt"/></subform></subform></subform>`,
			// The cell's own width is the column's; what is inside it is
			// placed from the cell's origin as it always is.
			[]string{"draw f.T.R.Cell.A 1,2 3x4"}},
	} {
		if got := laid(laidOut(t, page(`w="500pt" h="500pt"`, tc.body))); strings.Join(got, "\n") != strings.Join(tc.want, "\n") {
			t.Errorf("%s:\n  got  %v\n  want %v", tc.what, got, tc.want)
		}
	}
}

// TestACalculatedHeightIsATrueNoughtInAStack is what this slice moves: the
// corpus writes h="=0mm" on 955 rules and lines, and a stack has to carry the
// height NOUGHT past them rather than stop there.
func TestACalculatedHeightIsATrueNoughtInAStack(t *testing.T) {
	l := laidOut(t, page(`w="500pt" h="500pt"`, `
	  <draw name="A" w="10pt" h="7pt"/>
	  <draw name="Line" w="10pt" h="=0mm"/>
	  <draw name="B" w="10pt" h="11pt"/>`))
	same(t, "the page", laid(l), []string{
		"draw f.A 0,0 10x7",
		// The rule is drawn, nought tall, where A ends...
		"draw f.Line 0,7 10x0",
		// ... and B begins there too, rather than being reported unplaced.
		"draw f.B 0,7 10x11",
	})
}

func TestWhatAStackReportsRatherThanPlaces(t *testing.T) {
	for _, tc := range []struct {
		what string
		body string
		want []string
	}{
		{"the child whose height cannot be arrived at is placed, and nothing after it",
			// B has no height, no text to measure one from, and a maxH, which
			// is the one case pdf.js answers with the room it has rather than
			// with a number of the template's own.
			`<draw name="A" w="1pt" h="5pt"/><draw name="B" w="1pt" maxH="9pt"/>
			 <draw name="C" w="1pt" h="1pt"/>`,
			[]string{
				"f.B: " + boundByTheRoom("maxH"),
				"f.C: a tb layout stacks its children, and the height of the one above it is not computed: " +
					boundByTheRoom("maxH"),
			}},
		{"a height written in something that is not a length",
			`<draw name="A" w="1pt" h="96px"/><draw name="B" w="1pt" h="1pt"/>`,
			[]string{
				`f.A: its size is written as w="1pt" h="96px", which is not a size`,
				`f.B: a tb layout stacks its children, and the height of the one above it is not computed: ` +
					`its height is written as h="96px", which is not a length`,
			}},
		{"a container's own height written in something that is not a length",
			`<subform name="S" h="96px"><draw name="A" w="1pt" h="1pt"/></subform>
			 <draw name="B" w="1pt" h="1pt"/>`,
			[]string{
				`f.B: a tb layout stacks its children, and the height of the one above it is not computed: ` +
					`its height is written as h="96px", which is not a length`,
			}},
		{"a margin that is not in lengths, on the container that stacks",
			`<margin topInset="1pt" rightInset="a bit"/><draw name="A" w="1pt" h="1pt"/>`,
			[]string{"f.A: the container that stacks it writes a margin that is not in lengths"}},
		{"a margin that is not in lengths, on something being stacked",
			`<subform name="S"><margin bottomInset="a bit"/><draw name="A" w="1pt" h="1pt"/></subform>
			 <draw name="B" w="1pt" h="1pt"/>`,
			[]string{"f.B: a tb layout stacks its children, and the height of the one above it is not computed: " +
				"its margin is not written in lengths"}},
		{"an origin inside a positioned container that is not a place",
			`<subform name="S"><draw name="A" y="down a bit" w="1pt" h="1pt"/></subform>
			 <draw name="B" w="1pt" h="1pt"/>`,
			[]string{
				`f.S.A: its origin is written as x="" y="down a bit", which is not a place`,
				`f.B: a tb layout stacks its children, and the height of the one above it is not computed: ` +
					`it holds something whose origin is written as y="down a bit", which is not a place`,
			}},
		{"a line-wrapping layout inside a stack",
			`<subform name="S" layout="lr-tb"><draw name="A" w="1pt" h="1pt"/></subform>
			 <draw name="B" w="1pt" h="1pt"/>`,
			[]string{"f.B: a tb layout stacks its children, and the height of the one above it is not computed: " +
				"a lr-tb layout wraps its children onto lines, and how many lines they come to is not computed here"}},
		{"a row with no columns above it",
			`<subform name="T" layout="table">
			   <subform name="R" layout="row"><draw name="A" w="1pt" h="1pt"/></subform></subform>`,
			[]string{"f.T.R.A: a row cuts its cells from the columnWidths of the container above it, " +
				"which writes none this reads"}},
		{"a table whose columnWidths are not lengths",
			`<subform name="T" layout="table" columnWidths="10pt wide">
			   <subform name="R" layout="row"><draw name="A" w="1pt" h="1pt"/></subform></subform>`,
			[]string{"f.T.R.A: a row cuts its cells from the columnWidths of the container above it, " +
				"which writes none this reads"}},
		{"a table whose columnWidths are the rest of a width",
			// pdf.js reads -1 as "every column left", which is a share of a
			// width this slice does not compute.
			`<subform name="T" layout="table" columnWidths="10pt -1">
			   <subform name="R" layout="row"><draw name="A" w="1pt" h="1pt"/></subform></subform>`,
			[]string{"f.T.R.A: a row cuts its cells from the columnWidths of the container above it, " +
				"which writes none this reads"}},
	} {
		if got := notLaid(laidOut(t, page(`w="500pt" h="500pt"`, tc.body))); strings.Join(got, "\n") != strings.Join(tc.want, "\n") {
			t.Errorf("%s:\n  got  %v\n  want %v", tc.what, got, tc.want)
		}
	}
}

func TestWhatDoesNotFitGoesOntoTheNextPage(t *testing.T) {
	// The content area is 20 points tall. A and B fill it exactly; C would
	// begin below it, so it opens a second sheet and begins again at its top.
	l := laidOut(t, page(`w="500pt" h="20pt"`, `
	  <draw name="A" w="1pt" h="12pt"/>
	  <draw name="B" w="1pt" h="8pt"/>
	  <draw name="C" w="1pt" h="5pt"/>
	  <draw name="D" w="1pt" h="5pt"/>`))
	same(t, "the sheets", byPage(l), []string{
		"0: draw f.A 0,0 1x12", "0: draw f.B 0,12 1x8",
		"1: draw f.C 0,0 1x5", "1: draw f.D 0,5 1x5"})
	same(t, "what was left off", notLaid(l), nil)
}

func TestTheSlopAStackIsAllowed(t *testing.T) {
	// pdf.js rounds the difference and allows two points of it: a column of
	// heights written in millimetres and added up in points comes out a
	// fraction over a page it was drawn to fill. One child, on a page twenty
	// points tall.
	//
	// Z is a draw of no height at all, and it is there to claim the sheet's
	// one free pass: the FIRST thing on a sheet that moves in one piece is not
	// measured against anything (layout.js:266-268), so without it every
	// height here would be allowed and the slop would not be under test. It
	// takes no room, so the twenty points are still A's.
	for _, tc := range []struct {
		h      string
		placed int
	}{
		{"20pt", 1},
		{"22pt", 1},   // two points over, which is the slop itself
		{"22.4pt", 1}, // rounds to two points over, and so is still within it
		{"22.6pt", 0}, // rounds to three, which is past it
		{"30pt", 0},
	} {
		l := laidOut(t, page(`w="500pt" h="20pt"`,
			`<draw name="Z" w="1pt" h="0pt"/><draw name="A" w="1pt" h="`+tc.h+`"/>`))
		if n := len(l.Pages[0].Boxes) - 1; n != tc.placed {
			t.Errorf("a child %s tall on a 20pt page came out %d placed, want %d", tc.h, n, tc.placed)
		}
	}
}

func TestTheRoomAStackHasIsTheRoomItWasGiven(t *testing.T) {
	// A container's children have the container's own height to fill, never
	// more than what it was itself given: pdf.js caps the available space at
	// min(this.h || Infinity, availableSpace.height).
	l := laidOut(t, page(`w="500pt" h="500pt"`, `
	  <subform name="S" layout="tb" h="10pt">
	    <draw name="A" w="1pt" h="9pt"/><draw name="B" w="1pt" h="9pt"/></subform>`))
	// S may be split, so B is not refused: the sheet turns and S begins again
	// at the top of the next one with its ten points of room back.
	same(t, "the sheets", byPage(l), []string{"0: draw f.S.A 0,0 1x9", "1: draw f.S.B 0,0 1x9"})
	same(t, "what was left off", notLaid(l), nil)
}

func TestAContentAreaWithNoHeightBoundsNothing(t *testing.T) {
	for _, tc := range []struct {
		what string
		area string
	}{
		{"none written", `w="500pt"`},
		{"one nobody can read", `w="500pt" h="tall"`},
	} {
		l := laidOut(t, page(tc.area, `<draw name="A" w="1pt" h="9000pt"/>
		  <draw name="B" w="1pt" h="1pt"/>`))
		if got := laid(l); len(got) != 2 {
			t.Errorf("%s: %v", tc.what, got)
		}
	}
}

func TestAPageAreaWithNoMediumBoundsNothingEither(t *testing.T) {
	// The furniture a page area draws is laid out in the sheet's own frame,
	// and a page area whose medium writes no size gives it no bound.
	l := laidOut(t, `<template><subform name="f"><pageSet><pageArea name="P" layout="tb">
	  <contentArea w="10pt" h="10pt"/>
	  <draw name="One" w="1pt" h="9000pt"/><draw name="Two" w="1pt" h="1pt"/>
	  </pageArea></pageSet></subform></template>`)
	same(t, "the page", laid(l), []string{"draw f.P.One 0,0 1x9000", "draw f.P.Two 0,9000 1x1"})
	if given(0) != unbounded || given(7) != 7 {
		t.Errorf("given(0)=%v given(7)=%v", given(0), given(7))
	}
	if !math.IsInf(float64(unbounded), 1) {
		t.Errorf("unbounded is %v", unbounded)
	}
}

func TestABoxSaysWhenTheTemplateHidesIt(t *testing.T) {
	l := laidOut(t, page(`w="500pt" h="500pt"`, `
	  <draw name="A" w="1pt" h="1pt" presence="hidden"/>
	  <draw name="B" w="1pt" h="1pt"/>`))
	b := l.Pages[0].Boxes
	if len(b) != 2 || !b[0].Hidden || b[1].Hidden {
		t.Errorf("the boxes came out %+v", b)
	}
}

func TestAHeightIsMeasuredOnceAndRemembered(t *testing.T) {
	// The memo is what keeps a form of a dozen levels from being walked once
	// per level. A node is marked before its children are measured, so a tree
	// that somehow held itself would stop rather than recurse for ever.
	p := newPlacer()
	n := &FormNode{Kind: "draw", Template: &Node{Attr: map[string]string{"h": "5pt"}}}
	loop := &FormNode{Kind: "subform", Template: &Node{Attr: map[string]string{}}}
	loop.Kids = []*FormNode{loop}
	if h, why := p.heightOf(n, unbounded, 0); h != 5 || why != "" {
		t.Errorf("first time: %v %q", h, why)
	}
	p.heights[heightKey{n, unbounded, 0}] = height{h: 99}
	if h, _ := p.heightOf(n, unbounded, 0); h != 99 {
		t.Errorf("it was measured again rather than remembered: %v", h)
	}
	if _, why := p.heightOf(loop, unbounded, 0); why != measuringItself {
		t.Errorf("a tree holding itself gave %q", why)
	}
}

func TestReadingAMargin(t *testing.T) {
	for _, tc := range []struct {
		what string
		src  string
		want insets
		ok   bool
	}{
		{"none at all", ``, insets{}, true},
		{"all four", `<margin topInset="1pt" rightInset="2pt"
		  bottomInset="3pt" leftInset="4pt"/>`, insets{1, 2, 3, 4}, true},
		{"the ones it leaves out are nought", `<margin topInset="1pt"/>`, insets{top: 1}, true},
		{"one that is not a length", `<margin leftInset="a bit"/>`, insets{}, false},
	} {
		got, ok := marginOf(parse(t, `<template><subform>`+tc.src+`</subform></template>`).Child("subform"))
		if got != tc.want || ok != tc.ok {
			t.Errorf("%s: %+v %v, want %+v %v", tc.what, got, ok, tc.want, tc.ok)
		}
		if got.vertical() != got.top+got.bottom {
			t.Errorf("%s: the vertical insets came to %v", tc.what, got.vertical())
		}
	}
}

func TestARowWhoseCellHasNoHeightHasNoHeightEither(t *testing.T) {
	// A row is as tall as its tallest cell, so one cell nobody can measure
	// leaves the row unmeasured — and the table above it with it.
	l := laidOut(t, page(`w="500pt" h="500pt"`, `
	  <subform name="T" layout="table" columnWidths="10pt 10pt">
	    <subform name="R" layout="row"><draw name="A" w="1pt" maxH="9pt"/></subform>
	  </subform>
	  <draw name="B" w="1pt" h="5pt"/>`))
	same(t, "what was left off", notLaid(l), []string{
		"f.T.R.A: " + boundByTheRoom("maxH"),
		// The reason names where the stack actually stopped, which is the table
		// rather than the subform above it.
		"f.B: a table layout stacks its children, and the height of the one above it is not computed: " +
			boundByTheRoom("maxH")})
}

func TestInsideAContainerThatMovesWholeAStackStillStops(t *testing.T) {
	// G is kept intact, so what is inside it is laid out by the stacking that
	// does not turn pages. A margin nobody can read stops the container that
	// writes it, and the height it therefore has not got stops the stack it
	// sits in.
	l := laidOut(t, page(`w="500pt" h="500pt"`, `
	  <subform name="G" layout="tb"><keep intact="contentArea"/>
	    <subform name="Bad" layout="tb"><margin topInset="96px"/>
	      <draw name="A" w="1pt" h="5pt"/></subform>
	    <draw name="B" w="1pt" h="5pt"/></subform>`))
	same(t, "the page", laid(l), nil)
	same(t, "what was left off", notLaid(l), []string{
		"f.G.Bad.A: the container that stacks it writes a margin that is not in lengths",
		"f.G.B: a tb layout stacks its children, and the height of the one above it is not computed: " +
			"its margin is not written in lengths"})
}
