// Copyright (c) 2026, the go-pdfkit/xfa authors
// All rights reserved.
//
// SPDX-License-Identifier: BSD-3-Clause

package xfa

import (
	"strings"
	"testing"
)

// paths is every node of an expanded form, as "kind path", so that a test can
// say what the expansion produced in one line each.
func paths(f *Form) []string {
	var out []string
	f.Root.Walk(func(n *FormNode) {
		if n != f.Root {
			out = append(out, n.Kind+" "+n.Path)
		}
	})
	return out
}

func TestExpandRepeatsAContainerOncePerRecord(t *testing.T) {
	// The whole reason the tree exists: the template writes one Row and the
	// data holds three, so the form has three, each in its own place.
	tmpl := parse(t, `<template><subform name="form1">
	  <subform name="Table">
	    <subform name="Row"><occur max="-1" min="1"/>
	      <field name="Qty"/>
	    </subform>
	  </subform>
	</subform></template>`)
	data, err := ParseDatasets(strings.NewReader(
		`<xfa:datasets xmlns:xfa="http://www.xfa.org/schema/xfa-data/1.0/"><xfa:data>
		  <form1><Table>
		    <Row><Qty>1</Qty></Row><Row><Qty>2</Qty></Row><Row><Qty>3</Qty></Row>
		  </Table></form1>
		</xfa:data></xfa:datasets>`))
	if err != nil {
		t.Fatal(err)
	}
	form := Expand(tmpl, data)
	got := strings.Join(paths(form), "\n")
	want := strings.Join([]string{
		"subform form1",
		"subform form1.Table",
		"subform form1.Table.Row",
		"field form1.Table.Row.Qty",
		"subform form1.Table.Row[1]",
		"field form1.Table.Row[1].Qty",
		"subform form1.Table.Row[2]",
		"field form1.Table.Row[2].Qty",
	}, "\n")
	if got != want {
		t.Errorf("the form expanded to\n%s\nwant\n%s", got, want)
	}
	// The three rows are three nodes cloned from one template element, and
	// each carries its own record's value.
	var values []string
	var templates []*Node
	form.Root.Walk(func(n *FormNode) {
		if n.Kind == "field" {
			values = append(values, n.Value)
			templates = append(templates, n.Template)
		}
	})
	if strings.Join(values, ",") != "1,2,3" {
		t.Errorf("the rows carry %v", values)
	}
	if templates[0] != templates[1] || templates[1] != templates[2] {
		t.Error("the three rows were not cloned from the same template element")
	}
	if form.Truncated || form.Unsupported != nil {
		t.Errorf("truncated=%v unsupported=%v", form.Truncated, form.Unsupported)
	}
}

func TestExpandCarriesDrawsAndTheInsideOfAnExclGroup(t *testing.T) {
	// Neither is in [Bind]'s answer: a draw holds no data, and an exclGroup's
	// buttons share the group's one value. Both are on the paper.
	tmpl := parse(t, `<template><subform name="form1">
	  <draw name="Title"/>
	  <draw name="Rule"/>
	  <exclGroup name="Sex"><field name="M"/><field name="F"/></exclGroup>
	</subform></template>`)
	form := Expand(tmpl, nil)
	got := strings.Join(paths(form), "\n")
	want := strings.Join([]string{
		"subform form1",
		"draw form1.Title",
		"draw form1.Rule",
		"exclGroup form1.Sex",
		"field form1.Sex.M",
		"field form1.Sex.F",
	}, "\n")
	if got != want {
		t.Errorf("the form expanded to\n%s\nwant\n%s", got, want)
	}
	// The draws reached no Binding: Bind has nothing to say about them.
	for _, f := range Bind(tmpl, nil).Fields {
		if f.Kind == "draw" {
			t.Errorf("Bind returned a draw: %v", f)
		}
	}
}

func TestExpandCarriesTheInsideOfABoundExclGroup(t *testing.T) {
	// The same group, this time with data. Binding stops at the group — the
	// buttons have no separate value — so the tree is filled in beside it and
	// has to come out the same shape as when nothing was bound.
	tmpl := parse(t, `<template><subform name="form1">
	  <exclGroup name="Sex"><field name="M"/><field name="F"/></exclGroup>
	</subform></template>`)
	data, err := ParseDatasets(strings.NewReader(
		`<xfa:datasets xmlns:xfa="http://www.xfa.org/schema/xfa-data/1.0/"><xfa:data>
		  <form1><Sex>F</Sex></form1>
		</xfa:data></xfa:datasets>`))
	if err != nil {
		t.Fatal(err)
	}
	form := Expand(tmpl, data)
	got := strings.Join(paths(form), "\n")
	want := "subform form1\nexclGroup form1.Sex\nfield form1.Sex.M\nfield form1.Sex.F"
	if got != want {
		t.Errorf("the form expanded to\n%s\nwant\n%s", got, want)
	}
	var group *FormNode
	form.Root.Walk(func(n *FormNode) {
		if n.Kind == "exclGroup" {
			group = n
		}
	})
	if !group.Bound || group.Value != "F" {
		t.Errorf("the group came out bound=%v value=%q", group.Bound, group.Value)
	}
	for _, k := range group.Kids {
		if k.Bound {
			t.Errorf("%s came out bound: the value belongs to the group", k.Path)
		}
	}
}

func TestExpandNumbersDrawsApartFromFields(t *testing.T) {
	// Adding the static text to the tree must not renumber the fields: a
	// caller holding the path "Total" would find it had become "Total[1]".
	tmpl := parse(t, `<template><subform name="form1">
	  <draw name="Total"/><field name="Total"/><draw name="Total"/>
	</subform></template>`)
	got := strings.Join(paths(Expand(tmpl, nil)), "\n")
	want := "subform form1\ndraw form1.Total\nfield form1.Total\ndraw form1.Total[1]"
	if got != want {
		t.Errorf("the form expanded to\n%s\nwant\n%s", got, want)
	}
}

func TestExpandWalksThroughThePageSet(t *testing.T) {
	// A form whose fields all sit on the page area rather than in the body is
	// a real shape, and the page's own furniture has to be in the tree too.
	form := Expand(parse(t, simplest), nil)
	var kinds []string
	form.Root.Walk(func(n *FormNode) { kinds = append(kinds, n.Kind) })
	if strings.Join(kinds, " ") != "template subform pageSet pageArea subform field field" {
		t.Errorf("the form expanded to %v", kinds)
	}
}

func TestExpandOfNothing(t *testing.T) {
	if f := Expand(nil, nil); f.Root != nil {
		t.Errorf("a nil template expanded to %v", f.Root)
	}
	// Walk on a nil node is how a caller of a nil root gets away with it.
	(*FormNode)(nil).Walk(func(*FormNode) { t.Error("visited a nil node") })
}

func TestExpandStopsAtTheDepthLimit(t *testing.T) {
	// The shadow walk inside a value-carrying container has its own recursion
	// and its own guard. A template that nests exclGroups all the way down is
	// a file playing games, not a form.
	var b strings.Builder
	b.WriteString(`<template><subform name="form1"><exclGroup name="g">`)
	for i := 0; i < maxDepth+2; i++ {
		b.WriteString(`<exclGroup name="g">`)
	}
	b.WriteString(`<field name="deep"/>`)
	for i := 0; i < maxDepth+2; i++ {
		b.WriteString(`</exclGroup>`)
	}
	b.WriteString(`</exclGroup></subform></template>`)
	tmpl, err := ParseTemplate(strings.NewReader(b.String()))
	if err != nil {
		t.Skipf("the reader refused it first: %v", err)
	}
	n := 0
	Expand(tmpl, nil).Root.Walk(func(*FormNode) { n++ })
	if n > maxDepth+4 {
		t.Errorf("it followed %d nodes down", n)
	}
}
