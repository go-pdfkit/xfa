// Copyright (c) 2026, the go-pdfkit/xfa authors
// All rights reserved.
//
// SPDX-License-Identifier: BSD-3-Clause

package xfa

import "strings"

// What text a leaf holds, which is the other half of measuring it.

// valueKinds is the order pdf.js looks through a <value>'s one-of children in.
//
// A value holds exactly one of them, so the order rarely decides anything; it
// is written down because pdf.js's is not document order but the order the
// properties are declared in Value's constructor (template.js:6027-6039),
// which Object.getOwnPropertyNames then walks (:6080). A file that writes two
// is answered the same way by both.
//
// "image" is left out on purpose: pdf.js skips it (:6081-6083), because the
// bytes of a picture are not a caption.
var valueKinds = []string{
	"arc", "boolean", "date", "dateTime", "decimal", "exData",
	"float", "integer", "line", "rectangle", "text", "time",
}

// richContent is the content type of a <exData> holding XHTML rather than a
// string. pdf.js measures one by walking the markup and pushing a font for
// each span (html_utils.js:246-261); this does not.
const richContent = "text/html"

// notMeasurable is why a leaf holding rich text still has no height: the
// markup arrived as ESCAPED text rather than as elements. pdf.js parses that
// string again with a parser of its own (parser.js:154-162); this reads a
// template once. Twenty-five of the corpus's exData carry no elements and none
// of the twenty-five carries any text either, so none of them reaches this.
const notMeasurable = "its text is XHTML written as escaped markup rather than as elements, " +
	"which the reference parses a second time and this does not"

// A content is what a leaf has to measure: a string, or the XHTML of a rich
// text, which is a tree.
type content struct {
	text string
	// rich is the <exData contentType="text/html"> holding the markup, whose
	// children are the XHTML elements. pdf.js keeps the parsed root on the
	// exData ($content, template.js:2244-2251) and hands it to layoutText,
	// which pushes its glyphs rather than a string's
	// (html_utils.js:196-205).
	rich *Node
}

// empty says there is nothing to measure. A rich text is never empty in this
// sense even where it comes to no glyphs, because pdf.js measures it either
// way and reads the answer, whereas it does not measure an empty string at all
// (html_utils.js:246-277).
func (c content) empty() bool { return c.rich == nil && c.text == "" }

// push adds the content's glyphs to a measurement.
func (c content) push(t *textMeasure) {
	if c.rich == nil {
		t.addString(c.text)
		return
	}
	for _, k := range c.rich.Kids {
		t.pushRich(k)
	}
}

// valueContent is what a <value> gives a measurer, following pdf.js's
// Value[$text] (template.js:6074-6090) for a string and its exData for a rich
// text: the content of its one child, trimmed where it is a string.
//
// ok is false where the value holds a rich text this cannot read. See
// [notMeasurable].
func valueContent(v *Node) (c content, ok bool) {
	if v == nil {
		return content{}, true
	}
	if ex := v.Child("exData"); ex != nil {
		if ex.Get("contentType") != richContent {
			return content{text: strings.TrimSpace(ex.Text)}, true
		}
		if ex.Child(textKind) != nil {
			return content{}, false
		}
		return content{rich: ex}, true
	}
	for _, kind := range valueKinds {
		if c := v.Child(kind); c != nil {
			return content{text: strings.TrimSpace(c.Text)}, true
		}
	}
	return content{}, true
}

// leafContent is what a field or a draw would draw in itself, and whether
// there is a <value> at all to take it from.
//
// It answers three ways. has is false when there is no <value> to take
// anything from, which is not the same as a value holding an empty string, and
// ok is false when there is a value this cannot read.
//
// A bound field draws its data rather than the template's default, because
// pdf.js's binder replaces the value outright: _bindValue calls $setValue with
// the data's text (bind.js:88-100) and _setValue creates the <value> where the
// template wrote none and overwrites its child where it wrote one
// (template.js:189-196). It does so only for a data VALUE, and for a
// multi-select field taking its choices from a group; where the two are not
// the same kind of thing pdf.js warns and leaves the template's default alone
// (bind.js:101-103), so this leaves it too.
func leafContent(n *FormNode) (c content, ok, has bool) {
	if n.Data != nil && (isDataValue(n.Data) || isMultiSelect(n.Template)) {
		// A rich value that came from the DATA is flattened to its words by
		// [dataValue] and measured as a string. pdf.js parses it as markup
		// with a parser of its own (bind.js:92, createText) and would give it
		// the paragraphs back; no corpus form fills one in.
		return content{text: n.Value}, true, true
	}
	v := n.Template.Child("value")
	if v == nil {
		return content{}, true, false
	}
	c, ok = valueContent(v)
	return c, ok, true
}
