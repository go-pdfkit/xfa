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
// A field a person has not filled in has an empty value rather than no entry.
// The difference matters: "this field exists and is empty" and "there is no
// such field" are different answers, and a caller checking whether a form is
// complete needs the first.
func Values(data *Node) map[string]string {
	out := map[string]string{}
	if data == nil {
		return out
	}
	for _, kid := range data.Kids {
		gather(kid, "", out)
	}
	return out
}

// gather walks one branch of the data tree.
func gather(n *Node, prefix string, into map[string]string) {
	name := n.Get("name")
	if name == "" {
		// A data node is usually named by its ELEMENT rather than by a name
		// attribute — <Number1>1.00</Number1> — which is the opposite of the
		// template, where the element says what a thing is and the attribute
		// says what it is called.
		name = n.Kind
	}
	path := name
	if prefix != "" {
		path = prefix + "." + name
	}
	if len(n.Kids) == 0 {
		into[path] = n.Text
		return
	}
	// A node with children is a group. It may also carry text — a value beside
	// its own subtree — and that text belongs to the group's own name.
	if n.Text != "" {
		into[path] = n.Text
	}
	for _, kid := range n.Kids {
		gather(kid, path, into)
	}
}

// Value is one field's value, by its whole name.
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
// subform one thing while its data calls it another. Joining these paths to
// the ones [Values] returns is right for a form that binds implicitly and
// wrong for one that does not, which is why this package does not do it: see
// the note in the package documentation about what is not built yet.
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
