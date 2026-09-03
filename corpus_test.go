// Copyright (c) 2026, the go-pdfkit/xfa authors
// All rights reserved.
//
// SPDX-License-Identifier: BSD-3-Clause

package xfa

import (
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"
)

// TestOverTheCorpus reads every template a corpus holds. It is skipped unless
// one is named: no form of anybody's enters this repository, and the suite has
// to pass on a machine that has none.
//
//	XFACORPUS=/path/to/templates go test -run OverTheCorpus -v
func TestOverTheCorpus(t *testing.T) {
	dir := os.Getenv("XFACORPUS")
	if dir == "" {
		t.Skip("no XFACORPUS")
	}
	names, err := filepath.Glob(filepath.Join(dir, "*.template.xml"))
	if err != nil || len(names) == 0 {
		t.Skipf("no templates in %s", dir)
	}
	sort.Strings(names)
	kinds := map[string]int{}
	total, biggest := 0, 0
	start := time.Now()
	for _, name := range names {
		f, err := os.Open(name)
		if err != nil {
			t.Fatal(err)
		}
		root, err := ParseTemplate(f)
		f.Close()
		if err != nil {
			t.Errorf("%s: %v", filepath.Base(name), err)
			continue
		}
		n := 0
		root.Walk(func(node *Node) {
			n++
			kinds[node.Kind]++
		})
		total += n
		if n > biggest {
			biggest = n
		}
	}
	t.Logf("%d templates, %d nodes, biggest %d, %d element kinds, in %v",
		len(names), total, biggest, len(kinds), time.Since(start).Round(time.Millisecond))
	readTheData(t, dir)
	type kv struct {
		k string
		n int
	}
	var list []kv
	for k, n := range kinds {
		list = append(list, kv{k, n})
	}
	sort.Slice(list, func(i, j int) bool { return list[i].n > list[j].n })
	for i, e := range list {
		if i >= 12 {
			break
		}
		t.Logf("  %8d  %s", e.n, e.k)
	}
}

// readTheData reads the datasets beside each template and says how much of
// each form is filled in. A form nobody has touched is as informative as a
// filled one: it says the names come out even when the values do not.
func readTheData(t *testing.T, dir string) {
	t.Helper()
	names, _ := filepath.Glob(filepath.Join(dir, "*.datasets.xml"))
	sort.Strings(names)
	for _, name := range names {
		stem := strings.TrimSuffix(name, ".datasets.xml")
		tf, err := os.Open(stem + ".template.xml")
		if err != nil {
			continue
		}
		tmpl, err := ParseTemplate(tf)
		tf.Close()
		if err != nil {
			continue
		}
		df, err := os.Open(name)
		if err != nil {
			continue
		}
		data, err := ParseDatasets(df)
		df.Close()
		if err != nil {
			t.Errorf("%s: %v", filepath.Base(name), err)
			continue
		}
		fields := FieldNames(tmpl)
		values := Values(data)
		// How many of the template's paths the data happens to answer. It is
		// not "how much is filled in": a form binds its fields to its data
		// explicitly, so a path that does not line up may be bound and may be
		// absent, and this cannot yet tell which. It is here as the measure of
		// how far implicit binding gets, which is what decides whether <bind>
		// is worth building.
		same := 0
		for _, f := range fields {
			if _, ok := values[f]; ok {
				same++
			}
		}
		t.Logf("  %-40s %5d fields, %5d values, %5d paths in common",
			filepath.Base(stem), len(fields), len(values), same)
	}
}

// TestBindingOverTheCorpus joins every template it can find to the datasets
// beside it and counts what came of it. Like the reader above it is skipped
// unless a corpus is named.
//
//	XFACORPUS=/path/to/parts go test -run BindingOverTheCorpus -v
func TestBindingOverTheCorpus(t *testing.T) {
	dir := os.Getenv("XFACORPUS")
	if dir == "" {
		t.Skip("no XFACORPUS")
	}
	names, err := filepath.Glob(filepath.Join(dir, "*.template.xml"))
	if err != nil || len(names) == 0 {
		t.Skipf("no templates in %s", dir)
	}
	sort.Strings(names)

	var forms, parsed, withData, fields, bound, nonEmpty, truncated int
	why := map[string]int{}
	unsupported := 0
	matches := map[string]int{}
	binds := 0
	perForm := map[string]int{}
	for _, name := range names {
		forms++
		tmpl := readNode(t, name, true)
		if tmpl == nil {
			continue
		}
		parsed++
		tmpl.Walk(func(n *Node) {
			if n.Kind == "bind" {
				binds++
				matches[matchOf(n)]++
			}
		})
		stem := strings.TrimSuffix(name, ".template.xml")
		data := readNode(t, stem+".datasets.xml", false)
		if data != nil && len(data.Kids) > 0 {
			withData++
		}
		r := Bind(tmpl, data)
		fields += len(r.Fields)
		perForm[filepath.Base(stem)] = len(r.Fields)
		for _, f := range r.Fields {
			if f.Bound {
				bound++
			}
			if f.Value != "" {
				nonEmpty++
			}
		}
		unsupported += len(r.Unsupported)
		for _, u := range r.Unsupported {
			why[u.Why]++
		}
		if r.Truncated {
			truncated++
		}
	}
	t.Logf("%d templates, %d parsed, %d with a non-empty data tree", forms, parsed, withData)
	t.Logf("%d <bind> elements: %v", binds, matches)
	t.Logf("%d fields bound out of %d placed, %d carry a value", bound, fields, nonEmpty)
	t.Logf("%d unsupported constructs: %v", unsupported, why)
	t.Logf("%d forms truncated", truncated)
}

// readNode reads one part, reporting a failure to parse rather than stopping.
func readNode(t *testing.T, name string, isTemplate bool) *Node {
	t.Helper()
	f, err := os.Open(name)
	if err != nil {
		return nil
	}
	defer f.Close()
	var n *Node
	if isTemplate {
		n, err = ParseTemplate(f)
	} else {
		n, err = ParseDatasets(f)
	}
	if err != nil {
		t.Logf("%s: %v", filepath.Base(name), err)
		return nil
	}
	return n
}

// TestNoValueIsLostOverTheCorpus counts what the data carries and what
// [Values] hands back, and requires them to be the same number. Skipped
// unless a corpus is named.
//
//	XFACORPUS=/path/to/parts go test -run NoValueIsLost -v
//
// Counting is the whole test. A path scheme that collides does not fail, it
// returns a smaller map, and the only way to see that from outside is to know
// how many values went in. Before the repeats were numbered, 10 125 of the
// 71 346 values the 560 packages carry never came back, across 216 forms.
func TestNoValueIsLostOverTheCorpus(t *testing.T) {
	dir := os.Getenv("XFACORPUS")
	if dir == "" {
		t.Skip("no XFACORPUS")
	}
	names, err := filepath.Glob(filepath.Join(dir, "*.datasets.xml"))
	if err != nil || len(names) == 0 {
		t.Skipf("no datasets in %s", dir)
	}
	sort.Strings(names)
	var carried, returned, losing int
	for _, name := range names {
		data := readNode(t, name, false)
		if data == nil {
			continue
		}
		n, got := countValues(data), len(Values(data))
		carried += n
		returned += got
		if n != got {
			losing++
			t.Errorf("%s: %d values carried, %d returned", filepath.Base(name), n, got)
		}
	}
	t.Logf("%d datasets: %d values carried, %d returned, %d lost across %d forms",
		len(names), carried, returned, carried-returned, losing)
}

// countValues is how many values the data actually carries: one for every leaf
// and one for every group that holds text of its own, which is exactly the set
// [Values] is meant to hand back.
func countValues(n *Node) int {
	c := 0
	for _, k := range n.Kids {
		if len(k.Kids) == 0 || k.Text != "" {
			c++
		}
		c += countValues(k)
	}
	return c
}

// countTemplateFields counts a template's <field> elements the way the
// instrument at /Users/Shared/xfacount/count.py counts them: by the layout of
// the ENCLOSING SUBFORM, with an absent layout read as "position", and with a
// literal w and h required. It is here so that the number this package
// produces is compared against a number produced the same way, and so that a
// later change to either can be seen to move one and not the other.
//
// c is, in order: position with a literal size, under a flow layout, needing
// text measurement, and the total.
func countTemplateFields(n *Node, parentLayout string, c *[4]int) {
	for _, k := range n.Kids {
		switch k.Kind {
		case "subform":
			lay := k.Get("layout")
			if !flowLayouts[lay] {
				lay = "position"
			}
			countTemplateFields(k, lay, c)
		case "field":
			c[3]++
			_, okW, errW := k.Measure("w")
			_, okH, errH := k.Measure("h")
			switch {
			case flowLayouts[parentLayout]:
				c[1]++
			case !okW || !okH || errW != nil || errH != nil:
				c[2]++
			default:
				c[0]++
			}
		default:
			countTemplateFields(k, parentLayout, c)
		}
	}
}

// TestPlacementOverTheCorpus is the number this slice is judged by: how many
// of the corpus's fields it actually places, against how many a count over the
// templates says it should reach.
//
//	XFACORPUS=/path/to/parts go test -run PlacementOverTheCorpus -v
//
// It also holds this package to the invariant the reporting is worth anything
// for: every field and every draw of the expanded form is in exactly one of
// the placed boxes and the unplaced list. A count that does not add up is a
// count of something else.
func TestPlacementOverTheCorpus(t *testing.T) {
	dir := os.Getenv("XFACORPUS")
	if dir == "" {
		t.Skip("no XFACORPUS")
	}
	names, err := filepath.Glob(filepath.Join(dir, "*.template.xml"))
	if err != nil || len(names) == 0 {
		t.Skipf("no templates in %s", dir)
	}
	sort.Strings(names)

	var tmplCount [4]int
	var expandedFields, expandedDraws, placedFields, placedDraws int
	why := map[string]int{}
	shortfall := map[string]int{}
	for _, name := range names {
		tmpl := readNode(t, name, true)
		if tmpl == nil {
			continue
		}
		countTemplateFields(tmpl, "position", &tmplCount)
		stem := strings.TrimSuffix(name, ".template.xml")
		form := Expand(tmpl, readNode(t, stem+".datasets.xml", false))

		ef, ed := 0, 0
		form.Root.Walk(func(k *FormNode) {
			switch k.Kind {
			case "field":
				ef++
			case "draw":
				ed++
			}
		})
		expandedFields += ef
		expandedDraws += ed

		l := Place(form)
		pf, pd, uf, ud := 0, 0, 0, 0
		for _, p := range l.Pages {
			for _, b := range p.Boxes {
				if b.Kind == "field" {
					pf++
				} else {
					pd++
				}
			}
		}
		for _, u := range l.Unplaced {
			if u.Kind == "field" {
				uf++
				why[u.Why]++
			} else {
				ud++
			}
		}
		if pf+uf != ef || pd+ud != ed {
			t.Errorf("%s: %d fields expanded, %d placed and %d unplaced; %d draws, %d and %d",
				filepath.Base(stem), ef, pf, uf, ed, pd, ud)
		}
		placedFields += pf
		placedDraws += pd
		shortfall[filepath.Base(stem)] = pf
	}

	t.Logf("template: %d fields — %d position with a literal size, %d flow, %d needing measurement",
		tmplCount[3], tmplCount[0], tmplCount[1], tmplCount[2])
	t.Logf("expanded: %d fields, %d draws", expandedFields, expandedDraws)
	t.Logf("PLACED:   %d fields, %d draws — against %d predicted, %+d",
		placedFields, placedDraws, tmplCount[0], placedFields-tmplCount[0])
	type kv struct {
		k string
		n int
	}
	var list []kv
	for k, n := range why {
		list = append(list, kv{k, n})
	}
	sort.Slice(list, func(i, j int) bool { return list[i].n > list[j].n })
	for _, e := range list {
		t.Logf("  unplaced %6d  %s", e.n, e.k)
	}
}

// judgeBox is one box pdf.js placed, as /Users/Shared/xfajudge/judge.mjs dumps
// them. That script drives pdf.js's own XFAFactory over a template and its
// datasets and walks the HTML it emits, accumulating left and top down the
// chain of positioned ancestors and resolving the CSS transforms pdf.js writes
// for anchorType and rotate.
type judgeBox struct {
	Kind   string   `json:"kind"`
	Chain  string   `json:"chain"`
	X      *float64 `json:"x"`
	Y      *float64 `json:"y"`
	W      *float64 `json:"w"`
	H      *float64 `json:"h"`
	Turned bool     `json:"turned"`
}

// TestPlacementAgainstPdfjs checks this package's geometry against pdf.js's,
// for the same templates and the same data.
//
//	XFACORPUS=/path/to/parts XFAPDFJS=/path/to/dumps go test -run AgainstPdfjs -v
//
// # Why this and not a fixture
//
// A box at coordinates this package computed says nothing about whether the
// coordinates are right. pdf.js is the only other free implementation of XFA
// layout, and under a positioned layout it computes the same thing this does
// — style.left and style.top from the node's own x and y
// (html_utils.js:112-123) — so its output is a reference to compare against
// rather than a description to trust. It arrives at the anchorType and rotate
// part quite differently, as a CSS transform with percentage translations
// (html_utils.js:44-79, 125-133) against this package's port of
// getTransformedBBox, so those agree or disagree on their arithmetic rather
// than on a shared formula.
//
// # What it does not cover
//
//   - Flow layouts. pdf.js hands those to the browser's flexbox and emits no
//     coordinates for them at all, so it and this package are blind in the
//     same place. Where this slice reports a field unplaced under a flow
//     layout, pdf.js has nothing to compare it with.
//   - Borders, margins and insets, which neither side resolves numerically
//     here: pdf.js writes them as CSS calc() (html_utils.js:426-443).
//   - Text measurement, and so every element with no literal size.
//   - Anything under a rotated ancestor, which the dump flags: an axis-aligned
//     offset cannot carry a rotation down to a descendant.
//
// # How boxes are paired
//
// By kind and by the element's own name, within one form, and only where both
// sides produced the same number of boxes with that name. Anything else is
// counted as unpairable rather than guessed at — a pairing that lines two
// lists up by position would report agreement wherever both sides are wrong in
// the same order, and one that lined them up by size would report it wherever
// two boxes are the same size.
func TestPlacementAgainstPdfjs(t *testing.T) {
	dir, dumps := os.Getenv("XFACORPUS"), os.Getenv("XFAPDFJS")
	if dir == "" || dumps == "" {
		t.Skip("no XFACORPUS or no XFAPDFJS")
	}
	names, err := filepath.Glob(filepath.Join(dir, "*.template.xml"))
	if err != nil || len(names) == 0 {
		t.Skipf("no templates in %s", dir)
	}
	sort.Strings(names)
	var forms, missing, exact, rounding, differ, unpairable int
	worst := map[string]int{}
	for _, name := range names {
		stem := strings.TrimSuffix(name, ".template.xml")
		form := filepath.Base(stem)
		raw, err := os.ReadFile(filepath.Join(dumps, form+".json"))
		if err != nil || len(raw) < 2 {
			missing++
			continue
		}
		var dump struct {
			Boxes []judgeBox `json:"boxes"`
		}
		if err := json.Unmarshal(raw, &dump); err != nil {
			t.Errorf("%s: %v", form, err)
			continue
		}
		forms++
		theirs := map[string][]judgeBox{}
		for _, b := range dump.Boxes {
			if n := leafName(b.Chain); n != "" {
				theirs[b.Kind+" "+n] = append(theirs[b.Kind+" "+n], b)
			}
		}
		mine := map[string][]Box{}
		l := Place(Expand(readNode(t, name, true), readNode(t, stem+".datasets.xml", false)))
		for _, p := range l.Pages {
			for _, b := range p.Boxes {
				if b.Node.Name != "" {
					mine[b.Kind+" "+b.Node.Name] = append(mine[b.Kind+" "+b.Node.Name], b)
				}
			}
		}
		for k, ms := range mine {
			ts := theirs[k]
			if len(ts) != len(ms) {
				unpairable += len(ms)
				continue
			}
			for i, m := range ms {
				d, ok := apart(m, ts[i])
				switch {
				case !ok:
					unpairable++
				case d <= 0.011:
					exact++
				case d <= 0.05:
					// pdf.js rounds every length to two decimals as it emits
					// it (measureToString, html_utils.js:35-41), and a box
					// four containers deep has that rounding four times over.
					rounding++
				default:
					differ++
					worst[form]++
				}
			}
		}
	}
	paired := exact + rounding + differ
	if paired == 0 {
		t.Fatalf("%d forms, nothing paired", forms)
	}
	t.Logf("%d forms compared, %d with no dump", forms, missing)
	t.Logf("%d boxes paired, %d unpairable", paired, unpairable)
	t.Logf("  agree exactly       %6d  %5.2f%%", exact, 100*float64(exact)/float64(paired))
	t.Logf("  pdf.js rounding     %6d  %5.2f%%", rounding, 100*float64(rounding)/float64(paired))
	t.Logf("  DISAGREE            %6d  %5.2f%%", differ, 100*float64(differ)/float64(paired))
	type kv struct {
		k string
		n int
	}
	var list []kv
	for k, n := range worst {
		list = append(list, kv{k, n})
	}
	sort.Slice(list, func(i, j int) bool { return list[i].n > list[j].n })
	for i, e := range list {
		if i >= 8 {
			break
		}
		t.Logf("  %6d in %s", e.n, e.k)
	}
}

// leafName is the element's own name at the end of a dumped chain.
func leafName(chain string) string {
	if i := strings.LastIndex(chain, "."); i >= 0 {
		return chain[i+1:]
	}
	return chain
}

// apart is how far two boxes are from each other, in points, on their furthest
// side. It is false when pdf.js did not produce a comparable box: a size it
// left to the browser, or a rotated ancestor an offset cannot carry down.
func apart(m Box, t judgeBox) (float64, bool) {
	if t.Turned || t.X == nil || t.Y == nil || t.W == nil || t.H == nil {
		return 0, false
	}
	d := 0.0
	for _, pair := range [][2]float64{
		{float64(m.X), *t.X}, {float64(m.Y), *t.Y},
		{float64(m.W), *t.W}, {float64(m.H), *t.H},
	} {
		if v := math.Abs(pair[0] - pair[1]); v > d {
			d = v
		}
	}
	return d, true
}
