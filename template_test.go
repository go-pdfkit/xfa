// Copyright (c) 2026, the go-pdfkit/xfa authors
// All rights reserved.
//
// SPDX-License-Identifier: BSD-3-Clause

package xfa

import (
	"strings"
	"testing"
)

// simplest is the shape every template has, taken from the smallest real one:
// a subform holding the page geometry and the fields.
const simplest = `<template xmlns="http://www.xfa.org/schema/xfa-template/3.3/">
<subform layout="tb" locale="en_US" name="form1">
  <pageSet><pageArea id="Page1" name="Page1">
    <contentArea h="756pt" w="576pt" x="0.25in" y="0.25in"/>
    <medium long="792pt" short="612pt" stock="default"/>
  </pageArea></pageSet>
  <subform h="756pt" w="576pt">
    <field h="9mm" name="Number1" w="62mm">
      <ui><numericEdit><border><edge/></border></numericEdit></ui>
      <caption><value><text>First number</text></value></caption>
    </field>
    <field access="readOnly" h="9mm" name="Sum" w="62mm"><ui><numericEdit/></ui></field>
  </subform>
</subform>
</template>`

func parse(t *testing.T, src string) *Node {
	t.Helper()
	n, err := ParseTemplate(strings.NewReader(src))
	if err != nil {
		t.Fatalf("parsing: %v", err)
	}
	return n
}

func TestATemplateIsReadAsATree(t *testing.T) {
	root := parse(t, simplest)
	if root.Kind != "template" {
		t.Fatalf("the root is a <%s>", root.Kind)
	}
	form := root.Child("subform")
	if form == nil {
		t.Fatal("no subform under the template")
	}
	if form.Get("layout") != "tb" || form.Get("name") != "form1" {
		t.Errorf("the subform is %v", form.Attr)
	}
	// The page geometry, three levels down.
	area := form.Child("pageSet").Child("pageArea").Child("contentArea")
	if area == nil {
		t.Fatal("no content area")
	}
	if got := measureOr(area.Get("w"), 0).Points(); got != 576 {
		t.Errorf("the content area is %v points wide", got)
	}
	if got := measureOr(area.Get("x"), 0).Points(); got != 18 {
		t.Errorf("its origin is at x=%v points", got)
	}
	// The fields, in the order the form places them.
	body := form.Children("subform")
	if len(body) != 1 {
		t.Fatalf("%d body subforms", len(body))
	}
	fields := body[0].Children("field")
	if len(fields) != 2 {
		t.Fatalf("%d fields", len(fields))
	}
	if fields[0].Get("name") != "Number1" || fields[1].Get("name") != "Sum" {
		t.Errorf("the fields came out as %q and %q",
			fields[0].Get("name"), fields[1].Get("name"))
	}
	// A caption is text, and it is where a label comes from.
	cap := fields[0].Child("caption").Child("value").Child("text")
	if cap == nil || cap.Text != "First number" {
		t.Errorf("the caption is %+v", cap)
	}
}

func TestNamespacesAreDropped(t *testing.T) {
	// A template writes the same element under xfa-template and under no
	// namespace at all depending on who produced it, and the difference
	// carries nothing.
	root := parse(t, `<xfa:template xmlns:xfa="http://www.xfa.org/schema/xfa-template/3.3/">`+
		`<xfa:subform xfa:name="one"/></xfa:template>`)
	sub := root.Child("subform")
	if sub == nil {
		t.Fatal("the namespaced subform was not found")
	}
	if sub.Get("name") != "one" {
		t.Errorf("its name is %q", sub.Get("name"))
	}
}

func TestWhatADesignerLeavesBehindIsSkipped(t *testing.T) {
	// A template written by Adobe's designer carries hundreds of processing
	// instructions recording what the designer did. They are not the form.
	root := parse(t, `<template><?templateDesigner expand 1?><subform name="a"/>`+
		`<!-- a note --></template>`)
	if len(root.Kids) != 1 || root.Kids[0].Kind != "subform" {
		t.Errorf("the tree came out as %+v", root.Kids)
	}
}

func TestTextIsGatheredWhereverItIsSplit(t *testing.T) {
	// An XML reader hands character data back in pieces, and a caption split
	// across a comment must not lose its second half.
	root := parse(t, `<template><text>one<!--x-->two</text></template>`)
	if got := root.Child("text").Text; got != "one two" {
		t.Errorf("the text came out as %q", got)
	}
}

func TestHalfATemplateIsRefused(t *testing.T) {
	// Half a template is not a form, and laying one out would put half a form
	// on paper without saying so.
	for _, tc := range []struct{ name, src, want string }{
		{"it never closes", `<template><subform>`, "reading the template"},
		{"it closes what it never opened", `</template>`, "reading the template"},
		{"there is more than one", `<template/><template/>`, "more than one root"},
		{"it is not a template", `<config/>`, "not a template"},
		{"there is nothing there", ``, "there is nothing here"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ParseTemplate(strings.NewReader(tc.src))
			if err == nil {
				t.Fatal("it was read as a template")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("it said %q", err)
			}
		})
	}
}

func TestATemplateThatNestsForEver(t *testing.T) {
	// A file playing games rather than a form.
	var b strings.Builder
	b.WriteString("<template>")
	for i := 0; i < maxDepth+10; i++ {
		b.WriteString("<subform>")
	}
	if _, err := ParseTemplate(strings.NewReader(b.String())); err == nil {
		t.Error("a template nested past the limit was read")
	}
}

func TestLookingUpWhatIsNotThere(t *testing.T) {
	// Reading a template means asking for things that are optional, so asking
	// for one that is absent has to be ordinary rather than fatal.
	root := parse(t, simplest)
	var nothing *Node
	if nothing.Child("x") != nil || nothing.Get("x") != "" || nothing.Children("x") != nil {
		t.Error("a node that is not there answered for itself")
	}
	nothing.Walk(func(*Node) { t.Error("a node that is not there was walked") })
	if root.Child("nosuch") != nil || len(root.Children("nosuch")) != 0 {
		t.Error("an element that is not there was found")
	}
}

func TestWhatSurroundsTheTemplate(t *testing.T) {
	// A packet may carry whitespace or a declaration around the template. It
	// is not part of it and is not a reason to refuse it.
	root := parse(t, "\n  <template><subform/></template>\n")
	if root.Kind != "template" || len(root.Kids) != 1 {
		t.Errorf("the tree came out as %+v", root)
	}
}

func TestAMismatchedCloseIsForgiven(t *testing.T) {
	// The reader is lenient about a close that names the wrong element, and
	// that is deliberate: a file written carelessly by a designer still reads
	// as the form it meant. What it must not do is lose the subform.
	root := parse(t, `<template><subform name="a"></template>`)
	if sub := root.Child("subform"); sub == nil || sub.Get("name") != "a" {
		t.Errorf("the subform was lost: %+v", root.Kids)
	}
}

func TestWalkingTheWholeTree(t *testing.T) {
	n := 0
	parse(t, simplest).Walk(func(*Node) { n++ })
	if n != 18 {
		t.Errorf("%d nodes walked, want 18", n)
	}
}
