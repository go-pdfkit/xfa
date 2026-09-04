// Copyright (c) 2026, the go-pdfkit/xfa authors
// All rights reserved.
//
// SPDX-License-Identifier: BSD-3-Clause

package xfa

import (
	"strings"
	"testing"
)

// sheets wraps a body in a form whose paper is written out in full, so that a
// test can say what the page set is as well as what goes on it.
func sheets(set, body string) string {
	return `<template><subform name="f" layout="tb"><pageSet ` + set + `</pageSet>` + body + `</subform></template>`
}

// sheet is one page area of a given height, with one content area filling it.
func sheet(name, attr, h, extra string) string {
	return `<pageArea name="` + name + `" ` + attr + `><medium long="1000pt" short="1000pt"/>` +
		extra + `<contentArea w="500pt" h="` + h + `pt"/></pageArea>`
}

// bricks are n draws a point wide and h tall, named A0, A1 and so on: a column
// whose arithmetic can be worked out by hand.
func bricks(n int, h string) string {
	var b strings.Builder
	for i := range n {
		b.WriteString(`<draw name="A` + string(rune('0'+i)) + `" w="1pt" h="` + h + `pt"/>`)
	}
	return b.String()
}

func TestASheetIsTurnedWhenTheStackReachesItsBottom(t *testing.T) {
	// Four bricks ten points tall, on sheets with room for two and a half.
	l := laidOut(t, sheets(`>`+sheet("P", "", "25", ""), bricks(4, "10")))
	same(t, "the sheets", byPage(l), []string{
		"0: draw f.A0 0,0 1x10", "0: draw f.A1 0,10 1x10",
		"1: draw f.A2 0,0 1x10", "1: draw f.A3 0,10 1x10"})
}

func TestEveryOpenContainerBeginsAgainAtTheTopOfTheNextSheet(t *testing.T) {
	// The inner subform is stacked inside the outer one, and both carry a
	// margin. B does not fit below A, so the sheet turns — and BOTH margins
	// apply again from the top of the new content area: 3+5 down and 2+4 across.
	l := laidOut(t, sheets(`>`+sheet("P", "", "30", ""), `
	  <subform name="Out" layout="tb">
	    <margin leftInset="2pt" topInset="3pt"/>
	    <subform name="In" layout="tb">
	      <margin leftInset="4pt" topInset="5pt"/>
	      <draw name="A" w="1pt" h="20pt"/>
	      <draw name="B" w="1pt" h="20pt"/>
	    </subform>
	  </subform>`))
	same(t, "the sheets", byPage(l), []string{
		"0: draw f.Out.In.A 6,8 1x20",
		"1: draw f.Out.In.B 6,8 1x20"})
}

func TestAContainerKeptIntactMovesInOnePiece(t *testing.T) {
	// The room is 30 points and the group is 25, so the group would fit if it
	// could begin below A. It is kept intact, so it goes whole onto the next
	// sheet rather than leaving its first child behind.
	group := `<subform name="G" layout="tb"><keep intact="contentArea"/>
	    <draw name="B" w="1pt" h="15pt"/><draw name="C" w="1pt" h="10pt"/></subform>`
	l := laidOut(t, sheets(`>`+sheet("P", "", "30", ""), `<draw name="A" w="1pt" h="20pt"/>`+group))
	same(t, "the sheets", byPage(l), []string{
		"0: draw f.A 0,0 1x20",
		"1: draw f.G.B 0,0 1x15", "1: draw f.G.C 0,15 1x10"})

	// The same group with keep intact="none" is split instead: B stays where
	// it fits and C goes on.
	loose := strings.Replace(group, `intact="contentArea"`, `intact="none"`, 1)
	l = laidOut(t, sheets(`>`+sheet("P", "", "30", ""), `<draw name="A" w="1pt" h="10pt"/>`+loose))
	same(t, "the sheets", byPage(l), []string{
		"0: draw f.A 0,0 1x10", "0: draw f.G.B 0,10 1x15",
		"1: draw f.G.C 0,0 1x10"})
}

func TestAPositionedContainerMovesInOnePieceToo(t *testing.T) {
	// A positioned container cannot be split: where its children go is their
	// own x and y within it, and half of it on one sheet would put them in two
	// different frames.
	l := laidOut(t, sheets(`>`+sheet("P", "", "30", ""), `
	  <draw name="A" w="1pt" h="20pt"/>
	  <subform name="G" h="20pt"><draw name="B" x="2pt" y="3pt" w="1pt" h="4pt"/></subform>`))
	same(t, "the sheets", byPage(l), []string{
		"0: draw f.A 0,0 1x20", "1: draw f.G.B 2,3 1x4"})
}

func TestTheFirstThingOnASheetGoesDownWhateverItsHeight(t *testing.T) {
	// The brick is taller than a whole content area, so no sheet will ever
	// hold it. pdf.js does not refuse it: checkDimensions returns true while
	// the sheet has had nothing that moves in one piece (layout.js:266-268),
	// so it goes down and hangs over the bottom. What follows it does not fit
	// beside it and turns the page, where it is first in its turn.
	l := laidOut(t, sheets(`>`+sheet("P", "", "20", ""), `
	  <draw name="A" w="1pt" h="50pt"/><draw name="B" w="1pt" h="5pt"/>`))
	same(t, "the sheets", byPage(l), []string{
		"0: draw f.A 0,0 1x50", "1: draw f.B 0,0 1x5"})
	same(t, "what was left off", notLaid(l), nil)
}

func TestWhatFitsNoRoomInsideAContainerThatMovesWholeIsReported(t *testing.T) {
	// The group is kept intact and writes ten points of its own height, so its
	// second child has nowhere to go: carrying it onto another sheet would
	// leave the rest of the group behind, which is the one thing keeping a
	// container intact forbids.
	//
	// Z is there so that the group is not the FIRST thing on the sheet that
	// moves in one piece. That one is laid out with noLayoutFailure on for its
	// whole subtree (template.js:5075-5178) and nothing inside it is checked
	// at all, which the test below this one is about.
	l := laidOut(t, sheets(`>`+sheet("P", "", "100", ""), `
	  <draw name="Z" w="1pt" h="5pt"/>
	  <subform name="G" layout="tb" h="10pt"><keep intact="contentArea"/>
	    <draw name="A" w="1pt" h="9pt"/><draw name="B" w="1pt" h="9pt"/></subform>`))
	same(t, "the sheets", byPage(l), []string{
		"0: draw f.Z 0,0 1x5", "0: draw f.G.A 0,5 1x9"})
	same(t, "what was left off", notLaid(l), []string{"f.G.B: " + noRoomInside})
}

func TestNothingInsideTheFirstContainerOfASheetIsCheckedAtAll(t *testing.T) {
	// The same group, now first on the sheet. pdf.js switches noLayoutFailure
	// on before the first unsplittable container's own check and off again
	// only on the way out of it (template.js:313-326, 5075-5178), and every
	// branch of checkDimensions returns true while it is on — so B goes down
	// inside a group ten points tall that already holds nine.
	l := laidOut(t, sheets(`>`+sheet("P", "", "100", ""), `
	  <subform name="G" layout="tb" h="10pt"><keep intact="contentArea"/>
	    <draw name="A" w="1pt" h="9pt"/><draw name="B" w="1pt" h="9pt"/></subform>`))
	same(t, "the sheets", byPage(l), []string{
		"0: draw f.G.A 0,0 1x9", "0: draw f.G.B 0,9 1x9"})
	same(t, "what was left off", notLaid(l), nil)
}

func TestASheetTheBodyNeverReachedIsNotShipped(t *testing.T) {
	// A break after the last child asks for a sheet nothing then goes on.
	l := laidOut(t, sheets(`>`+sheet("P", "", "100", ""), `
	  <subform name="S" layout="tb"><breakAfter targetType="pageArea" startNew="1"/>
	    <draw name="A" w="1pt" h="5pt"/></subform>`))
	if len(l.Pages) != 1 {
		t.Errorf("%d sheets, want 1", len(l.Pages))
	}
}

func TestABreakBeforeStartsAFreshSheet(t *testing.T) {
	l := laidOut(t, sheets(`>`+sheet("P", "", "100", ""), `
	  <draw name="A" w="1pt" h="5pt"/>
	  <subform name="S" layout="tb"><breakBefore targetType="pageArea" startNew="1"/>
	    <draw name="B" w="1pt" h="5pt"/></subform>`))
	same(t, "the sheets", byPage(l), []string{"0: draw f.A 0,0 1x5", "1: draw f.S.B 0,0 1x5"})
}

func TestABreakWithNothingToDoDoesNothing(t *testing.T) {
	for _, tc := range []struct {
		what, br string
	}{
		{"targetType auto", `<breakBefore startNew="1"/>`},
		{"a target nothing answers", `<breakBefore targetType="pageArea" target="Nowhere"/>`},
		{"pageArea without startNew, naming the page in hand", `<breakBefore targetType="pageArea" target="P"/>`},
		{"a deprecated break before pageEven", `<break before="pageEven" startNew="1"/>`},
		{"a content area that is the one in hand", `<breakBefore targetType="contentArea" target="P.C"/>`},
	} {
		l := laidOut(t, sheets(`>`+sheet("P", "", "100", ""), `
		  <draw name="A" w="1pt" h="5pt"/>
		  <subform name="S" layout="tb">`+tc.br+`<draw name="B" w="1pt" h="5pt"/></subform>`))
		if len(l.Pages) != 1 {
			t.Errorf("%s: %d sheets, want 1", tc.what, len(l.Pages))
		}
	}
}

func TestADeprecatedBreakIsReadAsABreakBefore(t *testing.T) {
	l := laidOut(t, sheets(`>`+sheet("P", "", "100", ""), `
	  <draw name="A" w="1pt" h="5pt"/>
	  <subform name="S" layout="tb"><break before="pageArea" startNew="1"/>
	    <draw name="B" w="1pt" h="5pt"/></subform>`))
	same(t, "the sheets", byPage(l), []string{"0: draw f.A 0,0 1x5", "1: draw f.S.B 0,0 1x5"})
}

func TestABreakBeforeTheFormChoosesTheFirstSheetRatherThanTurningIt(t *testing.T) {
	// pdf.js consumes this one to pick the page the form starts on, so it must
	// not also fire as a break: the form begins on P2 and stays there.
	l := laidOut(t, `<template><subform name="f" layout="tb">
	  <breakBefore targetType="pageArea" target="P2" startNew="1"/>
	  <pageSet>`+sheet("P1", "", "100", `<draw name="One" w="1pt" h="1pt"/>`)+
		sheet("P2", "", "100", `<draw name="Two" w="1pt" h="1pt"/>`)+`</pageSet>
	  <draw name="A" w="1pt" h="5pt"/></subform></template>`)
	same(t, "the sheets", byPage(l), []string{
		"0: draw f.P2.Two 0,0 1x1", "0: draw f.A 0,0 1x5"})
	same(t, "what was left off", notLaid(l), []string{
		"f.P1.One: its page area is never used: no page of this form is one"})
}

func TestABreakToAnotherPageAreaGoesThere(t *testing.T) {
	// The break is on the SECOND subform. On the first it would be the one
	// pdf.js consumes to choose the sheet the form starts on rather than a
	// break at all — which [TestABreakBeforeTheFormChoosesTheFirstSheetRatherThanTurningIt]
	// is about.
	l := laidOut(t, sheets(`>`+sheet("P1", "", "100", "")+sheet("P2", `id="p2id"`, "100", `<draw name="Two" w="1pt" h="1pt"/>`), `
	  <subform name="First" layout="tb"><draw name="A" w="1pt" h="5pt"/></subform>
	  <subform name="S" layout="tb"><breakBefore targetType="pageArea" target="#p2id"/>
	    <draw name="B" w="1pt" h="5pt"/></subform>`))
	same(t, "the sheets", byPage(l), []string{
		"0: draw f.First.A 0,0 1x5", "1: draw f.P2.Two 0,0 1x1", "1: draw f.S.B 0,0 1x5"})
}

func TestTheFurnitureIsDrawnOnEverySheetItsPageAreaMakes(t *testing.T) {
	l := laidOut(t, sheets(`>`+sheet("P", "", "20", `<draw name="Rule" w="1pt" h="1pt"/>`), bricks(3, "15")))
	same(t, "the sheets", byPage(l), []string{
		"0: draw f.P.Rule 0,0 1x1", "0: draw f.A0 0,0 1x15",
		"1: draw f.P.Rule 0,0 1x1", "1: draw f.A1 0,0 1x15",
		"2: draw f.P.Rule 0,0 1x1", "2: draw f.A2 0,0 1x15"})
}

// twoAreas is a page area of two content areas, one above the other, so that a
// sheet can be filled twice before it is turned.
func twoAreas(name, occur string) string {
	return `<pageArea name="` + name + `">` + occur + `<medium long="1000pt" short="1000pt"/>` +
		`<contentArea name="Top" x="0pt" y="0pt" w="500pt" h="20pt"/>` +
		`<contentArea name="Low" x="100pt" y="200pt" w="500pt" h="20pt"/></pageArea>`
}

func TestASheetIsFilledContentAreaByContentAreaBeforeItIsTurned(t *testing.T) {
	l := laidOut(t, sheets(`>`+twoAreas("P", ""), bricks(5, "15")))
	same(t, "the sheets", byPage(l), []string{
		"0: draw f.A0 0,0 1x15",
		"0: draw f.A1 100,200 1x15",
		"1: draw f.A2 0,0 1x15",
		"1: draw f.A3 100,200 1x15",
		"2: draw f.A4 0,0 1x15"})
	if n := len(l.Pages[0].Areas); n != 2 {
		t.Errorf("the sheet offers %d content areas, want 2", n)
	}
}

func TestABreakToAContentAreaStaysOnTheSheet(t *testing.T) {
	l := laidOut(t, sheets(`>`+twoAreas("P", ""), `
	  <subform name="F" layout="tb"><draw name="A" w="1pt" h="5pt"/></subform>
	  <subform name="S" layout="tb"><breakBefore targetType="contentArea" startNew="1"/>
	    <draw name="B" w="1pt" h="5pt"/></subform>`))
	same(t, "the sheets", byPage(l), []string{
		"0: draw f.F.A 0,0 1x5", "0: draw f.S.B 100,200 1x5"})
}

func TestAStackWithNoSheetLeftReportsWhatIsLeftOver(t *testing.T) {
	// The one page area may make one sheet, and the third brick does not fit
	// on it.
	l := laidOut(t, sheets(`><occur max="1"/><pageArea name="P"><occur max="1"/>`+
		`<medium long="1000pt" short="1000pt"/><contentArea w="500pt" h="25pt"/></pageArea>`, bricks(3, "10")))
	same(t, "the sheets", byPage(l), []string{"0: draw f.A0 0,0 1x10", "0: draw f.A1 0,10 1x10"})
	same(t, "what was left off", notLaid(l), []string{"f.A2: " + noNextPage})
}

func TestABreakWithNowhereToGoStopsRatherThanPretending(t *testing.T) {
	// The page area may make one sheet and the break asks for another.
	l := laidOut(t, sheets(`><occur max="1"/><pageArea name="P"><occur max="1"/>`+
		`<medium long="1000pt" short="1000pt"/><contentArea w="500pt" h="100pt"/></pageArea>`, `
	  <subform name="F" layout="tb"><draw name="A" w="1pt" h="5pt"/></subform>
	  <subform name="S" layout="tb"><breakBefore targetType="pageArea" startNew="1"/>
	    <draw name="B" w="1pt" h="5pt"/></subform>
	  <draw name="C" w="1pt" h="5pt"/>`))
	same(t, "the sheets", byPage(l), []string{"0: draw f.F.A 0,0 1x5"})
	same(t, "what was left off", notLaid(l), []string{"f.S.B: " + noNextPage, "f.C: " + noNextPage})

	// The same for a break AFTER a child, and for one on the outermost subform
	// itself, where there is nothing to lay out at all.
	l = laidOut(t, sheets(`><occur max="1"/><pageArea name="P"><occur max="1"/>`+
		`<medium long="1000pt" short="1000pt"/><contentArea w="500pt" h="100pt"/></pageArea>`, `
	  <subform name="S" layout="tb"><breakAfter targetType="pageArea" startNew="1"/>
	    <draw name="A" w="1pt" h="5pt"/></subform>
	  <draw name="B" w="1pt" h="5pt"/>`))
	same(t, "what was left off after a breakAfter", notLaid(l), []string{"f.B: " + noNextPage})

	// And for the deprecated <break after=...>, which is read as a breakAfter.
	l = laidOut(t, sheets(`><occur max="1"/><pageArea name="P"><occur max="1"/>`+
		`<medium long="1000pt" short="1000pt"/><contentArea w="500pt" h="100pt"/></pageArea>`, `
	  <subform name="S" layout="tb"><break after="pageArea" startNew="1"/>
	    <draw name="A" w="1pt" h="5pt"/></subform>
	  <draw name="B" w="1pt" h="5pt"/>`))
	same(t, "a deprecated break after", notLaid(l), []string{"f.B: " + noNextPage})
}

func TestADeprecatedBreakCanChooseTheSheetTheFormStartsOn(t *testing.T) {
	l := laidOut(t, `<template><subform name="f" layout="tb">
	  <break before="pageArea" beforeTarget="P2"/>
	  <pageSet>`+sheet("P1", "", "100", `<draw name="One" w="1pt" h="1pt"/>`)+
		sheet("P2", "", "100", `<draw name="Two" w="1pt" h="1pt"/>`)+`</pageSet>
	  <draw name="A" w="1pt" h="5pt"/></subform></template>`)
	same(t, "the sheets", byPage(l), []string{
		"0: draw f.P2.Two 0,0 1x1", "0: draw f.A 0,0 1x5"})
}

func TestASecondPageSetIsStillSomewhereABreakCanReach(t *testing.T) {
	// The break names, by id, a page area of a SECOND page set — one the form
	// does not start from, because pdf.js reads pageAreas from the first
	// (template.js:5443). It is still a page area with a page set around it,
	// so the form can start there and go on from there.
	l := laidOut(t, `<template><subform name="f" layout="tb">
	  <breakBefore targetType="pageArea" target="#two"/>
	  <pageSet>`+sheet("P1", "", "100", "")+`</pageSet>
	  <pageSet>`+sheet("P2", `id="two"`, "20", "")+`</pageSet>
	  `+bricks(3, "15")+`</subform></template>`)
	same(t, "the sheets", byPage(l), []string{
		"0: draw f.A0 0,0 1x15", "1: draw f.A1 0,0 1x15", "2: draw f.A2 0,0 1x15"})
}

func TestAPageAreaWithNoPageSetAroundItIsNowhereToGoOn(t *testing.T) {
	// A page area written outside every page set. A break can name it by id
	// and the form can be put on it — pdf.js would too — but nothing holds a
	// sequence it belongs to, so there is no sheet after it.
	l := laidOut(t, `<template><subform name="f" layout="tb">
	  <pageSet>`+sheet("P1", "", "20", "")+`</pageSet>
	  `+sheet("Loose", `id="two"`, "20", "")+`
	  <subform name="F" layout="tb"><draw name="A" w="1pt" h="5pt"/></subform>
	  <subform name="S" layout="tb"><breakBefore targetType="pageArea" target="#two"/>
	    `+bricks(3, "15")+`</subform></subform></template>`)
	same(t, "the sheets", byPage(l), []string{
		"0: draw f.F.A 0,0 1x5", "1: draw f.S.A0 0,0 1x15"})
	same(t, "what was left off", notLaid(l), []string{
		"f.S.A1: " + noNextPage, "f.S.A2: " + noNextPage})
}

func TestANestedContainerIsBrokenAcrossTheSheetAndGoesOnBelowWhatFollowsIt(t *testing.T) {
	// The inner subform stacks and may be split, and it writes ten points of
	// its own height, so B — twenty tall — has nowhere to go inside it on this
	// sheet. It is not dropped: In keeps A where A fits, the sheet turns, and
	// In begins again at the top of the next content area with B. That is what
	// pdf.js's saved generator does, and what it means for a container to be
	// split rather than moved.
	//
	// C then follows B rather than A, because what In contributes to the stack
	// above it is what it holds ON THIS sheet (template.js:5222).
	l := laidOut(t, sheets(`>`+sheet("P", "", "100", ""), `
	  <subform name="Out" layout="tb">
	    <subform name="In" layout="tb" h="10pt">
	      <draw name="A" w="1pt" h="9pt"/><draw name="B" w="1pt" h="20pt"/></subform>
	    <draw name="C" w="1pt" h="5pt"/></subform>`))
	same(t, "the sheets", byPage(l), []string{
		"0: draw f.Out.In.A 0,0 1x9",
		"1: draw f.Out.In.B 0,0 1x20", "1: draw f.Out.C 0,20 1x5"})
	same(t, "what was left off", notLaid(l), nil)
}

func TestASheetSmallerThanTheOneBeforeItStillTakesTheFirstThing(t *testing.T) {
	// P1 has room for the brick and P2 does not. The brick fits no room left
	// on P1, so the sheet turns — and on P2 it is the first thing that moves
	// in one piece, which pdf.js will not fail whatever its height.
	l := laidOut(t, sheets(`>`+sheet("P1", "", "30", `<occur max="1"/>`)+sheet("P2", "", "10", ""), `
	  <draw name="A" w="1pt" h="20pt"/><draw name="B" w="1pt" h="20pt"/>`))
	same(t, "the sheets", byPage(l), []string{
		"0: draw f.A 0,0 1x20", "1: draw f.B 0,0 1x20"})
	same(t, "what was left off", notLaid(l), nil)
}

func TestAMarginNobodyCanReadStopsTheContainerRatherThanTheForm(t *testing.T) {
	l := laidOut(t, sheets(`>`+sheet("P", "", "100", ""), `
	  <subform name="Bad" layout="tb"><margin topInset="96px"/>
	    <draw name="A" w="1pt" h="5pt"/></subform>
	  <draw name="B" w="1pt" h="5pt"/>`))
	same(t, "the sheets", byPage(l), []string{"0: draw f.B 0,0 1x5"})
	same(t, "what was left off", notLaid(l),
		[]string{"f.Bad.A: the container that stacks it writes a margin that is not in lengths"})
}

func TestAnOriginNobodyCanReadStopsTheWholeBody(t *testing.T) {
	l := laidOut(t, `<template><subform name="f" layout="tb" x="96px">
	  <pageSet>`+sheet("P", "", "100", "")+`</pageSet>
	  <draw name="A" w="1pt" h="5pt"/></subform></template>`)
	same(t, "what was left off", notLaid(l),
		[]string{`f.A: its origin is written as x="96px" y="", which is not a place`})
}

func TestOneHeightNobodyCanArriveAtStopsEveryStackAboveIt(t *testing.T) {
	// A draw with no height stops the stack it is in AND every stack above it:
	// where the one after it begins is this one's height, and so is where the
	// container's next sibling begins.
	l := laidOut(t, sheets(`>`+sheet("P", "", "100", ""), `
	  <subform name="In" layout="tb">
	    <draw name="A" w="1pt" maxH="9pt"/><draw name="B" w="1pt" h="5pt"/></subform>
	  <draw name="C" w="1pt" h="5pt"/>`))
	same(t, "the sheets", byPage(l), nil)
	same(t, "what was left off", notLaid(l), []string{
		"f.In.A: " + boundByTheRoom("maxH"),
		"f.In.B: a tb layout stacks its children, and the height of the one above it is not computed: " +
			boundByTheRoom("maxH"),
		"f.C: a tb layout stacks its children, and the height of the one above it is not computed: " +
			boundByTheRoom("maxH")})
}

func TestABreakBeforeTheWholeFormWithNowhereToGoLeavesNothingBehind(t *testing.T) {
	// The outermost subform asks to start on a fresh sheet, and its page area
	// may make only one. Nothing of the form is placed — and nothing of it is
	// silently dropped either.
	l := laidOut(t, `<template><subform name="f" layout="tb">
	  <breakBefore targetType="pageArea" startNew="1"/>
	  <pageSet><occur max="1"/><pageArea name="P"><occur max="1"/><medium long="1000pt" short="1000pt"/>
	    <contentArea w="500pt" h="100pt"/></pageArea></pageSet>
	  <draw name="A" w="1pt" h="5pt"/><draw name="B" w="1pt" h="5pt"/></subform></template>`)
	same(t, "the sheets", byPage(l), nil)
	same(t, "what was left off", notLaid(l), []string{"f.A: " + noNextPage, "f.B: " + noNextPage})
}

func TestABreakToASheetThatIsSpentTakesTheSequenceInstead(t *testing.T) {
	// P1 offers two content areas and may make one sheet; P2 offers one and
	// may make any number. The body fills both of P1's, moves to P2, and then
	// a break names P1's SECOND content area — which cannot be gone to,
	// because P1 is spent. The sequence takes over, and the content area the
	// break asked for does not exist on the sheet it lands on.
	l := laidOut(t, sheets(`>`+twoAreas("P1", `<occur max="1"/>`)+
		`<pageArea name="P2"><medium long="1000pt" short="1000pt"/>`+
		`<contentArea x="7pt" y="0pt" w="500pt" h="20pt"/></pageArea>`, `
	  <subform name="F" layout="tb">`+bricks(3, "15")+`</subform>
	  <subform name="S" layout="tb"><breakBefore targetType="contentArea" target="P1.Low" startNew="1"/>
	    <draw name="B" w="1pt" h="5pt"/></subform>`))
	same(t, "the sheets", byPage(l), []string{
		"0: draw f.F.A0 0,0 1x15",
		"0: draw f.F.A1 100,200 1x15",
		"1: draw f.F.A2 7,0 1x15",
		"2: draw f.S.B 7,0 1x5"})
}

func TestSplittabilityIsAskedOfTheWholeChainAndNotOfOneNode(t *testing.T) {
	// pdf.js asks the container ABOVE before it looks at the container itself
	// (template.js:4943-4946), and the answer is no as soon as anything in the
	// chain says no. A tb subform inside a positioned one is not splittable,
	// however splittable it looks on its own.
	//
	// Nothing this package flows through can reach that case, because the flow
	// only ever descends through containers that already passed — so this is
	// the witness that the chain is ASKED rather than assumed, and the guard
	// that would catch a flow that one day descends somewhere else.
	form := Expand(parse(t, `<template><subform name="f" layout="tb">
	  <subform name="Pos"><subform name="In" layout="tb"/></subform>
	  <area name="Ar"><subform name="Under" layout="tb"/></area>
	  <subform name="Kept" layout="tb"><keep intact="contentArea"/></subform>
	  <exclGroup name="X" layout="tb"><keep intact="contentArea"/></exclGroup>
	  <exclGroup name="Row" layout="row"/>
	  <field name="L"/></subform></template>`), nil)
	p := &placer{up: map[*FormNode]*FormNode{}}
	p.mapUp(form.Root)
	named := map[string]*FormNode{}
	form.Root.Walk(func(n *FormNode) { named[n.Name] = n })
	for _, tc := range []struct {
		name string
		want bool
		why  string
	}{
		{"f", true, "tb, and the <template> above it ends the recursion saying yes"},
		{"Pos", false, "a positioned layout is never split"},
		{"In", false, "tb, but the positioned container above it is not splittable"},
		{"Ar", false, "an <area> inherits the base answer, which is no"},
		{"Under", false, "tb, but nothing inside an area is ever split"},
		{"Kept", false, "a subform kept intact is an author saying do not break this"},
		{"X", true, "an exclGroup has the same rule WITHOUT the keep clause"},
		{"Row", false, "a layout containing row is never split"},
		{"L", false, "a field is not a container"},
	} {
		if got := p.splittable(named[tc.name]); got != tc.want {
			t.Errorf("%s came out %v, want %v: %s", tc.name, got, tc.want, tc.why)
		}
	}
}

// TestARootThatWritesNoLayoutFlows is the one node pdfium reads differently
// from pdf.js, and the difference is a sheet count rather than a coordinate.
//
// pdf.js reads an absent layout as "position" wherever it appears, so the
// outermost subform of these three templates would be laid out in one piece on
// one sheet and the <break> inside it would never be reached — a positioned
// container is never flowed. pdfium's GetLayout
// (cxfa_contentlayoutprocessor.cpp:365-379) returns Tb instead, but only where
// the node's parent is the <form> root, and only where the attribute does not
// parse. See [placer.forcedTb].
//
// Three of the corpus's four such roots are forms this package put on one
// sheet where pdfium and pdf.js both use two.
func TestARootThatWritesNoLayoutFlows(t *testing.T) {
	body := `<pageSet><pageArea name="P"><medium long="100pt" short="100pt"/>
	    <contentArea w="100pt" h="100pt"/></pageArea></pageSet>
	  <subform name="One"><field name="A" w="5pt" h="5pt"/></subform>
	  <subform name="Two"><break before="pageArea" startNew="1"/>
	    <field name="B" w="5pt" h="5pt"/></subform>`
	for _, tc := range []struct {
		what  string
		root  string
		pages int
		want  []string
	}{
		{"no layout at all: pdfium's Tb, so the break fires", `<subform name="f">`, 2,
			[]string{"field f.One.A 0,0 5x5", "field f.Two.B 0,0 5x5"}},
		{"layout written as position: the attribute parses, so no forcing", `<subform name="f" layout="position">`, 1,
			[]string{"field f.One.A 0,0 5x5", "field f.Two.B 0,0 5x5"}},
		{"a layout nobody can read is an absent one", `<subform name="f" layout="sideways">`, 2,
			[]string{"field f.One.A 0,0 5x5", "field f.Two.B 0,0 5x5"}},
		{"a written flow layout is itself", `<subform name="f" layout="tb">`, 2,
			[]string{"field f.One.A 0,0 5x5", "field f.Two.B 0,0 5x5"}},
	} {
		l := laidOut(t, `<template>`+tc.root+body+`</subform></template>`)
		if len(l.Pages) != tc.pages {
			t.Errorf("%s: %d sheets, want %d", tc.what, len(l.Pages), tc.pages)
		}
		same(t, tc.what, laid(l), tc.want)
	}
}
