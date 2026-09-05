// Copyright (c) 2026, the go-pdfkit/xfa authors
// All rights reserved.
//
// SPDX-License-Identifier: BSD-3-Clause

package xfa

import (
	"strings"
	"testing"
)

// rich reads a template holding one draw whose value is the given XHTML, and
// measures it at the given width.
func rich(t *testing.T, para, markup string, wide Measure) (Measure, Measure) {
	t.Helper()
	root := parse(t, `<template><subform name="f"><draw name="A">`+para+
		`<value><exData contentType="text/html">`+markup+
		`</exData></value></draw></subform></template>`)
	var draw *Node
	root.Walk(func(n *Node) {
		if n.Kind == "draw" {
			draw = n
		}
	})
	c, ok := valueContent(draw.Child("value"))
	if !ok {
		t.Fatal("the markup was not read")
	}
	pm, lineHeight, why := paraOf(draw)
	if why != "" {
		t.Fatal(why)
	}
	m := newTextMeasure(nil, nil, pm, lineHeight)
	c.push(m)
	w, h, _ := m.compute(wide)
	return w, h
}

func TestRichTextIsMeasuredInTheOrderTheMarkupWritesIt(t *testing.T) {
	// Half the corpus's <p> elements hold text both before and after a span,
	// so the pieces have to come out in order. Six characters either side of a
	// four-character span is fourteen characters, and at 200 points that is
	// one line: ten points, plus the <p>'s own line break, which adds nothing.
	_, h := rich(t, "", `<body><p>before<span>MIDD</span>after</p></body>`, 200)
	if h != 10 {
		t.Errorf("one line came to %v", h)
	}
	w, _ := rich(t, "", `<body><p>before<span>MIDD</span>after</p></body>`, 200)
	if w != 1.02*150 {
		t.Errorf("fifteen characters came to %v wide", w)
	}
}

func TestEachParagraphEndsALineAndChargesItsMargins(t *testing.T) {
	// pdf.js's P pushes a line break and then addPara (xhtml.js:490-495). Two
	// paragraphs are two lines — 10 then 12 — and two margins.
	_, h := rich(t, `<para spaceAbove="3pt" spaceBelow="1pt"/>`,
		`<body><p>ab</p><p>cd</p></body>`, 200)
	if h != 22+2*4 {
		t.Errorf("two paragraphs came to %v, want 30", h)
	}
}

func TestABreakIsALineBreakAndNothingElse(t *testing.T) {
	// pdf.js's Br does not call super, so it opens no paragraph and charges no
	// margin (xhtml.js:409-411).
	_, h := rich(t, `<para spaceAbove="5pt"/>`, `<body>ab<br/>cd</body>`, 200)
	if h != 22 {
		t.Errorf("a break came to %v, want two lines and no margin", h)
	}
}

func TestAStyleMayMoveAParagraphAndNothingElse(t *testing.T) {
	// In this regime a style's typeface, size, weight, posture and letter
	// spacing are all discarded with the typeface that is not found. Its
	// margins are not.
	_, plain := rich(t, "", `<body><p style="font-size:40pt">ab</p></body>`, 200)
	_, moved := rich(t, "", `<body><p style="margin-top:5pt;margin-bottom:2pt">ab</p></body>`, 200)
	if plain != 10 {
		t.Errorf("a font size of forty points changed the height to %v", plain)
	}
	if moved != 17 {
		t.Errorf("the margins came to %v, want 17", moved)
	}
}

func TestTheMarginShorthandIsReadAsTheReferenceReadsIt(t *testing.T) {
	// pdf.js splits the shorthand on a space followed by a TAB rather than on
	// a run of either (xhtml.js:268), so "3pt 9pt" is ONE value — read by a
	// pattern that takes the number and drops the rest — and not two.
	for _, tc := range []struct {
		what  string
		style string
		want  Measure
	}{
		{"one value is every side", "margin:4pt", 8},
		{"two values written the way a form writes them are one", "margin:3pt 9pt", 6},
		{"more than four leaves every inset unwritten", "margin:1pt \t2pt \t3pt \t4pt \t5pt", 0},
		{"top alone", "margin-top:6pt", 6},
		{"bottom alone", "margin-bottom:6pt", 6},
		{"a declaration with no colon is not a margin", "margin", 0},
	} {
		m := paraOfStyle(tc.style)
		if m.top+m.bottom != tc.want {
			t.Errorf("%s: %q came to %v + %v, want %v total", tc.what, tc.style, m.top, m.bottom, tc.want)
		}
	}
	// Four values are clockwise from the top, so the bottom is the third.
	if m := paraOfStyle("margin:1pt \t2pt \t3pt \t4pt"); m.top != 1 || m.bottom != 3 {
		t.Errorf("four values came to %+v", m)
	}
	// Three are top, horizontal, bottom.
	if m := paraOfStyle("margin:1pt \t2pt \t3pt"); m.top != 1 || m.bottom != 3 {
		t.Errorf("three values came to %+v", m)
	}
}

func TestALengthInAStyleIsCSSAndNotXFA(t *testing.T) {
	// pdf.js's getMeasurement (utils.js:78-103) is not [ParseMeasure]: a
	// pixel IS a unit in a style, a unit it does not know is dropped and the
	// number kept, and anything unreadable is nought rather than an error.
	for _, tc := range []struct {
		in   string
		want Measure
	}{
		{"12pt", 12},
		{"1in", 72},
		{"1cm", 72 / 2.54},
		{"10mm", 720 / 25.4},
		{"12px", 12},
		{"-3pt", -3},
		{"2.5pt", 2.5},
		{"0mm", 0},
		{"7furlongs", 7},
		{" 8pt ", 8},
		{"", 0},
		{"wide", 0},
		{"+pt", 0},
	} {
		if got := styleMeasure(tc.in); got != tc.want {
			t.Errorf("%q came to %v, want %v", tc.in, got, tc.want)
		}
	}
}

func TestWhitespaceInsideRichTextIsCollapsedUnlessTheStyleKeepsIt(t *testing.T) {
	// pdf.js REMOVES newlines and then collapses runs of space into one,
	// unless the element writes xfa-spacerun:yes (xhtml.js:225-229). 44 099
	// spans of the corpus write it.
	_, collapsed := rich(t, "", "<body><p>a   b</p></body>", 2000)
	_, kept := rich(t, "", `<body><p style="xfa-spacerun:yes">a   b</p></body>`, 2000)
	if collapsed != 10 || kept != 10 {
		t.Fatalf("heights %v %v", collapsed, kept)
	}
	wc, _ := rich(t, "", "<body><p>a   b</p></body>", 2000)
	wk, _ := rich(t, "", `<body><p style="xfa-spacerun:yes">a   b</p></body>`, 2000)
	if wc != 1.02*30 || wk != 1.02*50 {
		t.Errorf("collapsed came to %v and kept to %v", wc, wk)
	}
	// A newline inside a paragraph is not a line break: it is removed, and the
	// spaces around it come to one.
	_, joined := rich(t, "", "<body><p>a\n   b</p></body>", 2000)
	if joined != 10 {
		t.Errorf("a newline broke a line: %v", joined)
	}
}

func TestABodyDropsWhitespaceBetweenItsChildren(t *testing.T) {
	// <body> and <html> do not accept whitespace-only text (xhtml.js:206), so
	// the newlines a designer leaves between paragraphs add nothing.
	_, h := rich(t, "", "<body>\n  <p>ab</p>\n  <p>cd</p>\n</body>", 200)
	if h != 22 {
		t.Errorf("the whitespace between paragraphs came to %v", h)
	}
}

func TestTheLastNoBreakSpaceOfARunIsBreakable(t *testing.T) {
	// pdf.js does this to every text node of the document (parser.js:61-63).
	// It decides line breaking: "Form AB(S11)" in a 63-point column is
	// four lines with the space and three without it.
	if got := breakableNbsp("a  b"); got != "a  b" {
		t.Errorf("a run of two came out %q", got)
	}
	root := parse(t, `<template><subform name="f"><draw name="A"><value><text>a`+" "+`b</text></value></draw></subform></template>`)
	var got string
	root.Walk(func(n *Node) {
		if n.Kind == "text" {
			got = n.Text
		}
	})
	if got != "a b" {
		t.Errorf("the template read it as %q", got)
	}
}

func TestRichTextWrittenAsEscapedMarkupIsNotMeasured(t *testing.T) {
	// pdf.js parses the string a second time (parser.js:154-162); this reads a
	// template once, and says so rather than measuring the markup as words.
	root := parse(t, `<template><subform name="f" layout="tb">
	  <pageSet><pageArea name="P"><contentArea w="500pt" h="500pt"/></pageArea></pageSet>
	  <draw name="A" w="100pt"><value><exData contentType="text/html">&lt;body&gt;hi&lt;/body&gt;</exData></value></draw>
	  </subform></template>`)
	same(t, "what was left off", notLaid(Place(Expand(root, nil))),
		[]string{"f.A: " + notMeasurable})
}

func TestAContentTypeThatIsNotMarkupIsAString(t *testing.T) {
	root := parse(t, `<template><subform name="f"><draw name="A">
	  <value><exData contentType="text/plain">  hello  </exData></value></draw></subform></template>`)
	var draw *Node
	root.Walk(func(n *Node) {
		if n.Kind == "draw" {
			draw = n
		}
	})
	c, ok := valueContent(draw.Child("value"))
	if !ok || c.rich != nil || c.text != "hello" {
		t.Errorf("a plain exData came out %+v %v", c, ok)
	}
}

func TestWhatAValueGivesAMeasurer(t *testing.T) {
	for _, tc := range []struct {
		what string
		body string
		want string
	}{
		{"nothing at all", "", ""},
		{"a text", "<value><text> hi </text></value>", "hi"},
		{"an integer", "<value><integer>42</integer></value>", "42"},
		{"a picture is not a caption", "<value><image>AAAA</image></value>", ""},
		{"a rectangle says nothing", "<value><rectangle/></value>", ""},
	} {
		root := parse(t, `<template><subform name="f"><draw name="A">`+tc.body+`</draw></subform></template>`)
		var draw *Node
		root.Walk(func(n *Node) {
			if n.Kind == "draw" {
				draw = n
			}
		})
		c, ok := valueContent(draw.Child("value"))
		if !ok || c.text != tc.want {
			t.Errorf("%s: came out %q %v, want %q", tc.what, c.text, ok, tc.want)
		}
	}
	if !(content{}).empty() || (content{text: "a"}).empty() {
		t.Error("empty says whether there is anything to measure")
	}
	if !acceptsWhitespace("span") || acceptsWhitespace("body") || acceptsWhitespace("html") {
		t.Error("only body and html refuse whitespace")
	}
	if strings.Contains(textKind, " ") {
		t.Error("the name of a run of text is one word")
	}
}

func TestARunThatComesToNothingIsNoRunAtAll(t *testing.T) {
	// A paragraph holding only a newline: pdf.js removes the newline and is
	// left with an empty string, which `if (str)` (xhtml.js:234) drops.
	_, h := rich(t, "", "<body><p>\n</p></body>", 200)
	if h != 0 {
		t.Errorf("an empty paragraph came to %v", h)
	}
}

func TestTwoRunsOfTextSideBySideAreOneRun(t *testing.T) {
	// A CDATA section ends one run of character data and begins another with
	// no element between them. pdf.js accumulates them into one $content and
	// makes one #text child of it (xfa_object.js:876-887), so they must not
	// become two runs here either — the glyphs would be the same, but the
	// TREE would say something the reference's does not.
	root := parse(t, `<template><subform name="f"><draw name="A"><value>`+
		`<exData contentType="text/html"><body><p>ab<![CDATA[cd]]>ef</p></body></exData>`+
		`</value></draw></subform></template>`)
	var p *Node
	root.Walk(func(n *Node) {
		if n.Kind == "p" {
			p = n
		}
	})
	if len(p.Kids) != 1 || p.Kids[0].Kind != textKind || p.Kids[0].Text != "abcdef" {
		t.Errorf("the paragraph came out with %d children: %+v", len(p.Kids), p.Kids)
	}
}
