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
// [Values] and [FieldNames] remain what they were — what the data says, and
// what the template says, each on its own — for a caller that wants one side
// without the other. [Values] names a repeated sibling the way a binding does,
// "Row[1]", because a form's table is repeated siblings and naming them alike
// was losing 10 125 of the corpus's 71 346 values.
package xfa
