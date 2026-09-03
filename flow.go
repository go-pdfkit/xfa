// Copyright (c) 2026, the go-pdfkit/xfa authors
// All rights reserved.
//
// SPDX-License-Identifier: BSD-3-Clause

package xfa

import (
	"fmt"
	"math"
	"strings"
)

// unbounded is the height available where nothing bounds it: a page area whose
// medium writes no size, or a content area with no h. XFA leaves both optional
// and pdf.js reads them with no default (template.js:1554-1567), so a form can
// arrive with nothing to check a fit against. Refusing to place anything then
// would be worse than placing it: the arithmetic is the same, only the
// question "does it still fit" has no answer.
var unbounded = Measure(math.Inf(1))

// fitSlop is how far past the bottom a box may reach and still be taken to
// fit. It is pdf.js's, to the point of the rounding it does first:
//
//	const ERROR = 2;                             // layout.js:275
//	Math.round(h - space.height) <= ERROR        // layout.js:349
//
// Two points is a quarter of a line. It is there because a form's heights are
// written in millimetres and inches and added up in points, and a column of
// twenty of them that was meant to fill a page exactly comes out a fraction
// over it.
const fitSlop = 2

// insets are the four margins of a container, in points.
//
// They are the container's own, outside whatever it holds: pdf.js emits a
// subform's margin as a CSS margin and adds it to the height the subform
// reports to its parent (template.js:5221-5223). A field's and a draw's margin
// is not this — both turn it into padding before emitting it
// (template.js:2917-2920, 1949-1952), so it sits inside a box whose size the
// template already wrote, and changes nothing outside.
type insets struct {
	top, right, bottom, left Measure
}

// vertical and horizontal are what the insets add to a container's size.
func (in insets) vertical() Measure   { return in.top + in.bottom }
func (in insets) horizontal() Measure { return in.left + in.right }

// marginOf reads a container's <margin>. Each inset defaults to nought, as it
// does in pdf.js (getMeasurement(attributes.topInset, "0"),
// template.js:3758-3768); ok is false when one is written and is not a length,
// because a margin nobody can read is not a margin of nought.
func marginOf(n *Node) (in insets, ok bool) {
	m := n.Child("margin")
	if m == nil {
		return insets{}, true
	}
	for _, f := range []struct {
		name string
		to   *Measure
	}{
		{"topInset", &in.top},
		{"rightInset", &in.right},
		{"bottomInset", &in.bottom},
		{"leftInset", &in.left},
	} {
		v, _, err := m.Measure(f.name)
		if err != nil {
			return insets{}, false
		}
		*f.to = v
	}
	return in, true
}

// hidden says the template asks for the element not to be drawn at all, and so
// for the flow above it to leave it no room.
//
// pdf.js returns HTMLResult.EMPTY for presence "hidden" and "inactive"
// (template.js:5019-5021 for a subform, 1898-1900 for a draw), and
// $childrenToHTML calls $addHTML only for a result that carries html
// (xfa_object.js:410-418) — so a hidden child never reaches the accumulator
// and the one after it moves up into its place. "invisible" is a different
// answer: pdf.js gives it CSS visibility:hidden (html_utils.js:133-141), which
// keeps its room.
func hidden(n *Node) bool {
	switch n.Get("presence") {
	case "hidden", "inactive":
		return true
	default:
		return false
	}
}

// height is what one node contributes to the flow of the container above it,
// or why that cannot be said.
type height struct {
	h   Measure
	why string
}

// heightOf is the vertical room a node takes where its parent stacks its
// children, and it is the whole of this slice: a tb container's second child
// begins where its first one ends, so every height between the two has to be
// arrived at before either can be placed.
//
// It is memoised per node because a container is measured once for the stack
// it sits in and again when it is laid out, and a form nests a dozen deep.
func (p *placer) heightOf(n *FormNode) (Measure, string) {
	if m, ok := p.heights[n]; ok {
		return m.h, m.why
	}
	// A cycle cannot arise — the expanded form is a tree — but a node being
	// measured is marked before its children are, so that a change which broke
	// that would stop rather than recurse for ever.
	p.heights[n] = height{why: measuringItself}
	h, why := p.measure(n)
	p.heights[n] = height{h, why}
	return h, why
}

// measuringItself is the answer for a node asked for its own height while it
// is being measured. See [placer.heightOf].
const measuringItself = "it contains itself, which a form cannot"

// measure computes what heightOf memoises.
func (p *placer) measure(n *FormNode) (Measure, string) {
	if hidden(n.Template) {
		// It is drawn nowhere and takes no room. It is still placed, and
		// carries [Box.Hidden] to say so.
		return 0, ""
	}
	if n.Kind == "field" || n.Kind == "draw" {
		h, ok, err := n.Template.Measure("h")
		switch {
		case err != nil:
			return 0, fmt.Sprintf("its height is written as h=%q, which is not a length", n.Template.Get("h"))
		case !ok:
			return 0, noHeightWritten
		}
		return h, ""
	}
	in, ok := marginOf(n.Template)
	if !ok {
		return 0, "its margin is not written in lengths"
	}
	content, why := p.contentHeight(n)
	if why != "" {
		return 0, why
	}
	own, ok, err := n.Template.Measure("h")
	if err != nil {
		return 0, fmt.Sprintf("its height is written as h=%q, which is not a length", n.Template.Get("h"))
	}
	if !ok {
		own = 0
	}
	// pdf.js: Math.max(this[$extra].height + marginV, this.h || 0)
	// (template.js:5222). A container is as tall as what it holds even where
	// the template writes a height, which is why holding a thing of unknown
	// height leaves the container's own height unknown too rather than falling
	// back on what is written.
	return max(content+in.vertical(), own), ""
}

// noHeightWritten is the answer for a leaf whose height only its own text
// could give. pdf.js's layoutNode (html_utils.js:207-288) supplies it by
// measuring, and there is no other fallback.
const noHeightWritten = "the template does not write its height, which only measuring its text would give"

// contentHeight is how tall what a container holds comes out, before the
// container's own margin and its own written height are taken into account.
func (p *placer) contentHeight(n *FormNode) (Measure, string) {
	kids := contained(n)
	switch lay := layoutOf(n); lay {
	case "tb", "table":
		// pdf.js: extra.height += h (layout.js:145-159), from nought.
		var sum Measure
		for _, k := range kids {
			h, why := p.heightOf(k)
			if why != "" {
				return 0, why
			}
			sum += h
		}
		return sum, ""
	case "row", "rl-row":
		// pdf.js: extra.height = Math.max(extra.height, h) (layout.js:135-143),
		// and every cell already placed is then stretched to it.
		var tallest Measure
		for _, k := range kids {
			h, why := p.heightOf(k)
			if why != "" {
				return 0, why
			}
			tallest = max(tallest, h)
		}
		return tallest, ""
	case "lr-tb", "rl-tb":
		return 0, fmt.Sprintf("a %s layout wraps its children onto lines, and how many lines they come to is not computed here", lay)
	default:
		// Positioned: each child is where it says it is, and the container
		// reaches as far as the furthest of them.
		// pdf.js: extra.height = Math.max(extra.height, y + h) (layout.js:101-106).
		var reach Measure
		for _, k := range kids {
			if hidden(k.Template) {
				continue
			}
			y, _, err := k.Template.Measure("y")
			if err != nil {
				return 0, fmt.Sprintf("it holds something whose origin is written as y=%q, which is not a place", k.Template.Get("y"))
			}
			h, why := p.heightOf(k)
			if why != "" {
				return 0, why
			}
			reach = max(reach, y+h)
		}
		return reach, ""
	}
}

// colSpanOf is how many of a table's columns a cell takes.
//
// pdf.js validates it as n >= 1 or n == -1 and falls back to 1 otherwise
// (template.js:2670-2674); -1 means every column left in the row.
func colSpanOf(n *Node) int {
	v := wholeOr(n.Get("colSpan"), 1)
	if v >= 1 || v == -1 {
		return v
	}
	return 1
}

// columnWidths is the list of column widths a container writes for the rows
// inside it, and ok is false when it writes none this can read.
//
// pdf.js reads it as whitespace-separated measurements with -1 for "the rest"
// (template.js:4819-4822). A -1 is the rest of a width this slice does not
// compute, so a list carrying one is refused whole rather than in part. No
// columnWidths in the corpus carries one.
func columnWidths(n *Node) ([]Measure, bool) {
	s := n.Get("columnWidths")
	if s == "" {
		return nil, false
	}
	var out []Measure
	for _, f := range strings.Fields(s) {
		m, err := ParseMeasure(f)
		if err != nil || m < 0 {
			return nil, false
		}
		out = append(out, m)
	}
	return out, len(out) > 0
}

// columnWidth is how wide a cell spanning span columns from col is, and which
// column the next cell starts at.
//
// pdf.js sums a slice and takes the next column modulo the number of them
// (html_utils.js:88-101), so a row with more cells than the table has columns
// begins again at the first — which is how a template writes a table whose
// rows hold two records side by side.
func columnWidth(cols []Measure, col, span int) (w Measure, next int) {
	if span == -1 {
		for _, c := range cols[col:] {
			w += c
		}
		return w, 0
	}
	end := min(col+span, len(cols))
	for _, c := range cols[col:end] {
		w += c
	}
	return w, (col + span) % len(cols)
}
