// Copyright (c) 2026, the go-pdfkit/xfa authors
// All rights reserved.
//
// SPDX-License-Identifier: BSD-3-Clause

package xfa

// A FormNode is one container of the expanded form: one subform, one field,
// one draw, as the document actually holds it rather than as the template
// writes it.
//
// The difference is repetition. A template writes a table row once and says,
// in its <occur>, that there may be many; the document has one row per record.
// [Expand] does that expansion, so a FormNode tree has one node per occurrence
// and a template tree has one per element. Two nodes cloned from the same
// template element share their Template and differ in their Path and their
// value.
type FormNode struct {
	// Template is the element this was made from. Everything the template
	// says that this struct does not carry — x, y, w, h, layout, border,
	// caption — is read from there.
	Template *Node
	// Kind is the element's name: "subform", "field", "draw", "pageArea".
	Kind string
	// Name is what the template calls it, which may be empty.
	Name string
	// Path is where this occurrence sits in the form: dotted names from the
	// root, with an index — "Row[2]" — on the second and later of repeated
	// siblings. It is the same path [Binding] carries, for the containers
	// that carry one.
	Path string
	// Kids are the containers inside it, in the order the form places them.
	Kids []*FormNode
	// Data is the data element this occurrence was bound to, or nil.
	Data *Node
	// Value is what that element says. Empty when nothing was bound.
	Value string
	// Bound says a data node was found for this occurrence.
	Bound bool
}

// Walk calls visit on the node and everything under it, depth first, in the
// order the form places them.
func (n *FormNode) Walk(visit func(*FormNode)) {
	if n == nil {
		return
	}
	visit(n)
	for _, k := range n.Kids {
		k.Walk(visit)
	}
}

// A Form is a template and the data that fills it, joined and expanded: the
// tree layout runs over.
type Form struct {
	// Root is the <template> element's node. Under it sit the form's
	// outermost subform and, inside that, the page set.
	Root *FormNode
	// Unsupported are the <bind> expressions that were not followed, as
	// [Bind] reports them.
	Unsupported []Unsupported
	// Truncated says expansion stopped early. See [Result.Truncated].
	Truncated bool
}

// Expand joins a template to its data and returns the form tree, with every
// container repeated once per record that fills it.
//
// # Why this exists beside [Bind]
//
// [Bind] answers "what is in this form", and a flat list of fields answers it
// well. Layout asks a different question — where does each one go — and the
// answer is in the shape: a field's place is its own x and y added to those of
// every container above it, and a container repeated eleven times is in eleven
// different places. A list of bindings has nowhere to put the eleven.
//
// This follows pdf.js, whose binder does the same thing and for the same
// reason: Binder.bind returns a clone of the template (bind.js:61) into which
// _bindOccurrences (bind.js:385-417) inserts one copy of the container per
// matched data node. Layout there never sees the template either.
//
// # Where it goes further than [Bind]
//
// It descends into an exclGroup. A group of radio buttons carries one value
// between them, so binding stops at the group; but each button is drawn in its
// own place inside the group's box, so layout needs them. They appear with
// Bound false, which is what they are: the value belongs to the group.
//
// It carries <draw> — the static text, lines and boxes a form is mostly made
// of. A draw holds no data, so [Bind] has nothing to say about it and does not
// report it. It is half of what is on the paper.
//
// # Where it stops, as [Bind] does
//
// It does not clone a container to satisfy occur's initial count when the data
// is empty, which pdf.js's _createOccurrences (bind.js:421-462) does. Those
// clones carry no data; they are blank rows on a blank form.
func Expand(template, data *Node) *Form {
	b := newBinder(template, data)
	return &Form{Root: b.root, Unsupported: b.unsupported, Truncated: b.truncated}
}

// formKinds are the elements that become nodes of the expanded form: the ones
// that hold a place on the page. Everything else a template writes — ui,
// caption, border, value, occur — is read from [FormNode.Template] where it is
// wanted, and would only be a second copy of the template here.
var formKinds = map[string]bool{
	"subform":    true,
	"subformSet": true,
	"area":       true,
	"exclGroup":  true,
	"field":      true,
	"draw":       true,
	"pageSet":    true,
	"pageArea":   true,
}

// enter adds one occurrence of a container to the expanded form and makes it
// the parent of whatever is laid out inside it. It returns the parent to put
// back when that is done.
func (b *binder) enter(node *Node, path string) *FormNode {
	prev := b.cur
	fn := &FormNode{Template: node, Kind: node.Kind, Name: node.Get("name"), Path: path}
	prev.Kids = append(prev.Kids, fn)
	b.cur = fn
	return prev
}

// leaf adds a container with nothing laid out inside it.
func (b *binder) leaf(node *Node, path string) {
	b.cur = b.enter(node, path)
}

// shadow adds the containers inside a value-carrying one to the expanded form
// without binding them. See the note in [Expand] about exclGroup.
func (b *binder) shadow(node *Node, path string, depth int) {
	if depth >= maxDepth {
		return
	}
	seen := map[string]int{}
	for _, k := range node.Kids {
		if !formKinds[k.Kind] {
			continue
		}
		p := place(path, k.Get("name"), seen, 1)[0]
		prev := b.enter(k, p)
		b.shadow(k, p, depth+1)
		b.cur = prev
	}
}
