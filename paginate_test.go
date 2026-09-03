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

func TestWhatFitsNoSheetAtAllIsReportedRatherThanTurnedForEverAfter(t *testing.T) {
	// The brick is taller than a whole content area, so no sheet will ever
	// hold it. Turning the page would not help and would leave a blank sheet
	// behind, so it is reported and the stack goes on without it.
	l := laidOut(t, sheets(`>`+sheet("P", "", "20", ""), `
	  <draw name="A" w="1pt" h="50pt"/><draw name="B" w="1pt" h="5pt"/>`))
	same(t, "the sheets", byPage(l), []string{"0: draw f.B 0,0 1x5"})
	same(t, "what was left off", notLaid(l), []string{"f.A: " + tooTallForAPage})
}

func TestWhatFitsNoRoomInsideAContainerThatMovesWholeIsReported(t *testing.T) {
	// The group is kept intact and writes ten points of its own height, so its
	// second child has nowhere to go: carrying it onto another sheet would
	// leave the rest of the group behind, which is the one thing keeping a
	// container intact forbids.
	l := laidOut(t, sheets(`>`+sheet("P", "", "100", ""), `
	  <subform name="G" layout="tb" h="10pt"><keep intact="contentArea"/>
	    <draw name="A" w="1pt" h="9pt"/><draw name="B" w="1pt" h="9pt"/></subform>`))
	same(t, "the sheets", byPage(l), []string{"0: draw f.G.A 0,0 1x9"})
	same(t, "what was left off", notLaid(l), []string{"f.G.B: " + noRoomInside})
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
