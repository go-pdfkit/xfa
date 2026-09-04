// Copyright (c) 2026, the go-pdfkit/xfa authors
// All rights reserved.
//
// SPDX-License-Identifier: BSD-3-Clause

package xfa

import (
	"strings"
	"testing"
)

// order asks the page sequence for n sheets and names the page area of each,
// with "-" where it says there is no next one. It drives the machine the way
// [placer.advance] does: the page number counts the sheet just finished before
// the next one is asked for.
func order(t *testing.T, set string, n int) []string {
	t.Helper()
	form := Expand(parse(t, `<template><subform name="f"><pageSet `+set+`</pageSet></subform></template>`), nil)
	root := firstOfKind(form.Root, "subform")
	p := newPager(root)
	area, _ := p.first(root)
	var out []string
	for range n {
		if area == nil {
			out = append(out, "-")
			break
		}
		out = append(out, area.Name)
		p.number++
		area = p.next(area)
	}
	return out
}

// bare is a page area with nothing on it, for the tests that are about the
// sequence rather than about what goes on a sheet.
func bare(name, attr, occur string) string {
	return `<pageArea name="` + name + `" ` + attr + `>` + occur + `</pageArea>`
}

func TestOnePageAreaMakesEverySheet(t *testing.T) {
	// A page area with no <occur> may be used for ever, so pdf.js hands the
	// same one back at every turn (PageArea[$getNextPage], template.js:4064).
	same(t, "the sequence", order(t, ">"+bare("P", "", ""), 4), []string{"P", "P", "P", "P"})
}

func TestAPageAreaMaxBoundsONERUNOfTheSetAndNotTheForm(t *testing.T) {
	// The max is how many sheets a page area makes before the sequence moves
	// on, not how many it makes in the whole form. Two page areas each good
	// for two sheets, in a set with no <occur>: the set may run again, and
	// running again offers them again. See [pager.cleanKids].
	same(t, "the sequence",
		order(t, ">"+bare("A", "", `<occur max="2"/>`)+bare("B", "", `<occur max="2"/>`), 9),
		[]string{"A", "A", "B", "B", "A", "A", "B", "B", "A"})
}

func TestAnAbsentMaxBoundsNothingWhateverTheMinSays(t *testing.T) {
	// A written min does NOT stand in for an absent max, and the one page
	// area here would give a single sheet if it did. See [occurMax], where
	// pdf.js and pdfium are read together, and us-ssa__ssa-3371-bk, which
	// writes exactly this and which pdf.js puts on nine sheets.
	for _, tc := range []struct {
		what, occur string
	}{
		{"a min with no max", `<occur min="1"/>`},
		{"an occur saying nothing", `<occur/>`},
		{"max unbounded", `<occur max="-1"/>`},
		{"no occur at all", ``},
	} {
		same(t, tc.what, order(t, ">"+bare("P", "", tc.occur), 3), []string{"P", "P", "P"})
	}
}

func TestASpentPageAreaHandsOnToTheNext(t *testing.T) {
	// P1 may be used once. P2 has no occur, so once the sequence has reached
	// it, it makes every sheet after that.
	same(t, "the sequence",
		order(t, ">"+bare("P1", "", `<occur max="1"/>`)+bare("P2", "", ""), 4),
		[]string{"P1", "P2", "P2", "P2"})
}

func TestTheSequenceDescendsIntoANestedPageSet(t *testing.T) {
	same(t, "the sequence",
		order(t, ">"+bare("A", "", `<occur max="1"/>`)+`<pageSet>`+bare("B", "", "")+`</pageSet>`, 3),
		[]string{"A", "B", "B"})
}

func TestAnExhaustedNestedPageSetHandsBackToTheOneAboveIt(t *testing.T) {
	// A page set offers ALL its page areas before any of its nested sets
	// (template.js:4182-4192), so the nested one is reached only once A is
	// spent. The nested set may be used ONCE, so when it is asked a second
	// time it hands back to the set above rather than restarting itself.
	//
	// The set above has no <occur> and restarts for ever, and restarting it
	// offers A again. It does not offer B again: cleaning a page set resets
	// the page areas below it and not the page sets' own counts, which is
	// exactly what pdf.js's $cleanPage does (template.js:4160-4167) and what
	// makes a SET's <occur> a bound on the whole form where a page area's is
	// not.
	same(t, "the sequence",
		order(t, ">"+bare("A", "", `<occur max="1"/>`)+
			`<pageSet><occur max="1"/>`+bare("B", "", `<occur max="1"/>`)+`</pageSet>`, 5),
		[]string{"A", "B", "A", "A", "A"})
}

func TestAPageSetHoldingNothingYieldsNothing(t *testing.T) {
	// The empty set can never produce a sheet, so it says so rather than
	// starting itself again for ever.
	same(t, "the sequence",
		order(t, ">"+bare("A", "", `<occur max="1"/>`)+`<pageSet></pageSet>`, 3),
		[]string{"A", "-"})
}

func TestASetThatRunsAgainOffersItsPagesAgain(t *testing.T) {
	// Two page areas, each good for one sheet, in a set with no <occur>. This
	// is the branch pdf.js exhausts its stack in: the set may be used again
	// and puts its own place in its list back to the start
	// (template.js:4193-4198), but it does not clean the page areas, so going
	// round finds them spent and comes back to the same line unchanged.
	//
	// Restarting a page set MEANS offering its page areas again — pdfium sets
	// cur_page_count_ = 1 on the page area it settles on
	// (cxfa_viewlayoutprocessor.cpp:1240-1244) — so the sequence goes round
	// rather than stopping, and terminates because it has a page to give.
	//
	// It is the shape of all seven forms pdf.js dies on. See
	// [TestTheSevenFormsPdfjsRecursesForeverOn].
	same(t, "the sequence",
		order(t, ">"+bare("A", "", `<occur max="1"/>`)+bare("B", "", `<occur max="1"/>`), 5),
		[]string{"A", "B", "A", "B", "A"})
}

func TestASpentPageSetIsTheEndOfTheForm(t *testing.T) {
	// Every page area is spent and the set itself may be used only once. There
	// is no page set above it to ask, so the form has run out of pages.
	//
	// pdf.js's answer here is its SECOND infinite loop: it cleans the page
	// areas below and calls itself (template.js:4230-4231), but leaves its own
	// pageIndex at the end of the list, so the call arrives back unchanged.
	// pdfium stops at the root page set and returns nullptr
	// (cxfa_viewlayoutprocessor.cpp:1450-1470). Without this branch a page
	// set's <occur> would bound nothing at all.
	same(t, "the sequence",
		order(t, `><occur max="1"/>`+bare("A", "", `<occur max="1"/>`), 3),
		[]string{"A", "-"})
}

func TestADuplexPageSetPicksItsSheetByParity(t *testing.T) {
	for _, tc := range []struct {
		what, areas string
		want        []string
	}{
		{"by parity and position",
			bare("Odd", `oddOrEven="odd" pagePosition="rest"`, "") +
				bare("Even", `oddOrEven="even" pagePosition="rest"`, ""),
			[]string{"Odd", "Even", "Odd", "Even"}},
		{"any parity, that position",
			bare("Odd", `oddOrEven="odd" pagePosition="rest"`, "") +
				bare("Any", `oddOrEven="any" pagePosition="rest"`, ""),
			[]string{"Odd", "Any", "Odd", "Any"}},
		{"any parity, any position",
			bare("Odd", `oddOrEven="odd" pagePosition="rest"`, "") + bare("Any", "", ""),
			[]string{"Odd", "Any", "Odd", "Any"}},
		{"nothing matches, so the first one",
			bare("First", `oddOrEven="odd" pagePosition="first"`, "") +
				bare("Last", `oddOrEven="even" pagePosition="last"`, ""),
			[]string{"First", "First", "First", "First"}},
	} {
		same(t, tc.what, order(t, `relation="duplexPaginated">`+tc.areas, 4), tc.want)
	}
	// A page set with no page area at all has nothing to pick from.
	same(t, "nothing to pick", order(t, `relation="simplexPaginated">`+
		bare("A", "", "")+`<pageSet relation="duplexPaginated"></pageSet>`, 2), []string{"A", "A"})
}

func TestAFormWithNoPageAreaHasNowhereToPutAnything(t *testing.T) {
	same(t, "the sequence", order(t, "></pageSet><pageSet>", 2), []string{"-"})
}

func TestWhatABreakTargetCanName(t *testing.T) {
	form := Expand(parse(t, `<template><subform name="f"><pageSet>`+
		`<pageArea name="P1" id="one"><contentArea name="C1"/><contentArea name="C2"/></pageArea>`+
		`<pageSet name="Inner"><pageArea name="P2"/></pageSet></pageSet></subform></template>`), nil)
	root := firstOfKind(form.Root, "subform")
	p := newPager(root)
	for _, tc := range []struct {
		target, want string
		index        int
	}{
		{"", "", -1},
		{"#one", "P1", -1},
		{"#nobody", "", -1},
		{"P1", "P1", -1},
		{"Inner.P2", "P2", -1},
		{"P1.C2", "P1", 1},
		{"P1.#contentArea", "P1", 0},
		{"P1.#medium", "", -1},
		{"Inner.#contentArea", "", -1},
		{"P1.Nothing", "", -1},
		{"Nothing.P1", "", -1},
		{"P1..C2", "", -1},
	} {
		got, idx := p.resolve(root, tc.target)
		name := ""
		if got != nil {
			name = got.Name
		}
		if name != tc.want || idx != tc.index {
			t.Errorf("%q resolved to %q at %d, want %q at %d", tc.target, name, idx, tc.want, tc.index)
		}
	}
}

func TestWhatABreakDoes(t *testing.T) {
	form := Expand(parse(t, `<template><subform name="f"><pageSet>`+
		`<pageArea name="P1"><contentArea name="C1"/><contentArea name="C2"/></pageArea>`+
		`<pageArea name="P2"><contentArea name="D1"/></pageArea></pageSet></subform></template>`), nil)
	root := firstOfKind(form.Root, "subform")
	p := newPager(root)
	cur, _ := p.first(root)
	for _, tc := range []struct {
		what string
		spec breakSpec
		slot int
		want string
	}{
		{"auto does nothing", breakSpec{"auto", "", true}, 0, "no"},
		{"a target nothing answers does nothing", breakSpec{"pageArea", "Nowhere", true}, 0, "no"},
		{"startNew with no target opens a sheet of the page area in hand",
			breakSpec{"pageArea", "", true}, 0, "P1 @0 fresh"},
		{"startNew with a target opens a sheet of it",
			breakSpec{"pageArea", "P2", true}, 0, "P2 @0 fresh"},
		{"a target that is a content area is not a page area",
			breakSpec{"pageArea", "P1.C2", true}, 0, "P1 @0 fresh"},
		{"without startNew, another page area is still gone to",
			breakSpec{"pageArea", "P2", false}, 0, "P2 @0 fresh"},
		{"without startNew, the page area in hand is not", breakSpec{"pageArea", "P1", false}, 0, "no"},
		{"a content area with no target is the next one along",
			breakSpec{"contentArea", "", true}, 0, "- @1 here"},
		{"a later content area of the sheet in hand stays on it",
			breakSpec{"contentArea", "P1.C2", true}, 0, "- @1 here"},
		{"an earlier one opens a fresh sheet",
			breakSpec{"contentArea", "P1.C1", true}, 1, "P1 @0 fresh"},
		{"one on another page area goes there",
			breakSpec{"contentArea", "P2.D1", true}, 0, "P2 @0 fresh"},
		{"without startNew, a later one on the sheet in hand",
			breakSpec{"contentArea", "P1.C2", false}, 0, "- @1 here"},
		{"without startNew, one on another page area",
			breakSpec{"contentArea", "P2.D1", false}, 0, "P2 @0 fresh"},
		{"without startNew, the content area in hand does nothing",
			breakSpec{"contentArea", "P1.C1", false}, 0, "no"},
		{"without startNew and with no target, nothing",
			breakSpec{"contentArea", "", false}, 0, "no"},
		{"a page area target that is not a page area at all",
			breakSpec{"pageArea", "P1.C1", false}, 0, "no"},
	} {
		to, does := p.fire(root, tc.spec, cur, tc.slot)
		got := "no"
		if does {
			name, where := "-", "here"
			if to.area != nil {
				name = to.area.Name
			}
			if to.page {
				where = "fresh"
			}
			got = strings.Join([]string{name, "@" + string(rune('0'+to.index)), where}, " ")
		}
		if got != tc.want {
			t.Errorf("%s: %q, want %q", tc.what, got, tc.want)
		}
	}
}

func TestASpentPageAreaIsNotReachedByNamingIt(t *testing.T) {
	// P2 may be used once and the break names it twice. The second time it is
	// no longer usable, so the sequence takes over from where it is rather
	// than going where the break said.
	l := laidOut(t, sheets(`>`+sheet("P1", "", "100", "")+
		`<pageArea name="P2"><occur max="1"/><medium long="1000pt" short="1000pt"/>`+
		`<draw name="Two" w="1pt" h="1pt"/><contentArea w="500pt" h="100pt"/></pageArea>`+
		sheet("P3", "", "100", ""), `
	  <subform name="F" layout="tb"><draw name="A" w="1pt" h="5pt"/></subform>
	  <subform name="S" layout="tb"><breakBefore targetType="pageArea" target="P2" startNew="1"/>
	    <draw name="B" w="1pt" h="5pt"/></subform>
	  <subform name="T" layout="tb"><breakBefore targetType="pageArea" target="P2" startNew="1"/>
	    <draw name="C" w="1pt" h="5pt"/></subform>`))
	same(t, "the sheets", byPage(l), []string{
		"0: draw f.F.A 0,0 1x5",
		"1: draw f.P2.Two 0,0 1x1", "1: draw f.S.B 0,0 1x5",
		"2: draw f.T.C 0,0 1x5"})
}

func TestAPageSetIsStartedAgainAtMostOncePerSheetAsked(t *testing.T) {
	// The guard on restarting, and the one shape that still needs it once a
	// restart offers the page areas below it again.
	//
	// The middle set holds no page area of its own and one nested set, which
	// may run once. When the nested one is spent it hands back; the middle one
	// has no <occur>, so it starts again, and starting again offers the nested
	// one again — which is spent, and hands back. Nothing in that circle is a
	// page, and without the guard it is pdf.js's recursion in a second place.
	same(t, "the sequence",
		order(t, ">"+bare("A", "", `<occur max="1"/>`)+
			`<pageSet><pageSet><occur max="1"/>`+bare("B", "", `<occur max="1"/>`)+
			`</pageSet></pageSet>`, 4),
		[]string{"A", "B", "-"})
}

// TestTheSevenFormsPdfjsRecursesForeverOn builds the page set of each of the
// seven corpus forms pdf.js exhausts its stack on, and asks each for more
// sheets than it has page areas.
//
// No corpus document enters this repository, so what is reproduced is the
// page set itself, transcribed attribute for attribute. Every one of the seven
// is the same thing: a page set with no <occur>, so usable for ever, over page
// areas that all write a max. pdf.js restarts the set, is handed back a page
// area that is spent, restarts again, and dies; here the restart cleans them,
// so the sequence goes round and every answer is a sheet.
func TestTheSevenFormsPdfjsRecursesForeverOn(t *testing.T) {
	for _, tc := range []struct {
		form, set string
		want      []string
	}{
		{"ca-cra__rc1-fill-11-25e", ">" + bare("Page1", "", `<occur min="1" max="1"/>`),
			[]string{"Page1", "Page1", "Page1", "Page1"}},
		{"ca-cra__t2042-fill-24e", ">" + bare("Pg1", "", `<occur max="1"/>`) +
			bare("Landscape", "", `<occur max="1"/>`),
			[]string{"Pg1", "Landscape", "Pg1", "Landscape"}},
		{"ca-cra__t2042-fill-25e", ">" + bare("Pg1", "", `<occur max="1"/>`) +
			bare("Landscape", "", `<occur max="1"/>`),
			[]string{"Pg1", "Landscape", "Pg1", "Landscape"}},
		{"us-ssa__ha-4631", `name="MasterPages">` + bare("MPPage1", "", `<occur max="1" min="1"/>`),
			[]string{"MPPage1", "MPPage1", "MPPage1", "MPPage1"}},
		{"us-ssa__ssa-372", ">" + bare("Page1", "", `<occur max="1" min="1"/>`),
			[]string{"Page1", "Page1", "Page1", "Page1"}},
		{"us-ssa__ssa-5062", ">" + bare("Page1", "", `<occur max="1" min="1"/>`),
			[]string{"Page1", "Page1", "Page1", "Page1"}},
		{"us-ssa__ssa-766", ">" + bare("Page1", "", `<occur max="1"/>`),
			[]string{"Page1", "Page1", "Page1", "Page1"}},
	} {
		same(t, tc.form, order(t, tc.set, len(tc.want)), tc.want)
	}
}

// TestTheEighthFormIsNotTheSeven is us-ssa__ssa-3371-bk, which pdf.js does lay
// out — on NINE sheets — from a page area writing <occur min="1"/> and nothing
// else. Read as a max of one it gives a single sheet and 127 of its fields are
// left off the form. See [occurMax].
func TestTheEighthFormIsNotTheSeven(t *testing.T) {
	same(t, "us-ssa__ssa-3371-bk", order(t, ">"+bare("Page1", "", `<occur min="1"/>`), 4),
		[]string{"Page1", "Page1", "Page1", "Page1"})
}

func TestADuplexPageSetWithNoPageAreaHasNothingToPickFrom(t *testing.T) {
	same(t, "the sequence",
		order(t, ">"+bare("A", "", `<occur max="1"/>`)+
			`<pageSet relation="duplexPaginated"></pageSet>`, 3),
		[]string{"A", "-"})
}
