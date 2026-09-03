// Copyright (c) 2026, the go-pdfkit/xfa authors
// All rights reserved.
//
// SPDX-License-Identifier: BSD-3-Clause

package xfa

import (
	"fmt"
	"math"
)

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
	// Hidden says the template asks for it not to be drawn: presence
	// "hidden" or "inactive". pdf.js emits display:none for both
	// (html_utils.js:133-141) and a flow layout gives them no room, so the
	// element after one begins where it does. It is reported rather than left
	// out, because a caller counting what is on a page needs to know that
	// something is there and asked not to be seen.
	Hidden bool
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
// them rather than by their own coordinates. See [Place] for which of them
// this package follows.
var flowLayouts = map[string]bool{
	"lr-tb":  true,
	"rl-row": true,
	"rl-tb":  true,
	"row":    true,
	"table":  true,
	"tb":     true,
}

// Place lays a form out on one page.
//
// # What it does
//
// Under a POSITIONED layout every container has an x and a y that mean what
// they say, so a box's place on the page is its own x and y added to those of
// every container above it, down from the content area's origin, with
// anchorType and rotate resolved on the way (pdf.js layout.js:202-259). That
// is what pdf.js computes too — under "position" it emits style.left and
// style.top from the node's own x and y (html_utils.js:112-123).
//
// Under a FLOW layout the coordinates are thrown away (html_utils.js:347-350)
// and the children are stacked instead. This follows two of the six:
//
//   - tb and table stack downwards. The first child sits at the container's
//     own origin, and each one after it begins where the one above it ends
//     (layout.js:144-158, extra.height += h). A container's own height is the
//     taller of what it holds and what the template writes for it
//     (template.js:5221-5223), so the heights are arrived at from the bottom
//     of the tree upwards, out of the literal w and h of the fields and draws
//     at the leaves.
//   - row cuts its cells from the columnWidths of the table above it
//     (html_utils.js:81-106), a cell spanning colSpan of them, and stretches
//     every cell to the height of the tallest (layout.js:135-143).
//
// A container's margin is its own, outside what it holds: it moves the
// children in by the left and top insets and adds all four to the height the
// container reports upwards (template.js:5217-5223, layout.js:162-171). A
// field's and a draw's margin is not — both turn it into padding
// (template.js:1949-1952, 2917-2920), inside a box the template already sized.
//
// # What it deliberately does not do, and reports instead
//
//   - lr-tb, and rl-tb, rl-row. The first wraps its children onto lines, which
//     needs their widths and a line-breaking rule; the last two fill from the
//     right, which needs the container's width. Only the first child of an
//     lr-tb is placed, at the container's origin.
//   - Text measurement. A field whose template writes no height has no height
//     until its text is measured — and under a stack, neither has anything
//     below it, because where the next child begins is the height of this one.
//   - Splitting and pagination. One page area and its first content area.
//     What does not fit is reported rather than carried onto a second page or
//     drawn hanging off the bottom.
//   - Borders. A child's origin is the inside of its parent's margin, not the
//     inside of its parent's border.
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
	p := &placer{layout: l, heights: map[*FormNode]height{}}
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
	content := area.Template.Child("contentArea")
	if content != nil {
		page.Content = contentRect(content)
	}
	p.page = &page

	// The page area's own children are the furniture — the letterhead, the
	// rules, the page number — and they sit in the page's frame, not the
	// content area's. pdf.js pushes them straight into the page div
	// (template.js:4111-4114) and the content area's div beside them.
	p.placeAt(area, frame{avail: given(page.Height)}, nil)

	if content == nil {
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
	//
	// The content area's height is the room the whole body has, and it is what
	// a stack measures a fit against: "const space = { width: contentArea.w,
	// height: contentArea.h }" (template.js:5556).
	p.place(root, frame{x: page.Content.X, y: page.Content.Y, avail: contentAvail(content)})
	l.Pages = []Page{page}
	return l
}

// contentAvail is the vertical room a content area gives the body. A content
// area that writes no height, or one nobody can read, bounds nothing: pdf.js
// reads h with no default (template.js:1556) and would compare against NaN.
func contentAvail(content *Node) Measure {
	h, ok, err := content.Measure("h")
	if !ok || err != nil {
		return unbounded
	}
	return h
}

// given is a page's height where the medium wrote one, and no bound where it
// did not. [pageSize] answers nought for both, because a page of no size is
// not a page.
func given(h Measure) Measure {
	if h == 0 {
		return unbounded
	}
	return h
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
	// heights memoises what each node contributes to the stack above it. See
	// [placer.heightOf].
	heights map[*FormNode]height
}

// A frame is where an element goes and what room it has there.
type frame struct {
	// x, y is where its own box begins, on the page.
	x, y Measure
	// avail is how much vertical room it has: the content area's height, less
	// whatever the containers between here and there have already spent.
	avail Measure
	// cols are the columnWidths of the container above it, which a row cuts
	// its cells from and every other layout ignores.
	cols []Measure
}

// A cell is the size a row imposes on a child of its own, in place of the size
// the template writes for it: the width comes from the table's columnWidths
// (html_utils.js:81-106) and the height from the tallest cell in the row,
// which pdf.js writes back over every cell already placed (layout.js:135-143).
type cell struct {
	w, h      Measure
	stretched bool
}

// otherPageArea is why an element on a page this slice does not lay out is not
// on the page it does.
const otherPageArea = "it is on another page area: this slice lays out the first one only"

// overflows is why an element that would begin below the room its container
// has is not placed. pdf.js does not refuse it: it fails the container, and
// the page loop carries what is left onto the next content area
// (template.js:5502-5600). That is the next slice.
const overflows = "there is no room left for it where it is stacked, and this slice does not carry what overflows onto another page"

// place puts one container and everything under it on the page, at its own x
// and y within the frame it is given.
func (p *placer) place(n *FormNode, f frame) {
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
	f.x, f.y = f.x+x, f.y+y
	if n.Kind == "field" || n.Kind == "draw" {
		p.leaf(n, f, true, nil)
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
		r, rotate := transformedBBox(n.Template, f.x, f.y, w, h)
		if rotate != 0 {
			p.rejectAll(n, "its contents are turned, which this slice does not follow")
			return
		}
		f.x, f.y = r.X, r.Y
	}
	p.children(n, f)
}

// placeAt puts one container at an origin already decided, with its own x, y
// and anchorType left out of it. That is what a flow layout does to its
// children: pdf.js zeroes the coordinates (fixDimensions, html_utils.js:347-350)
// and both the position and the anchorType converters return without emitting
// anything unless the enclosing layout is positioned (html_utils.js:44-48,
// 112-118).
func (p *placer) placeAt(n *FormNode, f frame, over *cell) {
	if n.Kind == "field" || n.Kind == "draw" {
		p.leaf(n, f, false, over)
		return
	}
	p.children(n, f)
}

// children puts everything inside a container on the page, from the origin the
// container ended up at, in the way the container's layout says.
func (p *placer) children(n *FormNode, f frame) {
	for _, kid := range n.Kids {
		if kid.Kind == "pageSet" {
			// The paper rather than the body: the page areas under it describe
			// the sheets. One of them is the sheet being laid out and its
			// furniture is already on it; the rest are not.
			p.rejectExcept(kid, p.chosen, otherPageArea)
		}
	}
	kids := contained(n)
	// A row asks the container above it for its columns
	// ($getSubformParent().columnWidths, template.js:5096-5099), so they are
	// handed down one level whatever the layout here is.
	cols, _ := columnWidths(n.Template)
	switch lay := layoutOf(n); lay {
	case "tb", "table":
		p.stack(n, kids, lay, f, cols)
	case "row":
		p.cells(n, kids, f)
	case "lr-tb", "rl-tb", "rl-row":
		p.firstOnly(kids, lay, f, cols)
	default:
		for _, kid := range kids {
			p.place(kid, frame{x: f.x, y: f.y, avail: f.avail, cols: cols})
		}
	}
}

// stack puts a tb or a table container's children one below the other.
func (p *placer) stack(n *FormNode, kids []*FormNode, lay string, f frame, cols []Measure) {
	in, ok := marginOf(n.Template)
	if !ok {
		p.rejectKids(kids, "the container that stacks it writes a margin that is not in lengths")
		return
	}
	// The room the children have is the room the container has, never more
	// than the height the container is given, less its own insets:
	// availableSpace = min(this.h || Infinity, availableSpace.height)
	// (template.js:5062-5065) and then getAvailableSpace subtracts the margin
	// (layout.js:162-171).
	room := f.avail
	if own, okH, errH := n.Template.Measure("h"); okH && errH == nil {
		room = min(room, own)
	}
	room -= in.vertical()
	x, y := f.x+in.left, f.y+in.top
	var off Measure
	for i, kid := range kids {
		h, why := p.heightOf(kid)
		if why != "" {
			// Where this one begins is known exactly, so it is placed. Where
			// the one after it begins is this one's height, so it is not, and
			// neither is anything after that.
			p.placeAt(kid, frame{x: x, y: y + off, avail: room - off, cols: cols}, nil)
			p.rejectKids(kids[i+1:], fmt.Sprintf(
				"a %s layout stacks its children, and the height of the one above it is not computed: %s", lay, why))
			return
		}
		// pdf.js rounds before comparing and allows two points of slop
		// (layout.js:275, 349). See [fitSlop].
		if math.Round(float64(off+h-room)) > fitSlop {
			p.rejectKids(kids[i:], overflows)
			return
		}
		p.placeAt(kid, frame{x: x, y: y + off, avail: room - off, cols: cols}, nil)
		off += h
	}
}

// cells puts a row's children side by side, each as wide as the columns it
// spans and all of them as tall as the tallest.
func (p *placer) cells(n *FormNode, kids []*FormNode, f frame) {
	if len(f.cols) == 0 {
		p.rejectKids(kids, "a row cuts its cells from the columnWidths of the container above it, which writes none this reads")
		return
	}
	// Every cell is stretched to the row's height, which is the tallest of
	// them (layout.js:135-143). Where one of them has no height, none of them
	// is stretched and each keeps its own.
	tall, why := p.contentHeight(n)
	over := cell{h: tall, stretched: why == ""}
	room := f.avail
	if why == "" {
		room = tall
	}
	x, col := f.x, 0
	for _, kid := range kids {
		if hidden(kid.Template) {
			// pdf.js returns EMPTY before it reads colSpan, so a hidden cell
			// takes no column and the next one is where it would have been.
			p.placeAt(kid, frame{x: x, y: f.y, avail: room}, nil)
			continue
		}
		w, next := columnWidth(f.cols, col, colSpanOf(kid.Template))
		mine := over
		mine.w = w
		p.placeAt(kid, frame{x: x, y: f.y, avail: room}, &mine)
		x, col = x+w, next
	}
}

// firstOnly places the first child of a layout this slice does not follow, and
// says why the rest are not there.
func (p *placer) firstOnly(kids []*FormNode, lay string, f frame, cols []Measure) {
	for i, kid := range kids {
		switch {
		case lay != "lr-tb":
			// pdf.js gives rl-tb and rl-row CSS row-reverse
			// (web/xfa_layer_builder.css:265-273), so even the first child
			// begins at the container's width less its own.
			p.rejectAll(kid, fmt.Sprintf(
				"a %s layout fills from the right, which needs a width not computed here", lay))
		case i > 0:
			p.rejectAll(kid, fmt.Sprintf(
				"a %s layout wraps its children onto lines, and where the one before it ends is not computed here", lay))
		default:
			// pdf.js accumulates from nought — extra.height starts at 0 and
			// the first child is pushed before anything is added to it
			// (layout.js:145-160) — so the first child of an lr-tb sits at the
			// container's own origin. That is the flow algorithm's own answer
			// for one child, not an approximation.
			p.placeAt(kid, frame{x: f.x, y: f.y, avail: f.avail, cols: cols}, nil)
		}
	}
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
// apply, which they do not when a flow layout put it where it is; over is the
// size a row imposes in place of the template's, or nil.
func (p *placer) leaf(n *FormNode, f frame, anchor bool, over *cell) {
	w, okW, errW := n.Template.Measure("w")
	h, okH, errH := n.Template.Measure("h")
	if over != nil {
		w, okW, errW = over.w, true, nil
		if over.stretched {
			h, okH, errH = over.h, true, nil
		}
	}
	switch {
	case errW != nil || errH != nil:
		p.reject(n, fmt.Sprintf("its size is written as w=%q h=%q, which is not a size",
			n.Template.Get("w"), n.Template.Get("h")))
	case !okW || !okH:
		// pdf.js's layoutNode (html_utils.js:207-288) supplies the missing one
		// by measuring the content, and there is no other fallback.
		p.reject(n, "the template does not write its "+missing(okW, okH)+
			", which only measuring its text would give")
	default:
		r, rotate := Rect{X: f.x, Y: f.y, W: w, H: h}, 0
		if anchor {
			r, rotate = transformedBBox(n.Template, f.x, f.y, w, h)
		}
		p.page.Boxes = append(p.page.Boxes, Box{
			Node: n, Kind: n.Kind, Path: n.Path, Rect: r, Rotate: rotate,
			Value: n.Value, Hidden: hidden(n.Template),
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

// rejectKids records every field and draw under each of a list of containers.
func (p *placer) rejectKids(kids []*FormNode, why string) {
	for _, k := range kids {
		p.rejectAll(k, why)
	}
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
