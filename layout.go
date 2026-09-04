// Copyright (c) 2026, the go-pdfkit/xfa authors
// All rights reserved.
//
// SPDX-License-Identifier: BSD-3-Clause

package xfa

import (
	"fmt"
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
// Every field and draw of the form's BODY is in exactly one of [Page.Boxes]
// and [Layout.Unplaced]. That is the point of the type: a layout that reaches
// four fifths of a form is useful, and one that silently drops the other fifth
// is not, because nothing downstream can tell a form that was laid out from a
// form that was half laid out.
//
// A page area's own furniture is the one exception, and it is not a leak: a
// letterhead belongs to the sheet rather than to the form, so it is placed
// once on every sheet that page area makes, and appears in [Layout.Unplaced]
// only where its page area is never used at all.
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
	// Areas are the content areas the page offers the body, in order: the
	// boxes the form's body is laid out in, as against the furniture the page
	// area draws around them. Nearly every page area in the wild holds one.
	Areas []Rect
	// Boxes are the elements placed on it, in the order the form places them.
	// The page area's own furniture comes first and is drawn again on every
	// sheet that page area makes; the body follows.
	Boxes []Box
}

// A Layout is a form placed on paper.
type Layout struct {
	// Pages is the sheets the form came to, in order.
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

// Place lays a form out on paper.
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
// # Where it runs off the bottom, it turns the page
//
// A form is longer than a sheet, and the page it goes onto next is not simply
// "another of the same". Which page area comes next is decided by a state
// machine — the page set's relation, each page area's <occur>, the parity of
// the page number, and the explicit <breakBefore> and <breakAfter> the
// template writes — and [pager] follows pdf.js's (template.js:4064-4236,
// 5418-5657) rather than assuming. Every container of the chain being flowed
// begins again at the top of the new content area, which is what pdf.js
// arrives at by re-entering the whole tree with a new space.
//
// A container that would have to be BROKEN in two for its parts to fit is not
// broken: pdf.js keeps [$extra].children, a generator and a failingNode to do
// that (layout.js:38-53), and this does not. A container that may be split
// (Subform[$isSplittable], template.js:4940-4975) has its children distributed
// across pages instead, which is the same thing where the container itself
// draws nothing; one that may not — a positioned layout, a row, or anything
// kept intact — moves whole, and is reported unplaced where it fits no page at
// all.
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
//   - Breaking one container in two across a page boundary, as above.
//   - Borders. A child's origin is the inside of its parent's margin, not the
//     inside of its parent's border.
//
// Each of those leaves its elements in [Layout.Unplaced] with the reason
// written out. Nothing is dropped: every field and draw of the form's body
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
	p := newPlacer()
	p.layout, p.root, p.pager = l, root, newPager(root)
	area, consumed := p.pager.first(root)
	if area == nil {
		// pdf.js reads pageAreas[0] with no guard (template.js:5482) and
		// throws. There is nowhere to put anything.
		p.rejectAll(root, "the form has no page area")
		l.Pages = []Page{{}}
		return l
	}
	if consumed != nil {
		// $toPages consumes this one to CHOOSE the first page rather than to
		// break to it (template.js:5474-5477), so it must not fire again.
		p.fired[breakKey{consumed, false}] = true
	}
	p.mapUp(form.Root)
	p.openArea(area, 0, true)
	if len(contentAreas(area)) == 0 {
		// pdf.js filters the page's children for the content area's div and
		// indexes the result (template.js:5532-5546); with none, the loop over
		// content areas does not run and the body is never laid out at all.
		p.rejectAll(root, "the page area has no content area")
	} else {
		p.body(root)
	}
	p.rejectUnusedPages(root)
	p.dropEmptyPages()
	return l
}

// mapUp records each container's layout parent, which is where a row reads
// its columns from. It follows [contained]: a subformSet is transparent and a
// page set is not a container of the body at all.
func (p *placer) mapUp(n *FormNode) {
	for _, k := range contained(n) {
		p.up[k] = n
		p.mapUp(k)
	}
}

// rejectUnusedPages reports the furniture of every page area the form never
// reached. They are not on the paper and they are not nowhere.
func (p *placer) rejectUnusedPages(root *FormNode) {
	root.Walk(func(n *FormNode) {
		if n.Kind == "pageArea" && !p.used[n] {
			p.rejectAll(n, "its page area is never used: no page of this form is one")
		}
	})
}

// dropEmptyPages throws away a sheet the body put nothing on.
//
// pdf.js does the same (template.js:5502-5510, 5588-5590): a break can send
// the layout onto a fresh sheet after everything has been put on the one in
// hand, and the empty one is popped rather than shipped. A sheet carrying only
// the page area's own furniture is empty in this sense — nothing of the FORM
// is on it. The last sheet is kept whatever, because a form with nothing on it
// is still a form of one page.
func (p *placer) dropEmptyPages() {
	var keep []Page
	for i, page := range p.layout.Pages {
		if p.touched[i] > 0 || len(keep) == 0 && i == len(p.layout.Pages)-1 {
			keep = append(keep, page)
		}
	}
	p.layout.Pages = keep
}

// touch records that something of the body was laid out on the sheet in hand.
//
// pdf.js asks the same question of the html the body returned for a content
// area — hasSomething ||= html.children?.length > 0 (template.js:5545, 5568) —
// so a CONTAINER holding nothing that is drawn still counts. That is not a
// detail: thirty-eight forms of the corpus carry a break onto a last sheet
// whose only content is an empty subform, and pdf.js ships the sheet.
func (p *placer) touch() { p.touched[len(p.touched)-1]++ }

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

// contentWide is the horizontal room a content area gives the body, and no
// bound where it writes no width. See [contentAvail], which is the same thing
// downwards.
func contentWide(content *Node) Measure {
	w, ok, err := content.Measure("w")
	if !ok || err != nil {
		return unbounded
	}
	return w
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

// A placer carries the sheets being filled and the reasons for what is not on
// them.
type placer struct {
	layout *Layout
	root   *FormNode
	// heights memoises what each node contributes to the stack above it, at
	// the width it has where it sits. See [placer.heightOf].
	heights map[heightKey]height
	// widths and packs are the same thing across the page: what a node takes
	// on the line of a container that wraps, and how one such container's
	// children came out on its lines. Both arrive with lr-tb; see
	// [placer.widthOf] and [placer.linesOf].
	widths map[heightKey]width
	packs  map[heightKey]packing
	// up is each container's layout parent, which is where a row reads the
	// columnWidths it cuts its cells from. It follows the same rule as
	// [contained]: a subformSet is not a parent, its children belong to the
	// container above it (template.js:198-206, 4901-4907).
	up map[*FormNode]*FormNode

	// pager is the sequence of sheets; pageArea and slot are where in it the
	// flow has got to, and area and avail are that content area's box and the
	// room it gives.
	pager    *pager
	pageArea *FormNode
	slot     int
	area     Rect
	avail    Measure
	// wide is the room the content area gives the body across the page, which
	// is where a leaf's text is broken. pdf.js hands the body
	// { width: contentArea.w, height: contentArea.h } (template.js:5550).
	wide Measure
	// touched counts, for each sheet, how much of the BODY was laid out on
	// it. A sheet nothing of the body reached is not shipped; see
	// [placer.dropEmptyPages].
	touched []int
	// chain is the run of splittable containers open between the content area
	// and the element being placed, outermost first, and y is how far down the
	// current content area the flow has got.
	chain []*level
	y     Measure
	// free says nothing that has to move in one piece has been laid out on the
	// sheet in hand yet, so the next thing that does cannot fail to fit. It is
	// pdf.js's firstUnsplittable being null (layout.js:266-268), cleared once
	// per sheet (template.js:5536-5538). See [placer.whole].
	free bool
	// noFail says the flow is inside that first container, whose whole subtree
	// pdf.js also refuses to fail: noLayoutFailure is switched on before its
	// own check and off again only on the way out of it
	// (template.js:313-326, 5075-5178), and every checkDimensions branch reads
	// it (layout.js:286, 319, 339, 361, 373).
	noFail bool
	// blocked is why the flow stopped, once it has: a height no arithmetic
	// gives leaves everything below it in every open container with nowhere to
	// begin.
	blocked string
	// fired marks the breaks already consumed, and used the page areas
	// actually reached.
	fired map[breakKey]bool
	used  map[*FormNode]bool
}

// newPlacer is a placer with the maps it measures into ready.
//
// There are three of them because a node is asked three separate questions —
// how tall it is, how wide it is, and how its children fell onto its lines —
// and none of the three is derivable from the others. All three are written to
// the first time they are asked, so none may be left nil.
func newPlacer() *placer {
	return &placer{
		heights: map[heightKey]height{},
		widths:  map[heightKey]width{},
		packs:   map[heightKey]packing{},
		up:      map[*FormNode]*FormNode{},
		fired:   map[breakKey]bool{},
		used:    map[*FormNode]bool{},
	}
}

// cur is the sheet being filled.
func (p *placer) cur() *Page { return &p.layout.Pages[len(p.layout.Pages)-1] }

// A frame is where an element goes and what room it has there.
type frame struct {
	// x, y is where its own box begins, on the page.
	x, y Measure
	// avail is how much vertical room it has: the content area's height, less
	// whatever the containers between here and there have already spent.
	avail Measure
	// wide is how much horizontal room it has, which is what its text is
	// broken at where the template writes it no width. colW is the width the
	// columns of an enclosing row give it, or nought where no row does; see
	// [heightKey].
	wide, colW Measure
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
	kids := contained(n)
	// A row asks the container above it for its columns
	// ($getSubformParent().columnWidths, template.js:5096-5099), so they are
	// handed down one level whatever the layout here is.
	cols, _ := columnWidths(n.Template)
	lay := layoutOf(n)
	// The room the children have across the page. A margin nobody can read
	// leaves them none rather than none subtracted, so a leaf under it says it
	// has no width to break its text at instead of being broken at the wrong
	// one; the stack below rejects the same children again with the margin as
	// the reason.
	wide := noWidth
	if in, ok := marginOf(n.Template); ok {
		wide = innerWide(n, f.wide, f.colW, lay, in)
	}
	switch lay {
	case "tb", "table":
		p.stack(n, kids, lay, f, wide, cols)
	case "row":
		p.cells(n, kids, f)
	case "lr-tb", "rl-tb":
		p.wrap(n, kids, lay, f, wide, cols)
	case "rl-row":
		p.firstOnly(kids, lay, f, wide, cols)
	default:
		for _, kid := range kids {
			p.place(kid, frame{x: f.x, y: f.y, avail: f.avail, wide: wide, cols: cols})
		}
	}
}

// stack puts a tb or a table container's children one below the other.
func (p *placer) stack(n *FormNode, kids []*FormNode, lay string, f frame, wide Measure, cols []Measure) {
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
		h, why := p.heightOf(kid, wide, 0)
		if why != "" {
			// Where this one begins is known exactly, so it is placed. Where
			// the one after it begins is this one's height, so it is not, and
			// neither is anything after that.
			p.placeAt(kid, frame{x: x, y: y + off, avail: room - off, wide: wide, cols: cols}, nil)
			p.rejectKids(kids[i+1:], fmt.Sprintf(
				"a %s layout stacks its children, and the height of the one above it is not computed: %s", lay, why))
			return
		}
		// pdf.js rounds before comparing and allows two points of slop
		// (layout.js:275, 349). See [fitSlop]. Inside the first container of
		// the sheet that moves in one piece there is no comparison at all:
		// noLayoutFailure is on for the whole of its subtree, and every branch
		// of checkDimensions returns true while it is. See [placer.noFail].
		if !p.noFail && !fits(off+h, room) {
			// A container this deep is one that moves in one piece, so what
			// does not fit in it cannot be carried onto another page: it would
			// leave the rest of the container behind.
			p.rejectKids(kids[i:], noRoomInside)
			return
		}
		p.placeAt(kid, frame{x: x, y: y + off, avail: room - off, wide: wide, cols: cols}, nil)
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
	tall, why := p.contentHeight(n, f.wide)
	over := cell{h: tall, stretched: why == ""}
	room := f.avail
	if why == "" {
		room = tall
	}
	x, col := f.x, 0
	for _, kid := range kids {
		// A cell is measured against every column LEFT in the row rather than
		// against its own, because pdf.js hands it
		// Math.sumPrecise(columnWidths.slice(currentColumn)) for its space
		// (layout.js:184-189) and only replaces its width with its own columns
		// afterwards. See [heightKey].
		rest := remainingWide(f.cols, col)
		if hidden(kid.Template) {
			// pdf.js returns EMPTY before it reads colSpan, so a hidden cell
			// takes no column and the next one is where it would have been.
			p.placeAt(kid, frame{x: x, y: f.y, avail: room, wide: rest}, nil)
			continue
		}
		w, next := columnWidth(f.cols, col, colSpanOf(kid.Template))
		mine := over
		mine.w = w
		p.placeAt(kid, frame{x: x, y: f.y, avail: room, wide: rest, colW: w}, &mine)
		x, col = x+w, next
	}
}

// firstOnly places the first child of a layout this slice does not follow, and
// says why the rest are not there.
//
// Only rl-row is left of the three it was written for: lr-tb and rl-tb wrap
// onto lines now ([placer.wrap]). rl-row fills a ROW from the right, which
// needs the row's own width — the sum of the columnWidths above it — and the
// corpus writes no rl-row at all, so nothing measures whether it would be
// right.
func (p *placer) firstOnly(kids []*FormNode, lay string, f frame, wide Measure, cols []Measure) {
	for i, kid := range kids {
		switch {
		case i > 0:
			p.rejectAll(kid, fmt.Sprintf(
				"a %s layout fills from the right, which needs a width not computed here", lay))
		default:
			// pdf.js accumulates from nought — extra.height starts at 0 and
			// the first child is pushed before anything is added to it
			// (layout.js:145-160) — so the first child sits at the container's
			// own origin. That is the flow algorithm's own answer for one
			// child, not an approximation.
			p.placeAt(kid, frame{x: f.x, y: f.y, avail: f.avail, wide: wide, cols: cols}, nil)
		}
	}
}

// wrap puts a wrapping container's children on the lines [placer.pack] broke
// them onto.
//
// This is the container laid out in ONE piece — inside something that does not
// flow, or inside a container this package moves whole. A wrapping container
// of the flowing chain is [placer.flowLines], which is the same packing walked
// a line at a time so that a page can be turned between two of them.
//
// There is no fit check here, and that is deliberate rather than an omission:
// the height this container reported to whoever placed it is the height of
// this packing, so refusing a line that runs past the bottom would put a box
// somewhere its own container had already been measured as reaching.
// [placer.stack] can check, because a tb's height is the sum of its children's
// and dropping one is arithmetic the container above can still follow.
func (p *placer) wrap(n *FormNode, kids []*FormNode, lay string, f frame, wide Measure, cols []Measure) {
	in, ok := marginOf(n.Template)
	if !ok {
		p.rejectKids(kids, "the container that wraps it onto lines writes a margin that is not in lengths")
		return
	}
	fl, why := p.linesOf(n, wide)
	if why != "" {
		p.rejectKids(kids, fmt.Sprintf(
			"a %s layout wraps its children onto lines, and this one cannot be broken into them: %s", lay, why))
		return
	}
	room := f.avail
	if own, okH, errH := n.Template.Measure("h"); okH && errH == nil {
		room = min(room, own)
	}
	room -= in.vertical()
	x, y := f.x+in.left, f.y+in.top
	for _, b := range fl.boxes {
		p.placeAt(b.node, frame{
			x: lineX(lay, x, wide, b), y: y + b.y,
			avail: room - b.y, wide: b.wide, cols: cols,
		}, nil)
	}
}

// lineX is where one packed child's own box begins across the page.
//
// pdf.js emits no coordinate inside a line at all — createLine wraps the
// children in a div of class xfaLr or xfaRl (layout.js:57-65) and the browser
// lays them out with flexbox. xfaLr is flex-direction: row, so the children
// run from the container's left edge; xfaRl is row-reverse
// (xfa_layer_builder.css:263-273), so they run from its right. The packing is
// the same for both — only where the line is anchored differs.
func lineX(lay string, x, wide Measure, b lineBox) Measure {
	if lay == "rl-tb" {
		return x + wide - b.x - b.w
	}
	return x + b.x
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
	if errW != nil || errH != nil {
		p.reject(n, fmt.Sprintf("its size is written as w=%q h=%q, which is not a size",
			n.Template.Get("w"), n.Template.Get("h")))
		return
	}
	if !okW || !okH {
		// pdf.js's layoutNode (html_utils.js:207-288) supplies whichever is
		// missing by measuring the element's own text. It writes back only the
		// one the template left out — `if (w && this.w === "")`,
		// `if (h && this.h === "")` (template.js:1909, 1923) — so a leaf that
		// writes a width and no height keeps the width it wrote.
		size, why := p.leafSize(n, f.wide, f.colW)
		if why != "" {
			p.reject(n, why)
			return
		}
		if !okW {
			if w, okW = size.w, size.hasW; !okW {
				// pdf.js does not leave it unwritten either. See
				// [placer.unmeasured].
				if w, why = p.unmeasured(n, "minW", "maxW", "w"); why != "" {
					p.reject(n, why)
					return
				}
			}
		}
		if !okH {
			if h, okH = size.h, size.hasH; !okH {
				if h, why = p.unmeasured(n, "minH", "maxH", "h"); why != "" {
					p.reject(n, why)
					return
				}
			}
		}
	}
	r, rotate := Rect{X: f.x, Y: f.y, W: w, H: h}, 0
	if anchor {
		r, rotate = transformedBBox(n.Template, f.x, f.y, w, h)
	}
	page := p.cur()
	page.Boxes = append(page.Boxes, Box{
		Node: n, Kind: n.Kind, Path: n.Path, Rect: r, Rotate: rotate,
		Value: n.Value, Hidden: hidden(n.Template),
	})
}

// reject records one element this did not place.
func (p *placer) reject(n *FormNode, why string) {
	p.layout.Unplaced = append(p.layout.Unplaced, Unplaced{Node: n, Kind: n.Kind, Path: n.Path, Why: why})
}

// rejectAll records every field and draw under a container, so that a whole
// subtree this cannot reach is still counted one element at a time.
//
// It does not descend into a page set. What is under one is the paper rather
// than the body — the letterhead of a sheet, which is placed once per sheet
// that page area makes — and it is reported by [placer.rejectUnusedPages] or
// not at all.
func (p *placer) rejectAll(n *FormNode, why string) {
	if n.Kind == "pageSet" {
		return
	}
	if n.Kind == "field" || n.Kind == "draw" {
		p.reject(n, why)
		return
	}
	for _, k := range n.Kids {
		p.rejectAll(k, why)
	}
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
