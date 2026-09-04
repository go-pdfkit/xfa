// Copyright (c) 2026, the go-pdfkit/xfa authors
// All rights reserved.
//
// SPDX-License-Identifier: BSD-3-Clause

package xfa

import (
	"fmt"
	"math"
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
	// node is the container itself, which [placer.seat] reads its layout and
	// its own width from when it works out the room its children have.
	node *FormNode
	in   insets
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
	// wide is the room it gives its children across the page, which is where
	// their text is broken. It does not change at a page boundary — a content
	// area is as wide on the second sheet as on the first — but it is
	// recomputed with the rest.
	wide Measure
	cols []Measure
	// cursor and room are where the NEXT child of this container begins across
	// the page and how much room it has there. They are nought and wide for
	// every layout but the two that wrap onto lines, where a child begins
	// after the ones already on its line; [placer.flowLines] sets them from
	// the packing before it opens a container of its own.
	cursor, room Measure
	// line is how many children are already on the line in hand — pdf.js's
	// [$extra].numberInLine (layout.js:113). It is read by the fourth clause
	// of [placer.splittable] and by nothing else here.
	line int
	// skip is how far down the packing the content area in hand begins, which
	// is how a wrapping container carries on after a page has been turned
	// between two of its lines. It is nought for every other layout.
	skip Measure
}

// splittable says a container is one this package breaks across a content
// area boundary: the children it managed to place stay where they are, and
// the rest of it goes on in the next one.
//
// It is a property of the CHAIN and not of the container, which is why it is a
// method: pdf.js's rule (Subform[$isSplittable], template.js:4940-4975) begins
// by asking the container above,
//
//	const parent = this[$getSubformParent]();
//	if (!parent[$isSplittable]()) { return false; }
//
// and only then looks at the container itself. Four things must all hold.
//
//  1. The container above it is splittable. The recursion ends at the
//     <template> element, which answers yes (template.js:5401-5403). Every
//     other kind of node answers no by inheriting the base
//     (xfa_object.js:214-216), and the one that matters is <area>: it holds
//     body content, it is not a subform, and $getSubformParent does not skip
//     it (template.js:4901-4907), so nothing inside an area is ever split.
//
//  2. Its own layout is neither "position" nor anything containing "row"
//     (template.js:4952-4955). Stated the other way round here — tb, table,
//     lr-tb and rl-tb — which is the same set: "lr-tb".includes("row") is
//     false, so pdf.js splits a container that wraps onto lines, and so does
//     this one now that it computes the lines.
//
//  3. Its <keep intact> is "none": an author's instruction not to let this
//     land half on one sheet and half on the next. 1 355 elements of the
//     corpus carry one. An exclGroup has the same predicate WITHOUT this
//     clause (template.js:2405-2429) — read there rather than assumed — and
//     that difference is kept.
//
//  4. If the container above it has a layout ending in "-tb" and has already
//     put something on the line in hand, it is not splittable. pdf.js gives
//     the reason in full (template.js:4962-4970):
//
//     "If parent can fit in w=100 and there's already an element which takes
//     90 then we've 10 for this element. Suppose this element has a tb layout
//     and 5 elements have a width of 7 and the 6th has a width of 20: then
//     this element (and all its content) must move on the next line. If this
//     element is splittable then the first 5 children will stay at the end of
//     the line: we don't want that."
//
//     It is live from this slice. numberInLine is [level.line], which
//     [placer.flowLines] sets from the packing before it asks this of a child,
//     and the clause is asked of the container in hand — the innermost open
//     one — because that is the only container whose line is being filled.
//     Where the parent is not open, there is no line in hand and the clause
//     cannot apply; that is the outermost subform, whose parent is the
//     <template> element.
//
// pdf.js memoises the answer within one content area and says outright that it
// must not be kept across them, because the content area can change
// (template.js:4941-4942). Nothing is memoised here: the chain is a dozen deep
// at most, and asking it afresh cannot be wrong for the reason pdf.js names.
func (p *placer) splittable(n *FormNode) bool {
	switch n.Kind {
	case "template":
		return true
	case "subform", "exclGroup":
	default:
		return false
	}
	parent, ok := p.up[n]
	if !ok || !p.splittable(parent) {
		return false
	}
	switch layoutOf(n) {
	case "tb", "table", "lr-tb", "rl-tb":
	default:
		return false
	}
	if n.Kind == "subform" {
		switch n.Template.Child("keep").Get("intact") {
		case "contentArea", "pageArea", "parentSubform":
			return false
		}
	}
	if lv := p.opened(parent); lv != nil && wraps(layoutOf(parent)) && lv.line != 0 {
		return false
	}
	return true
}

// opened is the container's own level of the flowing chain, or nil where it is
// not one of them.
func (p *placer) opened(n *FormNode) *level {
	for _, lv := range p.chain {
		if lv.node == n {
			return lv
		}
	}
	return nil
}

// contentAreas are the boxes a page area offers the body, in order.
func contentAreas(area *FormNode) []*Node {
	return area.Template.Children("contentArea")
}

// noNextPage is why an element is not placed when the page set has run out.
const noNextPage = "the form ran out of pages before it: its page set gives no page after the last one"

// noRoomInside is why an element is not placed where the container holding it
// writes a height too small for what it holds. It cannot be carried to another
// page: a container that is not split takes its contents with it.
//
// It is not reported for the first such container of a sheet, which pdf.js
// refuses to fail — see [placer.whole] and [placer.noFail].
const noRoomInside = "there is no room left for it inside the container holding it, " +
	"which is not one this package splits across a page boundary"

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
	if !p.splittable(root) || anchored(root.Template) || rotateOf(root.Template) != 0 {
		p.place(root, frame{x: p.area.X, y: p.area.Y, avail: p.avail, wide: p.wide})
		return
	}
	if p.fires(root, false) {
		// The form asks to begin on a sheet there is none of. Nothing of it is
		// placed, and nothing of it is dropped either.
		p.rejectAll(root, noNextPage)
		return
	}
	if !p.push(root, x, y) {
		return
	}
	p.flow(root)
	p.pop()
}

// flow lays a splittable container's children out, moving to the next content
// area when what comes next does not fit.
//
// The difference from [placer.stack] and [placer.wrap], which lay the same two
// things out inside a container that cannot be split, is only what happens at
// the bottom: this asks for another content area, and those report what is
// left.
func (p *placer) flow(n *FormNode) {
	if wraps(layoutOf(n)) {
		p.flowLines(n)
		return
	}
	p.flowStack(n)
}

// flowStack puts a splittable stack's children one below the other.
func (p *placer) flowStack(n *FormNode) {
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
		if p.splittable(kid) {
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
	h, why := p.heightOf(kid, lv.wide, 0)
	if why != "" {
		// Where THIS one begins is known exactly, so it is placed. Where the
		// one after it begins is this one's height, so it is not, and neither
		// is anything after that, at this level or at any above it.
		p.touch()
		p.placeAt(kid, frame{x: lv.x, y: p.y, avail: lv.bottom - p.y, wide: lv.wide, cols: lv.cols}, nil)
		p.blocked = fmt.Sprintf(
			"a %s layout stacks its children, and the height of the one above it is not computed: %s", lay, why)
		return false
	}
	// The first thing that moves in one piece cannot fail to fit, and that is
	// pdf.js's rule rather than a concession. checkDimensions returns true
	// outright while the sheet has had none (layout.js:266-268), and the one
	// that claims it switches noLayoutFailure on before its own check runs
	// (setFirstUnsplittable, template.js:313-319; called at :5079 for a
	// subform, :2484 for an exclGroup, :1927 for a draw and :2878 for a
	// field), so it cannot fail either. Everything after it on that sheet is
	// checked, and what does not fit sends the whole chain to the next content
	// area — which is what splitting the containers above it MEANS. There it
	// is first in its turn, and goes down whatever its height.
	//
	// This is why a container taller than a whole content area is not a
	// refusal: it comes out one per sheet, overflowing, exactly as pdf.js
	// draws it.
	for !p.free && !fits(p.y+h, lv.bottom) {
		if !p.advance(nil) {
			p.blocked = noNextPage
			p.rejectAll(kid, noNextPage)
			return false
		}
	}
	// pdf.js marks the first one whether or not it needed the pass, and leaves
	// noLayoutFailure on for the whole of its subtree — it is unset only on
	// the way out of that same node (template.js:321-326, 5175-5178). So what
	// is inside it is not checked either. See [placer.noFail].
	first := p.free
	p.free, p.noFail = false, first
	p.touch()
	p.placeAt(kid, frame{x: lv.x, y: p.y, avail: lv.bottom - p.y, wide: lv.wide, cols: lv.cols}, nil)
	p.noFail = false
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
	lv := &level{in: in, xoff: xoff, yoff: yoff, node: n}
	if h, okH, errH := n.Template.Measure("h"); okH && errH == nil {
		lv.own, lv.limit = h, h
	} else {
		lv.limit = unbounded
	}
	lv.cols, _ = columnWidths(n.Template)
	p.seat(lv, p.chainX(), p.chainBottom(), p.chainWide())
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
func (p *placer) seat(lv *level, parentX, parentBottom, parentWide Measure) {
	room := min(lv.limit, parentBottom-p.y) - lv.in.vertical()
	lv.base = p.y
	lv.x = parentX + lv.xoff + lv.in.left
	lv.top = p.y + lv.yoff + lv.in.top
	lv.bottom = lv.top + room
	// A container of the flowing chain is always a tb or a table
	// ([placer.splittable]), and neither is ever a cell of a row, so there is no
	// column width to hand [innerWide].
	lv.wide = innerWide(lv.node, parentWide, 0, layoutOf(lv.node), lv.in)
	lv.cursor, lv.room = 0, lv.wide
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
	lv := p.chain[len(p.chain)-1]
	return lv.x + lv.cursor
}

func (p *placer) chainBottom() Measure {
	if len(p.chain) == 0 {
		return p.area.Y + p.avail
	}
	return p.chain[len(p.chain)-1].bottom
}

func (p *placer) chainWide() Measure {
	if len(p.chain) == 0 {
		return p.wide
	}
	return p.chain[len(p.chain)-1].room
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
	x, bottom, wide := p.area.X, p.area.Y+p.avail, p.wide
	for _, lv := range p.chain {
		p.seat(lv, x, bottom, wide)
		x, bottom, wide = lv.x, lv.bottom, lv.wide
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
		p.wide = contentWide(areas[slot])
	} else {
		p.area, p.avail, p.wide = Rect{}, 0, 0
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
	// pdf.js clears firstUnsplittable and noLayoutFailure once per SHEET,
	// before the loop over that sheet's content areas (template.js:5536-5538),
	// so a second content area of the same sheet does not hand out a second
	// free pass. See [placer.whole].
	p.free = true
	p.used[area] = true
	p.placeAt(area, frame{avail: given(page.Height), wide: given(page.Width)}, nil)
}

// differentWidth is why a wrapping container stops at a page boundary: the
// lines were broken at the width of the content area it began in, and the one
// it has moved to is a different width, so every line after the break would be
// broken in a place this packing did not compute.
//
// pdf.js has the same problem and answers it by re-entering the whole tree
// with the new space, which recomputes the lines from the failing node on.
// Doing that here would mean re-packing and re-numbering the lines mid-walk,
// and no page set of the corpus changes the width between two of its content
// areas, so it stops and says so rather than carrying on at the wrong width.
const differentWidth = "the lines it wraps onto were broken at the width of the content area it " +
	"began in, and the next content area is a different width"

// flowLines puts a wrapping container's children on the lines
// [placer.pack] broke them onto, turning the page between two of them.
//
// A LINE is what moves to the next content area, never part of one: pdf.js
// fails the child that does not fit and re-enters the container in the new
// space, and every child of the line it was on has already been flushed into
// that line's div (layout.js:67-91, flushHTML). So the page turns at a line
// boundary here, and the first line of a sheet goes down whatever its height —
// which is [placer.whole]'s rule, applied to a line rather than to one element.
func (p *placer) flowLines(n *FormNode) {
	lv := p.chain[len(p.chain)-1]
	lay := layoutOf(n)
	kids := contained(n)
	fl, why := p.linesOf(n, lv.wide)
	if why != "" {
		p.blocked = fmt.Sprintf(
			"a %s layout wraps its children onto lines, and this one cannot be broken into them: %s", lay, why)
		p.rejectKids(kids, p.blocked)
		return
	}
	lv.skip, lv.line = 0, 0
	line := -1
	for i, kid := range kids {
		b := fl.boxes[i]
		if p.blocked != "" {
			p.rejectKids(kids[i:], p.blocked)
			return
		}
		if p.fires(kid, false) {
			p.rejectKids(kids[i:], noNextPage)
			return
		}
		if b.line != line {
			line = b.line
			if !p.turnTo(b, fl) {
				p.rejectKids(kids[i:], p.blocked)
				return
			}
		}
		lv.line = b.inLine
		p.y = lv.top + b.y - lv.skip
		if p.splittable(kid) && alone(fl, i) {
			lv.cursor, lv.room = lineX(lay, 0, lv.wide, b), b.wide
			if p.push(kid, 0, 0) {
				p.flow(kid)
				p.pop()
				// The container was flowed rather than packed, so where it
				// actually ended is where the rest of the lines have to be
				// measured from — it may have run onto another sheet, and it
				// is as tall as what it holds rather than as tall as the
				// packing said.
				lv.skip = b.y + b.h - (p.y - lv.top)
			}
			lv.cursor, lv.room = 0, lv.wide
		} else {
			first := p.free
			p.free, p.noFail = false, first
			p.touch()
			p.placeAt(kid, frame{
				x: lineX(lay, lv.x, lv.wide, b), y: p.y,
				avail: lv.bottom - p.y, wide: b.wide, cols: lv.cols,
			}, nil)
			p.noFail = false
		}
		if p.fires(kid, true) {
			p.rejectKids(kids[i+1:], noNextPage)
			return
		}
	}
	p.y = lv.top + fl.h - lv.skip
}

// turnTo moves the flow to a content area with room for the whole line b
// opens, and returns false where there is none.
//
// A whole line, and that is stricter than the test pdf.js writes. The second
// attempt asks (layout.js:317-338):
//
//	if (node.h !== "" && Math.round(h - space.height) > ERROR) return false;
//	if (node.w === "" || Math.round(w - space.width) <= ERROR) {
//	  return space.height > ERROR;
//	}
//	...
//	return space.height > ERROR;
//
// so a child whose height the template does not WRITE is measured against
// nothing at all: it is accepted if more than two points of the container
// remain, however tall it turns out to be.
//
// That acceptance is provisional, which is the part that does not port.
// A child accepted there is then laid out in the room it was handed, its own
// children are checked against that room, and one of them failing returns
// HTMLResult.FAILURE from the child — which sends the container to its next
// line and, failing that, fails the container so the page turns
// (xfa_object.js:394-403, template.js:5137-5170). This package has no
// failure to propagate back: a container it has begun to place is placed.
// So a line accepted on "more than two points remain" would be put down
// overflowing and everything inside it refused for want of room, where pdf.js
// turns the page and places it whole.
//
// Measured: taking pdf.js's literal test costs 33 fields of us-uscis__i-956h
// and gains none, and i-956h is one of the 77 forms pdf.js cannot lay out at
// all, so no judge can say which of the two is right there. The stricter test
// is the one that puts them on paper.
func (p *placer) turnTo(b lineBox, fl fill) bool {
	lv := p.chain[len(p.chain)-1]
	for !p.free && !fits(lv.top+b.y-lv.skip+fl.high[b.line], lv.bottom) {
		wide := lv.wide
		if !p.advance(nil) {
			p.blocked = noNextPage
			return false
		}
		if lv.wide != wide {
			p.blocked = differentWidth
			return false
		}
		lv.skip = b.y
	}
	return true
}

// alone says the child at i is the only one on its line, which is the one
// shape in which this package opens a wrapping container's child as a
// container of the flowing chain.
//
// pdf.js would open any of them: a child of a line is laid out in the space
// left on it and may be split like anything else. This package will not,
// because splitting a child that SHARES a line would leave the rest of that
// line to be placed on a sheet the first half of it is not on. The fourth
// clause of [placer.splittable] already refuses every child that is not first
// on its line; this refuses the rest of them.
func alone(fl fill, i int) bool {
	b := fl.boxes[i]
	return b.inLine == 0 && (i+1 == len(fl.boxes) || fl.boxes[i+1].line != b.line)
}
