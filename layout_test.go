// Copyright (c) 2026, the go-pdfkit/xfa authors
// All rights reserved.
//
// SPDX-License-Identifier: BSD-3-Clause

package xfa

import (
	"fmt"
	"sort"
	"strings"
	"testing"
)

// laid is every box of a layout as "kind path x,y w,h", rounded, so that a
// test can state a whole page in one string per element.
func laid(l *Layout) []string {
	var out []string
	for _, p := range l.Pages {
		for _, b := range p.Boxes {
			out = append(out, fmt.Sprintf("%s %s %g,%g %gx%g",
				b.Kind, b.Path, b.X.Points(), b.Y.Points(), b.W.Points(), b.H.Points()))
		}
	}
	return out
}

// notLaid is every element a layout did not place, as "path: why".
func notLaid(l *Layout) []string {
	var out []string
	for _, u := range l.Unplaced {
		out = append(out, u.Path+": "+u.Why)
	}
	return out
}

func laidOut(t *testing.T, src string) *Layout {
	t.Helper()
	return Place(Expand(parse(t, src), nil))
}

func same(t *testing.T, what string, got, want []string) {
	t.Helper()
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("%s came out as\n  %s\nwant\n  %s",
			what, strings.Join(got, "\n  "), strings.Join(want, "\n  "))
	}
}

// worked is a form whose every coordinate is worked out by hand in the
// comment below it. Nothing in it is read off this package's own output.
const worked = `<template>
<subform name="form1" layout="tb">
  <pageSet><pageArea name="Page1">
    <medium long="792pt" short="612pt"/>
    <contentArea x="0.25in" y="0.5in" w="576pt" h="700pt"/>
    <draw name="Stamp" x="10pt" y="20pt" w="30pt" h="40pt"/>
  </pageArea></pageSet>
  <subform name="Body" x="5pt" y="7pt">
    <field name="A" x="1pt" y="2pt" w="100pt" h="12pt"/>
    <subform name="Inner" x="10pt" y="20pt">
      <field name="B" x="3pt" y="4pt" w="50pt" h="10pt"/>
    </subform>
  </subform>
  <subform name="Second"><field name="C" w="10pt" h="10pt"/></subform>
</subform>
</template>`

func TestAFormPlacedByHand(t *testing.T) {
	l := laidOut(t, worked)
	if len(l.Pages) != 1 {
		t.Fatalf("%d pages", len(l.Pages))
	}
	p := l.Pages[0]
	// The medium is 612 by 792 the portrait way up, and the content area
	// begins a quarter of an inch across and half an inch down: 18, 36.
	if p.Width != 612 || p.Height != 792 {
		t.Errorf("the page is %v by %v", p.Width, p.Height)
	}
	if p.Content != (Rect{X: 18, Y: 36, W: 576, H: 700}) {
		t.Errorf("the content area is %+v", p.Content)
	}
	same(t, "the page", laid(l), []string{
		// Stamp is the page area's own furniture, so it is measured from the
		// corner of the sheet and not from the content area: 0+10, 0+20.
		"draw form1.Page1.Stamp 10,20 30x40",
		// form1 is "tb", so Body is stacked rather than positioned: its own
		// x=5 y=7 are discarded and, being the first child, it sits exactly at
		// the content area's origin, 18,36.
		//   A:     18+1, 36+2                     = 19,38
		"field form1.Body.A 19,38 100x12",
		//   Inner: 18+10, 36+20 = 28,56, and then
		//   B:     28+3, 56+4                     = 31,60
		"field form1.Body.Inner.B 31,60 50x10",
	})
	// Second is the tb layout's second child: where it begins is the height of
	// Body, which is the flow layout this slice does not do.
	same(t, "what was left off", notLaid(l), []string{
		"form1.Second.C: a tb layout stacks its children, and where the one above it ends is not computed here",
	})
	if l.Fields() != 2 {
		t.Errorf("%d fields placed", l.Fields())
	}
}

func TestAPositionedFormNeedsNoFirstChildRule(t *testing.T) {
	// The same body under a positioned outermost subform: now every child is
	// placed at its own coordinates, Body's x=5 y=7 among them.
	l := laidOut(t, strings.Replace(worked, `name="form1" layout="tb"`, `name="form1"`, 1))
	same(t, "the page", laid(l), []string{
		"draw form1.Page1.Stamp 10,20 30x40",
		// Body: 18+5, 36+7 = 23,43; A: 24,45; Inner: 33,63; B: 36,67
		"field form1.Body.A 24,45 100x12",
		"field form1.Body.Inner.B 36,67 50x10",
		"field form1.Second.C 18,36 10x10",
	})
	if notLaid(l) != nil {
		t.Errorf("something was left off: %v", notLaid(l))
	}
}

func TestTheSizeOfTheSheet(t *testing.T) {
	for _, tc := range []struct {
		what   string
		medium string
		w, h   Measure
	}{
		{"portrait", `<medium long="792pt" short="612pt"/>`, 612, 792},
		{"landscape", `<medium long="792pt" short="612pt" orientation="landscape"/>`, 792, 612},
		{"no medium at all", ``, 0, 0},
		{"only one side", `<medium short="612pt"/>`, 0, 0},
		{"a side that is not a length", `<medium long="a4" short="612pt"/>`, 0, 0},
	} {
		l := laidOut(t, `<template><subform name="f"><pageSet><pageArea name="P">`+
			tc.medium+`<contentArea/></pageArea></pageSet></subform></template>`)
		if p := l.Pages[0]; p.Width != tc.w || p.Height != tc.h {
			t.Errorf("%s: the page is %v by %v, want %v by %v", tc.what, p.Width, p.Height, tc.w, tc.h)
		}
	}
}

func TestWhatIsReportedRatherThanPlaced(t *testing.T) {
	for _, tc := range []struct {
		what string
		src  string
		want []string
	}{
		{"a form with no page area",
			`<template><subform name="f"><field name="A" w="1pt" h="1pt"/></subform></template>`,
			[]string{"f.A: the form has no page area"}},
		{"a page area with no content area",
			`<template><subform name="f"><pageSet><pageArea name="P">
			  <draw name="Furniture" x="1pt" y="2pt" w="3pt" h="4pt"/></pageArea></pageSet>
			  <field name="A" w="1pt" h="1pt"/></subform></template>`,
			[]string{"f.A: the page area has no content area"}},
		{"a second page area",
			`<template><subform name="f"><pageSet>
			   <pageArea name="P1"><contentArea/><draw name="One" w="1pt" h="1pt"/></pageArea>
			   <pageArea name="P2"><draw name="Two" w="1pt" h="1pt"/></pageArea>
			  </pageSet></subform></template>`,
			[]string{"f.P2.Two: it is on another page area: this slice lays out the first one only"}},
		{"an origin that is not a place",
			`<template><subform name="f"><pageSet><pageArea name="P"><contentArea/></pageArea></pageSet>
			  <subform name="S" x="over there"><field name="A" w="1pt" h="1pt"/></subform></subform></template>`,
			[]string{`f.S.A: its origin is written as x="over there" y="", which is not a place`}},
		{"a size that is not a size",
			`<template><subform name="f"><pageSet><pageArea name="P"><contentArea/></pageArea></pageSet>
			  <field name="A" w="wide" h="1pt"/></subform></template>`,
			[]string{`f.A: its size is written as w="wide" h="1pt", which is not a size`}},
		{"a width nobody wrote",
			`<template><subform name="f"><pageSet><pageArea name="P"><contentArea/></pageArea></pageSet>
			  <field name="A" h="1pt"/></subform></template>`,
			[]string{"f.A: the template does not write its width, which only measuring its text would give"}},
		{"a height nobody wrote",
			`<template><subform name="f"><pageSet><pageArea name="P"><contentArea/></pageArea></pageSet>
			  <field name="A" w="1pt"/></subform></template>`,
			[]string{"f.A: the template does not write its height, which only measuring its text would give"}},
		{"neither one nor the other",
			`<template><subform name="f"><pageSet><pageArea name="P"><contentArea/></pageArea></pageSet>
			  <draw name="A"/></subform></template>`,
			[]string{"f.A: the template does not write its width or its height, which only measuring its text would give"}},
		{"a container anchored by a corner, with no size of its own",
			`<template><subform name="f"><pageSet><pageArea name="P"><contentArea/></pageArea></pageSet>
			  <subform name="S" anchorType="bottomLeft"><field name="A" w="1pt" h="1pt"/></subform></subform></template>`,
			[]string{"f.S.A: it is anchored by a corner other than its top left, and its size is not written: " +
				"only measuring its contents would give it"}},
		{"a container turned on its side",
			`<template><subform name="f"><pageSet><pageArea name="P"><contentArea/></pageArea></pageSet>
			  <subform name="S" rotate="90" w="9pt" h="9pt"><field name="A" w="1pt" h="1pt"/></subform></subform></template>`,
			[]string{"f.S.A: its contents are turned, which this slice does not follow"}},
		{"a layout that fills from the right",
			`<template><subform name="f"><pageSet><pageArea name="P"><contentArea/></pageArea></pageSet>
			  <subform name="S" layout="rl-tb"><field name="A" w="1pt" h="1pt"/></subform></subform></template>`,
			[]string{"f.S.A: a rl-tb layout fills from the right, which needs a width not computed here"}},
	} {
		if got := notLaid(laidOut(t, tc.src)); strings.Join(got, "\n") != strings.Join(tc.want, "\n") {
			t.Errorf("%s: %v, want %v", tc.what, got, tc.want)
		}
	}
}

func TestASubformSetHoldsNoPlaceOfItsOwn(t *testing.T) {
	// pdf.js yields a subformSet's children as if they were its parent's, and
	// skips it when it asks who the parent is. So its x and y do not count,
	// and its children are numbered among the parent's for the flow rule.
	l := laidOut(t, `<template><subform name="f" layout="tb">
	  <pageSet><pageArea name="P"><contentArea x="10pt" y="20pt"/></pageArea></pageSet>
	  <subformSet name="Set" x="1000pt" y="1000pt">
	    <field name="A" x="1pt" y="2pt" w="5pt" h="5pt"/>
	    <field name="B" w="5pt" h="5pt"/>
	  </subformSet>
	</subform></template>`)
	// A is the first child the tb layout sees, through the flattened
	// subformSet, so its own x and y go the way any first child's do and it
	// sits at the content area's origin.
	same(t, "the page", laid(l), []string{"field f.Set.A 10,20 5x5"})
	same(t, "what was left off", notLaid(l), []string{
		"f.Set.B: a tb layout stacks its children, and where the one above it ends is not computed here",
	})
}

func TestAnExclGroupPlacesItsButtons(t *testing.T) {
	l := laidOut(t, `<template><subform name="f">
	  <pageSet><pageArea name="P"><contentArea/></pageArea></pageSet>
	  <exclGroup name="Sex" x="10pt" y="10pt">
	    <field name="M" x="0pt" y="0pt" w="8pt" h="8pt"/>
	    <field name="F" x="0pt" y="20pt" w="8pt" h="8pt"/>
	  </exclGroup></subform></template>`)
	same(t, "the page", laid(l), []string{
		"field f.Sex.M 10,10 8x8",
		"field f.Sex.F 10,30 8x8",
	})
}

func TestAnAreaIsAPositionedContainer(t *testing.T) {
	// An area has no layout attribute at all, so its children are positioned
	// however the subform above it is laid out.
	l := laidOut(t, `<template><subform name="f" layout="tb">
	  <pageSet><pageArea name="P"><contentArea/></pageArea></pageSet>
	  <area name="Ar" x="100pt" y="100pt">
	    <field name="A" x="1pt" y="1pt" w="5pt" h="5pt"/>
	    <field name="B" x="2pt" y="2pt" w="5pt" h="5pt"/>
	  </area></subform></template>`)
	// The area is the tb layout's first child, so its own x and y go, and both
	// of its children are placed at the content area's origin plus their own.
	same(t, "the page", laid(l), []string{"field f.Ar.A 1,1 5x5", "field f.Ar.B 2,2 5x5"})
}

func TestABoxCarriesTheValueBoundToIt(t *testing.T) {
	tmpl := parse(t, `<template><subform name="form1">
	  <pageSet><pageArea name="P"><contentArea/></pageArea></pageSet>
	  <field name="Total" w="10pt" h="10pt"/></subform></template>`)
	data, err := ParseDatasets(strings.NewReader(
		`<xfa:datasets xmlns:xfa="http://www.xfa.org/schema/xfa-data/1.0/"><xfa:data>
		  <form1><Total>42</Total></form1></xfa:data></xfa:datasets>`))
	if err != nil {
		t.Fatal(err)
	}
	l := Place(Expand(tmpl, data))
	if len(l.Pages[0].Boxes) != 1 || l.Pages[0].Boxes[0].Value != "42" {
		t.Errorf("the box came out %+v", l.Pages[0].Boxes)
	}
}

func TestLayingOutNothing(t *testing.T) {
	if l := Place(nil); len(l.Pages) != 0 || l.Unplaced != nil {
		t.Errorf("a nil form laid out to %+v", l)
	}
	if l := Place(Expand(nil, nil)); len(l.Pages) != 0 {
		t.Errorf("an empty form laid out to %+v", l)
	}
	// A template with no subform is not a form: pdf.js reads
	// this.subform.children[0] and there is nothing there.
	if l := laidOut(t, `<template><proto/></template>`); len(l.Pages) != 0 {
		t.Errorf("a template with no subform laid out to %+v", l)
	}
}

func TestAnchorTypeAndRotateAreResolvedIntoTheBox(t *testing.T) {
	// pdf.js's getTransformedBBox (layout.js:202-259), worked through by hand
	// for a box at 100,200 measuring 40 by 10. The anchor names which corner
	// the x and the y are, and a turn swaps the sides.
	for _, tc := range []struct {
		anchor string
		rotate string
		want   Rect
		deg    int
	}{
		{"", "", Rect{100, 200, 40, 10}, 0},
		{"topLeft", "", Rect{100, 200, 40, 10}, 0},
		{"topCenter", "", Rect{80, 200, 40, 10}, 0},
		{"topRight", "", Rect{60, 200, 40, 10}, 0},
		{"middleLeft", "", Rect{100, 195, 40, 10}, 0},
		{"middleCenter", "", Rect{80, 195, 40, 10}, 0},
		{"middleRight", "", Rect{60, 195, 40, 10}, 0},
		{"bottomLeft", "", Rect{100, 190, 40, 10}, 0},
		{"bottomCenter", "", Rect{80, 190, 40, 10}, 0},
		{"bottomRight", "", Rect{60, 190, 40, 10}, 0},
		{"", "90", Rect{100, 160, 10, 40}, 90},
		{"", "180", Rect{60, 190, 40, 10}, 180},
		{"", "270", Rect{90, 200, 10, 40}, 270},
		{"bottomRight", "90", Rect{90, 200, 10, 40}, 90},
		// A turn that is not a right angle is not a turn: pdf.js validates it
		// the same way and falls back to nought.
		{"", "45", Rect{100, 200, 40, 10}, 0},
		{"", "-90", Rect{90, 200, 10, 40}, 270},
		{"", "450", Rect{100, 160, 10, 40}, 90},
		{"", "sideways", Rect{100, 200, 40, 10}, 0},
	} {
		n := &Node{Attr: map[string]string{"anchorType": tc.anchor, "rotate": tc.rotate}}
		got, deg := transformedBBox(n, 100, 200, 40, 10)
		if got != tc.want || deg != tc.deg {
			t.Errorf("anchorType=%q rotate=%q gave %+v at %d degrees, want %+v at %d",
				tc.anchor, tc.rotate, got, deg, tc.want, tc.deg)
		}
	}
}

func TestEveryFlowLayoutIsNamedInItsReason(t *testing.T) {
	// Each of the six has to be recognised, or its children would be placed at
	// coordinates the layout throws away.
	var names []string
	for lay := range flowLayouts {
		names = append(names, lay)
	}
	sort.Strings(names)
	for _, lay := range names {
		l := laidOut(t, `<template><subform name="f"><pageSet><pageArea name="P"><contentArea/>
		  </pageArea></pageSet><subform name="S" layout="`+lay+`">
		  <field name="A" w="1pt" h="1pt"/><field name="B" w="1pt" h="1pt"/>
		  </subform></subform></template>`)
		if len(notLaid(l)) == 0 || !strings.Contains(notLaid(l)[0], lay) {
			t.Errorf("%s: %v", lay, notLaid(l))
		}
		// The ones that fill from the left place their first child; the ones
		// that fill from the right place neither.
		want := 0
		if lay == "lr-tb" {
			want = 1
		}
		if len(l.Pages[0].Boxes) != want {
			t.Errorf("%s placed %d of the two", lay, len(l.Pages[0].Boxes))
		}
	}
}

func TestAContainerAnchoredByACornerMovesWhatIsInside(t *testing.T) {
	// pdf.js emits the anchor as a CSS transform on the container, and a
	// transform moves the subtree with it. Worked by hand: the subform names
	// its bottom left corner at 0,100 and is 50 tall, so its top left is at
	// 0,50, and the field one point across and two down from that is at 1,52.
	l := laidOut(t, `<template><subform name="f">
	  <pageSet><pageArea name="P"><contentArea/></pageArea></pageSet>
	  <subform name="S" anchorType="bottomLeft" y="100pt" w="100pt" h="50pt">
	    <field name="A" x="1pt" y="2pt" w="5pt" h="5pt"/>
	  </subform></subform></template>`)
	same(t, "the page", laid(l), []string{"field f.S.A 1,52 5x5"})
}

func TestTheShadowWalkStopsGoingDown(t *testing.T) {
	// The walk that fills in the inside of a value-carrying container is its
	// own recursion over a tree it did not build, so it needs its own floor.
	// [Expand] takes a *Node, and a caller may hand it one the reader never
	// made and never bounded.
	deep := &Node{Kind: "field", Attr: map[string]string{"name": "bottom"}}
	for i := 0; i < maxDepth+8; i++ {
		deep = &Node{Kind: "exclGroup", Attr: map[string]string{"name": "g"}, Kids: []*Node{deep}}
	}
	b := &binder{}
	b.root = &FormNode{Kind: "template"}
	b.cur = b.root
	b.shadow(deep, "", 0)
	n := 0
	b.root.Walk(func(*FormNode) { n++ })
	if n != maxDepth+1 {
		t.Errorf("it followed %d nodes down a tree %d deep, want %d", n, maxDepth+9, maxDepth+1)
	}
}
