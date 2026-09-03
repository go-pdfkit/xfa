// Copyright (c) 2026, the go-pdfkit/xfa authors
// All rights reserved.
//
// SPDX-License-Identifier: BSD-3-Clause

package xfa

import (
	"fmt"
	"math"
	"strconv"
	"strings"
)

// A Binding is one value-carrying container of a form — a field, or the
// exclusion group a set of radio buttons shares — joined to the data node it
// draws its value from.
//
// Every such container appears, filled or not. Bound says which: a field the
// form places but the data never mentions has Bound false and an empty Value,
// and that is a different answer from a field bound to an empty string. A
// caller checking whether a form is complete needs the difference.
type Binding struct {
	// Name is the field's own name, as the template writes it.
	Name string
	// Path is where the FORM puts it: dotted names from the root, with an
	// index — "Row[2]" — on the second and later of repeated siblings. A
	// nameless container contributes nothing to it.
	Path string
	// Kind is "field" or "exclGroup".
	Kind string
	// Node is the template element.
	Node *Node
	// Data is the data element it was bound to, or nil.
	Data *Node
	// DataPath is where that element sits in the DATA, which is a different
	// tree and often a differently named one. Empty when nothing was bound.
	DataPath string
	// Value is what the data says, trimmed. Empty when nothing was bound.
	Value string
	// Bound says a data node was found. A false here with a non-empty form is
	// the ordinary state of a blank document, not an error.
	Bound bool
}

// An Unsupported records a <bind> whose expression this package met and did
// not follow. It is reported rather than swallowed: a binder that quietly
// differs from Adobe on one construct is worse than one that says where it
// stops, because the caller cannot tell a field nobody filled in from a field
// whose value was not looked for.
type Unsupported struct {
	// Field is the form path of the container carrying the <bind>.
	Field string
	// Ref is the expression as written.
	Ref string
	// Why names the construct, in words fit to show somebody.
	Why string
}

// A Result is what a form holds once its template and its data are joined.
type Result struct {
	// Fields are the form's value-carrying containers, in the order the form
	// places them.
	Fields []Binding
	// Unsupported are the <bind> expressions that were not followed. Each one
	// leaves its field unbound.
	Unsupported []Unsupported
	// Truncated says binding stopped early because the form produced more
	// containers than this package will hold. It is a defence against a file
	// built to make a reader work forever, not something a real form does.
	Truncated bool
}

// Values are the bound fields by their form path. Fields that were not bound
// are left out, so a caller filling a document gets what the data actually
// says and nothing invented.
//
// A path repeated by two nameless branches keeps the first. Use [Result.Fields]
// where that matters; it keeps every one, in order.
func (r *Result) Values() map[string]string {
	out := map[string]string{}
	for _, f := range r.Fields {
		if !f.Bound {
			continue
		}
		if _, seen := out[f.Path]; !seen {
			out[f.Path] = f.Value
		}
	}
	return out
}

// maxBindings is how many fields a form may produce before this stops. A
// dynamic form repeats a subform once per record, so the count is driven by
// the data rather than by the template, and a file may say there are a great
// many records.
const maxBindings = 1 << 18

// Bind joins a template to the datasets that fill it and returns each of the
// form's fields with the value bound to it.
//
// # How a field finds its value
//
// XFA does not put the value under the field's own name. The template names a
// field for the layout and the data names it for the record, and the two need
// not agree — of fourteen dynamic forms measured here, two have the same count
// of fields and values and not one path in common. What joins them is either
// an explicit <bind ref="..."> or, far more often, a walk in which each
// container consumes the next unconsumed data node of its own name. Both are
// implemented, following Mozilla's pdf.js, which is the only complete free
// implementation of this.
//
// The <bind> element's match attribute is honoured in all four forms: once
// (the default), global, dataRef and none.
//
// # Which SOM expressions are read
//
// A ref is a SOM expression. These forms are read:
//
//	Name.Sub.Leaf      a path from the current data node, walking up to the
//	                   parent and its parents if the first name is not found
//	Name[2]            the third sibling of that name
//	Name[*]            every sibling of that name
//	$data.Name         rooted at <data>
//	$record.Name       rooted at the first record under <data>
//	$.Name             rooted at the current data node
//	!.data.Name        rooted at <datasets>
//	Parent..Leaf       Leaf anywhere below Parent
//	Parent.#field      by element name rather than by name attribute
//	Name.attr          an attribute reads like a child
//
// These are NOT read, and each one is reported in [Result.Unsupported] rather
// than guessed at:
//
//	Name.[expr]        a FormCalc subexpression
//	Name.(expr)        a JavaScript predicate
//	Name[-1]           a negative index
//	$template, $form, $layout, $host, $event, $connectionSet, $dataWindow,
//	$xfa               references outside the data, which name no value
//
// Measured over 560 real templates carrying 26 573 <bind> elements, none of
// the unsupported forms occurs: every ref is a plain path, a $record path, a
// $ path, or one of those with [*].
//
// # Where this deliberately differs from pdf.js
//
// It does not create data nodes. pdf.js, meeting a ref that matches nothing,
// invents the nodes the expression describes so that a person typing into the
// form has somewhere to put the answer. This reads a document rather than
// editing one, so the field is simply reported unbound — which is the same
// answer, since an invented node is empty.
//
// It does not repeat a container to satisfy occur's initial count. pdf.js,
// merging into empty data, clones a subform until there are as many as the
// template asks for. Those clones carry no data; they change the layout, not
// the values, and this returns values.
//
// It descends into pageSet and pageArea. pdf.js does not bind their contents
// at all, because it lays them out separately — but of one real form's 212
// fields, every single one sits under a pageArea, and dropping them silently
// would lose the whole form.
//
// It stops a global search that has begun to repeat. pdf.js's global lookup
// does not skip nodes it has already taken, so a container with no upper occur
// bound and match="global" asks for the same node forever; this stops at the
// repeat.
//
// A nil template gives an empty result; a nil data tree binds nothing, which
// is what a document nobody has filled in should say.
func Bind(template, data *Node) *Result {
	b := newBinder(template, data)
	return &Result{Fields: b.out, Unsupported: b.unsupported, Truncated: b.truncated}
}

// newBinder does the whole join. It is one walk, and both of the answers it
// can be asked for — the flat list [Bind] returns and the tree [Expand]
// returns — are made as it goes, so the two cannot disagree about which
// containers a form has.
func newBinder(template, data *Node) *binder {
	b := &binder{
		data:     data,
		parent:   map[*Node]*Node{},
		dataPath: map[*Node]string{},
		consumed: map[*Node]bool{},
		attrs:    map[*Node]map[string]*Node{},
		cursor:   map[cursorKey]int{},
	}
	if data != nil {
		b.wrapper = &Node{Kind: "datasets", Attr: map[string]string{}, Kids: []*Node{data}}
		b.parent[data] = b.wrapper
		b.dataPath[data] = ""
		b.mapTree(data, "")
		b.emptyMerge = len(data.Kids) == 0
	} else {
		b.emptyMerge = true
	}
	if template != nil {
		b.root = &FormNode{Template: template, Kind: template.Kind, Name: template.Get("name")}
		b.cur = b.root
		b.bindElement(template, "", data, 0)
	}
	return b
}

// bindableKinds are the template elements that can hold data. It is pdf.js's
// list: a draw carries a value but never a bound one, so it is not here.
var bindableKinds = map[string]bool{
	"subform":    true,
	"subformSet": true,
	"area":       true,
	"exclGroup":  true,
	"field":      true,
}

// settableKinds are those that carry a value of their own rather than passing
// data down to their children.
var settableKinds = map[string]bool{
	"field":     true,
	"exclGroup": true,
}

// pageKinds are walked through without binding anything themselves. See the
// note in [Bind] about why they are walked at all.
var pageKinds = map[string]bool{
	"pageSet":  true,
	"pageArea": true,
}

// A binder carries the state one call to [Bind] needs: which data nodes have
// been taken already, which way the merge is going, and where every data node
// sits.
type binder struct {
	data     *Node
	wrapper  *Node
	parent   map[*Node]*Node
	dataPath map[*Node]string
	consumed map[*Node]bool
	// attrs holds one node per attribute, made on demand, so that an
	// attribute taken as data can be marked consumed and named like any other.
	attrs map[*Node]map[string]*Node
	// cursor is how far a search for one name under one node has already got.
	// Without it a form of n containers over n records reads n^2 nodes, which
	// is what the reference implementation does and what a file built to make
	// a reader work for ever would ask for.
	cursor map[cursorKey]int
	// emptyMerge says the data tree holds no records at all.
	emptyMerge bool
	// mergeMode is nil until the form's first subform says which way to merge.
	mergeMode *bool
	// root is the expanded form, and cur the container being laid out inside.
	// See [Expand].
	root        *FormNode
	cur         *FormNode
	out         []Binding
	unsupported []Unsupported
	truncated   bool
}

// A cursorKey is one search that may be resumed: the same name, of the same
// kind, under the same data node. Everything the search steps over is either
// taken already or the wrong kind, and neither of those is ever undone, so
// beginning again where it stopped gives the same answer.
type cursorKey struct {
	node    *Node
	name    string
	isValue bool
}

// mapTree records where each data node sits, so that a search can walk up as
// well as down. The tree has no parent pointers of its own; a form does not
// need them and a binder cannot do without them.
func (b *binder) mapTree(n *Node, path string) {
	seen := map[string]int{}
	for _, k := range n.Kids {
		i := seen[k.Kind]
		seen[k.Kind] = i + 1
		p := k.Kind
		if i > 0 {
			p = fmt.Sprintf("%s[%d]", k.Kind, i)
		}
		if path != "" {
			p = path + "." + p
		}
		b.parent[k] = n
		b.dataPath[k] = p
		b.mapTree(k, p)
	}
}

// consumeData says each data node is taken by at most one field. It is XFA's
// normal way round, and it is what makes the walk order matter.
func (b *binder) consumeData() bool {
	return !b.emptyMerge && b.mergeMode != nil && *b.mergeMode
}

// bindElement walks one container of the form against one node of the data.
func (b *binder) bindElement(node *Node, path string, data *Node, depth int) {
	if depth >= maxDepth {
		return
	}
	seen := map[string]int{}
	// A draw is numbered apart from the containers that carry data, so that
	// adding the form's static text to the expanded tree cannot move a field's
	// path from "Total" to "Total[1]" behind a caller's back.
	drawn := map[string]int{}
	for _, child := range node.Kids {
		if b.truncated {
			return
		}
		if pageKinds[child.Kind] {
			p := place(path, child.Get("name"), seen, 1)[0]
			prev := b.enter(child, p)
			b.bindElement(child, p, data, depth+1)
			b.cur = prev
			continue
		}
		if child.Kind == "draw" {
			// Static text, rules and boxes: most of what is on the paper, and
			// nothing a binder has an opinion about.
			b.leaf(child, place(path, child.Get("name"), drawn, 1)[0])
			continue
		}
		if b.mergeMode == nil && child.Kind == "subform" {
			// XFA 3.3 p. 182: the outermost subform and the node holding the
			// current record are bound to each other whatever they are called.
			mode := child.Get("mergeMode") != "matchTemplate"
			b.mergeMode = &mode
			if data != nil && len(data.Kids) > 0 {
				b.bindOccurrences(child, path, seen, data.Kids[:1], depth)
			} else if b.emptyMerge {
				b.setAndBind(child, place(path, child.Get("name"), seen, 1)[0], nil, depth)
			}
			continue
		}
		if !bindableKinds[child.Kind] {
			continue
		}
		b.bindChild(child, path, seen, data, depth)
	}
}

// bindChild finds the data for one bindable container and binds it.
func (b *binder) bindChild(child *Node, path string, seen map[string]int, data *Node, depth int) {
	name := child.Get("name")
	// here numbers this container among its siblings and gives it its place in
	// the form's paths. It must be asked once, and every way out of this asks
	// it once.
	here := func() string { return place(path, name, seen, 1)[0] }

	global, ref := false, ""
	if bd := child.Child("bind"); bd != nil {
		switch matchOf(bd) {
		case "none":
			b.setAndBind(child, here(), data, depth)
			return
		case "global":
			global = true
		case "dataRef":
			if ref = bd.Get("ref"); ref == "" {
				b.setAndBind(child, here(), data, depth)
				return
			}
		}
	}
	min, max := occurInfo(child)

	var matches []*Node
	if ref != "" {
		found, why := b.search(data, ref)
		if why != "" {
			b.unsupported = append(b.unsupported, Unsupported{
				Field: place(path, name, map[string]int{}, 1)[0],
				Ref:   ref,
				Why:   why,
			})
		}
		if len(found) == 0 {
			// pdf.js would invent the nodes the expression names; an invented
			// node is empty, so an unbound field says the same thing.
			b.setAndBind(child, here(), nil, depth)
			return
		}
		if b.consumeData() {
			found = b.unconsumed(found)
		}
		if len(found) > max {
			found = found[:max]
		}
		if b.consumeData() {
			for _, n := range found {
				b.consumed[n] = true
			}
		}
		matches = found
	} else {
		if name == "" {
			b.setAndBind(child, path, data, depth)
			return
		}
		matches = b.byName(name, settableKinds[child.Kind], data, global, max)
		if matches == nil && !b.consumeData() {
			if min == 0 {
				// No data and none required: the container is not in the form
				// at all. XFA 3.3 G12.1428332.
				return
			}
			b.setAndBind(child, here(), nil, depth)
			return
		}
	}

	switch {
	case len(matches) > 0:
		b.bindOccurrences(child, path, seen, matches, depth)
	case min > 0:
		b.setAndBind(child, here(), data, depth)
	}
}

// byName finds the data for a container that has no ref, by its name.
func (b *binder) byName(name string, isValue bool, data *Node, global bool, max int) []*Node {
	if !b.consumeData() {
		// Nothing is being consumed, so the first node of that name answers
		// every container that asks for it. An empty merge has nothing to
		// consume either: this invents no data nodes, so there is nothing
		// below the top subform to take.
		found := b.firstByName(data, name, false)
		if found == nil {
			return nil
		}
		return []*Node{found}
	}
	var matches []*Node
	for len(matches) < max {
		found := b.findToConsume(name, isValue, data, global)
		if found == nil || b.consumed[found] {
			// A global search does not skip what it has already handed back,
			// so without this an unbounded occur asks for the same node until
			// the end of time.
			break
		}
		b.consumed[found] = true
		matches = append(matches, found)
	}
	return matches
}

// findToConsume takes the next unconsumed data node of that name, looking in
// the current node, then its parent, then its parent — and, if the bind is
// global, anywhere at all.
func (b *binder) findToConsume(name string, isValue bool, data *Node, global bool) *Node {
	node := data
	for i := 0; i < 3 && node != nil; i++ {
		if m := b.nextUnder(node, name, isValue); m != nil {
			return m
		}
		if node == b.data {
			break
		}
		node = b.parent[node]
	}
	if !global {
		return nil
	}
	if m := b.firstByName(b.data, name, true); m != nil {
		return m
	}
	return b.firstAttr(b.data, name)
}

// nextUnder is the next child of that name and kind that nobody has taken.
func (b *binder) nextUnder(node *Node, name string, isValue bool) *Node {
	key := cursorKey{node, name, isValue}
	i := b.cursor[key]
	for ; i < len(node.Kids); i++ {
		k := node.Kids[i]
		if k.Kind == name && !b.consumed[k] && isValue == isDataValue(k) {
			b.cursor[key] = i
			return k
		}
	}
	b.cursor[key] = i
	return nil
}

// firstByName is the first child of that name, optionally looking through
// every descendant.
func (b *binder) firstByName(n *Node, name string, deep bool) *Node {
	if n == nil {
		return nil
	}
	for _, k := range n.Kids {
		if k.Kind == name {
			return k
		}
		if deep {
			if m := b.firstByName(k, name, deep); m != nil {
				return m
			}
		}
	}
	return nil
}

// firstAttr is the first unconsumed attribute of that name anywhere in the
// data. XFA keeps global values in attributes as readily as in elements.
func (b *binder) firstAttr(n *Node, name string) *Node {
	if v, ok := n.Attr[name]; ok {
		a := b.attrNode(n, name, v)
		if !b.consumed[a] {
			return a
		}
	}
	for _, k := range n.Kids {
		if m := b.firstAttr(k, name); m != nil {
			return m
		}
	}
	return nil
}

// attrNode is the node standing for one attribute. The same attribute always
// gives the same node, so that taking it marks it taken.
func (b *binder) attrNode(owner *Node, name, value string) *Node {
	byName := b.attrs[owner]
	if byName == nil {
		byName = map[string]*Node{}
		b.attrs[owner] = byName
	}
	if n, ok := byName[name]; ok {
		return n
	}
	n := &Node{Kind: name, Attr: map[string]string{}, Text: value}
	path := "@" + name
	if p := b.dataPath[owner]; p != "" {
		path = p + "." + path
	}
	b.dataPath[n] = path
	byName[name] = n
	return n
}

// unconsumed drops the nodes another container has already taken.
func (b *binder) unconsumed(in []*Node) []*Node {
	var out []*Node
	for _, n := range in {
		if !b.consumed[n] {
			out = append(out, n)
		}
	}
	return out
}

// bindOccurrences binds one container once per data node it matched. This is
// what makes a dynamic form dynamic: a subform written once in the template
// appears once per record in the document.
func (b *binder) bindOccurrences(child *Node, path string, seen map[string]int, matches []*Node, depth int) {
	paths := place(path, child.Get("name"), seen, len(matches))
	for i, m := range matches {
		b.bindValue(child, paths[i], m, depth)
	}
}

// bindValue gives one container one data node.
func (b *binder) bindValue(node *Node, path string, data *Node, depth int) {
	prev := b.enter(node, path)
	defer func() { b.cur = prev }()
	if settableKinds[node.Kind] {
		defer b.shadow(node, path, depth+1)
		switch {
		case isDataValue(data):
			b.emit(node, path, data, dataValue(data))
		case isMultiSelect(node):
			// A multi-select list keeps one child per choice made.
			var picked []string
			for _, k := range data.Kids {
				picked = append(picked, k.Text)
			}
			b.emit(node, path, data, strings.Join(picked, "\n"))
		default:
			// A field matched to a group: the two are not the same kind of
			// thing, so there is no value to take.
			b.emit(node, path, data, "")
		}
		return
	}
	if !isDataValue(data) || !b.consumeData() {
		b.bindElement(node, path, data, depth+1)
	}
}

// setAndBind records a container that found no data of its own and carries on
// into its children, which may still find some.
func (b *binder) setAndBind(node *Node, path string, data *Node, depth int) {
	prev := b.enter(node, path)
	defer func() { b.cur = prev }()
	if settableKinds[node.Kind] {
		b.emit(node, path, nil, "")
	}
	b.bindElement(node, path, data, depth+1)
}

// emit records one field.
func (b *binder) emit(node *Node, path string, data *Node, value string) {
	// b.cur is this container's own node: every path into here has entered it.
	b.cur.Data, b.cur.Value, b.cur.Bound = data, value, data != nil
	if len(b.out) >= maxBindings {
		b.truncated = true
		return
	}
	b.out = append(b.out, Binding{
		Name:     node.Get("name"),
		Path:     path,
		Kind:     node.Kind,
		Node:     node,
		Data:     data,
		DataPath: b.dataPath[data],
		Value:    value,
		Bound:    data != nil,
	})
}

// place appends a name to a path once per occurrence, numbering repeats. A
// container with no name adds nothing, which keeps the wrappers a designer
// leaves behind out of the answer.
func place(path, name string, seen map[string]int, count int) []string {
	out := make([]string, count)
	base := 0
	if name != "" {
		base = seen[name]
		seen[name] = base + count
	}
	for i := range out {
		if name == "" {
			out[i] = path
			continue
		}
		c := name
		if base+i > 0 {
			c = fmt.Sprintf("%s[%d]", name, base+i)
		}
		if path != "" {
			c = path + "." + c
		}
		out[i] = c
	}
	return out
}

// matchOf reads a <bind>'s match attribute. XFA names four ways and treats
// anything else as the default, which is what a form written by hand needs.
func matchOf(bind *Node) string {
	switch m := bind.Get("match"); m {
	case "dataRef", "global", "none":
		return m
	default:
		return "once"
	}
}

// occurInfo is how few and how many times a container may appear.
//
// The defaults are not symmetrical and the asymmetry is XFA's: a bare <occur>
// means exactly one, but <occur min="3"/> means exactly three, because max
// falls back to min once min has been given.
func occurInfo(node *Node) (int, int) {
	occur := node.Child("occur")
	if occur == nil || node.Get("name") == "" {
		return 1, 1
	}
	minText, maxText := occur.Get("min"), occur.Get("max")
	min := 1
	if minText != "" {
		min = wholeOr(minText, 1)
	}
	max := 1
	switch {
	case maxText != "":
		max = wholeOr(maxText, -1)
	case minText != "":
		max = min
	}
	if max != -1 && max < min {
		max = min
	}
	if max == -1 {
		return min, math.MaxInt
	}
	return min, max
}

// wholeOr reads a whole number, or gives the fallback. A template written by
// a designer that has lost its way says max="" or max="many".
func wholeOr(s string, fallback int) int {
	n, err := strconv.Atoi(strings.TrimSpace(s))
	if err != nil {
		return fallback
	}
	return n
}

// isMultiSelect says a field lets a person choose more than one item.
func isMultiSelect(node *Node) bool {
	return node.Child("ui").Child("choiceList").Get("open") == "multiSelect"
}

// isDataValue says a data node holds a value rather than a group of them.
//
// The node may say so itself — LiveCycle writes xfa:dataNode="dataGroup" on
// every group in the datasets it produces — and otherwise it is a value when
// it has no elements inside it, or when what is inside it is rich text.
func isDataValue(n *Node) bool {
	switch n.Attr["dataNode"] {
	case "dataValue":
		return true
	case "dataGroup":
		return false
	}
	return len(n.Kids) == 0 || n.Kids[0].Kind == "body"
}

// dataValue is what a value node says. Rich text is flattened to its words:
// this reads what a form was filled with, not how it was styled.
func dataValue(n *Node) string {
	if len(n.Kids) == 0 {
		return n.Text
	}
	if n.Kids[0].Kind == "body" {
		return strings.TrimSpace(allText(n.Kids[0]))
	}
	return n.Text
}

// allText is every word under a node, in order.
func allText(n *Node) string {
	var parts []string
	n.Walk(func(k *Node) {
		if k.Text != "" {
			parts = append(parts, k.Text)
		}
	})
	return strings.Join(parts, " ")
}
