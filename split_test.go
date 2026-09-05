// Copyright (c) 2026, the go-pdfkit/xfa authors
// All rights reserved.
//
// SPDX-License-Identifier: BSD-3-Clause

package xfa

import (
	"strings"
	"testing"
)

// cutting is a positioned container of three draws at written y, inside a tb
// root, on sheets whose content area is 30 points tall. The container is 60
// points, so it cannot fit on any sheet whole.
//
//	B at y=0  h=10   -> 0..10
//	C at y=20 h=10   -> 20..30
//	D at y=50 h=10   -> 50..60
func cutting(keep, attrs string) string {
	return `<subform name="G" ` + attrs + `>` + keep + `
	    <draw name="B" w="1pt" y="0pt" h="10pt"/>
	    <draw name="C" w="1pt" y="20pt" h="10pt"/>
	    <draw name="D" w="1pt" y="50pt" h="10pt"/></subform>`
}

func TestAPositionedContainerIsCutOnlyWhereItsAuthorPermitsIt(t *testing.T) {
	// With no <keep> the subform is pdfium's ContentArea — GetIntact,
	// cxfa_node.cpp:1550-1557 — so it moves whole onto the next sheet and runs
	// off the bottom, which is what this package has always done and what
	// pdf.js does.
	l := laidOut(t, sheets(`>`+sheet("P", "", "30", ""),
		`<draw name="A" w="1pt" h="10pt"/>`+cutting("", "")))
	same(t, "an unpermitted container", byPage(l), []string{
		"0: draw f.A 0,0 1x10",
		"1: draw f.G.B 0,0 1x10", "1: draw f.G.C 0,20 1x10", "1: draw f.G.D 0,50 1x10"})

	// <keep intact="none"/> is the author's permission, and pdfium reads it
	// through GetIntactFromKeep. The cut falls at 20 — the room left below A —
	// bisecting nothing, and the second part begins again at the top of the
	// next sheet with 20 taken off every child's written y. D still does not
	// fit in the 30 that are left, so it is cut again at 50.
	l = laidOut(t, sheets(`>`+sheet("P", "", "30", ""),
		`<draw name="A" w="1pt" h="10pt"/>`+cutting(`<keep intact="none"/>`, "")))
	same(t, "a permitted container", byPage(l), []string{
		"0: draw f.A 0,0 1x10", "0: draw f.G.B 0,10 1x10",
		"1: draw f.G.C 0,0 1x10",
		"2: draw f.G.D 0,0 1x10"})
}

func TestACutIsWalkedUpToTheTopOfWhateverItLandsIn(t *testing.T) {
	// The room left below A is 25, which falls inside C (20..30). C is a draw,
	// and pdfium's GetIntact answers ContentArea for every draw
	// (cxfa_node.cpp:1586-1587), so the cut is walked up to C's own top at 20
	// — FindLayoutItemSplitPos:580-583. B alone stays on the first sheet.
	l := laidOut(t, sheets(`>`+sheet("P", "", "30", ""),
		`<draw name="A" w="1pt" h="5pt"/>`+cutting(`<keep intact="none"/>`, "")))
	same(t, "the walked-up cut", byPage(l), []string{
		"0: draw f.A 0,0 1x5", "0: draw f.G.B 0,5 1x10",
		"1: draw f.G.C 0,0 1x10",
		"2: draw f.G.D 0,0 1x10"})
}

func TestACutIsRefusedRatherThanBisectAField(t *testing.T) {
	// A field of a positioned container has pdfium's intact of "none"
	// (cxfa_node.cpp:1558-1584), so pdfium would cut it in half and emit the
	// node twice. This package will not: with the only cut position falling
	// inside the field, no cut is made at all and the container moves whole,
	// which is also what pdfium does when FindSplitPos comes back at nought.
	body := `<draw name="A" w="1pt" h="5pt"/>
	  <subform name="G"><keep intact="none"/>
	    <field name="B" w="1pt" y="0pt" h="40pt"/></subform>`
	l := laidOut(t, sheets(`>`+sheet("P", "", "30", ""), body))
	same(t, "the refused cut", byPage(l), []string{
		"0: draw f.A 0,0 1x5",
		"1: field f.G.B 0,0 1x40"})

	// The same field with a <keep> of its own is one the cut may not fall
	// inside, so it is walked up to the field's top instead — and there is
	// nothing above it, so no cut is possible there either and the container
	// still moves whole. This is the second way [placer.cut] arrives at "no
	// cut": pdfium's, rather than ours.
	l = laidOut(t, sheets(`>`+sheet("P", "", "30", ""),
		strings.Replace(body, `<field name="B" w="1pt" y="0pt" h="40pt"/>`,
			`<field name="B" w="1pt" y="0pt" h="40pt"><keep intact="contentArea"/></field>`, 1)))
	same(t, "the cut with nowhere to go", byPage(l), []string{
		"0: draw f.A 0,0 1x5",
		"1: field f.G.B 0,0 1x40"})
}

func TestWhatCannotBeCutAgainGoesDownWhole(t *testing.T) {
	// The first cut is made, and what is left holds one child taller than a
	// whole content area. There is no second cut, so the rest of the container
	// goes down on the sheet the flow has reached and overflows it — pdfium's
	// answer when FindSplitPos returns nought mid-container.
	l := laidOut(t, sheets(`>`+sheet("P", "", "30", ""),
		`<subform name="G"><keep intact="none"/>
		   <draw name="B" w="1pt" y="0pt" h="10pt"/>
		   <draw name="C" w="1pt" y="40pt" h="100pt"/></subform>`))
	// The sheet in between is the price, and it is pdfium's too: on it the cut
	// proposed at 60 is walked up to C's own top at 40, which is progress from
	// 30, so a part with nothing in it is emitted and the page ends.
	// FindSplitPos returns the same 10 (relative) there.
	same(t, "the uncuttable remainder", byPage(l), []string{
		"0: draw f.G.B 0,0 1x10",
		"2: draw f.G.C 0,0 1x100"})
}

func TestACutContainerCarriesTheFlowOnBelowIt(t *testing.T) {
	// The cut falls at 30, so B and C stay and D moves with 30 taken off its
	// written y — SplitLayoutItem's `pChildItem->s_pos_.y -= fSplitPos` — and
	// lands at 20 rather than at the top of the sheet. What the container then
	// adds to the stack is its height measured from where its last part began,
	// which is that item's `s_size_.height - fSplitPos` of 30: the flow reaches
	// exactly the bottom, and E turns the page again.
	l := laidOut(t, sheets(`>`+sheet("P", "", "30", ""),
		cutting(`<keep intact="none"/>`, "")+`<draw name="E" w="1pt" h="5pt"/>`))
	same(t, "the flow below a cut container", byPage(l), []string{
		"0: draw f.G.B 0,0 1x10", "0: draw f.G.C 0,20 1x10",
		"1: draw f.G.D 0,20 1x10",
		"2: draw f.E 0,0 1x5"})
}

func TestACutContainerKeepsItsOwnMarginAndOffsets(t *testing.T) {
	// The container's top inset moves every child down inside it and is part of
	// the height it is cut into, exactly as pdfium's layout items carry their
	// ancestors' insets (CXFA_ContentLayoutItem::GetAbsoluteRect). With a top
	// inset of 4 the children sit at 4, 24 and 54; the room left below A is 25,
	// which falls inside C at 24..34, so the cut is walked up to 24.
	l := laidOut(t, sheets(`>`+sheet("P", "", "30", ""),
		`<draw name="A" w="1pt" h="5pt"/>`+
			cutting(`<keep intact="none"/><margin topInset="4pt" leftInset="2pt"/>`, "")))
	same(t, "a cut container's margin", byPage(l), []string{
		"0: draw f.A 0,0 1x5", "0: draw f.G.B 2,9 1x10",
		"1: draw f.G.C 2,0 1x10",
		"2: draw f.G.D 2,0 1x10"})
}

func TestAHiddenChildIsInNoCutsArithmeticAndStillPlaced(t *testing.T) {
	// pdfium lays a hidden element out not at all — PresenceRequiresSpace is
	// false and no layout item is made — so it moves no cut. This package
	// reports it rather than dropping it, with the part its own y falls in.
	l := laidOut(t, sheets(`>`+sheet("P", "", "30", ""),
		`<draw name="A" w="1pt" h="5pt"/>
		 <subform name="G"><keep intact="none"/>
		   <draw name="B" w="1pt" y="0pt" h="10pt"/>
		   <draw name="H" w="1pt" y="20pt" h="10pt" presence="hidden"/>
		   <draw name="C" w="1pt" y="40pt" h="10pt"/></subform>`))
	// The cut falls at 25, where the room ran out, because nothing that counts
	// straddles it — so C keeps 15 of its written 40 on the sheet it moves to.
	same(t, "a hidden child of a cut container", byPage(l), []string{
		"0: draw f.A 0,0 1x5", "0: draw f.G.B 0,5 1x10", "0: draw f.G.H 0,25 1x10",
		"1: draw f.G.C 0,15 1x10"})
}

func TestACutStopsWhenTheFormRunsOutOfSheets(t *testing.T) {
	// One sheet only. The first part goes down, and there is no second sheet
	// for what is left, so the rest is reported rather than dropped.
	l := laidOut(t, sheets(`><occur max="1"/>`+sheet("P", `><occur max="1"/`, "30", ""),
		cutting(`<keep intact="none"/>`, "")))
	same(t, "the one sheet", byPage(l), []string{
		"0: draw f.G.B 0,0 1x10", "0: draw f.G.C 0,20 1x10"})
	same(t, "what is left", notLaid(l), []string{"f.G.D: " + noNextPage})
}

func TestACutContainerNobodyCanMeasureNeverReachesTheCut(t *testing.T) {
	// A margin that is not a length, a child whose y is not a place and a child
	// whose height is not a length each stop the container ONE STEP EARLIER
	// than the cut: [placer.whole] asks [placer.heightOf] first, and
	// [placer.measure] reads exactly those three. That is why [placer.cut] and
	// [placer.piecesOf] read them back without checking, and this says so with
	// a form rather than with a comment.
	for _, tc := range []struct{ what, body string }{
		{"a margin nobody can read", `<subform name="G"><keep intact="none"/>
		   <margin topInset="wide"/><draw name="B" w="1pt" y="0pt" h="40pt"/></subform>`},
		{"an origin nobody can read", `<subform name="G"><keep intact="none"/>
		   <draw name="B" w="1pt" y="over there" h="40pt"/></subform>`},
		{"a height nobody can read", `<subform name="G"><keep intact="none"/>
		   <draw name="B" w="1pt" y="0pt" h="tall"/></subform>`},
	} {
		l := laidOut(t, sheets(`>`+sheet("P", "", "30", ""),
			`<draw name="A" w="1pt" h="25pt"/>`+tc.body))
		if got := len(l.Pages); got != 1 {
			t.Errorf("%s came to %d sheets, want 1: nothing should have been cut", tc.what, got)
		}
		if got := len(l.Pages[0].Boxes); got != 1 {
			t.Errorf("%s put %d boxes on the sheet, want only the draw above it", tc.what, got)
		}
	}
}

func TestIntactFollowsWhatTheContainerAboveSays(t *testing.T) {
	// [intactOf] is pdfium's GetIntact, and the field arm is the one that reads
	// upwards. Each of its answers is asked here of a form laid out for it,
	// because the arms are reached through the cut rather than directly.
	p := newPlacer()
	src := `<template><subform name="f" layout="tb">
	  <subform name="G"><keep intact="none"/><field name="B" w="1pt" h="1pt"/></subform>
	  <subform name="K" layout="tb"><keep intact="contentArea"/><field name="C" w="1pt" h="1pt"/></subform>
	  <subform name="T" layout="tb"><field name="D" w="1pt" h="1pt"/></subform>
	  <subform name="P"><field name="G1" w="1pt" h="1pt"/></subform>
	  <subform name="R" layout="row"><field name="G2" w="1pt" h="1pt"/></subform>
	  <draw name="E" w="1pt" h="1pt"/>
	  <exclGroup name="X"><field name="F" w="1pt" h="1pt"/></exclGroup>
	</subform></template>`
	root := firstOfKind(Expand(parse(t, src), nil).Root, "subform")
	p.mapUp(root)
	find := func(name string) *FormNode {
		var got *FormNode
		root.Walk(func(n *FormNode) {
			if n.Template.Get("name") == name {
				got = n
			}
		})
		return got
	}
	for _, tc := range []struct{ name, want string }{
		{"G", "none"},        // a written keep wins outright
		{"K", "contentArea"}, // and so does one that keeps it together
		{"T", "none"},        // a tb subform with no keep
		{"P", "contentArea"}, // a POSITIONED subform with no keep: pdfium's default
		{"R", "contentArea"}, // and a row, which pdfium reads the same way
		{"f", "none"},        // the root, likewise
		{"E", "contentArea"}, // a draw, always
		{"X", "none"},        // anything else pdfium's switch does not name
		{"B", "none"},        // a field of a positioned container that is cut
		{"C", "contentArea"}, // a field of a container kept together
		{"D", "none"},        // a field of a tb container, XFA 3.0
	} {
		if got := intactOf(p, find(tc.name)); got != tc.want {
			t.Errorf("the intact of %s came out as %q, want %q", tc.name, got, tc.want)
		}
	}
	// A field whose layout parent is not a container of the body at all is kept
	// together, which is pdfium reading a null parent or a page area
	// (cxfa_node.cpp:1559-1562).
	if got := intactOf(newPlacer(), find("B")); got != "contentArea" {
		t.Errorf("the intact of a parentless field came out as %q, want contentArea", got)
	}
}
