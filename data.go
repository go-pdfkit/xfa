// Copyright (c) 2026, the go-pdfkit/xfa authors
// All rights reserved.
//
// SPDX-License-Identifier: BSD-3-Clause

package xfa

import (
	"fmt"
	"io"
)

// ParseDatasets reads the datasets part of an XFA package — what has been
// filled in — and returns the data under it.
//
// The tree mirrors the template's names. A form whose root subform is called
// form1 and whose field is called Number1 keeps its value at
// <data><form1><Number1>1.00</Number1></form1></data>, which is why the values
// can be found without laying the form out.
//
// What comes back is the <data> element, not the <datasets> wrapper: the
// wrapper carries the package's plumbing, and every path into the form starts
// below it.
func ParseDatasets(r io.Reader) (*Node, error) {
	root, err := parseXML(r)
	if err != nil {
		return nil, fmt.Errorf("xfa: reading the datasets: %w", err)
	}
	if root.Kind != "datasets" {
		return nil, fmt.Errorf("xfa: this is a <%s>, not datasets", root.Kind)
	}
	data := root.Child("data")
	if data == nil {
		return nil, fmt.Errorf("xfa: the datasets carry no data")
	}
	return data, nil
}

// Values are what a form has been filled with, by the whole name of each
// field: "form1.Page1.Line101".
//
// The names are joined with dots, as XFA's own scripting language names them,
// and the root of the data tree is included because a package may hold more
// than one form.
//
// Repeated siblings are numbered the way [Bind] numbers them and the way a SOM
// expression addresses them: the first of a name plain, the rest "Row[1]",
// "Row[2]", counting from zero. Without the numbers they collide, and a table
// of six rows comes back as one — 10 125 of the 71 346 values the corpus
// carries, across 216 of its 560 forms, were lost that way before this counted
// them.
//
// A field a person has not filled in has an empty value rather than no entry.
// The difference matters: "this field exists and is empty" and "there is no
// such field" are different answers, and a caller checking whether a form is
// complete needs the first.
//
// Every value the data carries comes back. There is one entry per leaf and one
// per group that carries text of its own, and no two share a key.
func Values(data *Node) map[string]string {
	out := map[string]string{}
	if data == nil {
		return out
	}
	gather(data, "", out)
	return out
}

// gather walks the children of one data node.
//
// The numbering is per parent and per name, which is what [Bind] does in
// mapTree: siblings are the only nodes that can collide once each parent has a
// path of its own.
func gather(n *Node, prefix string, into map[string]string) {
	seen := map[string]int{}
	for _, kid := range n.Kids {
		name := kid.Get("name")
		if name == "" {
			// A data node is usually named by its ELEMENT rather than by a
			// name attribute — <Number1>1.00</Number1> — which is the opposite
			// of the template, where the element says what a thing is and the
			// attribute says what it is called.
			name = kid.Kind
		}
		// place is the binder's own path builder, so a path from here and a
		// Binding.DataPath for the same node read alike.
		path := place(prefix, name, seen, 1)[0]
		if len(kid.Kids) == 0 {
			set(into, path, kid.Text)
			continue
		}
		// A node with children is a group. It may also carry text — a value
		// beside its own subtree — and that text belongs to the group's own
		// name.
		if kid.Text != "" {
			set(into, path, kid.Text)
		}
		gather(kid, path, into)
	}
}

// set records a value under a path, and never writes over one.
//
// Numbering the siblings makes two nodes of one parent distinct, and a parent
// with a path of its own keeps its subtree apart from anybody else's. What it
// cannot undo is a name that carries the punctuation a path is built from: a
// <A.B> beside an <A><B>, or a node whose name attribute is literally "A[1]",
// reaches a path another node has already taken. That is rare and it is legal,
// and a value dropped there is as lost as a value dropped anywhere else, so
// the later one moves along to the first free index rather than landing on the
// earlier one.
func set(into map[string]string, path, value string) {
	if _, taken := into[path]; !taken {
		into[path] = value
		return
	}
	for i := 1; ; i++ {
		p := fmt.Sprintf("%s[%d]", path, i)
		if _, taken := into[p]; !taken {
			into[p] = value
			return
		}
	}
}

// Value is one field's value, by its whole name. The second and later of
// repeated siblings are addressed by index — "form1.Row[1].Cell".
func Value(data *Node, path string) (string, bool) {
	v, ok := Values(data)[path]
	return v, ok
}

// FieldNames are the paths the TEMPLATE gives its fields, in the order it
// places them.
//
// The path is the template's, and it is NOT necessarily the path the data
// uses. XFA binds a field to its data explicitly, through <bind> elements —
// one real form here carries 222 of them — and a form may name its root
// subform one thing while its data calls it another. Joining the two by path
// is right for a form that binds implicitly and wrong for one that does not,
// which is why this does not try: [Bind] does the joining properly, and hands
// back each field with the value actually bound to it.
//
// Fields are looked for inside page areas as well as inside subforms. That is
// not a nicety: of the 212 fields in one real form, every one sits under a
// pageArea, and a walk that followed only subforms found none of them.
func FieldNames(template *Node) []string {
	var out []string
	var walk func(n *Node, prefix string)
	walk = func(n *Node, prefix string) {
		for _, kid := range n.Kids {
			name := kid.Get("name")
			path := prefix
			if name != "" {
				if path == "" {
					path = name
				} else {
					path = path + "." + name
				}
			}
			if kid.Kind == "field" && name != "" {
				out = append(out, path)
			}
			if containers[kid.Kind] {
				walk(kid, path)
			}
		}
	}
	if template != nil {
		walk(template, "")
	}
	return out
}

// containers are the template elements that hold other elements.
//
// pageSet and pageArea are in the list because a form puts content on the page
// itself — a heading, a frame, a block of fields repeated on every sheet — and
// leaving them out loses whatever is there. It is not a rare shape: of the 212
// fields in one real form, every single one sits under a pageArea.
var containers = map[string]bool{
	"template":   true,
	"subform":    true,
	"subformSet": true,
	"area":       true,
	"exclGroup":  true,
	"pageSet":    true,
	"pageArea":   true,
}
