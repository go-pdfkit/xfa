// Copyright (c) 2026, the go-pdfkit/xfa authors
// All rights reserved.
//
// SPDX-License-Identifier: BSD-3-Clause

package xfa

import "strings"

// This is XFA's Scripting Object Model reference syntax — the thing a
// <bind ref="..."> is written in. It is not XPath and translating it into
// XPath does not pay: its indices count from zero where XPath's count from
// one, an unqualified name walks up the tree until it finds a home rather than
// failing, and a name matches an attribute as readily as an element. The
// fleet's XPath engine (go-ruby-nokogiri) would have to be given a tree of its
// own node type and then talked out of all three differences.
//
// The shape follows pdf.js's som.js, which is the reference implementation.

// somOp is how one step of an expression reaches the next.
type somOp uint8

const (
	// somDot is a.b — a child of a named b.
	somDot somOp = iota
	// somDotDot is a..b — a b anywhere below a.
	somDotDot
	// somHash is a.#b — a child whose ELEMENT is b, rather than whose name
	// attribute is. A data node has no name attribute, so on the data side the
	// two coincide.
	somHash
)

// A somStep is one component of an expression.
type somStep struct {
	name  string
	op    somOp
	index int
	all   bool
}

// parseSOM reads an expression. The second return is empty when it was read,
// and otherwise names the construct that stopped it, in words fit to show
// somebody.
func parseSOM(expr string) ([]somStep, string) {
	name := somName(expr)
	if name == "" {
		return nil, "an expression that names nothing"
	}
	steps := []somStep{{name: name}}
	pos := len(name)
	for pos < len(expr) {
		if expr[pos] == '[' {
			pos++
			end := strings.IndexByte(expr[pos:], ']')
			if end < 0 {
				return nil, "an index that is never closed"
			}
			text := strings.TrimSpace(expr[pos : pos+end])
			pos += end + 1
			last := &steps[len(steps)-1]
			if text == "*" {
				last.all = true
				continue
			}
			n := wholeOr(text, -1)
			if n < 0 {
				return nil, "an index that is not a whole number: [" + text + "]"
			}
			last.index = n
			continue
		}
		// Whatever the separator is, it is one character; what follows it says
		// which kind of step this is.
		pos++
		if pos >= len(expr) {
			break
		}
		op := somDot
		switch expr[pos] {
		case '.':
			pos++
			op = somDotDot
		case '#':
			pos++
			op = somHash
		case '[':
			return nil, "a FormCalc subexpression: .[...]"
		case '(':
			return nil, "a JavaScript predicate: .(...)"
		}
		name = somName(expr[pos:])
		if name == "" {
			break
		}
		pos += len(name)
		steps = append(steps, somStep{name: name, op: op})
	}
	return steps, ""
}

// somName is the run of characters up to the next separator.
func somName(s string) string {
	if i := strings.IndexAny(s, ".["); i >= 0 {
		return s[:i]
	}
	return s
}

// outsideData are the shortcuts that name something other than the data. A
// bind that reaches for one of them is asking for the template, the layout or
// the running application, none of which is a value this package can give.
var outsideData = map[string]bool{
	"$template":      true,
	"$form":          true,
	"$layout":        true,
	"$host":          true,
	"$event":         true,
	"$connectionSet": true,
	"$dataWindow":    true,
	"$xfa":           true,
	"xfa":            true,
}

// search evaluates one expression against the data, starting from the node the
// containing form element is bound to. It returns the nodes found, and a
// reason when the expression held something it would not follow.
func (b *binder) search(container *Node, expr string) ([]*Node, string) {
	steps, why := parseSOM(expr)
	if why != "" {
		return nil, why
	}
	var nodes []*Node
	qualified := true
	first := 1
	switch head := steps[0].name; {
	case head == "$data":
		nodes = []*Node{b.data}
	case head == "$record":
		if b.data == nil || len(b.data.Kids) == 0 {
			return nil, ""
		}
		nodes = []*Node{b.data.Kids[0]}
	case head == "$":
		nodes = []*Node{container}
	case head == "!":
		nodes = []*Node{b.wrapper}
	case outsideData[head]:
		return nil, "a reference outside the data: " + head
	default:
		// An unqualified expression starts where the form node is bound, and
		// if that finds nothing it tries the parent, and the parent's parent,
		// until the data runs out. XFA 3.3 p. 114.
		nodes = []*Node{container}
		qualified = false
		first = 0
	}
	if nodes[0] == nil {
		return nil, ""
	}
	for i := first; i < len(steps); i++ {
		step := steps[i]
		var groups [][]*Node
		for _, n := range nodes {
			if found := b.step(n, step); len(found) > 0 {
				groups = append(groups, found)
			}
		}
		if len(groups) == 0 && !qualified && i == 0 {
			up := b.parent[container]
			if up == nil {
				return nil, ""
			}
			container = up
			nodes = []*Node{container}
			i = -1
			continue
		}
		nodes = nil
		for _, g := range groups {
			if step.all {
				nodes = append(nodes, g...)
			} else if step.index < len(g) {
				nodes = append(nodes, g[step.index])
			}
		}
	}
	return nodes, ""
}

// step takes one component of an expression from one node.
func (b *binder) step(n *Node, s somStep) []*Node {
	var out []*Node
	if v, ok := n.Attr[s.name]; ok {
		out = append(out, b.attrNode(n, s.name, v))
	}
	if s.op == somHash {
		// A class step does not look through the tree; it names the elements
		// directly inside.
		if len(out) > 0 {
			return out
		}
		return n.Children(s.name)
	}
	b.gather(n, s.name, s.op == somDotDot, &out)
	return out
}

// gather collects the children of that name, and if deep every descendant of
// that name below them.
func (b *binder) gather(n *Node, name string, deep bool, into *[]*Node) {
	for _, k := range n.Kids {
		if k.Kind == name {
			*into = append(*into, k)
		}
		if deep {
			b.gather(k, name, deep, into)
		}
	}
}
