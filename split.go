// Copyright (c) 2026, the go-pdfkit/xfa authors
// All rights reserved.
//
// SPDX-License-Identifier: BSD-3-Clause

package xfa

// cutPrecision is pdfium's kXFALayoutPrecision
// (cxfa_contentlayoutprocessor.h:28), the slop every comparison in its split
// arithmetic is made with. It is half a thousandth of a point: not a tolerance
// for heights written in millimetres — that is [fitSlop] — but a guard against
// a cut landing exactly on a child's edge and being read as falling inside it.
const cutPrecision = 0.0005

// cuttable says a container is one this package CUTS across a content area
// boundary rather than moving whole: it is laid out at its own coordinates and
// then sliced, so its children keep the y the template wrote for them.
//
// It is not [placer.splittable] and must not be confused with it. splittable
// gates a FLOW-based split, where a container's children are stacked by the
// cursor and the ones that did not fit begin again at the top of the next
// content area. That is pdf.js's mechanism and it is the wrong one here: a
// positioned container's children are placed by their own x and y, so resuming
// one under the flow cursor would move every one of them.
//
// # Why a positioned container is cut at all
//
// Because the author asked. pdfium decides what may be cut in
// FindLayoutItemSplitPos (cxfa_contentlayoutprocessor.cpp:503-588), which
// switches on CXFA_Node::GetIntact():
//
//	None                   recurse into the children; the cut may stand
//	ContentArea, PageArea  *fProposedSplitPos = fCurVerticalOffset — walk UP
//	default                return false — no change
//
// and GetIntact (cxfa_node.cpp:1536-1591) answers ContentArea for a subform
// whose layout is position or row — UNLESS the subform writes a <keep>, in
// which case GetIntactFromKeep (:2486-2500) returns what the author wrote. At
// the outermost call fCurVerticalOffset is nought, so a positioned subform with
// no keep drives the proposed cut to nought, FindSplitPos returns nought, and
// InsertFlowedItem's `if (fSplitPos > kXFALayoutPrecision)` (:2691) is false:
// the container moves whole.
//
// So pdfium does NOT cut positioned containers in general. It cuts one exactly
// where `<keep intact="none"/>` is written on it.
//
// pdf.js cannot reach that permission at all. Subform[$isSplittable]
// (template.js:4940-4975) answers false at
//
//	if (this.layout === "position" || this.layout.includes("row")) { return false; }
//
// BEFORE it ever reads this.keep.intact. This package followed pdf.js there and
// put 1 423 body leaves off the paper across 39 forms where pdfium put 25 across
// 6. Following pdfium is the standard that has decided every other tie in this
// work, and here the template's author asked for it in the file.
//
// # How large the permission is
//
// Fifty-eight positioned subforms across seventeen of the corpus's 560 forms
// write it. None of them is a form root, thirty-four write a height and thirteen
// write a margin, of which all but three are nought on every side. That is the
// whole population this touches — not the thirty-nine forms whose off-paper
// leaves the position clause dominates, which is a wider set that mostly does
// NOT carry the permission.
//
// # What it does not cover, and why
//
//   - A container that is not a subform. pdfium's GetIntact answers None for an
//     exclGroup, so pdfium would cut one; no exclGroup of the corpus writes a
//     keep of "none", so nothing would say whether it were right.
//   - A row. GetIntact reads position and row alike, so `<keep intact="none"/>`
//     on a row would be cut by pdfium too. A row's children are not at written
//     coordinates — they are cut from the columnWidths above and stretched to
//     the tallest — so cutting one is a different mechanism, and the corpus
//     writes none.
//   - A child of a container that WRAPS onto lines. [placer.whole] is reached
//     only from [placer.flowStack]; a LINE is what moves in [placer.flowLines],
//     and cutting one member of a line would leave the rest of that line on a
//     sheet the first half of it is not on.
//   - A container the template hides. pdfium does not lay one out at all —
//     PresenceRequiresSpace is false and no layout item is made — so there is
//     nothing to cut, and [placer.heightOf] gives it no height either.
func (p *placer) cuttable(n *FormNode) bool {
	return n.Kind == "subform" && layoutOf(n) == "position" && !hidden(n.Template) &&
		n.Template.Child("keep").Get("intact") == "none"
}

// intactOf is pdfium's CXFA_Node::GetIntact (cxfa_node.cpp:1536-1591): what a
// cut does when it lands inside this element.
//
// It is asked of the children of a container being cut, and the answer that
// matters is whether it is "none". Anything else means the cut may not fall
// inside the element and is walked up to its top.
//
// A written <keep intact> wins outright. GetIntactFromKeep has one exception —
// a keep of "none" on a ROW is ignored below XFA 2.08 — and it cannot fire
// here: every template of the corpus is an xfa-template 3.0 document, and
// [placer.cuttable] admits no row anyway.
//
// The field arm is the one that is easy to read past. A field's intact depends
// on the container above it: ContentArea where that container is itself kept
// together, and "none" where the container is positioned, a row or a table —
// which is exactly the case a cut arrives in. So a FIELD inside a container
// being cut may be cut in half, and pdfium does cut it in half. See [cutAt] for
// why this package will not.
func intactOf(p *placer, n *FormNode) string {
	if v := n.Template.Child("keep").Get("intact"); v != "" {
		return v
	}
	switch n.Kind {
	case "subform":
		switch layoutOf(n) {
		case "position", "row":
			return "contentArea"
		default:
			return "none"
		}
	case "draw":
		// A draw is never cut, whatever it holds and wherever it sits.
		return "contentArea"
	case "field":
		parent, ok := p.up[n]
		if !ok || parent.Kind == "pageArea" {
			return "contentArea"
		}
		if intactOf(p, parent) != "none" {
			return "contentArea"
		}
		// The remaining arm of pdfium's switch — a tb parent below XFA 2.08
		// with a written height — keeps a field together. Every corpus template
		// is 3.0, so it is named rather than written: a rule nothing measures is
		// a rule nobody can be wrong about out loud.
		return "none"
	default:
		return "none"
	}
}

// A piece is one child of a container being cut, with where it sits inside the
// container's own box.
//
// y is measured from the TOP of that box, with the container's own top inset
// already added, because that is where [placer.children] puts a positioned
// child and because pdfium's layout items carry their ancestors' insets in
// their position too (CXFA_ContentLayoutItem::GetAbsoluteRect,
// cxfa_contentlayoutitem.cpp:82-90).
type piece struct {
	node *FormNode
	y, h Measure
	// gone says the template hides it, so it takes no room and no cut is ever
	// walked up to it. pdfium does not lay a hidden element out at all —
	// PresenceRequiresSpace is false and no layout item is made — so it is in no
	// cut's arithmetic. It is still PLACED, with the part of the container its
	// own y falls in, because this package reports a hidden element rather than
	// dropping it.
	gone bool
	// keep says a cut may not fall inside it, which is [intactOf] answering
	// anything but "none".
	keep bool
}

// piecesOf measures every child of a container about to be cut, in the order
// the container holds them.
//
// Every y and every height here is readable, and that is not an assumption:
// [placer.whole] asks [placer.heightOf] for this container's own height before
// it asks for a cut, and the positioned arm of [placer.contentHeight] reads the
// same y of the same children and the same [placer.heightOf] of each, at the
// same width. A container holding something none of that can be read for is
// blocked there and never arrives — see [placer.cut] for the margin, which
// [placer.measure] reads in the same pass.
func (p *placer) piecesOf(n *FormNode, in insets, wide Measure) []piece {
	kids := contained(n)
	out := make([]piece, 0, len(kids))
	for _, k := range kids {
		y, _, _ := k.Template.Measure("y")
		h, _ := p.heightOf(k, wide, 0)
		out = append(out, piece{
			node: k, y: in.top + y, h: h,
			gone: hidden(k.Template),
			keep: intactOf(p, k) != "none",
		})
	}
	return out
}

// cutAt walks a proposed cut up until it falls inside nothing, which is
// pdfium's FindLayoutItemSplitPos (cxfa_contentlayoutprocessor.cpp:503-588).
//
// pdfium's rule: an item the cut does not land INSIDE never moves it — the
// guard at :509-513 returns false for one wholly above and for one wholly
// below. An item it does land inside moves the cut up to that item's own top
// where the item's GetIntact is ContentArea or PageArea, and is recursed into
// where it is None.
//
// bisects is the case this package will not follow. A child whose intact is
// "none" — a field of a positioned container is one, by [intactOf] — moves the
// cut nowhere, and pdfium then CUTS IT IN HALF: SplitLayoutItem emits two
// layout items for the one node (:729-864), the top half on one sheet and the
// bottom half on the next. A [Box] is one rectangle for one occurrence and
// [Layout] promises every body leaf appears in exactly one of [Page.Boxes] and
// [Layout.Unplaced]; a field emitted twice would break the promise the type
// exists for, and any judge pairing by name would drop it as ambiguous. So the
// cut is REFUSED there rather than moved somewhere pdfium never puts it: see
// [placer.cut].
//
// behind is how much of the container is already on earlier sheets, so that the
// walk cannot go back past what has been placed.
func cutAt(pieces []piece, behind, proposed Measure) (at Measure, bisects bool) {
	at = proposed
	for changed := true; changed; {
		changed = false
		for _, s := range pieces {
			if !inside(s, behind, at) || !s.keep {
				continue
			}
			at, changed = s.y, true
			break
		}
	}
	for _, s := range pieces {
		if inside(s, behind, at) {
			return at, true
		}
	}
	return at, false
}

// inside says a cut lands within one child, which is pdfium's guard at both
// ends (FindLayoutItemSplitPos, :509-513): a child the cut is at or above the
// top of, and one it is at or past the bottom of, is left alone. A child the
// flow has already carried past is not in the arithmetic at all, nor is one the
// template hides.
func inside(s piece, behind, at Measure) bool {
	if s.gone || s.y+s.h <= behind+cutPrecision {
		return false
	}
	return at > s.y+cutPrecision && at <= s.y+s.h-cutPrecision
}

// cut lays a positioned container out whole and then slices it across as many
// content areas as it needs, which is pdfium's mechanism rather than pdf.js's.
//
// The container is never put on the flowing chain. Its children are placed at
// the y the template wrote for them, less however much of the container is
// already on earlier sheets — so a child written at 744 pt inside a container
// whose first 744 pt are on the sheet before lands at the top of this one,
// which is what SplitLayoutItem does when it moves a child to the second item
// with `pChildItem->s_pos_.y -= fSplitPos` (:806). The partition is by the
// child's TOP y, which is the same line's test: `fSplitPos <= childY` sends it
// on, `fSplitPos >= childY + childH` keeps it here.
//
// total is the height the container reports to the stack above it, which
// [placer.whole] has already measured.
//
// It returns done=false where the flow cannot go on, and cut=false where no cut
// was made at all and the caller should place the container the way it always
// did. Nothing is placed on the second of those.
func (p *placer) cut(n *FormNode, lv *level, total Measure) (done, cut bool) {
	// The margin is readable, for the reason [placer.piecesOf] gives: the
	// height [placer.whole] already has in hand was computed by
	// [placer.measure], which stops at a margin it cannot read before it
	// measures anything else.
	in, _ := marginOf(n.Template)
	wide := innerWide(n, lv.wide, 0, "position", in)
	pieces := p.piecesOf(n, in, wide)
	cols, _ := columnWidths(n.Template)
	left := make([]bool, len(pieces))
	for i := range left {
		left[i] = true
	}

	var behind Measure
	for {
		at, last := unbounded, true
		if !fits(p.y+total-behind, lv.bottom) {
			// pdfium's FindSplitPos(fAvailHeight - fContentCurRowY),
			// InsertFlowedItem:2690, in the frame the flow has got to. The fit
			// it is asked after is [fits] rather than pdfium's own, because the
			// flow around it is pdf.js's and one page boundary cannot be
			// decided by two different tests.
			var bisects bool
			at, bisects = cutAt(pieces, behind, behind+lv.bottom-p.y)
			last = bisects || at <= behind+cutPrecision
			if last && behind == 0 {
				// The container was never cut. Nothing has been placed, and the
				// caller carries on as it always did.
				return false, false
			}
			// A last part that could not be cut goes down whole, which is what
			// pdfium does when FindSplitPos returns nought.
			if last {
				at = unbounded
			}
		}
		// pdf.js's free pass, applied to the part in hand: the first thing on a
		// sheet that moves in one piece cannot fail, and neither can anything
		// inside it. See [placer.whole].
		first := p.free
		p.free, p.noFail = false, first
		p.touch()
		top := p.y - behind
		for i, s := range pieces {
			if !left[i] || s.y+cutPrecision >= at {
				continue
			}
			left[i] = false
			// The container's own insets move its children in, exactly as
			// [placer.children] does for a positioned container that is not
			// cut. The top one is already in the part's own y, so the frame
			// carries it and the part's y is added to it by [placer.place]
			// reading the child's written y.
			p.place(s.node, frame{
				x: lv.x + in.left, y: top + in.top,
				avail: lv.bottom - top, wide: wide, cols: cols,
			})
		}
		p.noFail = false
		if last {
			// What the container adds to the stack above it is the whole of its
			// height measured from where this last part began, which is
			// SplitLayoutItem's second item being `s_size_.height - fSplitPos`
			// tall (:762).
			p.y = top + total
			return true, true
		}
		behind = at
		if !p.advance(p.overflowTo(n)) {
			p.blocked = noNextPage
			for i, s := range pieces {
				if left[i] {
					p.rejectAll(s.node, noNextPage)
				}
			}
			return false, true
		}
	}
}
