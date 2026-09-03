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

// notMeasurable is why a leaf that holds rich text has no height here.
const notMeasurable = "its height would come from measuring its text, and its text is XHTML " +
	"rather than a string, which this does not lay out"

// valueText is what a <value> gives a measurer, following pdf.js's
// Value[$text] (template.js:6074-6090): the content of its one child, trimmed.
//
// ok is false when there is a child this cannot read the text of — an exData
// of rich text — which is a different answer from a value holding nothing.
func valueText(v *Node) (text string, ok bool) {
	if v == nil {
		return "", true
	}
	if ex := v.Child("exData"); ex != nil {
		if ex.Get("contentType") == richContent {
			return "", false
		}
		return strings.TrimSpace(ex.Text), true
	}
	for _, kind := range valueKinds {
		if c := v.Child(kind); c != nil {
			return strings.TrimSpace(c.Text), true
		}
	}
	return "", true
}

// leafText is the string a field or a draw would draw in itself.
//
// It answers three ways. has is false when there is no <value> at all to take
// a string from, which is not the same as a value holding an empty one, and
// ok is false when there is a value this cannot read the text of.
//
// A bound field draws its data rather than the template's default, because
// pdf.js's binder replaces the value outright: _bindValue calls $setValue with
// the data's text (bind.js:88-100) and _setValue creates the <value> where the
// template wrote none and overwrites its child where it wrote one
// (template.js:189-196). It does so only for a data VALUE, and for a
// multi-select field taking its choices from a group; where the two are not
// the same kind of thing pdf.js warns and leaves the template's default alone
// (bind.js:101-103), so this leaves it too.
func leafText(n *FormNode) (text string, ok, has bool) {
	if n.Data != nil && (isDataValue(n.Data) || isMultiSelect(n.Template)) {
		return n.Value, true, true
	}
	v := n.Template.Child("value")
	if v == nil {
		return "", true, false
	}
	t, readable := valueText(v)
	return t, readable, true
}
