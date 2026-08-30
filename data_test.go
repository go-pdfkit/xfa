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
