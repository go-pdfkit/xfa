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

// A level is one container of the chain currently being flowed: the run of
// splittable containers between the content area and the element being placed.
//
// It is kept as a chain rather than as recursion state because of what happens
// at a page boundary. When the flow moves to the next content area, EVERY
// container in the chain begins again at the top of it — pdf.js re-enters the
// whole tree with the new space and each container's accumulated height
// restarts at nought (template.js:5055-5070) — so all their origins and all
// their room have to be recomputed together. See [placer.rebase].
type level struct {
	in insets
	// own is the height the template writes for it, or nought; limit is the
	// same thing as a bound, unbounded where none is written. The two differ
	// because a container with no height still holds its contents, and one
	// with a height still reaches past it if what it holds is taller
	// (template.js:5222).
	own, limit Measure
	// xoff and yoff are the container's own x and y, which count only where a
	// POSITIONED parent put it. A flow layout discards them
	// (html_utils.js:347-350), so they are nought for everything but the
	// outermost subform, whose parent is the content area.
	xoff, yoff Measure
	// base is where the container's own box begins, x is where its children
	// begin, top is where its content begins and bottom is as far as its
	// content may reach. All four are absolute on the page and all four are
	// recomputed at every page boundary.
	base, x, top, bottom Measure
	cols                 []Measure
}

// splittable says a container may be broken across a page boundary, so that
// the children before the break stay where they are and the rest go on.
//
// pdf.js's rule (Subform[$isSplittable], template.js:4940-4975): not a
// positioned layout, not a row, and not kept intact. A container that fails it
// moves whole. The last clause is not decoration — 1 355 elements in the
// corpus carry keep intact="contentArea", which is a designer saying "do not
// let this table row land half on one page and half on the next".
//
// The enclosing chain matters too, and is handled by construction rather than
// by a test: this is only ever asked of a container the flow has already
// reached, and the flow only reaches through splittable ones.
func splittable(n *FormNode) bool {
	lay := layoutOf(n)
	if lay == "position" || strings.Contains(lay, "row") {
		return false
	}
	switch n.Template.Child("keep").Get("intact") {
	case "contentArea", "pageArea", "parentSubform":
		return false
	}
	return true
}

// flowable says a container is one this slice carries across pages: it stacks
// downwards, which is the only direction this package computes, and it may be
// split.
func flowable(n *FormNode) bool {
	switch n.Kind {
	case "field", "draw":
		return false
	}
	lay := layoutOf(n)
	return (lay == "tb" || lay == "table") && splittable(n)
}

// contentAreas are the boxes a page area offers the body, in order.
func contentAreas(area *FormNode) []*Node {
	return area.Template.Children("contentArea")
}

// noNextPage is why an element is not placed when the page set has run out.
const noNextPage = "the form ran out of pages before it: its page set gives no page after the last one"

// tooTallForAPage is why an element that cannot be split is not placed when it
// does not fit a whole empty content area.
const tooTallForAPage = "it is taller than a whole content area and cannot be broken, " +
	"so it could only be placed by splitting it across a page boundary, which this slice does not do"

// noRoomInside is why an element is not placed where the container holding it
// writes a height too small for what it holds. It cannot be carried to another
// page: a container that is not split takes its contents with it.
const noRoomInside = "there is no room left for it inside the container holding it, " +
	"which is not one this slice splits across a page boundary"

// body lays the form's outermost subform out across as many pages as it needs.
func (p *placer) body(root *FormNode) {
	x, _, errX := root.Template.Measure("x")
	y, _, errY := root.Template.Measure("y")
	if errX != nil || errY != nil {
		p.rejectAll(root, fmt.Sprintf("its origin is written as x=%q y=%q, which is not a place",
			root.Template.Get("x"), root.Template.Get("y")))
		return
	}
	// A subform anchored by another corner, or turned, is placed by its
	// coordinates rather than by its contents, so it is laid out whole on the
	// page it starts on. Neither happens on an outermost subform in the
	// corpus; the check is here so that one would not be flowed as if its
	// origin were its top left.
	if !flowable(root) || anchored(root.Template) || rotateOf(root.Template) != 0 {
		p.place(root, frame{x: p.area.X, y: p.area.Y, avail: p.avail})
		return
	}
	if p.fires(root, false) {
		return
	}
	if !p.push(root, x, y) {
		return
	}
	p.flow(root)
	p.pop()
}

// flow puts a splittable stack's children one below the other, moving to the
// next content area when one of them does not fit.
//
// The difference from [placer.stack], which lays out the same thing inside a
// container that cannot be split, is only what happens at the bottom: this
// asks for another content area, and that one reports what is left.
func (p *placer) flow(n *FormNode) {
	lv := p.chain[len(p.chain)-1]
	kids := contained(n)
	for i, kid := range kids {
		if p.blocked != "" {
			p.rejectKids(kids[i:], p.blocked)
			return
		}
		if p.fires(kid, false) {
			p.rejectKids(kids[i:], noNextPage)
			return
		}
		if flowable(kid) {
			if !p.push(kid, 0, 0) {
				continue
			}
			p.flow(kid)
			p.pop()
		} else if !p.whole(kid, lv, layoutOf(n)) {
			p.rejectKids(kids[i+1:], p.blocked)
			return
		}
		if p.fires(kid, true) {
			p.rejectKids(kids[i+1:], noNextPage)
			return
		}
	}
}

// whole places one child of a flowing stack that is not itself flowed: a
// field, a draw, or a container that has to move in one piece. It returns
// false where the flow cannot go on.
func (p *placer) whole(kid *FormNode, lv *level, lay string) bool {
	h, why := p.heightOf(kid)
	if why != "" {
		// Where THIS one begins is known exactly, so it is placed. Where the
		// one after it begins is this one's height, so it is not, and neither
		// is anything after that, at this level or at any above it.
		p.touch()
		p.placeAt(kid, frame{x: lv.x, y: p.y, avail: lv.bottom - p.y, cols: lv.cols}, nil)
		p.blocked = fmt.Sprintf(
			"a %s layout stacks its children, and the height of the one above it is not computed: %s", lay, why)
		return false
	}
	if !fits(p.y+h, lv.bottom) {
		switch {
		case !fits(lv.top+h, lv.bottom) && p.chain[0] == lv:
			// It could not be placed on an empty page either, and there is no
			// container above it whose own height is what is too small.
			p.rejectAll(kid, tooTallForAPage)
			return true
		case !fits(lv.top+h, lv.bottom):
			p.rejectAll(kid, noRoomInside)
			return true
		case !p.advance(nil):
			p.blocked = noNextPage
			p.rejectAll(kid, noNextPage)
			return false
		case !fits(p.y+h, lv.bottom):
			// The new content area is smaller than the one it came from.
			p.rejectAll(kid, tooTallForAPage)
			return true
		}
	}
	p.touch()
	p.placeAt(kid, frame{x: lv.x, y: p.y, avail: lv.bottom - p.y, cols: lv.cols}, nil)
	p.y += h
	return true
}

// fits is pdf.js's own test, to the point of the rounding it does first:
// Math.round(h - space.height) <= ERROR, with ERROR = 2 (layout.js:275, 349).
func fits(reach, bottom Measure) bool {
	return math.Round(float64(reach-bottom)) <= fitSlop
}

// fires reads one of a container's breaks, works out what it does, and does
// it. It returns true where the layout could not go where the break sent it.
//
// A break fires ONCE. pdf.js marks the node the first time handleBreak sees it
// and returns false ever after (template.js:329-334), and its binder gives
// each occurrence of a repeated container its own copy of the node — so a
// table row repeated eleven times breaks eleven times, not once. The mark here
// is on the occurrence for the same reason.
func (p *placer) fires(n *FormNode, after bool) bool {
	key := breakKey{n, after}
	if p.fired[key] {
		return false
	}
	var spec breakSpec
	var ok bool
	if after {
		spec, ok = breakAfterOf(n)
	} else {
		spec, ok = breakBeforeOf(n)
	}
	if !ok {
		return false
	}
	p.fired[key] = true
	to, does := p.pager.fire(p.root, spec, p.pageArea, p.slot)
	if !does {
		return false
	}
	return !p.advance(&to)
}

// A breakKey names one of the two breaks a container may carry.
type breakKey struct {
	n     *FormNode
	after bool
}

// push opens a container of the flowing chain: it works out where its children
// begin and how much room they have, from where the flow has got to.
//
// It returns false where the container's margin cannot be read, which stops
// the subtree rather than taking a margin nobody can read as nought.
func (p *placer) push(n *FormNode, xoff, yoff Measure) bool {
	in, ok := marginOf(n.Template)
	if !ok {
		p.rejectAll(n, "the container that stacks it writes a margin that is not in lengths")
		return false
	}
	p.touch()
	lv := &level{in: in, xoff: xoff, yoff: yoff}
	if h, okH, errH := n.Template.Measure("h"); okH && errH == nil {
		lv.own, lv.limit = h, h
	} else {
		lv.limit = unbounded
	}
	lv.cols, _ = columnWidths(n.Template)
	p.seat(lv, p.chainX(), p.chainBottom())
	p.chain = append(p.chain, lv)
	return true
}

// seat works out one container's origin and its room from where the flow has
// got to, and moves the flow to the top of its content.
//
// The room a container has is pdf.js's: availableSpace.height =
// Math.min(this.h || Infinity, availableSpace.height) (template.js:5062-5065),
// less its own insets (getAvailableSpace, layout.js:162-171). The container's
// own y is NOT taken out of it — pdf.js hands the whole content area's height
// down whatever the offset — which is why the room is measured from after the
// offset rather than to an absolute bottom.
func (p *placer) seat(lv *level, parentX, parentBottom Measure) {
	room := min(lv.limit, parentBottom-p.y) - lv.in.vertical()
	lv.base = p.y
	lv.x = parentX + lv.xoff + lv.in.left
	lv.top = p.y + lv.yoff + lv.in.top
	lv.bottom = lv.top + room
	p.y = lv.top
}

// pop closes a container of the flowing chain and moves the flow past it.
//
// What the container adds to the stack above it is what pdf.js writes into its
// style.height: Math.max(extra.height + marginV, this.h || 0)
// (template.js:5222). Where the container was broken across a page,
// extra.height is what it holds ON THIS page, because pdf.js restarts the
// accumulation when it re-enters it.
func (p *placer) pop() {
	lv := p.chain[len(p.chain)-1]
	p.chain = p.chain[:len(p.chain)-1]
	p.y = lv.base + max(p.y-lv.top+lv.in.vertical(), lv.own)
}

// chainX and chainBottom are where the innermost open container puts its
// children and how far they may reach, or the content area's own when none is
// open.
func (p *placer) chainX() Measure {
	if len(p.chain) == 0 {
		return p.area.X
	}
	return p.chain[len(p.chain)-1].x
}

func (p *placer) chainBottom() Measure {
	if len(p.chain) == 0 {
		return p.area.Y + p.avail
	}
	return p.chain[len(p.chain)-1].bottom
}

// rebase begins every open container again at the top of the content area the
// flow has just moved to.
//
// This is the whole of what a page boundary does to the geometry. pdf.js
// arrives at it by re-entering the tree with a new space and letting each
// container recompute from nought; the chain is here so that the same thing
// can be done in one pass, without laying anything out twice.
func (p *placer) rebase() {
	p.y = p.area.Y
	x, bottom := p.area.X, p.area.Y+p.avail
	for _, lv := range p.chain {
		p.seat(lv, x, bottom)
		x, bottom = lv.x, lv.bottom
	}
}

// advance moves the flow to the next content area, and to the next sheet where
// the page area in hand has no more of them.
//
// A break says where to go; nothing means "wherever comes next". It returns
// false where there is nowhere: a page set that yields no further page, or a
// page area holding no content area at all.
func (p *placer) advance(to *breakTo) bool {
	area, slot := p.pageArea, 0
	fresh := true
	switch {
	case to != nil && to.area == nil && !to.page:
		// Another content area of the sheet in hand. Running off the end of
		// them is the same thing as running off the end of the sheet, which is
		// what makes a break to "the next content area" a page break on the
		// six hundred and fifty-three page areas of the corpus that hold one.
		if to.index < len(contentAreas(area)) {
			p.openArea(area, to.index, false)
			return true
		}
	case to != nil && to.area != nil:
		slot = to.index
		if p.pager.use(to.area) {
			p.pager.number++
			p.openArea(to.area, slot, true)
			return true
		}
	default:
		if p.slot+1 < len(contentAreas(area)) {
			p.openArea(area, p.slot+1, false)
			return true
		}
	}
	p.pager.number++
	next := p.pager.next(area)
	if next == nil {
		return false
	}
	if slot >= len(contentAreas(next)) {
		slot = 0
	}
	p.openArea(next, slot, fresh)
	return true
}

// openArea begins filling one content area, starting a fresh sheet first where
// the page area has changed or a break asked for one.
func (p *placer) openArea(area *FormNode, slot int, fresh bool) {
	if fresh {
		p.startPage(area)
	}
	p.pageArea, p.slot = area, slot
	areas := contentAreas(area)
	if slot < len(areas) {
		p.area = contentRect(areas[slot])
		p.avail = contentAvail(areas[slot])
	} else {
		p.area, p.avail = Rect{}, 0
	}
	p.rebase()
}

// startPage adds a sheet and draws the page area's own furniture on it.
//
// The furniture — the letterhead, the rules, the page number — sits in the
// page's frame rather than the content area's, and pdf.js pushes it straight
// into the page div with the content area's div beside it
// (template.js:4111-4114). A page area used for six sheets draws it on all
// six, which is the one place a form's elements are not in one-to-one
// correspondence with the boxes on the paper.
func (p *placer) startPage(area *FormNode) {
	page := Page{}
	page.Width, page.Height = pageSize(area.Template)
	for _, c := range contentAreas(area) {
		page.Areas = append(page.Areas, contentRect(c))
	}
	p.layout.Pages = append(p.layout.Pages, page)
	p.touched = append(p.touched, 0)
	p.used[area] = true
	p.placeAt(area, frame{avail: given(page.Height)}, nil)
}
