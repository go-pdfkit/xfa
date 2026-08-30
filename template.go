// Copyright (c) 2026, the go-pdfkit/xfa authors
// All rights reserved.
//
// SPDX-License-Identifier: BSD-3-Clause

package xfa

import (
	"encoding/xml"
	"fmt"
	"io"
	"strings"
)

// A Node is one element of a template, with the children it contains.
//
// The tree is kept whole rather than reduced to the elements this package
// understands today. XFA has some hundreds of element types and a form uses a
// few dozen; throwing the rest away at parse time would mean re-parsing to add
// each one, and would lose the ordering that layout depends on.
type Node struct {
	// Kind is the element's name, without its namespace: "subform", "field",
	// "draw", "pageArea".
	Kind string
	// Attr are its attributes, by name without namespace.
	Attr map[string]string
	// Text is the character data directly inside it, with surrounding space
	// trimmed. A <text> element's caption lives here.
	Text string
	// Kids are the elements inside it, in order. Order is not decoration: a
	// subform laid out top-to-bottom places its children in it.
	Kids []*Node
}

// Get returns an attribute, or the empty string.
func (n *Node) Get(name string) string {
	if n == nil {
		return ""
	}
	return n.Attr[name]
}

// Child is the first child of that kind, or nil. Most of the template is
// singular — a field has one ui, one border, one caption — so looking one up
// by name is how it is read.
func (n *Node) Child(kind string) *Node {
	if n == nil {
		return nil
	}
	for _, k := range n.Kids {
		if k.Kind == kind {
			return k
		}
	}
	return nil
}

// Children are every child of that kind, in order.
func (n *Node) Children(kind string) []*Node {
	if n == nil {
		return nil
	}
	var out []*Node
	for _, k := range n.Kids {
		if k.Kind == kind {
			out = append(out, k)
		}
	}
	return out
}

// Walk calls visit on the node and everything under it, depth first, in
// document order.
func (n *Node) Walk(visit func(*Node)) {
	if n == nil {
		return
	}
	visit(n)
	for _, k := range n.Kids {
		k.Walk(visit)
	}
}

// maxDepth is how deep a template may nest before this stops following it. A
// form built by a designer runs to a dozen levels; anything past this is a
// file playing games rather than a form.
const maxDepth = 256

// ParseTemplate reads the template part of an XFA package.
//
// It is lenient in one direction and strict in the other. Namespaces are
// dropped, because a template writes the same element under xfa-template and
// under no namespace at all depending on who produced it, and the difference
// carries nothing. Processing instructions and comments are skipped: a
// template written by Adobe's designer carries hundreds of them recording what
// the designer did, which is not part of the form.
//
// A document whose XML does not close is refused. Half a template is not a
// form, and laying one out would put half a form on paper without saying so.
func ParseTemplate(r io.Reader) (*Node, error) {
	root, err := parseXML(r)
	if err != nil {
		return nil, fmt.Errorf("xfa: reading the template: %w", err)
	}
	if root.Kind != "template" {
		return nil, fmt.Errorf("xfa: this is a <%s>, not a template", root.Kind)
	}
	return root, nil
}

// parseXML reads one part of an XFA package into a tree. Both parts are XML of
// the same shape, so both are read the same way; what distinguishes them is
// what the caller then expects at the root.
//
// Its errors carry no package prefix. The caller knows which part it asked
// for and says so, which is the half a reader of the message needs.
func parseXML(r io.Reader) (*Node, error) {
	dec := xml.NewDecoder(r)
	// A template may name entities the reader does not carry; treating an
	// unknown one as itself keeps a form readable rather than refusing it over
	// a stray &nbsp;.
	dec.Strict = false
	dec.Entity = xml.HTMLEntity

	var stack []*Node
	var root *Node
	for {
		tok, err := dec.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		switch t := tok.(type) {
		case xml.StartElement:
			if len(stack) >= maxDepth {
				return nil, fmt.Errorf("it nests deeper than %d elements", maxDepth)
			}
			n := &Node{Kind: t.Name.Local, Attr: map[string]string{}}
			for _, a := range t.Attr {
				if a.Name.Space == "xmlns" || a.Name.Local == "xmlns" {
					continue
				}
				n.Attr[a.Name.Local] = a.Value
			}
			if len(stack) == 0 {
				if root != nil {
					return nil, fmt.Errorf("there is more than one root")
				}
				root = n
			} else {
				parent := stack[len(stack)-1]
				parent.Kids = append(parent.Kids, n)
			}
			stack = append(stack, n)
		case xml.EndElement:
			// The stack cannot be empty here: the reader refuses an end tag
			// with nothing open before this sees it. It is lenient about a
			// MISmatched one, though — <subform> closed by </template> pops
			// the subform and then the template — which is why a file written
			// carelessly still reads as the form it meant.
			stack = stack[:len(stack)-1]
		case xml.CharData:
			if len(stack) == 0 {
				continue
			}
			if s := strings.TrimSpace(string(t)); s != "" {
				n := stack[len(stack)-1]
				if n.Text == "" {
					n.Text = s
				} else {
					n.Text += " " + s
				}
			}
		}
	}
	// The stack is balanced by now, whatever the file did: an element left
	// open is an unexpected EOF, which the reader reports above, and a
	// mismatched close pops what is open rather than leaving it.
	if root == nil {
		return nil, fmt.Errorf("there is nothing here")
	}
	return root, nil
}
