// Copyright (c) 2026, the go-pdfkit/xfa authors
// All rights reserved.
//
// SPDX-License-Identifier: BSD-3-Clause

package xfa

import (
	"encoding/xml"
	"fmt"
	"io"
	"regexp"
	"strings"
)

// A Node is one element of a template, with the children it contains.
//
// The tree is kept whole rather than reduced to the elements this package
// understands today. XFA has some hundreds of element types and a form uses a
// few dozen; throwing the rest away at parse time would mean re-parsing to add
// each one, and would lose the ordering that layout depends on.
type Node struct {
	// Kind is the element's name, without its namespace: "subform", "field",
	// "draw", "pageArea".
	Kind string
	// Attr are its attributes, by name without namespace.
	Attr map[string]string
	// Text is the character data directly inside it, with surrounding space
	// trimmed. A <text> element's caption lives here.
	//
	// Inside a rich text — the XHTML of an <exData contentType="text/html"> —
	// it is empty, and the character data is in "#text" children of [Node.Kids]
	// instead. Rich text is the one place where WHERE the text sits among the
	// elements decides how it reads: half of the corpus's <p> elements hold
	// text both before and after a <span>, and a paragraph's height is how
	// many lines its words come to in the order they are written. See
	// [parseXML].
	Text string
	// Kids are the elements inside it, in order. Order is not decoration: a
	// subform laid out top-to-bottom places its children in it.
	Kids []*Node
}

// Get returns an attribute, or the empty string.
func (n *Node) Get(name string) string {
	if n == nil {
		return ""
	}
	return n.Attr[name]
}

// Child is the first child of that kind, or nil. Most of the template is
// singular — a field has one ui, one border, one caption — so looking one up
// by name is how it is read.
func (n *Node) Child(kind string) *Node {
	if n == nil {
		return nil
	}
	for _, k := range n.Kids {
		if k.Kind == kind {
			return k
		}
	}
	return nil
}

// Children are every child of that kind, in order.
func (n *Node) Children(kind string) []*Node {
	if n == nil {
		return nil
	}
	var out []*Node
	for _, k := range n.Kids {
		if k.Kind == kind {
			out = append(out, k)
		}
	}
	return out
}

// Walk calls visit on the node and everything under it, depth first, in
// document order.
func (n *Node) Walk(visit func(*Node)) {
	if n == nil {
		return
	}
	visit(n)
	for _, k := range n.Kids {
		k.Walk(visit)
	}
}

// textKind is what a run of character data inside a rich text is called as a
// child of the element holding it. pdf.js gives it the same name: XmlObject
// moves what it has accumulated into a child called "#text" the moment an
// element child arrives, and again when the element closes (xfa_object.js:865-887).
const textKind = "#text"

// addRun records one run of character data inside a rich text, normalised the
// way pdf.js normalises it and appended to the run before it.
//
// The normalisation is done here, run by run, rather than once over the joined
// text, because that is where pdf.js does it (XFAParser.onText, parser.js:60-73,
// and XhtmlObject[$onText], xhtml.js:224-237) and the two are not the same: a
// run ending in spaces followed by one beginning with them collapses to two
// spaces done separately and to one done together.
func (n *Node) addRun(s string) {
	// The parser drops whitespace between elements that do not accept it, and
	// trims the rest. XhtmlObject accepts it everywhere but in <body> and
	// <html> (xhtml.js:206, 220-222); everything else in the tree, the <exData>
	// included, takes the default of not accepting it (xfa_object.js:183-185).
	if !acceptsWhitespace(n.Kind) {
		if strings.TrimSpace(s) == "" {
			return
		}
		s = strings.TrimSpace(s)
	}
	// A newline inside a rich text is not a line break: pdf.js REMOVES it and
	// then collapses what runs of whitespace are left into one space, unless
	// the element asks for its spaces to be kept (xhtml.js:225-229). 44 099
	// spans of the corpus ask.
	s = crlf.ReplaceAllString(s, "")
	if !strings.Contains(n.Attr["style"], "xfa-spacerun:yes") {
		s = spaces.ReplaceAllString(s, " ")
	}
	if s == "" {
		return
	}
	if k := len(n.Kids) - 1; k >= 0 && n.Kids[k].Kind == textKind {
		n.Kids[k].Text += s
		return
	}
	n.Kids = append(n.Kids, &Node{Kind: textKind, Attr: map[string]string{}, Text: s})
}

// acceptsWhitespace says an element keeps character data that is nothing but
// space. Only <body> and <html> do not (NoWhites, xhtml.js:206).
func acceptsWhitespace(kind string) bool {
	switch kind {
	case "body", "html", "exData":
		return false
	}
	return true
}

// breakableNbsp is pdf.js's first act on any text it reads: the LAST no-break
// space of a run becomes an ordinary one (parser.js:61-63). The comment there
// says why — "normally by definition a &nbsp is unbreakable but in real life
// Acrobat can break strings on &nbsp".
//
// It is not decoration. A no-break space is a place a line may NOT be broken,
// and a space is a place it may; turning the last one of a run into a space is
// what lets "Form\u00a0AB428" come apart at the end of a column.
func breakableNbsp(s string) string {
	return nbsps.ReplaceAllStringFunc(s, func(run string) string {
		return run[:len(run)-len("\u00a0")] + " "
	})
}

var (
	nbsps  = regexp.MustCompile("\u00a0+")
	crlf   = regexp.MustCompile("[\r\n]+")
	spaces = regexp.MustCompile(`\s+`)
)

// maxDepth is how deep a template may nest before this stops following it. A
// form built by a designer runs to a dozen levels; anything past this is a
// file playing games rather than a form.
const maxDepth = 256

// ParseTemplate reads the template part of an XFA package.
//
// It is lenient in one direction and strict in the other. Namespaces are
// dropped, because a template writes the same element under xfa-template and
// under no namespace at all depending on who produced it, and the difference
// carries nothing. Processing instructions and comments are skipped: a
// template written by Adobe's designer carries hundreds of them recording what
// the designer did, which is not part of the form.
//
// A document whose XML does not close is refused. Half a template is not a
// form, and laying one out would put half a form on paper without saying so.
func ParseTemplate(r io.Reader) (*Node, error) {
	root, err := parseXML(r, true)
	if err != nil {
		return nil, fmt.Errorf("xfa: reading the template: %w", err)
	}
	if root.Kind != "template" {
		return nil, fmt.Errorf("xfa: this is a <%s>, not a template", root.Kind)
	}
	return root, nil
}

// parseXML reads one part of an XFA package into a tree. Both parts are XML of
// the same shape, so both are read the same way; what distinguishes them is
// what the caller then expects at the root.
//
// Its errors carry no package prefix. The caller knows which part it asked
// for and says so, which is the half a reader of the message needs.
func parseXML(r io.Reader, rich bool) (*Node, error) {
	dec := xml.NewDecoder(r)
	// A template may name entities the reader does not carry; treating an
	// unknown one as itself keeps a form readable rather than refusing it over
	// a stray &nbsp;.
	dec.Strict = false
	dec.Entity = xml.HTMLEntity

	var stack []*Node
	var root *Node
	// richAt is where in the stack the <exData contentType="text/html"> that
	// opened a rich text sits, or minus one outside one.
	richAt := -1
	for {
		tok, err := dec.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		switch t := tok.(type) {
		case xml.StartElement:
			if len(stack) >= maxDepth {
				return nil, fmt.Errorf("it nests deeper than %d elements", maxDepth)
			}
			n := &Node{Kind: t.Name.Local, Attr: map[string]string{}}
			for _, a := range t.Attr {
				if a.Name.Space == "xmlns" || a.Name.Local == "xmlns" {
					continue
				}
				n.Attr[a.Name.Local] = a.Value
			}
			if rich && richAt < 0 && n.Kind == "exData" && n.Attr["contentType"] == richContent {
				richAt = len(stack)
			}
			if len(stack) == 0 {
				if root != nil {
					return nil, fmt.Errorf("there is more than one root")
				}
				root = n
			} else {
				parent := stack[len(stack)-1]
				parent.Kids = append(parent.Kids, n)
			}
			stack = append(stack, n)
		case xml.EndElement:
			// The stack cannot be empty here: the reader refuses an end tag
			// with nothing open before this sees it. It is lenient about a
			// MISmatched one, though — <subform> closed by </template> pops
			// the subform and then the template — which is why a file written
			// carelessly still reads as the form it meant.
			if richAt == len(stack)-1 {
				richAt = -1
			}
			stack = stack[:len(stack)-1]
		case xml.CharData:
			if len(stack) == 0 {
				continue
			}
			n := stack[len(stack)-1]
			// The no-break space is made breakable before anything else is
			// done with the text, wherever the text is, because that is where
			// pdf.js does it: XFAParser.onText (parser.js:60-73) is every text
			// node of an XFA document, the datasets as much as the template.
			// It decides line breaking outright — "Line 2 of Form\u00a0AB(S11)"
			// in a 63-point column is four lines with the space and three
			// without it — and 31 container heights of the corpus turned on it.
			raw := breakableNbsp(string(t))
			if richAt >= 0 {
				n.addRun(raw)
				continue
			}
			if s := strings.TrimSpace(raw); s != "" {
				if n.Text == "" {
					n.Text = s
				} else {
					n.Text += " " + s
				}
			}
		}
	}
	// The stack is balanced by now, whatever the file did: an element left
	// open is an unexpected EOF, which the reader reports above, and a
	// mismatched close pops what is open rather than leaving it.
	if root == nil {
		return nil, fmt.Errorf("there is nothing here")
	}
	return root, nil
}
