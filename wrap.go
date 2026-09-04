// Copyright (c) 2026, the go-pdfkit/xfa authors
// All rights reserved.
//
// SPDX-License-Identifier: BSD-3-Clause

package xfa

import (
	"fmt"
	"math"
)

// How a container that wraps its children onto lines works out where they go.
//
// This is pdf.js's lr-tb and rl-tb: the two layouts that fill a line across
// the page and begin a new one when the next child does not fit. The rule is
// addHTML's (layout.js:107-129) and checkDimensions' (layout.js:279-334), read
// together, because neither is the layout on its own — one says where a child
// goes once it has been accepted, the other says whether it is accepted at
// all, and the two-attempt protocol between them is the line breaking.
//
//	if (!extra.line || extra.attempt === 1) {
//	  extra.line = createLine(node, []);
//	  extra.children.push(extra.line);
//	  extra.numberInLine = 0;
//	}
//	extra.numberInLine += 1;
//	extra.line.children.push(html);
//	if (extra.attempt === 0) {
//	  extra.currentWidth += w;
//	  extra.height = Math.max(extra.height, extra.prevHeight + h);
//	} else {
//	  extra.currentWidth = w;
//	  extra.prevHeight = extra.height;
//	  extra.height += h;
//	  extra.attempt = 0;
//	}
//	extra.width = Math.max(extra.width, extra.currentWidth);
//
// attempt 0 is "try to put it on the line in hand". A child refused there
// raises the attempt to 1, which opens a fresh line, sets numberInLine back to
// nought and — in the branch above — drops straight back to line mode for
// everything after it. The height arithmetic differs between the two branches
// and the difference is the whole of it: on a line a child is measured from
// the TOP of that line (prevHeight + h), on a new line the accumulated height
// becomes the new line's top.
//
// # What this pass carries, and what it does not
//
// The break is decided on WIDTH alone here. checkDimensions also refuses a
// child taller than the room left (layout.js:284-290) and would put it on the
// next line for that reason; the room left is a quantity this measurement does
// not carry, because a container's height is arrived at before it is known
// where the container will go — which is the whole reason the measurement is a
// pass of its own, and the same limit [placer.contentHeight] has for tb.
//
// So the height every branch here compares against is unbounded, and the two
// checks come out as:
//
//   - attempt 0: a child whose width is written goes on the line if it fits in
//     what is left of it, and otherwise only if the line is still empty — the
//     first thing on a line goes down whatever its width. A child whose width
//     is NOT written goes on the line if there is any of it left at all
//     (space.width > ERROR, layout.js:303).
//   - attempt 1: a child whose width is written goes on the new line if it
//     fits in a whole one. If it does not, pdf.js asks whether a container
//     ABOVE this one still has room on ITS line ($isThereMoreWidth,
//     template.js:4913-4920) and fails the whole container if it has. That is
//     the one answer this pass refuses rather than guesses; see
//     [widerThanALine]. No child of the 790 wrapping containers of the corpus
//     is wider than the line it is on.

// A lineBox is one child of a wrapping container, packed.
type lineBox struct {
	node *FormNode
	// x and y are where its own box begins, measured from the container's
	// content origin. x counts from the container's left edge whatever the
	// layout: rl-tb reverses at placement, because the reversal is the CSS
	// class alone (xfaRl is flex-direction: row-reverse,
	// xfa_layer_builder.css:269-273) and the arithmetic above is the same for
	// both.
	x, y Measure
	// w and h are the size the packing gave it, which is the bbox pdf.js hands
	// to addHTML.
	w, h Measure
	// wide is the room it was measured against — what is left of the line for
	// a child put on it, a whole line for a child that opened one. Keeping it
	// means the placement breaks its text at the width the packing did.
	wide Measure
	// line is which line it went on, and inLine is how many children were
	// already on that line when it was asked. inLine is pdf.js's
	// numberInLine (layout.js:113), which is read by $isSplittable's fourth
	// clause and by $isThereMoreWidth.
	line, inLine int
}

// A fill is a wrapping container's children packed onto lines: what pdf.js
// leaves in [$extra].children, with the width and height it accumulated
// alongside.
type fill struct {
	boxes []lineBox
	// w and h are extra.width and extra.height: the widest line, and how far
	// down the last one reaches.
	w, h Measure
	// high is how tall each line came out. pdf.js keeps no such list — it
	// checks a child at a time — and [placer.turnTo] needs it because it moves
	// a whole line at once.
	high []Measure
}

// widerThanALine is why a wrapping container is refused: one of its children
// is wider than a whole line of it, and pdf.js's answer for that child depends
// on whether a container ABOVE this one still has room on its own line.
//
// pdf.js asks $isThereMoreWidth (template.js:4913-4920), which walks up
// through every ancestor and is true where any of them has a layout ending in
// "-tb" that is in line mode with something already on the line. Where it is
// true the whole container fails and the container above moves it to a line of
// its own; where it is false the child is put down overflowing
// (layout.js:332-336). The state it reads belongs to a layout in progress, and
// this measurement runs before the layout — so the answer is refused rather
// than assumed either way.
const widerThanALine = "one of its children is wider than a whole line of it, and whether pdf.js " +
	"puts it down overflowing or moves the whole container depends on whether a container above " +
	"it still has room on its own line, which this measurement does not carry"

// A packing is a memoised [fill], or why the container could not be packed.
type packing struct {
	f   fill
	why string
}

// linesOf packs a wrapping container's children onto lines, at the width the
// container gives them.
//
// It is memoised on the same key as [placer.heightOf], and for the same
// reason: the packing is asked for once when the container is measured for the
// stack above it, once when its own width is asked for, and once again when it
// is laid out, and a form nests a dozen deep. The three answers have to be the
// same one or the height a container reported to its parent would not be the
// height it takes on the paper.
func (p *placer) linesOf(n *FormNode, wide Measure) (fill, string) {
	k := heightKey{n: n, wide: wide}
	if m, ok := p.packs[k]; ok {
		return m.f, m.why
	}
	f, why := p.pack(n, wide)
	p.packs[k] = packing{f, why}
	return f, why
}

// pack is what linesOf memoises.
func (p *placer) pack(n *FormNode, wide Measure) (fill, string) {
	var f fill
	var curW, prevH Measure
	line, inLine := 0, 0
	for _, kid := range contained(n) {
		if hidden(kid.Template) {
			// pdf.js returns HTMLResult.EMPTY for a hidden child and
			// $childrenToHTML never calls $addHTML for it
			// (xfa_object.js:410-418), so it opens no line, takes no width and
			// is not counted on the one in hand. It is still packed, at the
			// cursor, because every element of the body has to come back
			// somewhere.
			f.boxes = append(f.boxes, lineBox{node: kid, x: curW, y: prevH, wide: wide - curW, line: line, inLine: inLine})
			continue
		}
		left := wide - curW
		// The child is measured against what is left of the line, which is the
		// space getAvailableSpace hands it at attempt 0 (layout.js:176-179).
		// A child that cannot be laid out in that space is a FAILURE and not a
		// defect: pdf.js's $childrenToHTML saves it as the failing node and
		// the container tries it again on the next line (xfa_object.js:394-403).
		// A container of its own refusing here is that same failure arriving
		// from further down — a draw whose written width does not fit in the
		// tail of a line refuses its own parent's packing — so it is treated
		// as one rather than reported.
		w, h, why := p.boxOf(kid, left)
		if why == "" && onLine(kid.Template, left, inLine) {
			f.boxes = append(f.boxes, lineBox{node: kid, x: curW, y: prevH, w: w, h: h, wide: left, line: line, inLine: inLine})
			curW += w
			f.h = max(f.h, prevH+h)
			inLine++
		} else {
			// The second attempt measures the child again, against a whole
			// line: pdf.js calls the failing node's $toHTML afresh with the
			// space getAvailableSpace gives at attempt 1
			// (xfa_object.js:398-401, layout.js:176-179), and a child whose
			// width comes from breaking its own text comes out differently
			// there.
			if w, h, why = p.boxOf(kid, wide); why != "" {
				return fill{}, why
			}
			if !onNewLine(kid.Template, wide) {
				return fill{}, widerThanALine
			}
			line, inLine = line+1, 1
			prevH = f.h
			f.boxes = append(f.boxes, lineBox{node: kid, x: 0, y: prevH, w: w, h: h, wide: wide, line: line, inLine: 0})
			curW = w
			f.h += h
		}
		f.w = max(f.w, curW)
		for len(f.high) <= line {
			f.high = append(f.high, 0)
		}
		f.high[line] = max(f.high[line], f.h-prevH)
	}
	return f, ""
}

// boxOf is the bbox one child of a wrapping container comes out as, at the
// room it has where it sits. It is what pdf.js hands addHTML.
func (p *placer) boxOf(n *FormNode, wide Measure) (w, h Measure, why string) {
	if w, why = p.widthOf(n, wide, 0); why != "" {
		return 0, 0, why
	}
	if h, why = p.heightOf(n, wide, 0); why != "" {
		return 0, 0, why
	}
	return w, h, ""
}

// onLine is checkDimensions' first attempt (layout.js:279-315), with the room
// below taken as unbounded. See the file comment.
//
// The width it compares is the TEMPLATE's, not the measured one: pdf.js reads
// node.w through getTransformedBBox (layout.js:200-203, 254-259), which is the
// attribute and not the box the child came out as. A child that writes no
// width is asked a different question — whether there is any line left at all.
func onLine(n *Node, left Measure, inLine int) bool {
	w, okW, _ := n.Measure("w")
	h, okH, _ := n.Measure("h")
	if okW && w == 0 || okH && h == 0 {
		// pdf.js: "if (node.w === 0 || node.h === 0) return true"
		// (layout.js:272-274), before any comparison at all.
		return true
	}
	if !okW {
		return left > fitSlop
	}
	if fits(w, left) {
		return true
	}
	// The line is empty, so no line of this container would hold it either:
	// pdf.js puts it down and lets it overflow (layout.js:296-298, where the
	// height it returns is unbounded here).
	return inLine == 0
}

// onNewLine is checkDimensions' second attempt (layout.js:317-338), with the
// room below taken as unbounded. False means the container itself fails, which
// is [widerThanALine].
func onNewLine(n *Node, wide Measure) bool {
	w, okW, _ := n.Measure("w")
	return !okW || fits(w, wide)
}

// noSpaceToHoldItTo is why a stacked container has no width of its own.
//
// pdf.js holds a tb's and a table's width down to the space it was given —
// MathClamp(w, extra.width, availableSpace.width) (layout.js:147, 154) — and
// there is no space here to hold it down to: the container is a cell of a row
// whose own container writes no columnWidths, or something above it writes a
// width that is not a length. See [noWidth].
const noSpaceToHoldItTo = "its width would be held down to the room it has, and nothing above it " +
	"says how much that is: a container writes a width that is not a length, or it is a cell of a " +
	"row whose container writes no columnWidths"

// A width is what one node takes across the line of the container above it, or
// why that cannot be said.
type width struct {
	w   Measure
	why string
}

// widthOf is how much room a node takes across the page where its parent puts
// its children side by side.
//
// It is the mirror of [placer.heightOf] and it exists for one caller: a
// container that wraps its children onto lines has to know how wide each of
// them is before it can say which line it goes on. Nothing else in this
// package needs a width — a tb stack does not, and a row's cells take theirs
// from the columnWidths above them — which is why it arrives with lr-tb and
// not before.
//
// It is memoised per node AND per width, like the height, because what a
// container comes out as depends on the room it was given: pdf.js holds a tb's
// width down to it (layout.js:154) and breaks a leaf's text at it.
func (p *placer) widthOf(n *FormNode, wide, colW Measure) (Measure, string) {
	k := heightKey{n, wide, colW}
	if m, ok := p.widths[k]; ok {
		return m.w, m.why
	}
	p.widths[k] = width{why: measuringItself}
	w, why := p.measureWide(n, wide, colW)
	p.widths[k] = width{w, why}
	return w, why
}

// measureWide computes what widthOf memoises.
func (p *placer) measureWide(n *FormNode, wide, colW Measure) (Measure, string) {
	if hidden(n.Template) {
		return 0, ""
	}
	if n.Kind == "field" || n.Kind == "draw" {
		w, ok, err := n.Template.Measure("w")
		switch {
		case err != nil:
			return 0, widthNotALength(n.Template)
		case colW != 0:
			// A cell of a row is as wide as the columns it spans, whatever the
			// template writes: fixDimensions replaces its w before it is laid
			// out (html_utils.js:328-345), which is the same substitution
			// [placer.leaf] makes with the cell it is handed.
			return colW, ""
		case ok:
			return w, ""
		}
		size, why := p.leafSize(n, wide, colW)
		if why != "" {
			return 0, why
		}
		if !size.hasW {
			return p.unmeasured(n, "minW", "maxW", "w")
		}
		return size.w, ""
	}
	own, ok, err := n.Template.Measure("w")
	switch {
	case err != nil:
		return 0, widthNotALength(n.Template)
	case colW != 0:
		own = colW
	case !ok:
		own = 0
	}
	in, okIn := marginOf(n.Template)
	if !okIn {
		return 0, "its margin is not written in lengths"
	}
	lay := layoutOf(n)
	space := spaceWide(n, wide, colW, lay)
	content, why := p.contentWidth(n, innerWide(n, wide, colW, lay, in), space)
	if why != "" {
		return 0, why
	}
	// pdf.js: Math.max(this[$extra].width + marginH, this.w || 0)
	// (template.js:5207). A container is as wide as what it holds even where
	// the template writes a width, which is the same rule its height follows.
	return max(content+in.horizontal(), own), ""
}

// contentWidth is how wide what a container holds comes out, before the
// container's own margin and its own written width are taken into account.
//
// wide is the room the container gives its children and space is the room the
// container itself has, which a stack holds its width down to. The two differ
// by the container's own left and right insets; see [spaceWide].
func (p *placer) contentWidth(n *FormNode, wide, space Measure) (Measure, string) {
	kids := contained(n)
	switch lay := layoutOf(n); lay {
	case "tb", "table":
		// pdf.js: extra.width = MathClamp(w, extra.width, availableSpace.width)
		// (layout.js:147, 154), which is min(max(w, extra.width), space): as
		// wide as the widest child, and never wider than the room it has.
		if math.IsInf(float64(space), -1) {
			return 0, noSpaceToHoldItTo
		}
		var acc Measure
		for _, k := range kids {
			w, why := p.widthOf(k, wide, 0)
			if why != "" {
				return 0, why
			}
			acc = min(max(w, acc), space)
		}
		return acc, ""
	case "row", "rl-row":
		// pdf.js: extra.width += w (layout.js:131-142). A row is as wide as
		// its cells laid end to end.
		cols := p.colsOf(n)
		var sum Measure
		col := 0
		for _, k := range kids {
			cellWide, cellCol := noWidth, Measure(0)
			if len(cols) > 0 {
				cellWide = remainingWide(cols, col)
				if !hidden(k.Template) {
					cellCol, col = columnWidth(cols, col, colSpanOf(k.Template))
				}
			}
			w, why := p.widthOf(k, cellWide, cellCol)
			if why != "" {
				return 0, why
			}
			sum += w
		}
		return sum, ""
	case "lr-tb", "rl-tb":
		// pdf.js: extra.width = Math.max(extra.width, extra.currentWidth)
		// (layout.js:128) — the widest line of it.
		f, why := p.linesOf(n, wide)
		if why != "" {
			return 0, why
		}
		return f.w, ""
	default:
		// Positioned: each child is where it says it is, and the container
		// reaches as far right as the furthest of them.
		// pdf.js: extra.width = Math.max(extra.width, x + w) (layout.js:99-105).
		var reach Measure
		for _, k := range kids {
			if hidden(k.Template) {
				continue
			}
			x, _, err := k.Template.Measure("x")
			if err != nil {
				return 0, fmt.Sprintf("it holds something whose origin is written as x=%q, which is not a place", k.Template.Get("x"))
			}
			w, why := p.widthOf(k, wide, 0)
			if why != "" {
				return 0, why
			}
			reach = max(reach, x+w)
		}
		return reach, ""
	}
}

// wraps says a layout puts its children on lines and begins a new one when the
// next does not fit. It is pdf.js's `layout.endsWith("-tb")` where that reads
// on a container being laid out (template.js:4962, 5138), which is lr-tb and
// rl-tb and nothing else: tb does not end in "-tb".
func wraps(lay string) bool { return lay == "lr-tb" || lay == "rl-tb" }
