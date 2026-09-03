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

func TestOccurSaysHowOftenAPageAreaMayBeUsed(t *testing.T) {
	for _, tc := range []struct {
		what, occur string
		want        []string
	}{
		{"max twice", `<occur max="2"/>`, []string{"P", "P", "-"}},
		{"a min with no max pins the max to it", `<occur min="1"/>`, []string{"P", "-"}},
		{"an occur saying nothing bounds nothing", `<occur/>`, []string{"P", "P", "P"}},
		{"max unbounded", `<occur max="-1"/>`, []string{"P", "P", "P"}},
	} {
		same(t, tc.what, order(t, ">"+bare("P", "", tc.occur), 3), tc.want)
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
	// spent. It may be used once and holds one page area that may be used
	// once, so after B it hands back to the set above — which has nothing left
	// either, and says so rather than going round again.
	same(t, "the sequence",
		order(t, ">"+bare("A", "", `<occur max="1"/>`)+
			`<pageSet><occur max="1"/>`+bare("B", "", `<occur max="1"/>`)+`</pageSet>`, 4),
		[]string{"A", "B", "-"})
}

func TestAPageSetHoldingNothingYieldsNothing(t *testing.T) {
	// The empty set can never produce a sheet, so it says so rather than
	// starting itself again for ever.
	same(t, "the sequence",
		order(t, ">"+bare("A", "", `<occur max="1"/>`)+`<pageSet></pageSet>`, 3),
		[]string{"A", "-"})
}

func TestASetThatMayRunAgainStillDoesNotUnspendItsPages(t *testing.T) {
	// Two page areas, each good for one sheet, in a set with no <occur>: the
	// set may be used again and puts its own place in its list back to the
	// start, which is exactly what pdf.js does (template.js:4193-4198). It does
	// NOT reset the page areas, so going round finds them spent and stops.
	//
	// This is the branch that recurses in pdf.js. It terminates here because a
	// set may restart at most once for each sheet it is asked for.
	same(t, "the sequence",
		order(t, ">"+bare("A", "", `<occur max="1"/>`)+bare("B", "", `<occur max="1"/>`), 4),
		[]string{"A", "B", "-"})
}

func TestASetThatMayNotRunAgainIsCleanedAndStartsOver(t *testing.T) {
	// Every page area is spent and the set itself may be used only once. The
	// last thing left is to forget how much has been used, which is what
	// $cleanPage means and what its name says (template.js:4160-4167) — so the
	// sequence begins again rather than ending.
	//
	// pdf.js reaches this line and recurses until its stack gives out, because
	// its own clean leaves the set's place in its list untouched. Cleaning the
	// set's own state as well as its children's is the whole fix.
	same(t, "the sequence",
		order(t, `><occur max="1"/>`+bare("A", "", `<occur max="1"/>`), 3),
		[]string{"A", "A", "A"})
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

func TestCleaningAPageSetForgetsWhatIsNestedInItToo(t *testing.T) {
	// The set may run once, its page area once, and the set nested in it once.
	// When all are spent the last thing left is to forget it all, and that has
	// to reach the nested set as well or the sequence starts again with half
	// of it still spent.
	//
	// B does not come second. pdf.js starts the form with pageSetIndex at
	// NOUGHT rather than at minus one (template.js:5490), so the first nested
	// page set counts as already gone through; only after the clean, which
	// puts it back to minus one, is it reached.
	same(t, "the sequence",
		order(t, `><occur max="1"/>`+bare("A", "", `<occur max="1"/>`)+
			`<pageSet><occur max="1"/>`+bare("B", "", `<occur max="1"/>`)+`</pageSet>`, 5),
		[]string{"A", "A", "B", "A", "B"})
}

func TestADuplexPageSetWithNoPageAreaHasNothingToPickFrom(t *testing.T) {
	same(t, "the sequence",
		order(t, ">"+bare("A", "", `<occur max="1"/>`)+
			`<pageSet relation="duplexPaginated"></pageSet>`, 3),
		[]string{"A", "-"})
}
