// Copyright (c) 2026, the go-pdfkit/xfa authors
// All rights reserved.
//
// SPDX-License-Identifier: BSD-3-Clause

package xfa

import (
	"fmt"
	"strings"
	"testing"
)

// wrapped lays a wrapping container out inside a content area 100 points wide
// and as tall as the test asks, and reports every box it placed.
func wrapped(t *testing.T, area, body string) *Layout {
	t.Helper()
	return laidOut(t, `<template><subform name="f" layout="tb">
	  <pageSet><pageArea name="P"><medium long="1000pt" short="1000pt"/>
	    <contentArea `+area+`/></pageArea></pageSet>`+body+`</subform></template>`)
}

func TestALineFillsAcrossAndTheNextBeginsBelowIt(t *testing.T) {
	// Worked by hand from addHTML (layout.js:107-129). The line is 100 wide.
	// A and B fill it exactly, so C cannot go on it: it opens a second line at
	// the height of the first, which is the taller of A and B.
	//
	//   line 0: A at 0,0 (40x10)  B at 40,0 (60x30)   height = 30
	//   line 1: C at 0,30 (50x20)                     height = 50
	//   line 1: D at 50,30 (30x5)
	l := wrapped(t, `w="100pt" h="500pt"`, `<subform name="S" layout="lr-tb" w="100pt">
	  <draw name="A" w="40pt" h="10pt"/><draw name="B" w="60pt" h="30pt"/>
	  <draw name="C" w="50pt" h="20pt"/><draw name="D" w="30pt" h="5pt"/></subform>
	  <draw name="E" w="1pt" h="1pt"/>`)
	want := []string{
		"draw f.S.A 0,0 40x10",
		"draw f.S.B 40,0 60x30",
		"draw f.S.C 0,30 50x20",
		"draw f.S.D 50,30 30x5",
		// The container is as tall as its lines, so what follows it in the
		// stack begins at 50.
		"draw f.E 0,50 1x1",
	}
	if got := laid(l); strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("got\n  %v\nwant\n  %v", got, want)
	}
	if len(notLaid(l)) != 0 {
		t.Errorf("unplaced: %v", notLaid(l))
	}
}

func TestAWrappingLayoutThatFillsFromTheRight(t *testing.T) {
	// pdf.js gives rl-tb the class xfaRl, which is flex-direction: row-reverse
	// (xfa_layer_builder.css:269-273): the same packing, anchored at the
	// container's right edge instead of its left. No form of the 560-form
	// corpus writes rl-tb or rl-row at all, so nothing here is measured
	// against a real one.
	l := wrapped(t, `w="100pt" h="500pt"`, `<subform name="S" layout="rl-tb" w="100pt">
	  <draw name="A" w="40pt" h="10pt"/><draw name="B" w="60pt" h="30pt"/>
	  <draw name="C" w="50pt" h="20pt"/></subform>`)
	want := []string{
		"draw f.S.A 60,0 40x10",
		"draw f.S.B 0,0 60x30",
		"draw f.S.C 50,30 50x20",
	}
	if got := laid(l); strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("got\n  %v\nwant\n  %v", got, want)
	}
}

func TestWhatAWrappingLayoutDoesWithEachKindOfChild(t *testing.T) {
	for _, tc := range []struct {
		what string
		body string
		want []string
	}{
		{"a hidden child takes no room and opens no line",
			// pdf.js returns EMPTY before addHTML, so the child after it
			// begins where it would have.
			`<subform name="S" layout="lr-tb" w="100pt">
			   <draw name="A" w="40pt" h="10pt" presence="hidden"/>
			   <draw name="B" w="40pt" h="10pt"/></subform>`,
			[]string{"draw f.S.A 0,0 40x10", "draw f.S.B 0,0 40x10"}},
		{"a child whose width is written as nought goes on the line whatever",
			// checkDimensions returns true outright for w === 0 or h === 0
			// (layout.js:270-273), before any comparison.
			`<subform name="S" layout="lr-tb" w="100pt">
			   <draw name="A" w="100pt" h="10pt"/>
			   <draw name="B" w="0pt" h="10pt"/></subform>`,
			[]string{"draw f.S.A 0,0 100x10", "draw f.S.B 100,0 0x10"}},
		{"a child whose height is written as nought goes on the line whatever",
			`<subform name="S" layout="lr-tb" w="100pt">
			   <draw name="A" w="100pt" h="10pt"/>
			   <draw name="B" w="40pt" h="0pt"/></subform>`,
			[]string{"draw f.S.A 0,0 100x10", "draw f.S.B 100,0 40x0"}},
		{"a child with no written width goes on while any of the line is left",
			// pdf.js asks space.width > ERROR for it (layout.js:303) rather
			// than measuring it, because it has nothing to measure.
			`<subform name="S" layout="lr-tb" w="100pt">
			   <draw name="A" w="99pt" h="10pt"/>
			   <draw name="B" h="10pt"><value><text>x</text></value></draw></subform>`,
			[]string{"draw f.S.A 0,0 99x10", "draw f.S.B 0,10 10.2x10"}},
		{"the first thing on a line goes down however wide it is",
			// It fits no line of this container, and pdf.js puts it down and
			// lets it overflow (layout.js:296-298) rather than moving it.
			`<subform name="S" layout="lr-tb" w="100pt">
			   <draw name="A" w="140pt" h="10pt"/><draw name="B" w="10pt" h="10pt"/></subform>`,
			[]string{"draw f.S.A 0,0 140x10", "draw f.S.B 0,10 10x10"}},
	} {
		if got := laid(wrapped(t, `w="100pt" h="500pt"`, tc.body)); strings.Join(got, "\n") != strings.Join(tc.want, "\n") {
			t.Errorf("%s:\n  got  %v\n  want %v", tc.what, got, tc.want)
		}
	}
}

func TestAChildIsMeasuredAgainstWhatIsLeftOfTheLineAndThenAgainstAWholeOne(t *testing.T) {
	// A draw with no written width is broken at the room it has, so the SAME
	// draw comes out two lines tall at the end of a line and one line tall at
	// the start of the next. pdf.js measures it twice for that reason: once in
	// getAvailableSpace's attempt-0 space and again in attempt 1's
	// (layout.js:176-179), and the second measurement is the one that is kept.
	l := wrapped(t, `w="100pt" h="500pt"`, `<subform name="S" layout="lr-tb" w="100pt">
	  <draw name="A" w="99pt" h="10pt"/>
	  <draw name="B"><value><text>aaaa bbbb cccc</text></value></draw></subform>`)
	got := laid(l)
	if len(got) != 2 || !strings.HasPrefix(got[1], "draw f.S.B 0,10 ") {
		t.Fatalf("B did not open a line of its own: %v", got)
	}
	// Two lines of the fallback font, which is what the text comes to across a
	// whole line. Measured against the one point left of the first line it
	// would have been one word per line and many times taller.
	if !strings.HasSuffix(got[1], "x22") {
		t.Errorf("B was kept at the width it was refused at: %v", got[1])
	}
}

func TestAChildRefusedInTheTailOfALineIsTriedOnTheNextOne(t *testing.T) {
	// The refusal comes from INSIDE the child: a draw of its own writes 40
	// points and there are 20 left, so the child's own packing cannot be done
	// at that width. pdf.js reads that as HTMLResult.FAILURE and moves the
	// child to the next line (xfa_object.js:394-403), where it is measured
	// against a whole one and succeeds.
	l := wrapped(t, `w="100pt" h="500pt"`, `<subform name="S" layout="lr-tb" w="100pt">
	  <draw name="A" w="80pt" h="10pt"/>
	  <subform name="T" layout="lr-tb">
	    <draw name="B" w="40pt" h="5pt"/><draw name="C" w="60pt" h="5pt"/></subform></subform>`)
	want := []string{"draw f.S.A 0,0 80x10", "draw f.S.T.B 0,10 40x5", "draw f.S.T.C 40,10 60x5"}
	if got := laid(l); strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("got\n  %v\nwant\n  %v", got, want)
	}
}

func TestAMarginOfAWrappingContainerMovesWhatIsInsideIt(t *testing.T) {
	// The container's own margin is outside what it holds, as it is for a
	// stack: the children begin inside the left and top insets and the line
	// they have is narrower by the left and right ones.
	l := wrapped(t, `w="100pt" h="500pt"`, `<subform name="S" layout="lr-tb" w="100pt">
	  <margin leftInset="10pt" topInset="4pt" rightInset="10pt" bottomInset="6pt"/>
	  <draw name="A" w="50pt" h="10pt"/><draw name="B" w="50pt" h="10pt"/>
	  <draw name="C" w="10pt" h="10pt"/></subform>
	  <draw name="D" w="1pt" h="1pt"/>`)
	want := []string{
		// The line is eighty wide, not a hundred: the left and right insets
		// come off it, so B does not fit beside A.
		"draw f.S.A 10,4 50x10",
		"draw f.S.B 10,14 50x10",
		"draw f.S.C 60,14 10x10",
		// 20 of lines, plus the four points above and the six below.
		"draw f.D 0,30 1x1",
	}
	if got := laid(l); strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("got\n  %v\nwant\n  %v", got, want)
	}
}

func TestWhatAWrappingContainerReportsRatherThanPlaces(t *testing.T) {
	for _, tc := range []struct {
		what string
		body string
		want []string
	}{
		{"a margin nobody can read",
			// It is inside a positioned container, so the wrapping container
			// is laid out in one piece and [placer.wrap] is what refuses it.
			`<subform name="Q"><subform name="S" layout="lr-tb" w="100pt">
			   <margin leftInset="a bit"/><draw name="A" w="10pt" h="10pt"/></subform></subform>`,
			[]string{"f.Q.S.A: the container that wraps it onto lines writes a margin that is not in lengths"}},
		{"something wider than a whole line of it",
			`<subform name="Q"><subform name="S" layout="lr-tb" w="10pt">
			   <draw name="A" w="10pt" h="1pt"/><draw name="B" w="20pt" h="1pt"/></subform></subform>`,
			[]string{
				"f.Q.S.A: a lr-tb layout wraps its children onto lines, and this one cannot be broken into them: " + widerThanALine,
				"f.Q.S.B: a lr-tb layout wraps its children onto lines, and this one cannot be broken into them: " + widerThanALine,
			}},
	} {
		if got := notLaid(wrapped(t, `w="100pt" h="500pt"`, tc.body)); strings.Join(got, "\n") != strings.Join(tc.want, "\n") {
			t.Errorf("%s:\n  got  %v\n  want %v", tc.what, got, tc.want)
		}
	}
}

func TestAWrappingContainerTurnsThePageBetweenTwoOfItsLines(t *testing.T) {
	// The content area holds 25 points. Two lines of 10 fit; the third does
	// not, so the page turns and the rest of the lines begin at the top of the
	// new one. That is a container SPLIT across a sheet, which pdf.js allows
	// for lr-tb because "lr-tb".includes("row") is false
	// (template.js:4952-4955).
	l := wrapped(t, `w="100pt" h="25pt"`, `<subform name="S" layout="lr-tb" w="100pt">
	  <draw name="A" w="60pt" h="10pt"/><draw name="B" w="60pt" h="10pt"/>
	  <draw name="C" w="60pt" h="10pt"/><draw name="D" w="60pt" h="10pt"/></subform>`)
	want := []string{
		"0: draw f.S.A 0,0 60x10",
		"0: draw f.S.B 0,10 60x10",
		"1: draw f.S.C 0,0 60x10",
		"1: draw f.S.D 0,10 60x10",
	}
	if got := byPage(l); strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("got\n  %v\nwant\n  %v", got, want)
	}
}

func TestTheFirstLineOfASheetGoesDownWhateverItsHeight(t *testing.T) {
	// It is [placer.whole]'s rule applied to a line: the first thing on a
	// sheet that cannot be broken up is not measured against anything
	// (layout.js:266-268), so a line taller than a whole content area comes
	// out one per sheet, overflowing, rather than being refused.
	l := wrapped(t, `w="100pt" h="15pt"`, `<subform name="S" layout="lr-tb" w="100pt">
	  <draw name="A" w="60pt" h="40pt"/><draw name="B" w="60pt" h="40pt"/></subform>`)
	want := []string{"0: draw f.S.A 0,0 60x40", "1: draw f.S.B 0,0 60x40"}
	if got := byPage(l); strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("got\n  %v\nwant\n  %v", got, want)
	}
}

func TestAWrappingContainerStopsWhereThePagesRunOut(t *testing.T) {
	// One sheet, and the page set gives no other. The first line goes down
	// free; the second has nowhere to go and says so, and so does everything
	// after it.
	l := laidOut(t, `<template><subform name="f" layout="tb">
	  <pageSet><pageArea name="P"><occur max="1"/><medium long="1000pt" short="1000pt"/>
	    <contentArea w="100pt" h="15pt"/></pageArea></pageSet>
	  <subform name="S" layout="lr-tb" w="100pt">
	    <draw name="A" w="60pt" h="40pt"/><draw name="B" w="60pt" h="40pt"/></subform></subform></template>`)
	if got, want := laid(l), []string{"draw f.S.A 0,0 60x40"}; strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("placed %v, want %v", got, want)
	}
	if got, want := notLaid(l), []string{"f.S.B: " + noNextPage}; strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("unplaced %v, want %v", got, want)
	}
}

func TestAWrappingContainerStopsWhereTheNextContentAreaIsANewWidth(t *testing.T) {
	// The lines were broken at 100 points and the second content area is 50
	// wide, so every line after the break would be broken somewhere this
	// packing did not compute. pdf.js re-enters the tree with the new space
	// and rebreaks; this stops and says so. No page set of the corpus changes
	// width between two of its content areas.
	l := laidOut(t, `<template><subform name="f" layout="tb">
	  <pageSet><pageArea name="P"><medium long="1000pt" short="1000pt"/>
	    <contentArea w="100pt" h="15pt"/><contentArea w="50pt" h="500pt"/></pageArea></pageSet>
	  <subform name="S" layout="lr-tb" w="100pt">
	    <draw name="A" w="60pt" h="10pt"/><draw name="B" w="60pt" h="10pt"/>
	    <draw name="C" w="60pt" h="10pt"/></subform></subform></template>`)
	if got, want := laid(l), []string{"draw f.S.A 0,0 60x10"}; strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("placed %v, want %v", got, want)
	}
	for _, u := range notLaid(l) {
		if !strings.HasSuffix(u, differentWidth) {
			t.Errorf("%s does not say the width changed", u)
		}
	}
}

func TestABreakInsideAWrappingContainerIsRead(t *testing.T) {
	// A child of a line carries breakBefore and breakAfter like any other, and
	// they are read where the flow reaches it rather than where the packing
	// put it.
	l := wrapped(t, `w="100pt" h="500pt"`, `<subform name="S" layout="lr-tb" w="100pt">
	  <draw name="A" w="10pt" h="10pt"/>
	  <subform name="T"><breakBefore targetType="pageArea" startNew="1"/>
	    <draw name="B" w="10pt" h="10pt"/></subform>
	  <draw name="C" w="10pt" h="10pt"/></subform>`)
	// The break turns the page and the flow begins again at the top of the new
	// content area. Where ACROSS the page it begins is what the packing said,
	// because the packing was done once and pdf.js would redo it: T is still
	// the second thing on its line.
	want := []string{"0: draw f.S.A 0,0 10x10", "1: draw f.S.T.B 10,0 10x10", "1: draw f.S.C 20,0 10x10"}
	if got := byPage(l); strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("got\n  %v\nwant\n  %v", got, want)
	}
}

func TestABreakAfterInsideAWrappingContainerStopsWhereThePagesDo(t *testing.T) {
	l := laidOut(t, `<template><subform name="f" layout="tb">
	  <pageSet><pageArea name="P"><occur max="1"/><medium long="1000pt" short="1000pt"/>
	    <contentArea w="100pt" h="500pt"/></pageArea></pageSet>
	  <subform name="S" layout="lr-tb" w="100pt">
	    <subform name="T"><breakAfter targetType="pageArea" startNew="1"/>
	      <draw name="A" w="10pt" h="10pt"/></subform>
	    <draw name="B" w="10pt" h="10pt"/></subform></subform></template>`)
	if got, want := notLaid(l), []string{"f.S.B: " + noNextPage}; strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("unplaced %v, want %v", got, want)
	}
}

func TestABreakBeforeOnTheFirstChildOfAWrappingContainerStopsWhereThePagesDo(t *testing.T) {
	l := laidOut(t, `<template><subform name="f" layout="tb">
	  <pageSet><pageArea name="P"><occur max="1"/><medium long="1000pt" short="1000pt"/>
	    <contentArea w="100pt" h="500pt"/></pageArea></pageSet>
	  <subform name="S" layout="lr-tb" w="100pt">
	    <subform name="T"><breakBefore targetType="pageArea" startNew="1"/>
	      <draw name="A" w="10pt" h="10pt"/></subform></subform></subform></template>`)
	if got, want := notLaid(l), []string{"f.S.T.A: " + noNextPage}; strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("unplaced %v, want %v", got, want)
	}
}

func TestAContainerOfALineIsOpenedOnlyWhenItIsAloneOnIt(t *testing.T) {
	// T is splittable and alone on its line, so it is opened as a container of
	// the flowing chain and its own children are stacked across the page
	// boundary. U shares a line with V, so it moves in one piece: splitting it
	// would leave the rest of that line to be placed on a sheet the first half
	// of it is not on.
	l := wrapped(t, `w="100pt" h="25pt"`, `<subform name="S" layout="lr-tb" w="100pt">
	  <subform name="T" layout="tb" w="100pt">
	    <draw name="A" w="10pt" h="10pt"/><draw name="B" w="10pt" h="10pt"/>
	    <draw name="C" w="10pt" h="10pt"/></subform></subform>`)
	want := []string{
		"0: draw f.S.T.A 0,0 10x10",
		"0: draw f.S.T.B 0,10 10x10",
		"1: draw f.S.T.C 0,0 10x10",
	}
	if got := byPage(l); strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("split across a sheet:\n  got  %v\n  want %v", got, want)
	}
	l = wrapped(t, `w="100pt" h="25pt"`, `<subform name="S" layout="lr-tb" w="100pt">
	  <subform name="U" layout="tb" w="50pt">
	    <draw name="A" w="10pt" h="10pt"/><draw name="B" w="10pt" h="10pt"/>
	    <draw name="C" w="10pt" h="10pt"/></subform>
	  <subform name="V" layout="tb" w="50pt"><draw name="D" w="10pt" h="10pt"/></subform></subform>`)
	want = []string{
		"0: draw f.S.U.A 0,0 10x10",
		"0: draw f.S.U.B 0,10 10x10",
		"0: draw f.S.U.C 0,20 10x10",
		"0: draw f.S.V.D 50,0 10x10",
	}
	if got := byPage(l); strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("moved whole:\n  got  %v\n  want %v", got, want)
	}
}

func TestSplittabilityIsRefusedToAChildThatIsNotFirstOnItsLine(t *testing.T) {
	// The fourth clause of $isSplittable (template.js:4962-4970), which
	// arrives with this slice because it reads numberInLine. T is first on its
	// line and splittable; U is second on the same line and is not, whatever
	// else it writes. Asked of the same subform in the two places so that the
	// clause is what differs and nothing else.
	src := func(before string) string {
		return `<template><subform name="f" layout="tb">
		  <pageSet><pageArea name="P"><medium long="1000pt" short="1000pt"/>
		    <contentArea w="100pt" h="500pt"/></pageArea></pageSet>
		  <subform name="S" layout="lr-tb" w="100pt">` + before +
			`<subform name="T" layout="tb" w="50pt"><draw name="A" w="1pt" h="1pt"/></subform>
		  </subform></subform></template>`
	}
	var got [2]bool
	for i, before := range []string{"", `<draw name="Z" w="10pt" h="1pt"/>`} {
		fm := Expand(parse(t, src(before)), nil)
		p := newPlacer()
		p.mapUp(fm.Root)
		root := firstOfKind(fm.Root, "subform")
		wrap := firstOfKind(root, "subform")
		// Open the chain by hand down to the wrapping container, which is
		// where numberInLine lives.
		p.chain = []*level{{node: root}, {node: wrap, line: i}}
		got[i] = p.splittable(firstOfKind(wrap, "subform"))
	}
	if got != [2]bool{true, false} {
		t.Errorf("splittable first on the line and after one: %v, want [true false]", got)
	}
}

func TestHowWideANodeComesOut(t *testing.T) {
	// widthOf is addHTML's width column for each layout, read off the boxes a
	// wrapping container puts on one line: each child's own x is the sum of
	// the widths of the ones before it.
	for _, tc := range []struct {
		what string
		body string
		want string
	}{
		{"a stack is as wide as its widest child",
			// MathClamp(w, extra.width, availableSpace.width), layout.js:154.
			`<subform name="T" layout="tb"><draw name="A" w="30pt" h="1pt"/>
			   <draw name="B" w="10pt" h="1pt"/></subform>`, "30"},
		{"and never wider than the room it has",
			`<subform name="T" layout="tb" w="20pt"><draw name="A" w="30pt" h="1pt"/></subform>`, "20"},
		{"a table is as wide as its widest row",
			`<subform name="T" layout="table" columnWidths="10pt 15pt">
			   <subform name="R" layout="row"><draw name="A" w="10pt" h="1pt"/>
			     <draw name="B" w="15pt" h="1pt"/></subform></subform>`, "25"},
		{"a row is as wide as its cells laid end to end",
			// extra.width += w (layout.js:131-142), and a cell is as wide as
			// the columns it spans whatever it writes.
			`<subform name="T" layout="table" columnWidths="10pt 15pt">
			   <subform name="R" layout="row"><draw name="A" w="99pt" h="1pt"/>
			     <draw name="B" w="99pt" h="1pt"/></subform></subform>`, "25"},
		{"a container that is a cell of a row is as wide as its columns",
			// fixDimensions replaces its own width with them before it is laid
			// out (html_utils.js:328-345), whatever it writes.
			`<subform name="T" layout="table" columnWidths="10pt">
			   <subform name="R" layout="row"><subform name="U" layout="tb" w="99pt">
			     <draw name="A" w="1pt" h="1pt"/></subform></subform></subform>`, "10"},
		{"a hidden cell of a row takes no column and no width",
			`<subform name="T" layout="table" columnWidths="10pt 15pt">
			   <subform name="R" layout="row"><draw name="A" w="9pt" h="1pt" presence="hidden"/>
			     <draw name="B" w="9pt" h="1pt"/></subform></subform>`, "10"},
		{"a wrapping container is as wide as its widest line",
			// extra.width = max(extra.width, currentWidth), layout.js:128, and
			// it can be wider than the container writes: A is first on its
			// line and goes down overflowing.
			`<subform name="T" layout="lr-tb" w="10pt"><draw name="A" w="30pt" h="1pt"/>
			   <draw name="B" w="5pt" h="1pt"/></subform>`, "30"},
		{"a positioned container reaches as far right as its furthest child",
			// extra.width = max(extra.width, x + w), layout.js:99-105.
			`<subform name="T"><draw name="A" x="10pt" w="20pt" h="1pt"/>
			   <draw name="B" x="1pt" w="2pt" h="1pt"/></subform>`, "30"},
		{"a hidden child of a positioned container reaches nowhere",
			`<subform name="T"><draw name="A" x="10pt" w="20pt" h="1pt" presence="hidden"/>
			   <draw name="B" x="1pt" w="2pt" h="1pt"/></subform>`, "3"},
		{"a container's own margin is outside what it holds",
			`<subform name="T" layout="tb"><margin leftInset="4pt" rightInset="6pt"/>
			   <draw name="A" w="20pt" h="1pt"/></subform>`, "30"},
		{"and its own written width is a floor under it",
			`<subform name="T" layout="tb" w="50pt"><draw name="A" w="20pt" h="1pt"/></subform>`, "50"},
		{"a leaf with no written width is measured from its text",
			`<subform name="T" layout="tb"><draw name="A" h="10pt"><value><text>ab</text>
			   </value></draw></subform>`, "20.4"},
		{"a leaf with neither takes its smallest",
			`<subform name="T" layout="tb"><draw name="A" minW="7pt" h="1pt"/></subform>`, "7"},
		{"a hidden container takes none",
			`<subform name="T" layout="tb" presence="hidden"><draw name="A" w="20pt" h="1pt"/></subform>`, "0"},
	} {
		// The marker sits after the measured container on the same line, so
		// where it begins is how wide that container came out.
		l := wrapped(t, `w="500pt" h="500pt"`, `<subform name="S" layout="lr-tb" w="500pt">`+
			tc.body+`<draw name="M" w="1pt" h="1pt"/></subform>`)
		var at string
		for _, b := range l.Pages[0].Boxes {
			if b.Path == "f.S.M" {
				at = fmt.Sprintf("%g", b.X.Points())
			}
		}
		if at != tc.want {
			t.Errorf("%s: the next child begins at %s, want %s (unplaced %v)", tc.what, at, tc.want, notLaid(l))
		}
	}
}

func TestWhatAWidthIsRefusedFor(t *testing.T) {
	for _, tc := range []struct {
		what string
		body string
		want string
	}{
		{"a leaf whose width is not a length",
			`<draw name="A" w="wide" h="1pt"/>`,
			`its width is written as w="wide", which is not a length, and its text has to be broken at a width`},
		{"a leaf nobody can measure",
			`<draw name="A" h="1pt"><value><exData contentType="text/plain">x</exData></value>
			   <para spaceAbove="a bit"/></draw>`,
			`its paragraph is written as spaceAbove="a bit", which is not a length`},
		{"a leaf whose largest width is bound by the room it has",
			`<draw name="A" maxW="9pt" h="1pt"/>`, boundByTheRoom("maxW")},
		{"a container whose width is not a length",
			`<subform name="T" layout="tb" w="wide"><draw name="A" w="1pt" h="1pt"/></subform>`,
			`its width is written as w="wide", which is not a length, and its text has to be broken at a width`},
		{"a container whose margin is not in lengths",
			`<subform name="T" layout="tb"><margin leftInset="a bit"/>
			   <draw name="A" w="1pt" h="1pt"/></subform>`,
			"its margin is not written in lengths"},
		{"a positioned container holding something nobody can measure across",
			`<subform name="T"><draw name="A" w="wide" h="1pt"/></subform>`,
			`its width is written as w="wide", which is not a length, and its text has to be broken at a width`},
		{"a child of a line whose height is not a length",
			// Its width measures and its height does not, and a child of a
			// line needs both before it can be put on one.
			`<draw name="A" w="5pt" h="tall"/>`,
			`its height is written as h="tall", which is not a length`},
		{"a container holding an origin that is not a place",
			`<subform name="T"><draw name="A" x="over there" w="1pt" h="1pt"/></subform>`,
			`it holds something whose origin is written as x="over there", which is not a place`},
		{"a stack with no room to hold its width down to",
			// A cell of a row whose container writes no columnWidths has no
			// width at all, so there is nothing to clamp against.
			`<subform name="T" layout="table"><subform name="R" layout="row">
			   <subform name="U" layout="tb"><draw name="A" w="1pt" h="1pt"/></subform></subform></subform>`,
			noSpaceToHoldItTo},
	} {
		l := wrapped(t, `w="500pt" h="500pt"`, `<subform name="S" layout="lr-tb" w="500pt">`+
			tc.body+`<draw name="M" w="1pt" h="1pt"/></subform>`)
		got := notLaid(l)
		if len(got) == 0 || !strings.HasSuffix(got[0], tc.want) {
			t.Errorf("%s:\n  got  %v\n  want a reason ending %q", tc.what, got, tc.want)
		}
	}
}

func TestANodeCannotBeAsWideAsItself(t *testing.T) {
	// The same guard the height has: a node being measured is marked before
	// its children are, so a change that made the expanded form a graph would
	// stop rather than recurse for ever.
	p := newPlacer()
	fm := Expand(parse(t, `<template><subform name="f">
	  <pageSet><pageArea name="P"><contentArea/></pageArea></pageSet>
	  <subform name="S" layout="tb"><draw name="A" w="1pt" h="1pt"/></subform></subform></template>`), nil)
	root := firstOfKind(fm.Root, "subform")
	n := firstOfKind(root, "subform")
	p.widths[heightKey{n: n, wide: 100}] = width{why: measuringItself}
	if _, why := p.widthOf(n, 100, 0); why != measuringItself {
		t.Errorf("width of a node being measured: %q", why)
	}
}

func TestAWrappingContainerLaidOutInOnePieceIsHeldToTheHeightItWrites(t *testing.T) {
	// Inside a positioned container, so it is [placer.wrap] rather than
	// [placer.flowLines] that lays it out: availableSpace.height =
	// min(this.h || Infinity, availableSpace.height) (template.js:5062-5065),
	// which is the room the children are told they have.
	l := wrapped(t, `w="100pt" h="500pt"`, `<subform name="Q">
	  <subform name="S" layout="lr-tb" w="100pt" h="5pt">
	    <draw name="A" w="60pt" h="10pt"/><draw name="B" w="60pt" h="10pt"/></subform></subform>`)
	want := []string{"draw f.Q.S.A 0,0 60x10", "draw f.Q.S.B 0,10 60x10"}
	if got := laid(l); strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("got\n  %v\nwant\n  %v", got, want)
	}
}

func TestWhatStopsInsideALineStopsEverythingAfterIt(t *testing.T) {
	// T is alone on its line and splittable, so it is opened as a container of
	// the flowing chain. It writes a height of five points and holds twenty,
	// and there is no second sheet to carry the rest onto — so the flow stops
	// inside it, and everything after it on the lines above stops too.
	l := laidOut(t, `<template><subform name="f" layout="tb">
	  <pageSet><pageArea name="P"><occur max="1"/><medium long="1000pt" short="1000pt"/>
	    <contentArea w="100pt" h="500pt"/></pageArea></pageSet>
	  <subform name="S" layout="lr-tb" w="100pt">
	    <draw name="A" w="100pt" h="10pt"/>
	    <subform name="T" layout="tb" w="100pt" h="5pt">
	      <draw name="B" w="10pt" h="10pt"/><draw name="D" w="10pt" h="10pt"/></subform>
	    <draw name="C" w="10pt" h="10pt"/></subform></subform></template>`)
	if got, want := laid(l), []string{"draw f.S.A 0,0 100x10"}; strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("placed %v, want %v", got, want)
	}
	want := []string{"f.S.T.B: " + noNextPage, "f.S.T.D: " + noNextPage, "f.S.C: " + noNextPage}
	if got := notLaid(l); strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("unplaced\n  %v\nwant\n  %v", got, want)
	}
}

func TestABreakInsideALineBeginsAgainAtTheTopOfTheNewContentArea(t *testing.T) {
	// Two content areas of one sheet, the second lower down the page. The
	// break moves to it and the flow begins at ITS top rather than carrying
	// the first one's offsets.
	l := laidOut(t, `<template><subform name="f" layout="tb">
	  <pageSet><pageArea name="P"><medium long="1000pt" short="1000pt"/>
	    <contentArea w="100pt" h="500pt"/>
	    <contentArea y="200pt" w="100pt" h="500pt"/></pageArea></pageSet>
	  <subform name="S" layout="lr-tb" w="100pt">
	    <draw name="A" w="10pt" h="10pt"/><draw name="B" w="100pt" h="10pt"/>
	    <subform name="T"><breakBefore targetType="contentArea" startNew="1"/>
	      <draw name="C" w="10pt" h="10pt"/></subform></subform></subform></template>`)
	want := []string{
		"0: draw f.S.A 0,0 10x10",
		"0: draw f.S.B 0,10 100x10",
		// The third line of the packing begins at 20 down the container; the
		// break puts it at the top of a content area 200 down the page.
		"0: draw f.S.T.C 0,200 10x10",
	}
	if got := byPage(l); strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("got\n  %v\nwant\n  %v", got, want)
	}
}
