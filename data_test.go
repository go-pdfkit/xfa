// Copyright (c) 2026, the go-pdfkit/xfa authors
// All rights reserved.
//
// SPDX-License-Identifier: BSD-3-Clause

package xfa

import (
	"reflect"
	"strings"
	"testing"
)

// filled is the datasets part of the corpus's simplest form, verbatim in
// shape: the data tree mirrors the template's names.
const filled = `<xfa:datasets xmlns:xfa="http://www.xfa.org/schema/xfa-data/1.0/">
<xfa:data><form1><Number1>1.00000000</Number1><Number2>2.00000000</Number2></form1></xfa:data>
</xfa:datasets>`

func TestWhatAFormHasBeenFilledWith(t *testing.T) {
	data, err := ParseDatasets(strings.NewReader(filled))
	if err != nil {
		t.Fatal(err)
	}
	// The <data> element comes back, not the <datasets> wrapper: every path
	// into the form starts below it.
	if data.Kind != "data" {
		t.Fatalf("got a <%s>", data.Kind)
	}
	got := Values(data)
	want := map[string]string{
		"form1":         "",
		"form1.Number1": "1.00000000",
		"form1.Number2": "2.00000000",
	}
	// A group with no text of its own has no entry.
	delete(want, "form1")
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
	if v, ok := Value(data, "form1.Number1"); !ok || v != "1.00000000" {
		t.Errorf("Value = %q, %v", v, ok)
	}
	if _, ok := Value(data, "form1.Nothing"); ok {
		t.Error("a field that is not there was found")
	}
}

func TestAGroupThatAlsoCarriesAValue(t *testing.T) {
	// A node with children may carry text as well — a value beside its own
	// subtree — and it belongs to the group's own name.
	data, err := ParseDatasets(strings.NewReader(
		`<datasets><data><g>mine<kid>theirs</kid></g></data></datasets>`))
	if err != nil {
		t.Fatal(err)
	}
	got := Values(data)
	if got["g"] != "mine" || got["g.kid"] != "theirs" {
		t.Errorf("got %v", got)
	}
}

func TestADataNodeNamedByAnAttribute(t *testing.T) {
	// A data node is usually named by its element, which is the opposite of
	// the template. Where it does carry a name, that wins.
	data, err := ParseDatasets(strings.NewReader(
		`<datasets><data><item name="Real">v</item></data></datasets>`))
	if err != nil {
		t.Fatal(err)
	}
	if got := Values(data); got["Real"] != "v" {
		t.Errorf("got %v", got)
	}
}

func TestWhatIsNotADatasets(t *testing.T) {
	for _, tc := range []struct{ name, src, want string }{
		{"a template", `<template/>`, "not datasets"},
		{"datasets with no data", `<datasets/>`, "carry no data"},
		{"nothing at all", ``, "nothing here"},
		{"half of it", `<datasets><data>`, "reading the datasets"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ParseDatasets(strings.NewReader(tc.src))
			if err == nil {
				t.Fatal("it was read as datasets")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("it said %q", err)
			}
		})
	}
}

func TestTheFieldsATemplateNames(t *testing.T) {
	// The other half of reading a filled form: the data says what is in the
	// fields, the template says which fields there are and in what order a
	// person meets them.
	tmpl := parse(t, simplest)
	got := FieldNames(tmpl)
	want := []string{"form1.Number1", "form1.Sum"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestNamesFromNothing(t *testing.T) {
	if got := FieldNames(nil); got != nil {
		t.Errorf("a template that is not there named %v", got)
	}
	if got := Values(nil); len(got) != 0 {
		t.Errorf("data that is not there held %v", got)
	}
}

func TestFieldsInsideTheContainersThatHoldThem(t *testing.T) {
	// A form nests: a subform holds a subformSet holds an exclGroup holds the
	// radio buttons. A walk that only follows subforms loses them — and a walk
	// that skips page areas loses more: of the 212 fields in one real form,
	// every single one sits under a pageArea.
	tmpl := parse(t, `<template><subform name="f">`+
		`<subformSet name="set"><subform name="inner"><field name="deep"/></subform></subformSet>`+
		`<exclGroup name="choice"><field name="yes"/><field name="no"/></exclGroup>`+
		`<area name="a"><field name="framed"/></area>`+
		`<pageSet><pageArea name="p1"><subform name="banner"><field name="onEveryPage"/></subform></pageArea></pageSet>`+
		`</subform></template>`)
	got := FieldNames(tmpl)
	want := []string{"f.set.inner.deep", "f.choice.yes", "f.choice.no", "f.a.framed",
		"f.p1.banner.onEveryPage"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestRepeatedSiblingsAreNumberedAtEveryDepth(t *testing.T) {
	// A repeating subform carries a table, and a table repeats at more than
	// one depth: rows within a table, cells within a row, and the table itself
	// where a form holds two of them. Naming them without an index collides
	// them, and every one but the last is gone with nothing to say it was ever
	// there — 10 125 values across the corpus.
	data, err := ParseDatasets(strings.NewReader(
		`<datasets><data><form1>` +
			`<Table><Row><Cell>a</Cell><Cell>b</Cell></Row>` +
			`<Row><Cell>c</Cell><Cell>d</Cell></Row>` +
			`<Row><Cell>e</Cell></Row></Table>` +
			`<Table><Row><Cell>f</Cell></Row></Table>` +
			`</form1></data></datasets>`))
	if err != nil {
		t.Fatal(err)
	}
	got := Values(data)
	// The indices count from zero and the first of a name goes unindexed,
	// which is how a SOM expression addresses a repeat and how Bind writes a
	// DataPath.
	want := map[string]string{
		"form1.Table.Row.Cell":       "a",
		"form1.Table.Row.Cell[1]":    "b",
		"form1.Table.Row[1].Cell":    "c",
		"form1.Table.Row[1].Cell[1]": "d",
		"form1.Table.Row[2].Cell":    "e",
		"form1.Table[1].Row.Cell":    "f",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
	if v, ok := Value(data, "form1.Table.Row[1].Cell[1]"); !ok || v != "d" {
		t.Errorf("Value = %q, %v", v, ok)
	}
}

func TestRepeatedRecordsAtTheRootAreKept(t *testing.T) {
	// The root of the data tree is numbered too: a package may hold more than
	// one record, and they are siblings like any other.
	data, err := ParseDatasets(strings.NewReader(
		`<datasets><data><r>one</r><r>two</r><r>three</r></data></datasets>`))
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{"r": "one", "r[1]": "two", "r[2]": "three"}
	if got := Values(data); !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestANameThatCarriesAPathsOwnPunctuation(t *testing.T) {
	// Numbering the siblings cannot help when a name holds the punctuation a
	// path is built from: <A.B> reaches the path <A><B> has taken, and a name
	// attribute may be written with an index in it. Neither is a reason to
	// drop the value, so the later one moves along.
	for _, tc := range []struct {
		name, src string
		want      map[string]string
	}{
		{"a dot inside an element name",
			`<A><B>x</B></A><A.B>y</A.B>`,
			map[string]string{"A.B": "x", "A.B[1]": "y"}},
		{"an index inside a name attribute",
			`<A>one</A><A>two</A><item name="A[1]">three</item>`,
			map[string]string{"A": "one", "A[1]": "two", "A[1][1]": "three"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			data, err := ParseDatasets(strings.NewReader(
				`<datasets><data>` + tc.src + `</data></datasets>`))
			if err != nil {
				t.Fatal(err)
			}
			if got := Values(data); !reflect.DeepEqual(got, tc.want) {
				t.Errorf("got %v, want %v", got, tc.want)
			}
		})
	}
}

func TestValuesAgreeWithTheBindersDataPaths(t *testing.T) {
	// The two halves of the package name a data node the same way. A caller
	// holding a Binding can look its neighbours up in Values without
	// translating, which is the whole point of borrowing the binder's scheme.
	data, err := ParseDatasets(strings.NewReader(
		`<datasets><data><form1><Row><C>a</C></Row><Row><C>b</C></Row></form1></data></datasets>`))
	if err != nil {
		t.Fatal(err)
	}
	tmpl := parse(t, `<template><subform name="form1">`+
		`<subform name="Row"><field name="C"/></subform>`+
		`<subform name="Row"><field name="C"/></subform>`+
		`</subform></template>`)
	values := Values(data)
	seen := 0
	for _, f := range Bind(tmpl, data).Fields {
		if !f.Bound {
			continue
		}
		seen++
		if v, ok := values[f.DataPath]; !ok || v != f.Value {
			t.Errorf("%s: Values has %q, %v; the binding says %q", f.DataPath, v, ok, f.Value)
		}
	}
	if seen != 2 {
		t.Fatalf("%d fields bound, want 2", seen)
	}
}
