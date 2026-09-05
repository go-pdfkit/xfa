// Copyright (c) 2026, the go-pdfkit/xfa authors
// All rights reserved.
//
// SPDX-License-Identifier: BSD-3-Clause

// Package xfa reads Adobe's XML Forms Architecture — the form description
// carried inside a PDF whose pages are a placeholder.
//
// # Why this exists
//
// A PDF form comes in two kinds, and only one of them is a PDF form. Measured
// over 2 240 real government forms: 560 carry an XFA package, and 546 of those
// are STATIC — a second, proprietary description of a form that is already
// drawn on the pages, which go-pdfkit/forms reads and fills without help.
//
// The other fourteen are DYNAMIC. Their pages hold a panel reading "Please
// wait... your PDF viewer may not be able to display this type of document",
// and the form exists only as XML, laid out when the document is opened. Adobe
// removed the format from PDF 2.0; no browser and no other reader lays one
// out. A person handed such a file has a document that looks blank and is not.
//
// # What it does, and what it will not
//
// This reads the template — the description of the form — into a typed tree,
// and the datasets, which hold what has been filled in. It does not run the
// forms' scripts, and that is a measured decision rather than a shortcut: of
// 5 744 scripts across those fourteen forms, 5 131 are handlers for enter,
// exit, change and click. They fire when somebody types. Only 188 run at load,
// so the form a reader first sees does not depend on them.
//
// FormCalc, XFA's other scripting language, is not read either: every script
// in the corpus declares itself as JavaScript.
//
// # Joining a field to its value
//
// [Bind] does that, and it is not a matter of matching names. A form binds its
// fields to its data explicitly, through <bind> elements — 26 573 of them
// across the 560 XFA packages in the corpus — and where it does not, each
// container consumes the next unclaimed data node of its own name, in document
// order. The two trees are named differently on purpose: the template names a
// field for the layout and the data names it for the record.
//
// Taking the fourteen dynamic forms and asking how many template paths the
// data happens to answer:
//
//	cerfa_12064      212 fields, 220 values,  0 paths in common
//	t657-fill-25e    459 fields, 459 values,  0 paths in common
//	cerfa_12818       72 fields,  72 values, 47 paths in common
//	CA-27_sample     136 fields,  99 values, 69 paths in common
//
// The same count of each and not one path in common is what settles it: two of
// these forms name every field twice over, and only the binding says which
// goes with which. Guessing by name would answer confidently and wrongly.
//
// Measured over all 560 packages:
//
//	560 templates read, 560 with a data tree
//	26 573 <bind> elements: 11 875 match="none"
//	                        11 544 match="global"
//	                         2 589 with no match, which means "once"
//	                           565 match="dataRef", every one with a ref
//	80 482 fields placed, 60 865 of them bound to a data node
//	     0 <bind> expressions holding a construct this does not read
//
// # Where a field goes
//
// [Expand] joins the two trees and returns the form as a document holds it —
// one node per occurrence, so a table row written once in the template and
// filled three times by the data is three nodes. Layout runs over that and
// never over the template, which is what pdf.js's binder does too and for the
// same reason (bind.js:61).
//
// [Place] then lays a form out on paper. Under a POSITIONED layout a box's
// place is its own x and y added to those of every container above it, down
// from the content area's origin, with anchorType and rotate resolved. Under a
// FLOW layout the coordinates are thrown away and the children are stacked
// instead, and this follows two of the six: tb and table stack downwards, each
// child beginning where the one above it ends, and row cuts its cells from the
// table's columnWidths. A container's height is the taller of what it holds
// and what the template writes for it, so the heights are arrived at from the
// leaves upwards.
//
// A leaf's own height is often not written either, and then it is its TEXT:
// broken into lines at the width it has, a line count times a line height. See
// the note on emWidth in text.go for the regime that is measured in, which is
// pdf.js's own where it resolves no font — one em per character, a first line
// one em tall and every line after it 1.2 ems. That is not a stand-in for
// something better: it is what the reference runs on a form whose fonts it
// cannot resolve, so its numbers and these are the same numbers. Where the
// text is the XHTML of a rich value it is walked in the order the markup
// writes it, since half of the corpus's paragraphs hold text both before and
// after a span.
//
// Where the stack runs off the bottom, the page turns. Which page comes next
// is a state machine rather than "another of the same" — the page set's
// relation, each page area's occur, the parity of the page number, and the
// explicit breaks the template writes — and [Place] follows pdf.js's
// (template.js:4064-4236, 5418-5657).
//
// A container that MAY be split is broken across the boundary: what it placed
// before the break stays where it is, and the rest of it begins again at the
// top of the next content area. Whether it may is a property of the whole
// CHAIN above it and not of the container alone, which is pdf.js's rule and
// the reason [placer.splittable] recurses upward. One that may not — a
// positioned layout, a row, anything with keep intact, anything inside an
// <area> — moves whole.
//
// Except a positioned container whose author wrote keep intact="none" on it.
// That is a permission pdf.js's clause order makes unreachable and pdfium
// reads, and such a container is laid out whole and then CUT across the
// boundary, its children keeping their written y. See [placer.cuttable].
//
// What moves whole is still put on the paper. The first such container of each
// sheet is not measured against anything: pdf.js's checkDimensions returns
// true while the sheet has had none (layout.js:266-268) and the one that
// claims that pass cannot fail either, nor can anything inside it. Everything
// after it on that sheet is checked, and what does not fit turns the page —
// where it is first in its turn. So a container taller than a whole content
// area comes out one per sheet, hanging over the bottom, which is what pdf.js
// draws.
//
// A page set says which sheet comes after this one, and its <occur> is what
// bounds a form: a page AREA's max caps how many sheets it makes in one run of
// the set holding it, and starting the set again offers it afresh. Both
// references say so and neither can quite carry it out — see [pager.cleanKids].
//
// Everything it does not reach — rl-row, which fills a row from the right, a
// form that runs out of pages, an element with no room inside a container that
// moves whole — comes back in [Layout.Unplaced] with the reason
// written out, one element at a time. Nothing of the body is dropped. A page
// area's own furniture is drawn once on every sheet that page area makes,
// which is the one thing not in one-to-one correspondence with the boxes on
// the paper.
//
// Measured over the same 560 packages:
//
//	81 750 fields in the body of the expanded forms
//	81 734 placed, 156 010 draws with them, on 3 090 sheets
//	    16 sit under a page area no sheet of the form ever is
//
// # What checks it
//
// pdf.js emits no coordinates for a child of a flow layout — it writes them
// into a flexbox column and lets the browser stack them — so for exactly the
// layouts this computes, its output says where the CONTAINER is and nothing
// about where the second child went. It does emit the accumulation itself, as
// a number: a subform's style.height is Math.max(extra.height + marginV,
// this.h || 0) (template.js:5222).
//
// It also emits one div per SHEET, with every element inside the one it
// belongs to — which is a thing it says outright, so the page a box landed on
// can be compared even where its coordinates cannot:
//
//	  7 764 container heights this package computes, all agreeing with pdf.js's
//	        to within 1/100 pt
//	    176 boxes placed where pdf.js also emits a place: all agreeing
//	152 446 more that pdf.js placed by flexbox, of which 149 488 came out at
//	        the container's own origin, 2 958 below or to the right of it, and
//	        NONE above or to the left, which would be outside the container
//	    431 forms where every element of the body was placed, so that the two
//	        are laying out the same thing
//	    428 of those agreeing with pdf.js on the NUMBER of sheets
//	117 378 boxes paired on them, every one on the same sheet as pdf.js put it,
//	        and none on another
//
// # Where a box goes ACROSS a line
//
// pdf.js says nothing about that either — an lr-tb container's children go in
// a flexbox div of class xfaLr and a table row's cells in one of class xfaRow,
// and the browser places them — so it was unjudged in both directions until
// pdfium was asked. pdfium is a renderer rather than a DOM emitter and
// computes the answer outright (CalculateRowChildPosition,
// cxfa_contentlayoutprocessor.cpp:2028-2160), though nothing in public/
// returns it and its own suite asserts no coordinate anywhere; the dump comes
// from a probe added to its embedder tests. It lays out 559 of the 560 forms,
// against pdf.js's 483.
//
//	    959 leaves under a container that wraps its children onto lines, all
//	        agreeing with pdfium on x
//	 19 516 leaves under a table row, 19 448 agreeing on x
//	167 632 leaves under neither — the control — 160 022 agreeing
//
// 64 of the 68 row disagreements are on ca-cra__rc1-fill-11-25e, where 552 of
// 804 CONTROL leaves disagree too, so x on that form is not comparable at all.
// The other four are on fr-cerfa__cerfa_12818, whose control agrees entirely:
// the row puts the cell where pdfium does, and pdfium then places the
// positioned children two levels inside it 3.6 pt further right than their
// written x. That one is open.
//
// pdfium runs the form's scripts and measures text with real fonts, neither of
// which this does, so y and the sheet a box landed on are informative rather
// than a verdict there. X survives: a positioned box's x is its written
// attribute, and a line member's is arithmetic over written widths.
//
// A box is compared as the same box on both sides. Four draws of
// us-ssa__ss-5-ar-inst are written w="-0.106in", and a negative extent is not a
// box reaching left of where it was put: pdfium normalises a widget's
// rectangle before using it (CFX_RectF::Normalize, cxfa_fffield.cpp:293) and so
// does this, where pdf.js cannot because CSS ignores a negative width. Until
// both sides were normalised the check called those four boxes defects.
//
// A box is paired by its whole chain of names and not by its own. Six subforms
// of us-irs__fw9 are called Bullet1, in three lists on three sheets; once each
// of them has a height they are six entries of one list, and nothing makes the
// two lists line up. Pairing on the chain turns a mispairing into an unpaired
// box rather than into a disagreement.
//
// Text measurement was the wall three counts in a row failed to see. A count
// over the templates said 446 fields — half of one per cent — need their text
// measured; only 8 466 draws and 598 fields of the corpus's 234 000 leaves
// lack a height. But a stack is a chain, and one unmeasurable height leaves
// every sibling below it with nowhere to begin: those leaves held up 44 246
// fields, more than half the corpus. Measuring them places 25 585 more.
//
// A length may be written as a CALCULATION, with a leading "=", and the corpus
// writes one shape of it: h="=0mm", on 955 draws of 101 forms, holding up
// 16 936 fields. pdfium's CXFA_Measurement strips the "=" deliberately and
// parses the rest leniently (cxfa_measurement.cpp, SetString), so it is nought;
// pdf.js reaches the same answer only because its pattern is unanchored and
// finds the "0mm" inside the string (utils.js:83-87). An expression is NOT
// evaluated: ="Foo.h * 2" is nought under the same rule, which is the
// reference's answer rather than a shortfall standing in for one. See
// [ParseMeasure]. It places 16 522 more fields, and reports 189 that were
// placed before: their containers now measure, and measure taller than a whole
// content area.
//
// Two things inside the measurement turned out to decide it, and neither is
// about fonts. The last no-break space of a run becomes an ORDINARY one
// (parser.js:61-63), which is where "Form\u00a0AB428" comes apart at the end
// of a column; without it 31 container heights disagreed. And a leaf whose
// text gives no height is not left unmeasured: computeBbox fills it in from
// minH (html_utils.js:290-324) and the stack above adds that.
//
// [Values] and [FieldNames] remain what they were — what the data says, and
// what the template says, each on its own — for a caller that wants one side
// without the other. [Values] names a repeated sibling the way a binding does,
// "Row[1]", because a form's table is repeated siblings and naming them alike
// was losing 10 125 of the corpus's 71 346 values.
package xfa
