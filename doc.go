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
// [Place] then lays a form out on one page under POSITIONED layout: a box's
// place is its own x and y added to those of every container above it, down
// from the content area's origin, with anchorType and rotate resolved.
// Everything it does not reach — flow layouts, sizes only text measurement
// would give, other page areas — comes back in [Layout.Unplaced] with the
// reason written out, one element at a time.
//
// Measured over the same 560 packages, and against pdf.js's own layout run
// over the same files:
//
//	82 386 fields in the templates, 82 378 in the expanded forms
//	13 331 placed, 69 047 reported unplaced
//	   of the unplaced, 68 891 are under a flow layout
//	21 933 boxes compared with pdf.js, 21 881 agreeing to within 1/100 pt
//	    47 more differing only by pdf.js's own two-decimal rounding
//	     5 differing, every one of them a colSpan width and none a place
//
// The gap between what is placed and the 70 297 fields whose own enclosing
// subform is positioned is the finding rather than the shortfall: 556 of the
// 560 outermost subforms are laid out "tb", so almost every positioned
// subform hangs below a flow one, and where a flow layout puts its second
// child is the flow layout itself.
//
// [Values] and [FieldNames] remain what they were — what the data says, and
// what the template says, each on its own — for a caller that wants one side
// without the other. [Values] names a repeated sibling the way a binding does,
// "Row[1]", because a form's table is repeated siblings and naming them alike
// was losing 10 125 of the corpus's 71 346 values.
package xfa
