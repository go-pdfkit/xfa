// Copyright (c) 2026, the go-pdfkit/xfa authors
// All rights reserved.
//
// SPDX-License-Identifier: BSD-3-Clause

package xfa

import (
	"fmt"
	"math"
	"strings"
	"testing"
)

// bindOf reads a template and a datasets and joins them, from the XML a form
// would actually carry. Building the trees by hand would test the binder
// against a shape no designer produces.
func bindOf(t *testing.T, template, datasets string) *Result {
	t.Helper()
	tmpl := parse(t, template)
	var data *Node
	if datasets != "" {
		d, err := ParseDatasets(strings.NewReader(datasets))
		if err != nil {
			t.Fatalf("parsing the datasets: %v", err)
		}
		data = d
	}
	return Bind(tmpl, data)
}

// wrap puts a datasets around what a form's data would say.
func wrap(inner string) string {
	return `<xfa:datasets xmlns:xfa="http://www.xfa.org/schema/xfa-data/1.0/"><xfa:data>` +
		inner + `</xfa:data></xfa:datasets>`
}

// shows renders a result as "path=value" lines, marking an unbound field, so
// that a test can say what it expects in one string.
func shows(r *Result) string {
	var b strings.Builder
	for _, f := range r.Fields {
		if !f.Bound {
			fmt.Fprintf(&b, "%s=-\n", f.Path)
			continue
		}
		fmt.Fprintf(&b, "%s=%s\n", f.Path, f.Value)
	}
	return b.String()
}

func want(t *testing.T, r *Result, expect string) {
	t.Helper()
	got := shows(r)
	expect = strings.TrimSpace(expect) + "\n"
	if strings.TrimSpace(expect) == "" {
		expect = ""
	}
	if got != expect {
		t.Errorf("got:\n%swant:\n%s", got, expect)
	}
}

// TestAFormWithNoBindAtAllBindsByName is the common case: nearly every real
// form leaves binding implicit, and each container takes the next data node of
// its own name.
func TestAFormWithNoBindAtAllBindsByName(t *testing.T) {
	r := bindOf(t, `<template><subform name="form1">
		<field name="Number1"><ui><numericEdit/></ui></field>
		<field name="Number2"><ui><numericEdit/></ui></field>
	</subform></template>`,
		wrap(`<form1><Number1>1.00</Number1><Number2>2.00</Number2></form1>`))
	want(t, r, `
form1.Number1=1.00
form1.Number2=2.00`)
	if len(r.Unsupported) != 0 || r.Truncated {
		t.Errorf("%v %v", r.Unsupported, r.Truncated)
	}
	if v := r.Values(); v["form1.Number1"] != "1.00" || len(v) != 2 {
		t.Errorf("Values: %v", v)
	}
}

// TestTheOutermostSubformIsBoundWhateverItIsCalled is XFA 3.3 p. 182: the top
// subform and the record are bound to each other by position, not by name,
// which is why a template called form1 reads data called Root.
func TestTheOutermostSubformIsBoundWhateverItIsCalled(t *testing.T) {
	r := bindOf(t, `<template><subform name="form1">
		<field name="A"><ui><textEdit/></ui></field>
	</subform></template>`,
		wrap(`<Root><A>yes</A></Root>`))
	want(t, r, `form1.A=yes`)
	if r.Fields[0].DataPath != "Root.A" {
		t.Errorf("the data path is %q", r.Fields[0].DataPath)
	}
}

// TestASecondDataNodeOfTheSameNameGoesToTheSecondField is what consuming
// means: two fields of one name take two different values, in order.
func TestASecondDataNodeOfTheSameNameGoesToTheSecondField(t *testing.T) {
	r := bindOf(t, `<template><subform name="f">
		<field name="A"><ui><textEdit/></ui></field>
		<field name="A"><ui><textEdit/></ui></field>
		<field name="A"><ui><textEdit/></ui></field>
	</subform></template>`,
		wrap(`<r><A>one</A><A>two</A></r>`))
	want(t, r, `
f.A=one
f.A[1]=two
f.A[2]=-`)
}

// TestASubformRepeatsOverACollection is what makes dynamic XFA dynamic: one
// subform in the template becomes one per record in the document.
func TestASubformRepeatsOverACollection(t *testing.T) {
	r := bindOf(t, `<template><subform name="f">
		<subform name="Row"><occur min="1" max="-1"/>
			<field name="Item"><ui><textEdit/></ui></field>
			<field name="Qty"><ui><numericEdit/></ui></field>
		</subform>
	</subform></template>`,
		wrap(`<r>
			<Row><Item>bolt</Item><Qty>4</Qty></Row>
			<Row><Item>nut</Item><Qty>8</Qty></Row>
			<Row><Item>washer</Item><Qty>16</Qty></Row>
		</r>`))
	want(t, r, `
f.Row.Item=bolt
f.Row.Qty=4
f.Row[1].Item=nut
f.Row[1].Qty=8
f.Row[2].Item=washer
f.Row[2].Qty=16`)
}

// TestOccurCapsHowManyTimesAContainerRepeats: max says how many records the
// form will show, and the rest of the data is simply not in it.
func TestOccurCapsHowManyTimesAContainerRepeats(t *testing.T) {
	r := bindOf(t, `<template><subform name="f">
		<subform name="Row"><occur min="1" max="2"/>
			<field name="Item"><ui><textEdit/></ui></field>
		</subform>
	</subform></template>`,
		wrap(`<r><Row><Item>a</Item></Row><Row><Item>b</Item></Row><Row><Item>c</Item></Row></r>`))
	want(t, r, `
f.Row.Item=a
f.Row[1].Item=b`)
}

// TestAContainerThatMayAppearNoTimesIsDroppedWhenThereIsNoData is XFA 3.3
// G12.1428332: min="0" and nothing to show means the container is not in the
// form at all, rather than in it and empty.
func TestAContainerThatMayAppearNoTimesIsDroppedWhenThereIsNoData(t *testing.T) {
	r := bindOf(t, `<template><subform name="f">
		<subform name="Present"><occur min="0" max="-1"/>
			<field name="A"><ui><textEdit/></ui></field>
		</subform>
		<subform name="Absent"><occur min="0" max="-1"/>
			<field name="B"><ui><textEdit/></ui></field>
		</subform>
	</subform></template>`,
		wrap(`<r><Present><A>here</A></Present></r>`))
	want(t, r, `f.Present.A=here`)
}

// TestMatchNoneKeepsAFieldOutOfTheData: a field the form does not save still
// appears, and its children still bind.
func TestMatchNoneKeepsAFieldOutOfTheData(t *testing.T) {
	r := bindOf(t, `<template><subform name="f">
		<field name="Total"><bind match="none"/><ui><numericEdit/></ui></field>
		<subform name="Box"><bind match="none"/>
			<field name="Inner"><ui><textEdit/></ui></field>
		</subform>
	</subform></template>`,
		wrap(`<r><Total>99</Total><Inner>read</Inner></r>`))
	want(t, r, `
f.Total=-
f.Box.Inner=read`)
}

// TestMatchGlobalReachesAcrossTheWholeData: a global value sits anywhere, and
// every field that asks for it gets it.
func TestMatchGlobalReachesAcrossTheWholeData(t *testing.T) {
	r := bindOf(t, `<template><subform name="f">
		<subform name="Deep">
			<field name="Company"><bind match="global"/><ui><textEdit/></ui></field>
		</subform>
	</subform></template>`,
		wrap(`<r><Elsewhere><Company>Acme</Company></Elsewhere><Deep/></r>`))
	want(t, r, `f.Deep.Company=Acme`)
}

// TestAGlobalValueMayBeAnAttribute: XFA keeps a global in an attribute as
// readily as in an element, and a form asking for it by name finds either.
func TestAGlobalValueMayBeAnAttribute(t *testing.T) {
	r := bindOf(t, `<template><subform name="f">
		<field name="Cerfa"><bind match="global"/><ui><textEdit/></ui></field>
	</subform></template>`,
		wrap(`<r><Head Cerfa="N 11055*01"/></r>`))
	want(t, r, `f.Cerfa=N 11055*01`)
	if p := r.Fields[0].DataPath; p != "r.Head.@Cerfa" {
		t.Errorf("the data path is %q", p)
	}
}

// TestAGlobalSearchThatFindsNothingLeavesTheFieldAlone.
func TestAGlobalSearchThatFindsNothingLeavesTheFieldAlone(t *testing.T) {
	r := bindOf(t, `<template><subform name="f">
		<field name="Missing"><bind match="global"/><ui><textEdit/></ui></field>
	</subform></template>`, wrap(`<r><Other>x</Other></r>`))
	want(t, r, `f.Missing=-`)
}

// TestAnUnboundedGlobalStopsRepeatingItself. pdf.js's global lookup does not
// skip what it has already handed back, so this shape asks for the same node
// for ever; here it stops at the repeat.
func TestAnUnboundedGlobalStopsRepeatingItself(t *testing.T) {
	r := bindOf(t, `<template><subform name="f">
		<subform name="Deep">
			<field name="G"><occur min="1" max="-1"/><bind match="global"/><ui><textEdit/></ui></field>
		</subform>
	</subform></template>`,
		wrap(`<r><Far><G>once</G></Far><Deep/></r>`))
	want(t, r, `f.Deep.G=once`)
}

// TestMatchDataRefFollowsAPath: the explicit case, where the data's path is
// nothing like the form's.
func TestMatchDataRefFollowsAPath(t *testing.T) {
	r := bindOf(t, `<template><subform name="Formulaire">
		<subform name="Page1">
			<field name="Pays"><bind match="dataRef" ref="$record.Metier.Adresse.Pays.Name"/><ui><textEdit/></ui></field>
		</subform>
	</subform></template>`,
		wrap(`<Formulaire><Metier><Adresse><Pays><Name>FR</Name></Pays></Adresse></Metier></Formulaire>`))
	want(t, r, `Formulaire.Page1.Pays=FR`)
}

// TestADataRefThatMatchesNothingLeavesTheFieldUnbound. pdf.js would invent
// the nodes the expression names so that somebody could type into them; an
// invented node is empty, so an unbound field says the same thing.
func TestADataRefThatMatchesNothingLeavesTheFieldUnbound(t *testing.T) {
	r := bindOf(t, `<template><subform name="f">
		<field name="A"><bind match="dataRef" ref="$data.nowhere.at.all"/><ui><textEdit/></ui></field>
	</subform></template>`, wrap(`<r><A>ignored</A></r>`))
	want(t, r, `f.A=-`)
}

// TestADataRefWithNoRefAtAllFallsBackToTheName.
func TestADataRefWithNoRefAtAllFallsBackToTheName(t *testing.T) {
	r := bindOf(t, `<template><subform name="f">
		<field name="A"><bind match="dataRef"/><ui><textEdit/></ui></field>
	</subform></template>`, wrap(`<r><A>x</A></r>`))
	want(t, r, `f.A=-`)
}

// TestAStarTakesEverySiblingOfThatName, which is how one form fills six
// address blocks from six records.
func TestAStarTakesEverySiblingOfThatName(t *testing.T) {
	r := bindOf(t, `<template><subform name="f">
		<subform name="Vivant"><occur min="1" max="-1"/>
			<bind match="dataRef" ref="$record.Precisions[*]"/>
			<field name="Name"><ui><textEdit/></ui></field>
		</subform>
	</subform></template>`,
		wrap(`<rec><Precisions><Name>one</Name></Precisions><Precisions><Name>two</Name></Precisions></rec>`))
	want(t, r, `
f.Vivant.Name=one
f.Vivant[1].Name=two`)
	if p := r.Fields[1].DataPath; p != "rec.Precisions[1].Name" {
		t.Errorf("the data path is %q", p)
	}
}

// TestARefIndexPicksOneSibling.
func TestARefIndexPicksOneSibling(t *testing.T) {
	r := bindOf(t, `<template><subform name="f">
		<field name="Second"><bind match="dataRef" ref="$record.P[1].N"/><ui><textEdit/></ui></field>
		<field name="Tenth"><bind match="dataRef" ref="$record.P[9].N"/><ui><textEdit/></ui></field>
	</subform></template>`,
		wrap(`<rec><P><N>one</N></P><P><N>two</N></P></rec>`))
	want(t, r, `
f.Second=two
f.Tenth=-`)
}

// TestAnUnqualifiedRefWalksUpUntilItFindsAHome. XFA 3.3 p. 114: a name that
// is not below the current node is looked for above it.
func TestAnUnqualifiedRefWalksUpUntilItFindsAHome(t *testing.T) {
	r := bindOf(t, `<template><subform name="f">
		<subform name="Inner">
			<field name="A"><bind match="dataRef" ref="Up.Value"/><ui><textEdit/></ui></field>
		</subform>
	</subform></template>`,
		wrap(`<rec><Up><Value>found</Value></Up><Inner/></rec>`))
	want(t, r, `f.Inner.A=found`)
}

// TestARefThatRunsOutOfParentsFindsNothing.
func TestARefThatRunsOutOfParentsFindsNothing(t *testing.T) {
	r := bindOf(t, `<template><subform name="f">
		<field name="A"><bind match="dataRef" ref="NoSuchThing.Value"/><ui><textEdit/></ui></field>
	</subform></template>`, wrap(`<rec><Other/></rec>`))
	want(t, r, `f.A=-`)
}

// TestADescendantRefReachesThroughWhateverIsBetween.
func TestADescendantRefReachesThroughWhateverIsBetween(t *testing.T) {
	r := bindOf(t, `<template><subform name="f">
		<field name="A"><bind match="dataRef" ref="$record..Leaf"/><ui><textEdit/></ui></field>
	</subform></template>`,
		wrap(`<rec><Mid><Deeper><Leaf>down here</Leaf></Deeper></Mid></rec>`))
	want(t, r, `f.A=down here`)
}

// TestAClassRefNamesTheElementRatherThanTheName.
func TestAClassRefNamesTheElementRatherThanTheName(t *testing.T) {
	r := bindOf(t, `<template><subform name="f">
		<field name="A"><bind match="dataRef" ref="$record.#Leaf"/><ui><textEdit/></ui></field>
		<field name="B"><bind match="dataRef" ref="$record.#Attr"/><ui><textEdit/></ui></field>
	</subform></template>`,
		wrap(`<rec Attr="from an attribute"><Leaf>from an element</Leaf></rec>`))
	want(t, r, `
f.A=from an element
f.B=from an attribute`)
}

// TestTheDollarShortcutStartsWhereTheContainerIs.
func TestTheDollarShortcutStartsWhereTheContainerIs(t *testing.T) {
	r := bindOf(t, `<template><subform name="f">
		<subform name="Inner">
			<field name="A"><bind match="dataRef" ref="$.Value"/><ui><textEdit/></ui></field>
		</subform>
	</subform></template>`,
		wrap(`<rec><Inner><Value>near</Value></Inner></rec>`))
	want(t, r, `f.Inner.A=near`)
}

// TestTheBangShortcutStartsAtTheDatasets, which is one level above the data.
func TestTheBangShortcutStartsAtTheDatasets(t *testing.T) {
	r := bindOf(t, `<template><subform name="f">
		<field name="A"><bind match="dataRef" ref="!.data.rec.Value"/><ui><textEdit/></ui></field>
	</subform></template>`,
		wrap(`<rec><Value>from the top</Value></rec>`))
	want(t, r, `f.A=from the top`)
}

// TestADataRefOutsideTheDataIsReportedRatherThanGuessedAt.
func TestADataRefOutsideTheDataIsReportedRatherThanGuessedAt(t *testing.T) {
	r := bindOf(t, `<template><subform name="f">
		<field name="A"><bind match="dataRef" ref="$template.form1.x"/><ui><textEdit/></ui></field>
	</subform></template>`, wrap(`<r><A>x</A></r>`))
	want(t, r, `f.A=-`)
	if len(r.Unsupported) != 1 {
		t.Fatalf("%d unsupported, want 1", len(r.Unsupported))
	}
	u := r.Unsupported[0]
	if u.Field != "f.A" || u.Ref != "$template.form1.x" ||
		!strings.Contains(u.Why, "outside the data") {
		t.Errorf("%+v", u)
	}
}

// TestTheSubexpressionsThisDoesNotRead names each construct it stops at, so
// that a caller can tell an unfilled field from an unread one.
func TestTheSubexpressionsThisDoesNotRead(t *testing.T) {
	for _, c := range []struct{ ref, why string }{
		{"a.[b == 1]", "FormCalc"},
		{"a.(b == 1)", "JavaScript"},
		{"a[-1]", "whole number"},
		{"a[2", "never closed"},
		{"[0]", "names nothing"},
	} {
		r := bindOf(t, `<template><subform name="f">
			<field name="A"><bind match="dataRef" ref="`+c.ref+`"/><ui><textEdit/></ui></field>
		</subform></template>`, wrap(`<r><a><b>x</b></a></r>`))
		if len(r.Unsupported) != 1 {
			t.Errorf("%s: %d unsupported, want 1", c.ref, len(r.Unsupported))
			continue
		}
		if !strings.Contains(r.Unsupported[0].Why, c.why) {
			t.Errorf("%s: %q does not mention %q", c.ref, r.Unsupported[0].Why, c.why)
		}
	}
}

// TestAnExpressionThatEndsInASeparatorStopsThere rather than reading past it.
func TestAnExpressionThatEndsInASeparatorStopsThere(t *testing.T) {
	for _, ref := range []string{"$record.", "$record..", "$record.#"} {
		r := bindOf(t, `<template><subform name="f">
			<field name="A"><bind match="dataRef" ref="`+ref+`"/><ui><textEdit/></ui></field>
		</subform></template>`, wrap(`<rec><A>x</A></rec>`))
		if len(r.Unsupported) != 0 {
			t.Errorf("%s: %v", ref, r.Unsupported)
		}
		// The expression names the record itself, which is a group: there is
		// no value in it to take.
		want(t, r, `f.A=`)
	}
}

// TestARecordShortcutWithNoRecord.
func TestARecordShortcutWithNoRecord(t *testing.T) {
	r := bindOf(t, `<template><subform name="f">
		<field name="A"><bind match="dataRef" ref="$record.x"/><ui><textEdit/></ui></field>
	</subform></template>`, wrap(``))
	want(t, r, `f.A=-`)
}

// TestARefFromAContainerThatIsNotBound: a subform bound to nothing hands its
// children nothing to search from.
func TestARefFromAContainerThatIsNotBound(t *testing.T) {
	r := bindOf(t, `<template><subform name="f">
		<subform name="Gone"><bind match="dataRef" ref="$data.missing"/>
			<field name="A"><bind match="dataRef" ref="Anything"/><ui><textEdit/></ui></field>
		</subform>
	</subform></template>`, wrap(`<r><Gone/></r>`))
	want(t, r, `f.Gone.A=-`)
}

// TestAFieldMatchedToAGroupTakesNoValue: XFA warns that the two are not the
// same kind of thing; the field is bound, and empty.
func TestAFieldMatchedToAGroupTakesNoValue(t *testing.T) {
	r := bindOf(t, `<template><subform name="f">
		<field name="A"><bind match="dataRef" ref="$record.A"/><ui><textEdit/></ui></field>
	</subform></template>`,
		wrap(`<rec><A><Inner>x</Inner></A></rec>`))
	want(t, r, `f.A=`)
	if !r.Fields[0].Bound {
		t.Error("it should still be bound")
	}
}

// TestAMultiSelectListKeepsEveryChoice, one per line.
func TestAMultiSelectListKeepsEveryChoice(t *testing.T) {
	r := bindOf(t, `<template><subform name="f">
		<field name="A"><bind match="dataRef" ref="$record.A"/>
			<ui><choiceList open="multiSelect"/></ui></field>
	</subform></template>`,
		wrap(`<rec><A><value>red</value><value>blue</value></A></rec>`))
	want(t, r, `f.A=red
blue`)
}

// TestAnExclusionGroupCarriesTheChoiceItsButtonsShare.
func TestAnExclusionGroupCarriesTheChoiceItsButtonsShare(t *testing.T) {
	r := bindOf(t, `<template><subform name="f">
		<exclGroup name="Sex">
			<field name="M"><ui><checkButton/></ui></field>
			<field name="F"><ui><checkButton/></ui></field>
		</exclGroup>
	</subform></template>`, wrap(`<r><Sex>F</Sex></r>`))
	want(t, r, `f.Sex=F`)
}

// TestAnExclusionGroupWithNoDataStillLetsItsButtonsBind.
func TestAnExclusionGroupWithNoDataStillLetsItsButtonsBind(t *testing.T) {
	r := bindOf(t, `<template><subform name="f">
		<exclGroup name="Sex">
			<field name="M"><ui><checkButton/></ui></field>
		</exclGroup>
	</subform></template>`, wrap(`<r><M>1</M></r>`))
	want(t, r, `
f.Sex=-
f.Sex.M=1`)
}

// TestASubformSetAndAnAreaAreWalkedThrough. Neither is a thing a person sees;
// both hold fields, and a binder that stopped at them would lose the form.
func TestASubformSetAndAnAreaAreWalkedThrough(t *testing.T) {
	r := bindOf(t, `<template><subform name="f">
		<subformSet name="Set"><subform name="Inner">
			<field name="A"><ui><textEdit/></ui></field>
		</subform></subformSet>
		<area name="Block"><field name="B"><ui><textEdit/></ui></field></area>
	</subform></template>`,
		wrap(`<r><Set><Inner><A>in a set</A></Inner></Set><Block><B>in an area</B></Block></r>`))
	want(t, r, `
f.Set.Inner.A=in a set
f.Block.B=in an area`)
}

// TestAContainerWithNoNameAddsNothingToThePath.
func TestAContainerWithNoNameAddsNothingToThePath(t *testing.T) {
	r := bindOf(t, `<template><subform name="f">
		<subform><field name="A"><ui><textEdit/></ui></field></subform>
	</subform></template>`, wrap(`<r><A>x</A></r>`))
	want(t, r, `f.A=x`)
}

// TestFieldsUnderAPageAreaAreFound. pdf.js binds nothing under a pageArea,
// because it lays those out separately; measured over the corpus, 628 of
// 82 386 template fields sit there, and one whole form has no others.
func TestFieldsUnderAPageAreaAreFound(t *testing.T) {
	r := bindOf(t, `<template><subform name="f">
		<pageSet name="Sheets"><pageArea name="Page1">
			<field name="A"><ui><textEdit/></ui></field>
		</pageArea></pageSet>
	</subform></template>`, wrap(`<r><A>on the page</A></r>`))
	want(t, r, `f.Sheets.Page1.A=on the page`)
}

// TestADrawIsNotAField: a caption carries a value and never a bound one.
func TestADrawIsNotAField(t *testing.T) {
	r := bindOf(t, `<template><subform name="f">
		<draw name="Title"><value><text>Application</text></value></draw>
		<field name="A"><ui><textEdit/></ui></field>
	</subform></template>`, wrap(`<r><A>x</A></r>`))
	want(t, r, `f.A=x`)
}

// TestAFormWithNothingFilledIn still says which fields there are. A blank
// document is the ordinary state of a form somebody has just downloaded.
func TestAFormWithNothingFilledIn(t *testing.T) {
	r := bindOf(t, `<template><subform name="f">
		<field name="A"><ui><textEdit/></ui></field>
		<subform name="Inner"><field name="B"><ui><textEdit/></ui></field></subform>
	</subform></template>`, wrap(``))
	want(t, r, `
f.A=-
f.Inner.B=-`)
	if len(r.Values()) != 0 {
		t.Errorf("Values: %v", r.Values())
	}
}

// TestMergingIntoNothingStillTakesEachNodeOnce: with no records at all the
// merge is by template rather than by consumption, and a name is answered by
// the first node that has it.
func TestMergingIntoEmptyDataMatchesTheTemplate(t *testing.T) {
	tmpl := parse(t, `<template><subform name="f">
		<field name="A"><ui><textEdit/></ui></field>
		<field name="A"><ui><textEdit/></ui></field>
	</subform></template>`)
	// A data tree that exists but holds no record: emptyMerge, so nothing is
	// consumed away from anybody.
	r := Bind(tmpl, &Node{Kind: "data", Attr: map[string]string{}})
	want(t, r, `
f.A=-
f.A[1]=-`)
}

// TestATemplateThatSaysMatchTemplate turns consumption off: two fields of one
// name both read the same node.
func TestATemplateThatSaysMatchTemplate(t *testing.T) {
	r := bindOf(t, `<template><subform name="f" mergeMode="matchTemplate">
		<field name="A"><ui><textEdit/></ui></field>
		<field name="A"><ui><textEdit/></ui></field>
		<subform name="Inner"><field name="B"><ui><textEdit/></ui></field></subform>
	</subform></template>`,
		wrap(`<r><A>shared</A><Inner><B>deep</B></Inner></r>`))
	want(t, r, `
f.A=shared
f.A[1]=shared
f.Inner.B=deep`)
}

// TestMatchTemplateStillDropsAContainerThatMayBeAbsent.
func TestMatchTemplateStillDropsAContainerThatMayBeAbsent(t *testing.T) {
	r := bindOf(t, `<template><subform name="f" mergeMode="matchTemplate">
		<subform name="Absent"><occur min="0" max="1"/>
			<field name="B"><ui><textEdit/></ui></field>
		</subform>
		<subform name="Required"><occur min="1" max="1"/>
			<field name="C"><ui><textEdit/></ui></field>
		</subform>
	</subform></template>`, wrap(`<r><Other/></r>`))
	want(t, r, `f.Required.C=-`)
}

// TestATemplateWhoseFirstChildIsNotASubform: nothing has said how to merge,
// so nothing is consumed.
func TestATemplateWhoseFirstChildIsNotASubform(t *testing.T) {
	r := bindOf(t, `<template>
		<field name="A"><ui><textEdit/></ui></field>
		<field name="A"><ui><textEdit/></ui></field>
	</template>`, wrap(`<A>loose</A>`))
	want(t, r, `
A=loose
A[1]=loose`)
}

// TestBindingNothingAtAll.
func TestBindingNothingAtAll(t *testing.T) {
	if r := Bind(nil, nil); len(r.Fields) != 0 || len(r.Unsupported) != 0 || r.Truncated {
		t.Errorf("%+v", r)
	}
	tmpl := parse(t, `<template><subform name="f"><field name="A"><ui><textEdit/></ui></field></subform></template>`)
	// No data at all is not the same as empty data, and it must not panic.
	want(t, Bind(tmpl, nil), `f.A=-`)
}

// TestATemplateThatNestsPastWhatIsRead stops rather than following it. The
// reader caps a parsed template at the same depth; this guards a tree built
// some other way.
func TestATemplateThatNestsPastWhatIsRead(t *testing.T) {
	root := &Node{Kind: "template", Attr: map[string]string{}}
	at := root
	for i := 0; i < maxDepth+2; i++ {
		next := &Node{Kind: "subform", Attr: map[string]string{"name": fmt.Sprintf("s%d", i)}}
		at.Kids = append(at.Kids, next)
		at = next
	}
	at.Kids = append(at.Kids, &Node{Kind: "field", Attr: map[string]string{"name": "Deep"}})
	r := Bind(root, nil)
	for _, f := range r.Fields {
		if f.Name == "Deep" {
			t.Error("it followed the tree past the depth it reads")
		}
	}
}

// TestAFormThatProducesMoreFieldsThanAreHeld stops and says so. A dynamic
// form repeats once per record, so the count is the data's to choose.
func TestAFormThatProducesMoreFieldsThanAreHeld(t *testing.T) {
	tmpl := parse(t, `<template><subform name="f">
		<field name="A"><occur min="1" max="-1"/><ui><textEdit/></ui></field>
		<field name="B"><ui><textEdit/></ui></field>
	</subform></template>`)
	rec := &Node{Kind: "rec"}
	for i := 0; i <= maxBindings; i++ {
		rec.Kids = append(rec.Kids, &Node{Kind: "A", Text: "x"})
	}
	r := Bind(tmpl, &Node{Kind: "data", Kids: []*Node{rec}})
	if !r.Truncated {
		t.Fatal("it did not stop")
	}
	if len(r.Fields) != maxBindings {
		t.Errorf("it held %d fields, want %d", len(r.Fields), maxBindings)
	}
}

// TestTwoBranchesThatLandOnOnePath: Values keeps the first, Fields keeps both.
func TestTwoBranchesThatLandOnOnePath(t *testing.T) {
	r := bindOf(t, `<template><subform name="f" mergeMode="matchTemplate">
		<subform><field name="A"><ui><textEdit/></ui></field></subform>
		<subform><field name="A"><ui><textEdit/></ui></field></subform>
	</subform></template>`, wrap(`<r><A>once</A></r>`))
	if len(r.Fields) != 2 {
		t.Fatalf("%d fields", len(r.Fields))
	}
	if v := r.Values(); len(v) != 1 || v["f.A"] != "once" {
		t.Errorf("Values: %v", v)
	}
}

// TestRichTextInAValueIsReadAsItsWords. A form may hold what a person typed
// into a rich-text box as XHTML; this reads what it says, not how it looks.
func TestRichTextInAValueIsReadAsItsWords(t *testing.T) {
	r := bindOf(t, `<template><subform name="f">
		<field name="A"><ui><textEdit/></ui></field>
	</subform></template>`,
		wrap(`<r><A><body xmlns="http://www.w3.org/1999/xhtml"><p>two</p><p>lines</p></body></A></r>`))
	want(t, r, `f.A=two lines`)
}

// TestANodeThatSaysWhichKindItIs. LiveCycle writes xfa:dataNode on every node
// of the datasets it produces, and it overrides what the shape suggests.
func TestANodeThatSaysWhichKindItIs(t *testing.T) {
	r := bindOf(t, `<template><subform name="f">
		<field name="Group"><ui><textEdit/></ui></field>
		<subform name="Value"><field name="Inner"><ui><textEdit/></ui></field></subform>
	</subform></template>`,
		wrap(`<r><Group xfa:dataNode="dataGroup"/>`+
			`<Value xfa:dataNode="dataValue"><Inner>ignored</Inner></Value></r>`))
	// Group says it is a group, so the field of the same name does not take
	// it; Value says it is a value, so the subform of that name does not go
	// into it.
	want(t, r, `
f.Group=-
f.Value.Inner=-`)
}

func TestOccurDefaultsAreNotSymmetrical(t *testing.T) {
	for _, c := range []struct {
		occur    string
		min, max int
	}{
		{`<occur/>`, 1, 1},
		{`<occur min="3"/>`, 3, 3},
		{`<occur max="4"/>`, 1, 4},
		{`<occur min="2" max="5"/>`, 2, 5},
		{`<occur min="7" max="2"/>`, 7, 7},
		{`<occur min="0" max="-1"/>`, 0, math.MaxInt},
		{`<occur min="many" max="lots"/>`, 1, math.MaxInt},
	} {
		n := parse(t, `<template><subform name="s">`+c.occur+`</subform></template>`).Child("subform")
		if min, max := occurInfo(n); min != c.min || max != c.max {
			t.Errorf("%s: got [%d %d], want [%d %d]", c.occur, min, max, c.min, c.max)
		}
	}
	// A container with no name repeats exactly once whatever its occur says.
	n := parse(t, `<template><subform><occur min="0" max="-1"/></subform></template>`).Child("subform")
	if min, max := occurInfo(n); min != 1 || max != 1 {
		t.Errorf("nameless: got [%d %d]", min, max)
	}
}

func TestTheFourWaysABindMayMatch(t *testing.T) {
	for text, kind := range map[string]string{
		`<bind/>`:                  "once",
		`<bind match="once"/>`:     "once",
		`<bind match="global"/>`:   "global",
		`<bind match="dataRef"/>`:  "dataRef",
		`<bind match="none"/>`:     "none",
		`<bind match="sideways"/>`: "once",
	} {
		n := parse(t, `<template>`+text+`</template>`).Child("bind")
		if got := matchOf(n); got != kind {
			t.Errorf("%s: got %q, want %q", text, got, kind)
		}
	}
}

// TestARefThatMatchesMoreThanTheContainerMayShow keeps the first few: occur
// says how many records the form holds, whatever the data offers.
func TestARefThatMatchesMoreThanTheContainerMayShow(t *testing.T) {
	r := bindOf(t, `<template><subform name="f">
		<subform name="Row"><occur min="1" max="2"/>
			<bind match="dataRef" ref="$record.P[*]"/>
			<field name="N"><ui><textEdit/></ui></field>
		</subform>
	</subform></template>`,
		wrap(`<rec><P><N>a</N></P><P><N>b</N></P><P><N>c</N></P></rec>`))
	want(t, r, `
f.Row.N=a
f.Row[1].N=b`)
}

// TestOneAttributeReadTwiceIsOneNode, so that taking it once takes it for
// good and its path is the same both times.
func TestOneAttributeReadTwiceIsOneNode(t *testing.T) {
	r := bindOf(t, `<template><subform name="f">
		<field name="A"><bind match="dataRef" ref="$record.Head.Cerfa"/><ui><textEdit/></ui></field>
		<field name="B"><bind match="dataRef" ref="$record.Head.Cerfa"/><ui><textEdit/></ui></field>
	</subform></template>`,
		wrap(`<rec><Head Cerfa="N 11055"/></rec>`))
	want(t, r, `
f.A=N 11055
f.B=-`)
	if p := r.Fields[0].DataPath; p != "rec.Head.@Cerfa" {
		t.Errorf("the data path is %q", p)
	}
}

// TestAPageAreaWithNoNameAddsNothingToThePath.
func TestAPageAreaWithNoNameAddsNothingToThePath(t *testing.T) {
	r := bindOf(t, `<template><subform name="f">
		<pageSet><pageArea><field name="A"><ui><textEdit/></ui></field></pageArea></pageSet>
	</subform></template>`, wrap(`<r><A>x</A></r>`))
	want(t, r, `f.A=x`)
}

// TestANodeThatCallsItselfAValueButHoldsElements takes its own text and
// leaves what is inside it alone. A form that says xfa:dataNode="dataValue"
// has said what it means, whatever the shape suggests.
func TestANodeThatCallsItselfAValueButHoldsElements(t *testing.T) {
	r := bindOf(t, `<template><subform name="f">
		<field name="A"><ui><textEdit/></ui></field>
	</subform></template>`,
		wrap(`<r><A xfa:dataNode="dataValue">the text<Inner>not this</Inner></A></r>`))
	want(t, r, `f.A=the text`)
}
