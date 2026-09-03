// Copyright (c) 2026, the go-pdfkit/xfa authors
// All rights reserved.
//
// SPDX-License-Identifier: BSD-3-Clause

package xfa

import "math"

// How big a field or a draw comes out when the template writes no size for it.
//
// This is pdf.js's layoutNode (html_utils.js:207-288) and the two callers that
// decide what to do with its answer: Draw[$toHTML] (template.js:1894-1925),
// which takes it as it stands, and Field[$toHTML] (:2796-2872), which adds a
// widget, a border and a caption to it and then holds the result between the
// field's minimum and its maximum.

// noFontLineNoGap is how tall one line is where there is nothing at all to
// measure: pdf.js's getMetrics returns the constants
// { lineHeight: 12, lineGap: 2, lineNoGap: 10 } when it resolves no font
// (fonts.js:173-179), and a field with no text takes lineNoGap for the height
// of its widget (template.js:2821).
const noFontLineNoGap Measure = 10

// checkButtonSize is the side of a check box whose <checkButton> writes none:
// getMeasurement(attributes.size, "10pt") (template.js:1312).
const checkButtonSize Measure = 10

// defaultEdgeThickness is how thick an edge of a border is where it writes no
// thickness: getMeasurement(attributes.thickness, "0.5pt") (template.js:2025).
// A border writing fewer than four edges repeats its last, and one writing
// none at all is four edges of this (template.js:906-914).
const defaultEdgeThickness Measure = 0.5

// noTextToMeasure is why a leaf whose height the template leaves out still has
// none: there is nothing to measure.
//
// pdf.js answers null rather than nought here, and the difference is the whole
// point: `if (h && this.h === "")` (template.js:1923) never runs, so the leaf
// keeps its height UNWRITTEN and the stack above it still cannot say where the
// next child begins. A leaf of no height and a leaf of unknown height are not
// the same thing.
const noTextToMeasure = "the template writes no height for it and it holds no text to measure one from"

// noWidthToBreakAt is why a leaf with text is still not measured: there is no
// width to break its lines at. See [noWidth].
const noWidthToBreakAt = "its height would come from breaking its text into lines, and there is " +
	"no width to break them at: it is a cell of a row whose container writes no columnWidths"

// marginNotLengths is why a leaf nobody can measure is not measured. A margin
// is taken off the width its text is broken at and added to the height that
// comes out, so one that is not a length stops the measurement rather than
// counting as nought.
const marginNotLengths = "its margin is not written in lengths, and a margin is both taken " +
	"off the width its text is broken at and added to the height that comes out"

// horizontal is what the left and right insets take off the width available to
// what is inside them.
func (in insets) horizontal() Measure { return in.left + in.right }

// A leafSize is what a measurement gives a field or a draw, and which of the
// two dimensions it gives at all.
//
// The two bools are pdf.js's `if (w && ...)` and `if (h && ...)`
// (template.js:1909, 1923, 2857, 2865): a dimension that came out null, and
// one that came out NOUGHT, are both left unwritten. So a leaf can come back
// with a height and no width, which is what a draw of one long line does.
type leafSize struct {
	w, h       Measure
	hasW, hasH bool
}

// measureText breaks a string into lines no wider than maxWidth and says how
// wide and how tall the block comes out. It is layoutText
// (html_utils.js:196-205) over the no-font glyphs of [textMeasure.addString].
func measureText(text string, maxWidth Measure) (w, h Measure) {
	var t textMeasure
	t.addString(text)
	w, h, _ = t.compute(maxWidth)
	return w, h
}

// textBox is layoutNode's answer for a node holding a string.
//
// own is the width the template writes for the node and wide is the room it
// has where it sits. pdf.js asks `node.w || availableSpace.width`
// (html_utils.js:244), so a width written as NOUGHT is no width and the room
// is used instead; it takes the node's own left and right insets off whichever
// it chose, and adds the top and bottom ones back onto the height
// (:279-285) — which it only ever does because it is only ever asked this of a
// node whose own dimensions are unwritten.
//
// ok is false when there is nothing to measure; why is set when there is
// something and it cannot be measured.
func textBox(text string, in insets, own, wide Measure) (w, h Measure, ok bool, why string) {
	if text == "" {
		return 0, 0, false, ""
	}
	maxWidth := wide
	if own != 0 {
		maxWidth = own
	}
	if math.IsInf(float64(maxWidth), -1) {
		return 0, 0, false, noWidthToBreakAt
	}
	w, h = measureText(text, maxWidth-in.horizontal())
	return w + in.horizontal(), h + in.vertical(), true, ""
}

// leafSize is how big a field or a draw comes out where the template writes no
// size, or why that cannot be said.
func (p *placer) leafSize(n *FormNode, wide, colW Measure) (leafSize, string) {
	if n.Kind == "draw" {
		// A draw in a row has its own width replaced by the columns it spans
		// BEFORE its text is measured: fixDimensions runs at template.js:1901
		// and layoutNode at :1908. A field's runs after, which is why colW is
		// carried separately rather than folded into wide.
		return drawSize(n, wide, colW)
	}
	return fieldSize(n, wide)
}

// drawSize is Draw[$toHTML]'s size (template.js:1894-1925): the text, and
// nothing else.
//
// A draw's minH and maxH do not come into it. pdf.js writes them as CSS
// min-height and max-height (setMinMaxDimensions, html_utils.js:178-193) and
// they never reach the number it reports to the container above, which is the
// bbox it returns.
func drawSize(n *FormNode, wide, colW Measure) (leafSize, string) {
	in, ok := marginOf(n.Template)
	if !ok {
		return leafSize{}, marginNotLengths
	}
	own, _, err := n.Template.Measure("w")
	if err != nil {
		return leafSize{}, widthNotALength(n.Template)
	}
	if colW != 0 {
		own = colW
	}
	text, readable, has := leafText(n)
	if !readable {
		return leafSize{}, notMeasurable
	}
	if !has {
		return leafSize{}, ""
	}
	w, h, measured, why := textBox(text, in, own, wide)
	if why != "" {
		return leafSize{}, why
	}
	if !measured {
		return leafSize{}, ""
	}
	return leafSize{w: w, h: h, hasW: w != 0, hasH: h != 0}, ""
}

// fieldSize is Field[$toHTML]'s size (template.js:2796-2872): the widget, the
// border its <ui> writes around the widget, and the caption, added up the way
// the caption's placement says, and then held between minW/minH and maxW/maxH.
func fieldSize(n *FormNode, wide Measure) (leafSize, string) {
	in, ok := marginOf(n.Template)
	if !ok {
		return leafSize{}, marginNotLengths
	}
	uiW, uiH, why := widgetSize(n, in, wide)
	if why != "" {
		return leafSize{}, why
	}
	width, height, why := captioned(n, uiW, uiH, wide)
	if why != "" {
		return leafSize{}, why
	}
	var out leafSize
	// pdf.js adds the field's own margin here, on top of the margin layoutNode
	// already added to the widget's own measurement (html_utils.js:279-285).
	// It is counted twice and it is counted twice here too: the reference's
	// numbers are what a form is judged against.
	if width != 0 {
		out.w, why = clamp(n.Template, "minW", "maxW", width+in.horizontal())
		if why != "" {
			return leafSize{}, why
		}
		out.hasW = true
	}
	if height != 0 {
		out.h, why = clamp(n.Template, "minH", "maxH", height+in.vertical())
		if why != "" {
			return leafSize{}, why
		}
		out.hasH = true
	}
	return out, ""
}

// widgetSize is how big the thing a field is edited with comes out, with the
// border its <ui> writes around it.
func widgetSize(n *FormNode, in insets, wide Measure) (w, h Measure, why string) {
	widget := uiWidget(n.Template.Child("ui"))
	if widget != nil && widget.Kind == "checkButton" {
		size, ok, err := widget.Measure("size")
		if err != nil {
			return 0, 0, "its check box is written as size=" + quoted(widget.Get("size")) +
				", which is not a length"
		}
		if !ok {
			size = checkButtonSize
		}
		w, h = size, size
	} else {
		text, readable, has := leafText(n)
		if !readable {
			return 0, 0, notMeasurable
		}
		own, _, err := n.Template.Measure("w")
		if err != nil {
			return 0, 0, widthNotALength(n.Template)
		}
		measured := false
		if has {
			w, h, measured, why = textBox(text, in, own, wide)
			if why != "" {
				return 0, 0, why
			}
		}
		if !measured {
			// pdf.js: uiH = getMetrics(this.font, /* real = */ true).lineNoGap
			// (template.js:2821), and uiW is left at nought.
			//
			// This is the line the reference cannot reach. getMetrics says at
			// :173-179 that it answers constants when no font is found, but
			// selectFont at :155-163 dereferences the typeface to choose the
			// weight and the posture BEFORE that test, so a field carrying a
			// <font> whose typeface is not one of the document's throws
			// instead. That is where pdf.js dies on 70 of the 560 corpus
			// forms. The constant it meant to return is the one taken here.
			w, h = 0, noFontLineNoGap
		}
	}
	bw, bh, why := borderDims(widget)
	if why != "" {
		return 0, 0, why
	}
	return w + bw, h + bh, ""
}

// captioned adds the caption to the widget's size, the way the caption's
// placement says (template.js:2829-2855).
//
// A caption with no text gives the field NO height when it sits beside the
// widget, even though the widget under it has one: pdf.js assigns the
// caption's measurement over the widget's whole (`width = w; height = h;`,
// :2838-2839) and only the dimension the placement then adds the widget's back
// into survives. This carries that by starting an unmeasured caption at
// nought, which is what adding to null does in JavaScript.
func captioned(n *FormNode, uiW, uiH, wide Measure) (width, height Measure, why string) {
	capt := n.Template.Child("caption")
	if capt == nil {
		return uiW, uiH, ""
	}
	in, ok := marginOf(capt)
	if !ok {
		return 0, 0, marginNotLengths
	}
	text, readable := valueText(capt.Child("value"))
	if !readable {
		return 0, 0, notMeasurable
	}
	reserve, err := captionReserve(capt)
	if err != nil {
		return 0, 0, "its caption is written as reserve=" + quoted(capt.Get("reserve")) +
			", which is not a length"
	}
	// pdf.js: width = reserve <= 0 ? availableSpace.width : reserve, for a
	// caption beside the widget; for one above or below it the reserve
	// replaces the available HEIGHT, which layoutNode never reads
	// (template.js:1178-1190).
	beside := captionBeside(capt)
	space := wide
	if beside && reserve > 0 {
		space = reserve
	}
	// A caption has no w of its own — the class does not carry one
	// (template.js:1144-1169) — so `node.w || availableSpace.width` is always
	// the room it has.
	width, height, _, why = textBox(text, in, 0, space)
	if why != "" {
		return 0, 0, why
	}
	if beside {
		width += uiW
	} else {
		height += uiH
	}
	return width, height, ""
}

// captionBeside says the caption sits to one side of the widget rather than
// above or below it, which decides whether the widget's size is added to the
// width or to the height (template.js:2841-2851).
//
// pdf.js validates placement against its five values and falls back to the
// first, which is "left" (template.js:1148-1154), so anything unrecognised is
// beside.
func captionBeside(capt *Node) bool {
	switch capt.Get("placement") {
	case "top", "bottom":
		return false
	}
	return true
}

// captionReserve is how much room the caption claims, rounded UP to a whole
// point: Math.ceil(getMeasurement(attributes.reserve)) (template.js:1161).
func captionReserve(capt *Node) (Measure, error) {
	r, ok, err := capt.Measure("reserve")
	if err != nil || !ok {
		return 0, err
	}
	return Measure(math.Ceil(float64(r))), nil
}

// clamp holds a field's measured dimension between the smallest and the
// largest the template will have it.
//
// pdf.js:
//
//	this.h = Math.min(this.maxH <= 0 ? Infinity : this.maxH,
//	                  this.minH + 1 < height ? height : this.minH);
//
// (template.js:2867-2870, and :2859-2862 for the width). The + 1 is pdf.js's:
// a measurement within a point of the minimum is taken to BE the minimum. 572
// of the 598 corpus fields that write no height write a minH, so this is not a
// corner.
func clamp(n *Node, small, large string, v Measure) (Measure, string) {
	lo, _, err := n.Measure(small)
	if err != nil {
		return 0, "its smallest size is written as " + small + "=" + quoted(n.Get(small)) +
			", which is not a length"
	}
	hi, _, err := n.Measure(large)
	if err != nil {
		return 0, "its largest size is written as " + large + "=" + quoted(n.Get(large)) +
			", which is not a length"
	}
	out := lo
	if lo+1 < v {
		out = v
	}
	if hi > 0 {
		out = min(out, hi)
	}
	return out, ""
}

// uiWidget is the element a field is edited with: the first child of its <ui>
// that is not <extras> or <picture>.
//
// pdf.js takes the first such property of Ui (template.js:5957-5972) and hands
// it to getBorderDims, so a <ui> holding nothing has no widget and no border.
func uiWidget(ui *Node) *Node {
	if ui == nil {
		return nil
	}
	for _, k := range ui.Kids {
		switch k.Kind {
		case "extras", "picture":
		default:
			return k
		}
	}
	return nil
}

// borderDims is what the border of a field's WIDGET adds to its size.
//
// It is getBorderDims (template.js:155-178), and it is worth reading twice
// because it crosses the two. A border's edges are written top, right, bottom,
// left, and its margin's insets in the same order; what pdf.js adds to the
// WIDTH is edges 0 and 2 with the top and bottom insets, and what it adds to
// the HEIGHT is edges 1 and 3 with the right and left ones. This is ported as
// it stands rather than corrected, because the reference's numbers are what a
// form is judged against — and a border of four equal edges inside an even
// margin, which is what a designer writes, comes to the same thing either way.
func borderDims(widget *Node) (w, h Measure, why string) {
	if widget == nil {
		return 0, 0, ""
	}
	b := widget.Child("border")
	if b == nil {
		return 0, 0, ""
	}
	edges := b.Children("edge")
	thick := make([]Measure, 4)
	last := defaultEdgeThickness
	for i := range 4 {
		if i < len(edges) {
			t, ok, err := edges[i].Measure("thickness")
			if err != nil {
				return 0, 0, "an edge of its border is written as thickness=" +
					quoted(edges[i].Get("thickness")) + ", which is not a length"
			}
			if !ok {
				t = defaultEdgeThickness
			}
			last = t
		}
		// Fewer than four edges repeat the last one written, and none at all
		// are four default edges (template.js:906-914).
		thick[i] = last
	}
	in, ok := marginOf(b)
	if !ok {
		return 0, 0, marginNotLengths
	}
	return thick[0] + thick[2] + in.vertical(), thick[1] + thick[3] + in.horizontal(), ""
}

// widthNotALength is why a leaf that has to be measured cannot be: its text is
// broken at a width, and the one the template writes is not one.
func widthNotALength(n *Node) string {
	return "its width is written as w=" + quoted(n.Get("w")) +
		", which is not a length, and its text has to be broken at a width"
}

// quoted puts a template's own text in quotes inside a reason, so that an
// empty string and a string of spaces read differently.
func quoted(s string) string { return "\"" + s + "\"" }
