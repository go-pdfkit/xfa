// Copyright (c) 2026, the go-pdfkit/xfa authors
// All rights reserved.
//
// SPDX-License-Identifier: BSD-3-Clause

package xfa

import "fmt"

// A Rect is a box on the page, in points.
//
// It is measured in XFA's own frame — from the TOP-LEFT corner, with Y
// increasing downwards — and not in PDF's, which counts up from the bottom
// left. The flip belongs to whatever draws the page, because it needs the page
// height to do it and because leaving it here would make every coordinate in
// this package disagree with the template it was read from.
type Rect struct {
	X, Y, W, H Measure
}

// A Box is one element of the form, placed.
type Box struct {
	// Node is the occurrence this box was placed for.
	Node *FormNode
	// Kind is "field" or "draw".
	Kind string
	// Path is [FormNode.Path], repeated here so a box can be reported on its
	// own.
	Path string
	// Rect is where it goes, absolute on the page, with anchorType and rotate
	// already resolved.
	Rect
	// Rotate is what the template says, in degrees clockwise: 0, 90, 180 or
	// 270. Rect is the box the rotated element occupies; this says how the
	// content inside it is turned.
	Rotate int
	// Value is what the data put in it, for a bound field. Empty otherwise.
	Value string
}

// An Unplaced is one element of the form this package did not place, and why.
//
// Every field and draw of the expanded form is in exactly one of [Page.Boxes]
// and [Layout.Unplaced]. That is the point of the type: a layout that reaches
// four fifths of a form is useful, and one that silently drops the other fifth
// is not, because nothing downstream can tell a form that was laid out from a
// form that was half laid out.
type Unplaced struct {
	// Node is the occurrence that was not placed.
	Node *FormNode
	// Kind is "field" or "draw".
	Kind string
	// Path is [FormNode.Path].
	Path string
	// Why says what stopped it, in words fit to show somebody.
	Why string
}

// A Page is one sheet, with what this package could put on it.
type Page struct {
	// Width and Height are the sheet, with a landscape medium already
	// swapped. Both are zero when the page area writes no medium, which
	// pdf.js also declines to guess at (template.js:4108).
	Width, Height Measure
	// Content is the content area: where the form's body is laid out, as
	// against the furniture the page area draws around it. Its origin is the
	// origin of every coordinate in the body.
	Content Rect
	// Boxes are the elements placed on it, in the order the form places them.
	Boxes []Box
}

// A Layout is a form placed on paper.
type Layout struct {
	// Pages is what was laid out. This slice places one page; see [Place].
	Pages []Page
	// Unplaced is everything it did not place, with a reason each.
	Unplaced []Unplaced
}

// Fields counts the placed boxes that are fields rather than static drawing.
func (l *Layout) Fields() int {
	n := 0
	for _, p := range l.Pages {
		for _, b := range p.Boxes {
			if b.Kind == "field" {
				n++
			}
		}
	}
	return n
}

// flowLayouts are the layouts that place a container's children by stacking
// them rather than by their own coordinates. This package does not do any of
// them yet; see [Place].
var flowLayouts = map[string]bool{
	"lr-tb":  true,
	"rl-row": true,
	"rl-tb":  true,
	"row":    true,
	"table":  true,
	"tb":     true,
}

// Place lays a form out on one page, under positioned layout only.
//
// # What it does
//
// Every container of the form has an x and a y, and under a positioned layout
// they mean what they say: the element goes there, in the frame of whatever
// contains it. So a box's place on the page is its own x and y added to those
// of every container above it, down from the content area's origin. That is
// what this computes, resolving anchorType and rotate on the way
// (pdf.js layout.js:202-259), and it is what pdf.js computes too — under
// "position" it emits style.left and style.top from the node's own x and y
// (html_utils.js:112-123), and delegates to the browser's flexbox only for the
// flow layouts.
//
// # What it deliberately does not do, and reports instead
//
//   - Flow layouts — tb, lr-tb, rl-tb, row, rl-row, table. Under those, x and
//     y are thrown away (html_utils.js:347-350) and the children are stacked;
//     pdf.js hands that to CSS, so there is no reference to follow and it is
//     the whole of the next slice. 14.1% of the corpus's fields.
//   - Text measurement. A field whose template writes no width has no width
//     until its text is measured. 0.5% of the corpus's fields.
//   - Splitting, pagination, breaks, leaders and trailers. One page area, its
//     first content area, and whatever does not fit hangs off the bottom.
//   - Borders, margins and insets. A child's origin is its parent's x and y,
//     not the inside of its parent's border.
//   - colSpan. A cell in a row or a table takes its width from the parent's
//     columnWidths rather than from its own w (html_utils.js:81-111, 330-346).
//     Where this slice places the first child of a row, it uses the child's
//     own w — and that is the ONLY thing pdf.js and this package disagree
//     about over the corpus: 5 boxes of 21 933, all of them the width, none
//     of them the place.
//
// Each of those leaves its elements in [Layout.Unplaced] with the reason
// written out. Nothing is dropped: every field and draw of the expanded form
// comes back in one list or the other.
//
// A nil form, or one with no outermost subform, lays out nothing.
func Place(form *Form) *Layout {
	l := &Layout{}
	if form == nil {
		return l
	}
	// pdf.js takes the same one: "const root = this.subform.children[0]"
	// (template.js:5439). A template with no subform is not a form.
	root := firstOfKind(form.Root, "subform")
	if root == nil {
		return l
	}
	p := &placer{layout: l}
	page := Page{}
	area := firstPageArea(root)
	if area == nil {
		// pdf.js reads pageAreas[0] with no guard (template.js:5482) and
		// throws. There is nowhere to put anything.
		p.rejectAll(root, "the form has no page area")
		l.Pages = []Page{page}
		return l
	}
	p.chosen = area
	page.Width, page.Height = pageSize(area.Template)
	if content := area.Template.Child("contentArea"); content != nil {
		page.Content = contentRect(content)
	}
	p.page = &page

	// The page area's own children are the furniture — the letterhead, the
	// rules, the page number — and they sit in the page's frame, not the
	// content area's. pdf.js pushes them straight into the page div
	// (template.js:4111-4114) and the content area's div beside them.
	p.placeAt(area, 0, 0)

	if area.Template.Child("contentArea") == nil {
		// pdf.js filters the page's children for the content area's div and
		// indexes the result (template.js:5532-5546); with none, the loop over
		// content areas does not run and the body is never laid out at all.
		for _, kid := range root.Kids {
			if kid.Kind == "pageSet" {
				p.rejectExcept(kid, area, otherPageArea)
				continue
			}
			p.rejectAll(kid, "the page area has no content area")
		}
		l.Pages = []Page{page}
		return l
	}
	// The body. pdf.js pushes the outermost subform's own html into the
	// content area's div (template.js:5563-5568), so the subform is placed
	// like any other container: at the content area's origin, offset by its
	// own x and y, and imposing its own layout on its children.
	p.place(root, page.Content.X, page.Content.Y)
	l.Pages = []Page{page}
	return l
}

// A placer carries the one page being filled and the reasons for what is not
// on it.
type placer struct {
	page   *Page
	layout *Layout
	// chosen is the page area being laid out. Every other one under the form's
	// page sets holds elements this slice does not reach, and they are
	// reported rather than left off the only sheet there is.
	chosen *FormNode
}

// otherPageArea is why an element on a page this slice does not lay out is not
// on the page it does.
const otherPageArea = "it is on another page area: this slice lays out the first one only"

// place puts one container and everything under it on the page, at its own x
// and y within the frame whose origin is ox, oy.
func (p *placer) place(n *FormNode, ox, oy Measure) {
	// x and y default to nought when a template leaves them out, as they do in
	// pdf.js: getMeasurement(attributes.x, "0pt"). Written and unreadable is a
	// different answer, and stops the subtree rather than putting it at nought.
	x, _, errX := n.Template.Measure("x")
	y, _, errY := n.Template.Measure("y")
	if errX != nil || errY != nil {
		p.rejectAll(n, fmt.Sprintf("its origin is written as x=%q y=%q, which is not a place",
			n.Template.Get("x"), n.Template.Get("y")))
		return
	}
	x, y = ox+x, oy+y
	if n.Kind == "field" || n.Kind == "draw" {
		p.leaf(n, x, y, true)
		return
	}
	// A container may be anchored by a corner other than its top left, or
	// turned. pdf.js emits both as a CSS transform on the element itself
	// (html_utils.js:44-79, 125-133), and a transform moves everything inside
	// it, so its children are measured from where it ends up rather than from
	// where its x and y say.
	if anchored(n.Template) || rotateOf(n.Template) != 0 {
		w, okW, _ := n.Template.Measure("w")
		h, okH, _ := n.Template.Measure("h")
		if !okW || !okH {
			// Its own size is what the anchor is measured against, and a
			// container's size is usually its contents'. That is the
			// measurement this slice does not do.
			p.rejectAll(n, "it is anchored by a corner other than its top left, "+
				"and its size is not written: only measuring its contents would give it")
			return
		}
		r, rotate := transformedBBox(n.Template, x, y, w, h)
		if rotate != 0 {
			p.rejectAll(n, "its contents are turned, which this slice does not follow")
			return
		}
		x, y = r.X, r.Y
	}
	p.children(n, x, y)
}

// placeAt puts one container at an origin already decided, with its own x, y
// and anchorType left out of it. That is what a flow layout does to its first
// child: pdf.js zeroes the coordinates (fixDimensions, html_utils.js:347-350)
// and both the position and the anchorType converters return without emitting
// anything unless the enclosing layout is positioned (html_utils.js:44-48,
// 112-118).
func (p *placer) placeAt(n *FormNode, x, y Measure) {
	if n.Kind == "field" || n.Kind == "draw" {
		p.leaf(n, x, y, false)
		return
	}
	p.children(n, x, y)
}

// children puts everything inside a container on the page, from the origin the
// container ended up at.
func (p *placer) children(n *FormNode, x, y Measure) {
	for _, kid := range n.Kids {
		if kid.Kind == "pageSet" {
			// The paper rather than the body: the page areas under it describe
			// the sheets. One of them is the sheet being laid out and its
			// furniture is already on it; the rest are not.
			p.rejectExcept(kid, p.chosen, otherPageArea)
		}
	}
	lay := layoutOf(n)
	kids := contained(n)
	if !flowLayouts[lay] {
		for _, kid := range kids {
			p.place(kid, x, y)
		}
		return
	}
	// A flow layout stacks its children and throws their coordinates away. The
	// FIRST one is still exactly placed: pdf.js accumulates from nought —
	// extra.height starts at 0 and the first child is pushed before anything
	// is added to it (layout.js:145-160) — so the first child of a tb, a
	// table, an lr-tb or a row sits at the container's own origin. Where the
	// second one begins is the height of the first, which is the flow layout
	// this slice does not do.
	//
	// This is what makes the slice reach a real form at all: 556 of the
	// corpus's 560 outermost subforms are "tb", so a rule that refused every
	// flow container would refuse practically everything.
	for i, kid := range kids {
		switch {
		case i > 0:
			p.rejectAll(kid, fmt.Sprintf(
				"a %s layout stacks its children, and where the one above it ends is not computed here", lay))
		case !firstAtOrigin[lay]:
			p.rejectAll(kid, fmt.Sprintf(
				"a %s layout fills from the right, which needs a width not computed here", lay))
		default:
			p.placeAt(kid, x, y)
		}
	}
}

// firstAtOrigin are the flow layouts whose first child sits at the container's
// own origin. The two that are missing fill from the right — pdf.js gives them
// CSS row-reverse (web/xfa_layer_builder.css:265-273) — so where their first
// child begins is the container's width less the child's.
var firstAtOrigin = map[string]bool{
	"lr-tb": true,
	"row":   true,
	"table": true,
	"tb":    true,
}

// contained is a container's children as layout sees them. A subformSet is not
// one of them: its children are yielded as if they were its parent's
// (pdf.js $getContainedChildren, template.js:198-206), and it is skipped when
// layout asks who the parent is ($getSubformParent, template.js:4901-4907). A
// page set is not one either — pdf.js leaves it out of the filter it walks
// children with (template.js:5090-5100).
func contained(n *FormNode) []*FormNode {
	var out []*FormNode
	for _, k := range n.Kids {
		switch k.Kind {
		case "pageSet":
		case "subformSet":
			out = append(out, contained(k)...)
		default:
			out = append(out, k)
		}
	}
	return out
}

// leaf places a field or a draw, which is where a form's width and height stop
// being optional. anchor says whether the element's own anchorType and rotate
// apply, which they do not when a flow layout put it where it is.
func (p *placer) leaf(n *FormNode, x, y Measure, anchor bool) {
	w, okW, errW := n.Template.Measure("w")
	h, okH, errH := n.Template.Measure("h")
	switch {
	case errW != nil || errH != nil:
		p.reject(n, fmt.Sprintf("its size is written as w=%q h=%q, which is not a size",
			n.Template.Get("w"), n.Template.Get("h")))
	case !okW || !okH:
		// This is the 0.5% of the corpus that needs text measurement:
		// pdf.js's layoutNode (html_utils.js:207-288) supplies the missing one
		// by measuring the content, and there is no other fallback.
		p.reject(n, "the template does not write its "+missing(okW, okH)+
			", which only measuring its text would give")
	default:
		r, rotate := Rect{X: x, Y: y, W: w, H: h}, 0
		if anchor {
			r, rotate = transformedBBox(n.Template, x, y, w, h)
		}
		p.page.Boxes = append(p.page.Boxes, Box{
			Node: n, Kind: n.Kind, Path: n.Path, Rect: r, Rotate: rotate, Value: n.Value,
		})
	}
}

// missing names which of the two a template left out.
func missing(okW, okH bool) string {
	switch {
	case !okW && !okH:
		return "width or its height"
	case !okW:
		return "width"
	default:
		return "height"
	}
}

// reject records one element this did not place.
func (p *placer) reject(n *FormNode, why string) {
	p.layout.Unplaced = append(p.layout.Unplaced, Unplaced{Node: n, Kind: n.Kind, Path: n.Path, Why: why})
}

// rejectExcept records every field and draw under a container except those
// under one subtree, which has been dealt with already.
func (p *placer) rejectExcept(n, chosen *FormNode, why string) {
	if n == chosen {
		return
	}
	if n.Kind == "field" || n.Kind == "draw" {
		p.reject(n, why)
		return
	}
	for _, k := range n.Kids {
		p.rejectExcept(k, chosen, why)
	}
}

// rejectAll records every field and draw under a container, so that a whole
// subtree this cannot reach is still counted one element at a time.
func (p *placer) rejectAll(n *FormNode, why string) {
	n.Walk(func(k *FormNode) {
		if k.Kind == "field" || k.Kind == "draw" {
			p.reject(k, why)
		}
	})
}

// layoutOf is the layout a container imposes on its children.
//
// An absent attribute means "position": pdf.js passes the option list to
// getStringOption beginning with "position" (template.js:2333 for exclGroup,
// :4833 for subform) and an absent or unrecognised value yields the first
// entry (utils.js:70-76). An area has no layout attribute at all, and its
// children are read as positioned.
func layoutOf(n *FormNode) string {
	if v := n.Template.Get("layout"); flowLayouts[v] {
		return v
	}
	return "position"
}

// firstOfKind is the first child of that kind, or nil.
func firstOfKind(n *FormNode, kind string) *FormNode {
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

// firstPageArea is the page the form starts on: the first page area under the
// outermost subform's page set, however deeply the page sets nest.
//
// pdf.js reads root.pageSet.pageArea.children[0] (template.js:5439-5482) after
// a break may have named another; this slice does not read breaks, so it takes
// the first.
func firstPageArea(root *FormNode) *FormNode {
	var found *FormNode
	root.Walk(func(k *FormNode) {
		if found == nil && k.Kind == "pageArea" {
			found = k
		}
	})
	return found
}

// pageSize is the sheet a page area asks for.
//
// A medium gives the short and the long side rather than a width and a height,
// and an orientation says which way round they go (pdf.js
// template.js:4091-4106). Both sides must be there: pdf.js warns and emits no
// size at all when either is missing, and inventing one here would put a form
// on paper this package never saw.
func pageSize(area *Node) (w, h Measure) {
	medium := area.Child("medium")
	if medium == nil {
		return 0, 0
	}
	short, okS, errS := medium.Measure("short")
	long, okL, errL := medium.Measure("long")
	if !okS || !okL || errS != nil || errL != nil {
		return 0, 0
	}
	if medium.Get("orientation") == "landscape" {
		return long, short
	}
	return short, long
}

// contentRect is where a content area sits on the page. x and y default to
// nought, w and h to nothing at all — pdf.js reads them with no default
// (template.js:1558-1567), and a content area with no size is one nothing is
// checked against, which is what this slice does anyway.
func contentRect(area *Node) Rect {
	var r Rect
	r.X, _, _ = area.Measure("x")
	r.Y, _, _ = area.Measure("y")
	r.W, _, _ = area.Measure("w")
	r.H, _, _ = area.Measure("h")
	return r
}

// transformedBBox turns an element's x, y, w and h into the box it actually
// occupies, once anchorType has said which of its corners x and y name and
// rotate has turned it.
//
// This is pdf.js's getTransformedBBox (layout.js:202-259), which pdf.js itself
// uses only to decide whether something fits: for drawing it emits x and y raw
// and leaves the anchor to a CSS translate and the turn to a CSS rotate
// (html_utils.js:44-79, 125-133). A page has no CSS, so the arithmetic has to
// be done here.
func transformedBBox(n *Node, x, y, w, h Measure) (Rect, int) {
	var cx, cy Measure
	switch n.Get("anchorType") {
	case "bottomCenter":
		cx, cy = w/2, h
	case "bottomLeft":
		cx, cy = 0, h
	case "bottomRight":
		cx, cy = w, h
	case "middleCenter":
		cx, cy = w/2, h/2
	case "middleLeft":
		cx, cy = 0, h/2
	case "middleRight":
		cx, cy = w, h/2
	case "topCenter":
		cx, cy = w/2, 0
	case "topRight":
		cx, cy = w, 0
	}
	var dx, dy Measure
	rotate := rotateOf(n)
	switch rotate {
	case 90:
		dx, dy = -cy, cx
		w, h = h, -w
	case 180:
		dx, dy = cx, cy
		w, h = -w, -h
	case 270:
		dx, dy = cy, -cx
		w, h = -h, w
	default:
		dx, dy = -cx, -cy
	}
	return Rect{
		X: x + dx + min(0, w),
		Y: y + dy + min(0, h),
		W: abs(w),
		H: abs(h),
	}, rotate
}

// anchored says an element names a corner other than its top left for its x
// and y to mean. "topLeft" and an absent attribute are the same thing: it is
// the first entry of the option list pdf.js passes to getStringOption
// (template.js), which is what an absent or unrecognised value yields.
func anchored(n *Node) bool {
	switch n.Get("anchorType") {
	case "", "topLeft":
		return false
	default:
		return true
	}
}

// rotateOf reads the rotate attribute. XFA allows a whole number of degrees
// anticlockwise that is a multiple of ninety, and anything else is not a
// rotation — pdf.js validates it the same way and falls back to nought
// (template.js, getInteger with validate: x => x % 90 === 0).
func rotateOf(n *Node) int {
	v := wholeOr(n.Get("rotate"), 0)
	if v%90 != 0 {
		return 0
	}
	v %= 360
	if v < 0 {
		v += 360
	}
	return v
}

// abs is the magnitude of a length. A rotation turns a width into a negative
// one, and a box has no negative side.
func abs(m Measure) Measure {
	if m < 0 {
		return -m
	}
	return m
}
