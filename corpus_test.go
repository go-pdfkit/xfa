// Copyright (c) 2026, the go-pdfkit/xfa authors
// All rights reserved.
//
// SPDX-License-Identifier: BSD-3-Clause

package xfa

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strconv"
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
// of the corpus's fields it actually places, beside a count over the templates
// made the way the instrument outside this repository makes it.
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
	var expandedFields, expandedDraws, placedFields, placedDraws, pastTheBottom int
	var pages, furniture, furnitureOff int
	why := map[string]int{}

	for _, name := range names {
		tmpl := readNode(t, name, true)
		if tmpl == nil {
			continue
		}
		countTemplateFields(tmpl, "position", &tmplCount)
		stem := strings.TrimSuffix(name, ".template.xml")
		form := Expand(tmpl, readNode(t, stem+".datasets.xml", false))

		// The body and the paper are counted apart. A page area's own
		// furniture is drawn on every sheet that page area makes, so it is the
		// one thing that is not in one-to-one correspondence with the boxes on
		// the paper, and counting it in would hide a real leak behind it.
		body := map[*FormNode]bool{}
		ef, ed := 0, 0
		bodyOf(form.Root, body)
		form.Root.Walk(func(k *FormNode) {
			if !body[k] {
				return
			}
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
		pages += len(l.Pages)
		pf, pd, uf, ud := 0, 0, 0, 0
		seen := map[*FormNode]bool{}
		for _, p := range l.Pages {
			for _, b := range p.Boxes {
				switch {
				case !body[b.Node]:
					// Furniture: it may be on many sheets, and is counted once.
					if !seen[b.Node] {
						seen[b.Node] = true
						furniture++
					}
				case b.Kind == "field":
					pf++
				default:
					pd++
				}
			}
		}
		for _, u := range l.Unplaced {
			if seen[u.Node] {
				t.Errorf("%s: %s is both placed and unplaced", filepath.Base(stem), u.Path)
			}
			switch {
			case !body[u.Node]:
				furnitureOff++
			case u.Kind == "field":
				uf++
				why[u.Why]++
				if u.Why == noNextPage || u.Why == noRoomInside {
					pastTheBottom++
				}
			default:
				ud++
			}
		}
		if pf+uf != ef || pd+ud != ed {
			t.Errorf("%s: %d fields expanded, %d placed and %d unplaced; %d draws, %d and %d",
				filepath.Base(stem), ef, pf, uf, ed, pd, ud)
		}
		placedFields += pf
		placedDraws += pd
	}

	t.Logf("template: %d fields — %d position with a literal size, %d flow, %d needing measurement",
		tmplCount[3], tmplCount[0], tmplCount[1], tmplCount[2])
	t.Logf("expanded: %d fields, %d draws in the body", expandedFields, expandedDraws)
	t.Logf("PLACED:   %d fields, %d draws, on %d pages", placedFields, placedDraws, pages)
	t.Logf("          %d page-area elements drawn as furniture, %d on no page at all",
		furniture, furnitureOff)
	// What the page machinery itself could not reach, as against what waits on
	// a height no arithmetic gives.
	t.Logf("          %d more fields have a place computed and nowhere to put it: no further page, "+
		"or taller than a whole content area", pastTheBottom)
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
	Kind string `json:"kind"`
	// Page is which of pdf.js's own page divs the box came out in. It is the
	// one thing pdf.js says outright about pagination, and the only reason
	// this slice has an external control at all.
	Page   int      `json:"page"`
	Chain  string   `json:"chain"`
	X      *float64 `json:"x"`
	Y      *float64 `json:"y"`
	W      *float64 `json:"w"`
	H      *float64 `json:"h"`
	Turned bool     `json:"turned"`
	// Flowed says a container above this one laid it out by stacking rather
	// than by coordinates. pdf.js emits no left or top under a flow layout —
	// it writes the children into a flexbox column and lets the browser stack
	// them — so X and Y are then the flow container's own origin and not this
	// element's place. The dump carries the flag so that a check cannot
	// mistake the one for the other.
	Flowed bool `json:"flowed"`
	// Layout is the CSS class pdf.js gave the container, for a container box.
	Layout string `json:"layout"`
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
//   - Where a flow layout put a box. pdf.js hands those to the browser's
//     flexbox and emits no coordinates for them at all, so the dump carries
//     the flow container's origin instead and says so with Flowed. Those are
//     counted apart, and all that is checked of them is that they lie within
//     the container rather than above or to the left of it.
//     [TestFlowHeightsAgainstPdfjs] is the check for the stacking itself.
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
	var forms, missing, exact, rounding, differ, unpairable, atOrigin, below, above int
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
		fm := Expand(readNode(t, name, true), readNode(t, stem+".datasets.xml", false))
		root := rootName(fm)
		theirs := map[string][]judgeBox{}
		for _, b := range dump.Boxes {
			if b.Kind == "container" {
				continue
			}
			if n := chainKey(b.Chain, root); n != "" {
				theirs[b.Kind+" "+n] = append(theirs[b.Kind+" "+n], b)
			}
		}
		mine := map[string][]Box{}
		l := Place(fm)
		for _, p := range l.Pages {
			for _, b := range p.Boxes {
				if k := ourChain(b.Path); b.Node.Name != "" {
					mine[b.Kind+" "+k] = append(mine[b.Kind+" "+k], b)
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
				if ts[i].Flowed {
					// pdf.js placed this one by flexbox and emitted no
					// coordinates for it, so what the dump carries is the
					// origin of the flow container it is in. What can be said
					// is one-sided and is worth saying: a stacked box may sit
					// at that origin, which is where the first child goes, or
					// below and to the right of it, which is where the rest
					// do. Above or to the left of it would be a defect.
					if ts[i].X == nil || ts[i].Y == nil {
						unpairable++
						continue
					}
					tx, ty := leftTop(*ts[i].X, *ts[i].Y, ts[i].W, ts[i].H)
					switch {
					case float64(m.X) < tx-0.05 || float64(m.Y) < ty-0.05:
						above++
						worst[form]++
					case float64(m.X) <= tx+0.05 && float64(m.Y) <= ty+0.05:
						atOrigin++
					default:
						below++
					}
					continue
				}
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
	t.Logf("%d more pdf.js placed by flexbox, where it emits the flow container's origin and no place of its own:",
		atOrigin+below+above)
	t.Logf("  at that origin      %6d   (a first child, which is where the flow puts it)", atOrigin)
	t.Logf("  below or right of it%6d   (stacked after one, which pdf.js does not say where)", below)
	t.Logf("  ABOVE or LEFT of it %6d   (outside the container: a defect)", above)
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

// leftTop is the corner of one of pdf.js's boxes, with a negative width or
// height folded into the origin the way this package folds it.
//
// # A judge has to count the way its subject counts
//
// Four draws of us-ssa__ss-5-ar-inst are written w="-0.106in" and the like: a
// NEGATIVE width. A negative extent is not a box that reaches left of where it
// was put. pdfium normalises a widget's rectangle before it uses it —
// CFX_RectF::Normalize moves the origin by the extent and takes its magnitude,
// and the XFA field and widget code call it (cxfa_fffield.cpp:293,
// cxfa_ffwidget.cpp:288) — and this package does the same in [transformedBBox].
//
// pdf.js does not, because CSS cannot: it writes width:-0.11px, which a
// browser ignores. So its dump keeps the negative number, and comparing OUR
// normalised left edge with THEIR unnormalised origin reported four boxes
// outside the container holding them that are the same box on both sides.
//
// It is the shape the last slice's name pairing had: the fault was in the
// comparison and it announced itself as a disagreement about the subject.
func leftTop(x, y float64, w, h *float64) (float64, float64) {
	if w != nil {
		x += math.Min(0, *w)
	}
	if h != nil {
		y += math.Min(0, *h)
	}
	return x, y
}

// leafName is the element's own name at the end of a dumped chain.
func leafName(chain string) string {
	if i := strings.LastIndex(chain, "."); i >= 0 {
		return chain[i+1:]
	}
	return chain
}

// chainKey is the name a box is paired by: its WHOLE path through the form
// rather than its own name.
//
// Pairing by the last name alone is what slices 1 to 3 did, and it holds only
// while the two sides list the same names in the same order. It stops holding
// the moment a form writes the same name in several places: us-irs__fw9 has
// six subforms called Bullet1, in three different lists on three sheets, and
// once every one of them has a height they are six entries of one list that
// have to line up. They do not have to: pdf.js emits what it laid out, in the
// order it laid it out, and this walks the expanded form. Pairing on the chain
// makes a mispairing show up as an unpaired box instead of as a disagreement.
//
// pdf.js's chain begins with the page area's own name, because the page div
// carries it; ours begins at the form's outermost subform. So the key is
// whatever follows the first mention of that subform. A box outside it — the
// page area's own furniture — has no key and is not paired.
func chainKey(chain, root string) string {
	for i := 0; i+len(root) <= len(chain); i++ {
		if chain[i:i+len(root)] != root {
			continue
		}
		if (i == 0 || chain[i-1] == '.') && (i+len(root) == len(chain) || chain[i+len(root)] == '.') {
			return chain[i:]
		}
	}
	return ""
}

// ourChain is the same key from this package's own path, which numbers the
// second and later of repeated siblings — "Row[2]" — where pdf.js writes the
// name again. The numbers come off so the two can be compared; what they were
// keeping apart is kept apart by the ORDER of the list the key leads to.
func ourChain(path string) string {
	var b strings.Builder
	for i := 0; i < len(path); i++ {
		if path[i] == '[' {
			j := strings.IndexByte(path[i:], ']')
			if j < 0 {
				break
			}
			i += j
			continue
		}
		b.WriteByte(path[i])
	}
	return b.String()
}

// rootName is what the form's outermost subform is called, which is where
// pdf.js's chain and this package's path meet. See [chainKey].
func rootName(form *Form) string {
	if r := firstOfKind(form.Root, "subform"); r != nil {
		return r.Name
	}
	return ""
}

// apart is how far two boxes are from each other, in points, on their furthest
// side. It is false when pdf.js did not produce a comparable box: a size it
// left to the browser, or a rotated ancestor an offset cannot carry down.
func apart(m Box, t judgeBox) (float64, bool) {
	if t.Turned || t.X == nil || t.Y == nil || t.W == nil || t.H == nil {
		return 0, false
	}
	// Both sides are compared as the corner and the extent of the same box.
	// See [leftTop].
	x, y := leftTop(*t.X, *t.Y, t.W, t.H)
	d := 0.0
	for _, pair := range [][2]float64{
		{float64(m.X), x}, {float64(m.Y), y},
		{float64(m.W), math.Abs(*t.W)}, {float64(m.H), math.Abs(*t.H)},
	} {
		if v := math.Abs(pair[0] - pair[1]); v > d {
			d = v
		}
	}
	return d, true
}

// TestFlowHeightsAgainstPdfjs is the check for what this slice adds, and it
// exists because the check above cannot reach it.
//
//	XFACORPUS=/path/to/parts XFAPDFJS=/path/to/dumps go test -run FlowHeights -v
//
// # Why a height and not a place
//
// pdf.js emits no coordinates for a child of a flow layout: it writes the
// children into a div with "display: flex; flex-direction: column"
// (web/xfa_layer_builder.css:255-263) and lets the browser stack them. So for
// exactly the layouts this slice computes, pdf.js's own output says where the
// CONTAINER is and nothing about where its second child went.
//
// It does emit the accumulation itself, as a number, in one place. A subform's
// own height is
//
//	Math.max(this[$extra].height + marginV, this.h || 0)      template.js:5222
//
// and that is written into style.height (template.js:5224-5229) and hoisted
// onto the wrapper by createWrapper (html_utils.js:480-494). extra.height is
// the sum this slice computes — extra.height += h, once per child
// (layout.js:144-158). So comparing container heights compares the arithmetic,
// one container at a time, against the implementation it was read from.
//
// # What it does not cover
//
//   - Where a container's children ended up inside it. Two orderings of the
//     same children come to the same total, and this cannot tell them apart.
//   - Any container whose height a template writes outright: pdf.js emits the
//     written number and so does this package, and the two agreeing says
//     nothing about the arithmetic. They are counted apart for that reason.
//   - Any container pdf.js split across pages, or laid out once per page, or
//     did not reach at all. Those come out with a different number of boxes on
//     one side than the other, and are left unpaired rather than guessed at.
//   - Every height that needs text measurement. pdf.js measures and this
//     package does not, so there is no number of ours to compare.
func TestFlowHeightsAgainstPdfjs(t *testing.T) {
	dir, dumps := os.Getenv("XFACORPUS"), os.Getenv("XFAPDFJS")
	if dir == "" || dumps == "" {
		t.Skip("no XFACORPUS or no XFAPDFJS")
	}
	names, err := filepath.Glob(filepath.Join(dir, "*.template.xml"))
	if err != nil || len(names) == 0 {
		t.Skipf("no templates in %s", dir)
	}
	sort.Strings(names)
	var forms, computed, written, stretched, agree, rounding, differ, unpairable, unmeasured int
	worst := map[string]int{}
	for _, name := range names {
		stem := strings.TrimSuffix(name, ".template.xml")
		form := filepath.Base(stem)
		raw, err := os.ReadFile(filepath.Join(dumps, form+".json"))
		if err != nil || len(raw) < 2 {
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
		fm := Expand(readNode(t, name, true), readNode(t, stem+".datasets.xml", false))
		root := rootName(fm)
		theirs := map[string][]judgeBox{}
		for _, b := range dump.Boxes {
			if b.Kind != "container" {
				continue
			}
			if n := chainKey(b.Chain, root); n != "" {
				theirs[n] = append(theirs[n], b)
			}
		}
		mine := map[string][]containerHeight{}
		p := newPlacer()
		p.mapUp(fm.Root)
		collect(p, fm.Root, bodyWide(fm), containerHeight{}, mine)
		for k, ms := range mine {
			ts := theirs[k]
			if len(ts) != len(ms) {
				unpairable += len(ms)
				continue
			}
			for i, m := range ms {
				switch {
				case !m.measured:
					unmeasured++
				case ts[i].H == nil:
					unpairable++
				case m.written:
					written++
				default:
					computed++
					if m.stretched {
						stretched++
					}
					switch d := math.Abs(float64(m.h) - *ts[i].H); {
					case d <= 0.011:
						agree++
					case d <= 0.05:
						// pdf.js rounds every length to two decimals as it
						// emits it (measureToString, html_utils.js:35-41).
						rounding++
					default:
						differ++
						worst[form]++
					}
				}
			}
		}
	}
	if computed == 0 {
		t.Fatalf("%d forms, no computed height to compare", forms)
	}
	t.Logf("%d forms", forms)
	t.Logf("%d container heights paired: %d this package computes, %d the template writes outright",
		computed+written, computed, written)
	t.Logf("  of the computed, %d are the height a row stretches its cells to", stretched)
	t.Logf("%d unpairable, %d this package cannot measure", unpairable, unmeasured)
	t.Logf("of the %d computed:", computed)
	t.Logf("  agree to 1/100 pt   %6d  %5.2f%%", agree, 100*float64(agree)/float64(computed))
	t.Logf("  pdf.js rounding     %6d  %5.2f%%", rounding, 100*float64(rounding)/float64(computed))
	t.Logf("  DISAGREE            %6d  %5.2f%%", differ, 100*float64(differ)/float64(computed))
	logWorst(t, worst)
}

// A containerHeight is what this package makes of one container's height.
type containerHeight struct {
	h Measure
	// measured says a height came out at all: false where something inside it
	// has no height until its text is measured.
	measured bool
	// written says the template gives the container a height of its own, so
	// that pdf.js emitting the same number says nothing about the arithmetic.
	written bool
	// stretched says the height here is a row's rather than the container's
	// own, because a row stretches its cells to it. See [collect].
	stretched bool
}

// collect walks the expanded form and records, for every named container, the
// height pdf.js should emit for it.
//
// That is the container's own height everywhere but in one place. A cell of a
// row is stretched to the height of the tallest cell in the row, and pdf.js
// applies the stretch by writing the row's height back over every cell it has
// already placed (layout.js:135-143) — so the style.height it emits for a cell
// is the ROW's height and not the cell's own. Comparing our cell height with
// that reports thirteen disagreements over the corpus, every one of them the
// stretch rather than the arithmetic. Comparing our ROW height with it checks
// the stretch instead, which is what pdf.js is saying there.
func bodyWide(form *Form) Measure {
	root := firstOfKind(form.Root, "subform")
	if root == nil {
		return unbounded
	}
	area, _ := newPager(root).first(root)
	if area == nil {
		return unbounded
	}
	areas := contentAreas(area)
	if len(areas) == 0 {
		return unbounded
	}
	return contentWide(areas[0])
}

func collect(p *placer, n *FormNode, wide Measure, row containerHeight, out map[string][]containerHeight) {
	switch n.Kind {
	case "subform", "exclGroup", "area":
		// pdf.js emits nothing at all for a hidden container
		// (template.js:5019-5021), so it is not among the boxes to pair with
		// and must not be among ours either.
		if hidden(n.Template) {
			return
		}
		if n.Name != "" {
			c := row
			if !c.stretched {
				h, why := p.heightOf(n, wide, 0)
				_, ok, errH := n.Template.Measure("h")
				c = containerHeight{h: h, measured: why == "", written: ok && errH == nil}
			}
			k := ourChain(n.Path)
			out[k] = append(out[k], c)
		}
	}
	var into containerHeight
	var cols []Measure
	lay := layoutOf(n)
	isRow := lay == "row" || lay == "rl-row"
	if isRow {
		h, why := p.contentHeight(n, wide)
		into = containerHeight{h: h, measured: why == "", stretched: true}
		cols = p.colsOf(n)
	}
	inner := noWidth
	if in, ok := marginOf(n.Template); ok {
		inner = innerWide(n, wide, 0, lay, in)
	}
	col := 0
	for _, k := range contained(n) {
		kw := inner
		if isRow {
			kw = noWidth
			if len(cols) > 0 {
				kw = remainingWide(cols, col)
				if !hidden(k.Template) {
					_, col = columnWidth(cols, col, colSpanOf(k.Template))
				}
			}
		}
		collect(p, k, kw, into, out)
	}
}

// logWorst names the forms a comparison went worst in.
func logWorst(t *testing.T, worst map[string]int) {
	t.Helper()
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

// bodyOf marks every node of the form that is the body rather than the paper.
// What is under a page set describes a sheet, and is drawn once per sheet that
// page area makes rather than once per element.
func bodyOf(n *FormNode, into map[*FormNode]bool) {
	if n.Kind == "pageSet" {
		return
	}
	into[n] = true
	for _, k := range n.Kids {
		bodyOf(k, into)
	}
}

// TestPaginationAgainstPdfjs checks how many sheets this package comes to, and
// which sheet each element lands on, against pdf.js's answer for the same
// form.
//
//	XFACORPUS=/path/to/parts XFAPDFJS=/path/to/dumps go test -run AgainstPdfjs -v
//
// # Why this is a judge where the last slice had none for its own work
//
// pdf.js emits one <div class="xfaPage"> per sheet and every element inside
// the one it belongs to. That is a thing it says outright, in the structure of
// its output rather than in a style — so page COUNT and per-page MEMBERSHIP
// are comparable even where a coordinate is not, which is exactly what the
// last slice could not check about its stacking and had to reach for container
// heights instead.
//
// # What it does not cover, and why the strict comparison is narrowed
//
// pdf.js measures text and this package does not, so on a form where a leaf
// writes no height pdf.js lays out elements this package leaves unplaced —
// and more elements need more sheets. Comparing page counts there would
// measure the missing font stack, not the pagination. So the count is compared
// strictly only on the forms where this package placed EVERY element of the
// body, where the two are laying out the same thing; the rest are reported
// apart, and the direction of the disagreement is reported with them, because
// a form where this package needs MORE sheets than pdf.js while placing FEWER
// elements would be a defect and not a shortfall.
//
// Membership is not checked where the count disagrees: two different
// paginations of the same form have no page to compare. It says nothing about
// WHERE on a page an element sits — [TestPlacementAgainstPdfjs] is that check
// — nor about the 77 forms pdf.js cannot lay out at all.
func TestPaginationAgainstPdfjs(t *testing.T) {
	dir, dumps := os.Getenv("XFACORPUS"), os.Getenv("XFAPDFJS")
	if dir == "" || dumps == "" {
		t.Skip("no XFACORPUS or no XFAPDFJS")
	}
	names, err := filepath.Glob(filepath.Join(dir, "*.template.xml"))
	if err != nil || len(names) == 0 {
		t.Skipf("no templates in %s", dir)
	}
	sort.Strings(names)
	var whole, wholeAgree, partial, partialFewer, partialSame, partialMore int
	var paired, samePage, otherPage int
	var theirPages, ourPages int
	disagreed := map[string]string{}
	for _, name := range names {
		stem := strings.TrimSuffix(name, ".template.xml")
		form := filepath.Base(stem)
		raw, err := os.ReadFile(filepath.Join(dumps, form+".json"))
		if err != nil || len(raw) < 2 {
			continue
		}
		var dump struct {
			Pages int        `json:"pages"`
			Boxes []judgeBox `json:"boxes"`
		}
		if err := json.Unmarshal(raw, &dump); err != nil {
			t.Errorf("%s: %v", form, err)
			continue
		}
		fm := Expand(readNode(t, name, true), readNode(t, stem+".datasets.xml", false))
		l := Place(fm)
		theirPages += dump.Pages
		ourPages += len(l.Pages)
		if len(l.Unplaced) != 0 {
			partial++
			switch {
			case len(l.Pages) < dump.Pages:
				partialFewer++
			case len(l.Pages) == dump.Pages:
				partialSame++
			default:
				partialMore++
			}
			continue
		}
		whole++
		if len(l.Pages) != dump.Pages {
			disagreed[form] = fmt.Sprintf("%d sheets against pdf.js's %d", len(l.Pages), dump.Pages)
			continue
		}
		wholeAgree++
		// The count agrees, so the sheets can be lined up and membership asked
		// of them. Boxes are paired as [TestPlacementAgainstPdfjs] pairs them:
		// by kind and by the whole chain of names down to the element, and
		// only where both sides produced the same number of them.
		root := rootName(fm)
		theirs := map[string][]int{}
		for _, b := range dump.Boxes {
			if b.Kind == "container" {
				continue
			}
			if n := chainKey(b.Chain, root); n != "" {
				theirs[b.Kind+" "+n] = append(theirs[b.Kind+" "+n], b.Page)
			}
		}
		mine := map[string][]int{}
		for i, p := range l.Pages {
			for _, b := range p.Boxes {
				if k := ourChain(b.Path); b.Node.Name != "" {
					mine[b.Kind+" "+k] = append(mine[b.Kind+" "+k], i)
				}
			}
		}
		for k, ms := range mine {
			ts, ok := theirs[k]
			if !ok || len(ts) != len(ms) {
				continue
			}
			for i, m := range ms {
				paired++
				if m == ts[i] {
					samePage++
				} else {
					otherPage++
					if _, seen := disagreed[form]; !seen {
						disagreed[form] = fmt.Sprintf("%s is on sheet %d, pdf.js puts it on %d", k, m, ts[i])
					}
				}
			}
		}
	}
	t.Logf("pdf.js laid out %d forms on %d sheets; this package puts the same forms on %d",
		whole+partial, theirPages, ourPages)
	t.Logf("forms where this package placed EVERY element: %d, of which %d agree on the number of sheets",
		whole, wholeAgree)
	t.Logf("forms where it did not: %d — %d on fewer sheets, %d on the same, %d on MORE",
		partial, partialFewer, partialSame, partialMore)
	t.Logf("boxes paired on those %d forms: %d, on the same sheet %d, on another %d",
		wholeAgree, paired, samePage, otherPage)
	shown := 0
	for form, what := range disagreed {
		if shown++; shown > 20 {
			break
		}
		t.Logf("  %-40s %s", form, what)
	}
}

// A lineFault is one property of a line that a form broke, with enough of the
// line written out to look at.
type lineFault struct {
	form, prop, detail string
}

// TestIntraLinePlacementProperties asks whether the boxes on one LINE are
// where a line's boxes have to be.
//
//	XFACORPUS=/path/to/parts go test -run IntraLinePlacement -v
//
// # Why a property and not an agreement
//
// This is the one part of the layout no judge in this repository can see.
// pdf.js emits no coordinate inside a line — createLine wraps a wrapping
// container's children in a div of class xfaLr or xfaRl (layout.js:57-65) and
// a table row in one of class xfaRow (xfa_layer_builder.css:298-302), all
// three of them flexboxes, and the browser places the children. So
// [TestPlacementAgainstPdfjs] has nothing to pair against, and every release
// since v0.5.0 has said so.
//
// pdfium is the other free implementation and DOES compute intra-line
// geometry — CalculateRowChildPosition, cxfa_contentlayoutprocessor.cpp:2028-2160
// — but nothing in it can be reached from outside: no export of public/
// returns a layout item's position, and its own suite asserts no coordinate
// anywhere. Its layout test (cxfa_layoutitem_embeddertest.cpp) counts pages.
//
// So this asks what a correct line must satisfy rather than who agrees with
// it. Each property is one a wrong packing usually breaks, and none of them is
// derived from the coordinates being checked: which line a box went on comes
// from the packing, and which cells are one row comes from the tree.
//
// # What it cannot see, and it is the same shape as before
//
// A line packed in the WRONG ORDER at the right widths satisfies every one of
// these: no two boxes overlap, each begins where the one before it ended, the
// line is no wider than its container. Order among the boxes of a line is
// checked against DOCUMENT order, so a packing that reversed two children
// would be caught — but one that put the right boxes on the wrong LINE, with
// each line still well formed, would not. Nor can a property say whether the
// widths themselves are right: a line of three boxes each half the width they
// should be packs perfectly.
func TestIntraLinePlacementProperties(t *testing.T) {
	dir := os.Getenv("XFACORPUS")
	if dir == "" {
		t.Skip("no XFACORPUS")
	}
	names, err := filepath.Glob(filepath.Join(dir, "*.template.xml"))
	if err != nil || len(names) == 0 {
		t.Skipf("no templates in %s", dir)
	}
	sort.Strings(names)

	var faults []lineFault
	// The packing's own population: containers, packings of them, lines and
	// boxes. A container packed at two different widths is two packings.
	var packedConts, packings, packedLines, packedBoxes, packedHidden, packedPairs int
	// The paper's population: lines whose members reached the page, and the
	// members that did.
	var paperLines, paperMembers, paperPairs, paperRows, paperCells, rowPairs int
	var spanning, unplacedMember int
	byLayout := map[string]int{}

	for _, name := range names {
		form := filepath.Base(strings.TrimSuffix(name, ".template.xml"))
		tmpl := readNode(t, name, true)
		if tmpl == nil {
			continue
		}
		stem := strings.TrimSuffix(name, ".template.xml")
		f := Expand(tmpl, readNode(t, stem+".datasets.xml", false))
		l, p := placeForm(f, nil)

		conts := map[*FormNode]bool{}
		for k, pk := range p.packs {
			if pk.why != "" {
				continue
			}
			conts[k.n] = true
			packings++
			byLayout[layoutOf(k.n)]++
			c, fs := checkOnePacking(form, k.n, k.wide, pk.f)
			packedLines += c.lines
			packedBoxes += c.boxes
			packedHidden += c.hidden
			packedPairs += c.pairLines
			faults = append(faults, fs...)
		}
		packedConts += len(conts)

		// The paper. A member of a line is a whole child of the wrapping
		// container, and what reached the page is its LEAVES, so its extent
		// is theirs — the union of the boxes placed for the elements under
		// it, taken per sheet because a member that was flowed may have been
		// split across two.
		ext := extentsOf(l, p)
		for cont := range conts {
			if layoutOf(cont) != "lr-tb" && layoutOf(cont) != "rl-tb" {
				continue
			}
			var fl fill
			for k, pk := range p.packs {
				if k.n == cont && pk.why == "" {
					fl = pk.f
				}
			}
			c, fs := checkOnPaper(form, cont, layoutOf(cont), lineGroups(fl), ext)
			paperLines += c.lines
			paperMembers += c.members
			paperPairs += c.compared
			spanning += c.spanning
			unplacedMember += c.unplaced
			faults = append(faults, fs...)
		}
		// A table row is the third flexbox, and its members are its cells in
		// document order: one line, always.
		f.Root.Walk(func(n *FormNode) {
			if layoutOf(n) != "row" && layoutOf(n) != "rl-row" {
				return
			}
			kids := contained(n)
			if len(kids) == 0 {
				return
			}
			c, fs := checkOnPaper(form, n, layoutOf(n), [][]*FormNode{kids}, ext)
			paperRows += c.lines
			paperCells += c.members
			rowPairs += c.compared
			spanning += c.spanning
			unplacedMember += c.unplaced
			faults = append(faults, fs...)
		})
	}

	t.Logf("THE PACKING, in the container's own coordinates")
	t.Logf("  %d wrapping containers reached, packed %d times between them", packedConts, packings)
	for lay, n := range byLayout {
		t.Logf("    %-6s %d packings", lay, n)
	}
	t.Logf("  %d lines, %d boxes on them, of which %d are hidden and take no room",
		packedLines, packedBoxes, packedHidden)
	t.Logf("  %d of those lines hold MORE THAN ONE box, which is what an order and an overlap "+
		"can be asked of at all", packedPairs)
	t.Logf("THE PAPER, absolute on the sheet")
	t.Logf("  %d lines of a wrapping container, %d members of them placed, %d of those members "+
		"on a line with a NEIGHBOUR to be compared against", paperLines, paperMembers, paperPairs)
	t.Logf("  %d table rows, %d cells of them placed, %d of those cells with a NEIGHBOUR",
		paperRows, paperCells, rowPairs)
	t.Logf("  %d members placed on more than one sheet, so not compared; %d with nothing on the paper",
		spanning, unplacedMember)

	if len(faults) == 0 {
		t.Logf("VIOLATIONS: none")
		return
	}
	counts := map[string]int{}
	for _, v := range faults {
		counts[v.prop]++
	}
	var props []string
	for k := range counts {
		props = append(props, k)
	}
	sort.Strings(props)
	t.Logf("VIOLATIONS: %d", len(faults))
	for _, k := range props {
		t.Logf("  %6d  %s", counts[k], k)
	}
	shown := map[string]int{}
	for _, v := range faults {
		if shown[v.prop] >= 5 {
			continue
		}
		shown[v.prop]++
		t.Logf("  %s: %s: %s", v.form, v.prop, v.detail)
	}
}

// lineGroups is a packing's boxes gathered into the lines they went on, in
// document order within each.
func lineGroups(fl fill) [][]*FormNode {
	var out [][]*FormNode
	for _, b := range fl.boxes {
		for len(out) <= b.line {
			out = append(out, nil)
		}
		out[b.line] = append(out[b.line], b.node)
	}
	return out
}

// checkOnePacking asks the five properties of one container's packing, in the
// coordinates the packing itself works in — where the room across the page is
// known exactly, because it is what the packing was given.
func checkOnePacking(form string, n *FormNode, wide Measure, fl fill) (c census, faults []lineFault) {
	name := n.Path
	lineOf := map[int][]lineBox{}
	for _, b := range fl.boxes {
		lineOf[b.line] = append(lineOf[b.line], b)
	}
	c.lines = len(lineOf)
	for li := range len(lineOf) {
		bs := lineOf[li]
		var seen int
		var prev *lineBox
		var top Measure
		var reach Measure
		for i := range bs {
			b := bs[i]
			c.boxes++
			if isHidden(b.node) {
				c.hidden++
				continue
			}
			seen++
			if b.x < 0 {
				faults = append(faults, lineFault{form, "a box begins before its container's content origin",
					fmt.Sprintf("%s line %d: %s at x=%g", name, li, b.node.Path, float64(b.x))})
			}
			if i == 0 {
				top = b.y
			} else if b.y != top {
				faults = append(faults, lineFault{form, "a box on a line is not in the line's vertical band",
					fmt.Sprintf("%s line %d: %s at y=%g, the line begins at %g",
						name, li, b.node.Path, float64(b.y), float64(top))})
			}
			if prev != nil {
				if b.x < prev.x {
					faults = append(faults, lineFault{form, "the boxes of a line are not in document order across it",
						fmt.Sprintf("%s line %d: %s at x=%g after %s at x=%g",
							name, li, b.node.Path, float64(b.x), prev.node.Path, float64(prev.x))})
				} else if float64(prev.x+prev.w-b.x) > fitSlop {
					faults = append(faults, lineFault{form, "two boxes of a line overlap",
						fmt.Sprintf("%s line %d: %s reaches %g, %s begins at %g",
							name, li, prev.node.Path, float64(prev.x+prev.w), b.node.Path, float64(b.x))})
				}
			}
			reach = max(reach, b.x+b.w)
			prev = &bs[i]
		}
		if seen > 1 {
			c.pairLines++
		}
		if reach > 0 && !fits(reach, wide) {
			faults = append(faults, lineFault{form, "a line is wider than the room its container gives",
				fmt.Sprintf("%s line %d reaches %g in %g", name, li, float64(reach), float64(wide))})
		}
	}
	return c, faults
}

// A census is how much of a population a check actually reached. It is kept
// apart from the violations because the two answer different questions, and
// the second is worthless without the first: a check that compared nothing
// reports no violation either.
type census struct {
	// lines is how many lines were looked at, boxes how many boxes were on
	// them, hidden how many of those take no room. pairLines is the lines
	// holding more than one box that takes room — the only ones an order or
	// an overlap is a question about.
	lines, boxes, hidden, pairLines int
	// members is how many members of a line reached the paper, compared how
	// many of those had a neighbour on their own line to be compared against,
	// spanning how many were placed on more than one sheet and unplaced how
	// many reached no sheet at all.
	members, compared, spanning, unplaced int
}

// A span is where one member of a line came out on the paper: the union of
// the boxes placed for the elements under it, on one sheet.
type span struct {
	page       int
	x, y, r, b Measure
	n          int
}

// extentsOf is, for every element the form placed, the span its subtree came
// to on each sheet.
//
// It is built from the boxes upwards rather than from the tree downwards
// because a container is not a box: only fields and draws reach [Page.Boxes],
// and a member of a line is usually neither. Hidden boxes are left out — the
// template asks for them not to be drawn and a flow layout gives them no room,
// so a hidden leaf's rectangle says nothing about where its container reaches.
func extentsOf(l *Layout, p *placer) map[*FormNode]map[int]*span {
	out := map[*FormNode]map[int]*span{}
	for pi, pg := range l.Pages {
		for _, b := range pg.Boxes {
			if b.Hidden || b.Rect.W == 0 && b.Rect.H == 0 {
				continue
			}
			for n := b.Node; n != nil; n = p.up[n] {
				per, ok := out[n]
				if !ok {
					per = map[int]*span{}
					out[n] = per
				}
				s, ok := per[pi]
				if !ok {
					per[pi] = &span{page: pi, x: b.Rect.X, y: b.Rect.Y,
						r: b.Rect.X + b.Rect.W, b: b.Rect.Y + b.Rect.H, n: 1}
					continue
				}
				s.x = min(s.x, b.Rect.X)
				s.y = min(s.y, b.Rect.Y)
				s.r = max(s.r, b.Rect.X+b.Rect.W)
				s.b = max(s.b, b.Rect.Y+b.Rect.H)
				s.n++
			}
		}
	}
	return out
}

// checkOnPaper asks the properties of a line again, of where its members
// actually came out on the sheet.
//
// This is the half [checkOnePacking] cannot reach. The packing works in the
// container's own coordinates and stops at its own children; between it and
// the paper are [lineX], which anchors a line at the container's left edge for
// lr-tb and at its right for rl-tb, the offsets of every container above, and
// — for a member that is alone on its line and splittable — a whole second
// route through the flowing chain. A fault in any of those is invisible to the
// packing and visible here.
//
// A member placed on more than one sheet is counted and not compared: its
// boxes are on two pieces of paper and no single rectangle holds them.
func checkOnPaper(form string, cont *FormNode, lay string, groups [][]*FormNode, ext map[*FormNode]map[int]*span) (c census, faults []lineFault) {
	name := cont.Path
	for li, kids := range groups {
		var on []*span
		var who []*FormNode
		for _, k := range kids {
			if isHidden(k) {
				continue
			}
			per := ext[k]
			if len(per) == 0 {
				c.unplaced++
				continue
			}
			if len(per) > 1 {
				c.spanning++
				continue
			}
			for _, s := range per {
				on = append(on, s)
				who = append(who, k)
			}
		}
		if len(on) == 0 {
			continue
		}
		c.lines++
		c.members += len(on)
		if len(on) == 1 {
			continue
		}
		c.compared += len(on)
		// Everything on one line belongs on one sheet.
		for i := 1; i < len(on); i++ {
			if on[i].page != on[0].page {
				faults = append(faults, lineFault{form, "two members of one line are on different sheets",
					fmt.Sprintf("%s line %d: %s on sheet %d, %s on sheet %d",
						name, li, who[0].Path, on[0].page+1, who[i].Path, on[i].page+1)})
			}
		}
		// Across the page, in the direction the layout fills.
		back := lay == "rl-tb" || lay == "rl-row"
		for i := 1; i < len(on); i++ {
			a, b := on[i-1], on[i]
			if a.page != b.page {
				continue
			}
			lo, hi := a, b
			if back {
				lo, hi = b, a
			}
			if hi.x < lo.x {
				faults = append(faults, lineFault{form, "the members of a line are not across the page in document order",
					fmt.Sprintf("%s line %d (%s): %s at x=%g after %s at x=%g",
						name, li, lay, who[i].Path, float64(b.x), who[i-1].Path, float64(a.x))})
			} else if float64(lo.r-hi.x) > fitSlop {
				faults = append(faults, lineFault{form, "two members of a line overlap on the paper",
					fmt.Sprintf("%s line %d (%s): %s spans %g..%g, %s spans %g..%g",
						name, li, lay, who[i-1].Path, float64(a.x), float64(a.r),
						who[i].Path, float64(b.x), float64(b.r))})
			}
		}
		// Down the page: one line is one band, so every member begins level
		// with the topmost of them.
		top, bottom := on[0].y, on[0].b
		for _, s := range on[1:] {
			top = min(top, s.y)
			bottom = max(bottom, s.b)
		}
		for i, s := range on {
			if float64(s.y-top) > fitSlop && float64(s.b-bottom) > fitSlop {
				faults = append(faults, lineFault{form, "a member of a line is outside the line's vertical band",
					fmt.Sprintf("%s line %d: %s spans %g..%g, the line spans %g..%g",
						name, li, who[i].Path, float64(s.y), float64(s.b), float64(top), float64(bottom))})
			}
		}
	}
	return c, faults
}

// isHidden says the template asks for this occurrence not to be drawn, which
// is what makes a box take no room on the line it is on.
func isHidden(n *FormNode) bool { return hidden(n.Template) }

// A pdfiumBox is one content layout item pdfium placed, as
// /Users/Shared/pdfiumbuild's probe dumps them. That probe is a source file
// added to pdfium's own embedder tests, because nothing in public/ returns a
// layout item's geometry: it walks CXFA_LayoutProcessor's pages and writes
// every CXFA_ContentLayoutItem's GetAbsoluteRect().
type pdfiumBox struct {
	page       int
	kind, path string
	x, y, w, h float64
}

// readPdfium reads one form's dump.
func readPdfium(name string) (map[string][]pdfiumBox, int, error) {
	b, err := os.ReadFile(name)
	if err != nil {
		return nil, 0, err
	}
	out := map[string][]pdfiumBox{}
	pages := 0
	for _, line := range strings.Split(string(b), "\n") {
		f := strings.Split(line, "\t")
		if len(f) == 2 && f[0] == "#pages" {
			pages, _ = strconv.Atoi(f[1])
			continue
		}
		if len(f) != 9 || strings.HasPrefix(f[0], "#") {
			continue
		}
		var v pdfiumBox
		v.page, _ = strconv.Atoi(f[0])
		v.kind, v.path = f[2], somPath(f[4])
		v.x, _ = strconv.ParseFloat(f[5], 64)
		v.y, _ = strconv.ParseFloat(f[6], 64)
		v.w, _ = strconv.ParseFloat(f[7], 64)
		v.h, _ = strconv.ParseFloat(f[8], 64)
		out[v.kind+" "+v.path] = append(out[v.kind+" "+v.path], v)
	}
	return out, pages, nil
}

// somPath turns pdfium's SOM expression into the path this package writes.
//
// pdfium indexes every step and names the form tree from the root of the XFA
// model — "xfa[0].form[0].topmostSubform[0].Page1[0]" — where [FormNode.Path]
// begins at the outermost subform and indexes only the second and later of
// repeated siblings. A step pdfium spells with a leading "#" is an element the
// template did not name, which contributes nothing to [FormNode.Path] either.
// The two are the same walk written differently, so this is a rewrite and not
// a guess.
//
// A dot INSIDE a name is escaped, which is why the steps are cut by
// [somSteps] and not by strings.Split. 35 of the 559 dumped forms name an
// element that way — "ArtifactedHeader[0].A\.Original[0]" — and splitting on
// every dot put a backslash in the rewritten path, so the key matched nothing
// this package emits and 592 leaves were paired with nothing at all. The
// agreement rates were never wrong, both sides being unpaired, but they could
// not see those boxes.
func somPath(som string) string {
	steps := somSteps(som)
	var out []string
	for _, s := range steps {
		name, idx := s, 0
		if i := strings.IndexByte(s, '['); i >= 0 && strings.HasSuffix(s, "]") {
			name = s[:i]
			idx, _ = strconv.Atoi(s[i+1 : len(s)-1])
		}
		if name == "xfa" || name == "form" || strings.HasPrefix(name, "#") {
			continue
		}
		if idx > 0 {
			name = fmt.Sprintf("%s[%d]", name, idx)
		}
		out = append(out, name)
	}
	return strings.Join(out, ".")
}

// somSteps cuts a SOM expression at its UNESCAPED dots. pdfium writes a name
// holding a dot with a backslash before it (CXFA_Object::GetSOMExpression),
// and the backslash is not part of the name.
func somSteps(som string) []string {
	var out []string
	var cur strings.Builder
	for i := 0; i < len(som); i++ {
		switch {
		case som[i] == '\\' && i+1 < len(som):
			i++
			cur.WriteByte(som[i])
		case som[i] == '.':
			out = append(out, cur.String())
			cur.Reset()
		default:
			cur.WriteByte(som[i])
		}
	}
	return append(out, cur.String())
}

// TestIntraLineAgainstPdfium checks where a box went ACROSS a line against a
// reference that computes the answer.
//
//	XFACORPUS=/path/to/parts XFAPDFIUM=/path/to/dumps go test -run AgainstPdfium -v
//
// # Why this is the check that was missing
//
// pdf.js hands a line to CSS flexbox and emits no coordinate inside one, so
// every release since v0.5.0 has said intra-line placement is unjudged in both
// directions. pdfium is not a DOM emitter but a renderer, and computes the
// answer outright: CalculateRowChildPosition
// (cxfa_contentlayoutprocessor.cpp:2028-2160) walks a line assigning each item
// an x and accumulating its width, and CXFA_ContentLayoutItem::GetAbsoluteRect
// adds the offsets of the items above it.
//
// None of that is reachable from outside. No export of public/ returns a
// layout item's geometry — FPDFAnnot_GetFormFieldAtPoint reads the PDF's own
// AcroForm and not the XFA layout — and pdfium's suite asserts no coordinate
// anywhere in the tree: its one XFA layout test counts pages. So the dump
// comes from a source file added to pdfium's embedder tests, built with
// pdf_enable_xfa. The probe is nine lines of walk around GetAbsoluteRect.
//
// # What is compared, and what pairs
//
// LEAVES: fields and draws, which both sides emit a rectangle for. A
// container's own box is not compared — this package emits none — but a
// container placed at the wrong x puts every leaf under it at the wrong x, so
// the leaves carry the question.
//
// Boxes are paired by kind and by the element's SOM path, rewritten from
// pdfium's spelling by [somPath], and only where both sides produced exactly
// one box with that key. Where either produced several — an element on more
// than one sheet, most often a page area's furniture — the key is dropped
// rather than guessed at.
//
// # What it cannot settle
//
// pdfium runs the form's scripts, measures text with real fonts and has its
// own pagination; this package does none of the three. So a leaf under a
// container whose height came from measured text is at a different y and often
// on a different sheet, and a disagreement there is not about placement. X
// across the page is the quantity that survives: a positioned box's x is its
// written attribute and a line member's x is arithmetic over written widths.
// Hence the split below — under a line, under a row, and everywhere else,
// which is the control. A disagreement in all three is not the line's.
func TestIntraLineAgainstPdfium(t *testing.T) {
	dir, dumps := os.Getenv("XFACORPUS"), os.Getenv("XFAPDFIUM")
	if dir == "" || dumps == "" {
		t.Skip("no XFACORPUS or no XFAPDFIUM")
	}
	names, err := filepath.Glob(filepath.Join(dir, "*.template.xml"))
	if err != nil || len(names) == 0 {
		t.Skipf("no templates in %s", dir)
	}
	sort.Strings(names)

	// Three populations and the same three questions of each.
	type tally struct{ paired, sameX, sameY, sameW int }
	var line, row, rest tally
	// A disagreement under a line means nothing on a form where the control
	// disagrees too, so the two are kept apart from the start.
	var lineBadWhole, rowBadWhole, lineBadOwn, rowBadOwn, formsWhole int
	var forms, noDump int
	var oursOnly, theirsOnly, ambiguous int
	// The same two losses again for the boxes this test exists for, because a
	// population that quietly halved would make 100% mean nothing.
	var lineOursOnly, lineAmbiguous, rowOursOnly, rowAmbiguous int
	var own []string

	for _, name := range names {
		form := filepath.Base(strings.TrimSuffix(name, ".template.xml"))
		theirs, _, err := readPdfium(filepath.Join(dumps, form+".tsv"))
		if err != nil {
			noDump++
			continue
		}
		tmpl := readNode(t, name, true)
		if tmpl == nil {
			continue
		}
		stem := strings.TrimSuffix(name, ".template.xml")
		f := Expand(tmpl, readNode(t, stem+".datasets.xml", false))
		l, _ := placeForm(f, nil)
		forms++

		// Which line-forming container each element sits under, if any. It is
		// read off the tree, never off the coordinates.
		under := map[*FormNode]string{}
		f.Root.Walk(func(n *FormNode) {
			lay := layoutOf(n)
			what := ""
			switch lay {
			case "lr-tb", "rl-tb":
				what = "line"
			case "row", "rl-row":
				what = "row"
			default:
				return
			}
			var mark func(*FormNode)
			mark = func(k *FormNode) {
				if _, seen := under[k]; !seen {
					under[k] = what
				}
				for _, kid := range contained(k) {
					mark(kid)
				}
			}
			for _, kid := range contained(n) {
				mark(kid)
			}
		})

		ours := map[string][]Box{}
		for _, pg := range l.Pages {
			for _, b := range pg.Boxes {
				ours[b.Kind+" "+b.Path] = append(ours[b.Kind+" "+b.Path], b)
			}
		}
		type miss struct {
			key   string
			what  string
			mine  float64
			their float64
		}
		var missed []miss
		var ctrl, ctrlBad int
		for key, mine := range ours {
			what := under[mine[0].Node]
			them, ok := theirs[key]
			if !ok {
				oursOnly++
				switch what {
				case "line":
					lineOursOnly++
				case "row":
					rowOursOnly++
				}
				continue
			}
			if len(mine) != 1 || len(them) != 1 {
				ambiguous++
				switch what {
				case "line":
					lineAmbiguous++
				case "row":
					rowAmbiguous++
				}
				continue
			}
			to := &rest
			switch what {
			case "line":
				to = &line
			case "row":
				to = &row
			default:
				ctrl++
			}
			to.paired++
			bad := math.Abs(float64(mine[0].Rect.X)-them[0].x) > 0.01
			if !bad {
				to.sameX++
			} else {
				if what == "" {
					ctrlBad++
				} else {
					missed = append(missed, miss{key, what, float64(mine[0].Rect.X), them[0].x})
				}
			}
			if math.Abs(float64(mine[0].Rect.Y)-them[0].y) <= 0.01 {
				to.sameY++
			}
			if math.Abs(float64(mine[0].Rect.W)-them[0].w) <= 0.01 {
				to.sameW++
			}
		}
		for key, them := range theirs {
			if len(them) == 0 || them[0].kind != "field" && them[0].kind != "draw" {
				continue
			}
			if _, ok := ours[key]; !ok {
				theirsOnly++
			}
		}
		// The control on this form: every leaf under neither a line nor a
		// row. Where it disagrees, x on this form is not comparable at all
		// and a disagreement under a line here says nothing about lines.
		whole := ctrl > 0 && ctrlBad > 0
		if !whole {
			formsWhole++
		}
		for _, m := range missed {
			switch {
			case m.what == "line" && whole:
				lineBadWhole++
			case m.what == "line":
				lineBadOwn++
			case whole:
				rowBadWhole++
			default:
				rowBadOwn++
			}
			if !whole && len(own) < 20 {
				own = append(own, fmt.Sprintf("%s: %s under a %s: x=%g, pdfium %g",
					form, m.key, m.what, m.mine, m.their))
			}
		}
	}

	t.Logf("%d forms with a pdfium dump, %d without one", forms, noDump)
	t.Logf("%d of those forms agree with pdfium on x for EVERY leaf under neither a line nor a row, "+
		"which is the only ground a disagreement under one can be read from", formsWhole)
	t.Logf("unpaired: %d keys this package emits and pdfium does not, %d leaves pdfium emits and it "+
		"does not, %d where one side emitted the key more than once", oursOnly, theirsOnly, ambiguous)
	say := func(what string, v tally) {
		if v.paired == 0 {
			t.Logf("  %-28s nothing paired", what)
			return
		}
		t.Logf("  %-28s %6d paired   x %6d (%.2f%%)   y %6d (%.2f%%)   w %6d (%.2f%%)",
			what, v.paired,
			v.sameX, 100*float64(v.sameX)/float64(v.paired),
			v.sameY, 100*float64(v.sameY)/float64(v.paired),
			v.sameW, 100*float64(v.sameW)/float64(v.paired))
	}
	say("under a wrapping container", line)
	t.Logf("        of the rest under one: %d pdfium does not emit, %d one side emitted more than once",
		lineOursOnly, lineAmbiguous)
	say("under a table row", row)
	t.Logf("        of the rest under one: %d pdfium does not emit, %d one side emitted more than once",
		rowOursOnly, rowAmbiguous)
	say("under neither — the control", rest)
	t.Logf("x DISAGREEMENTS, split by whether the form's control agrees:")
	t.Logf("  under a wrapping container: %d on a form whose control also disagrees, %d on one whose does not",
		lineBadWhole, lineBadOwn)
	t.Logf("  under a table row:          %d on a form whose control also disagrees, %d on one whose does not",
		rowBadWhole, rowBadOwn)
	for _, w := range own {
		t.Logf("  %s", w)
	}
}

// A pdfiumSheet is one sheet as pdfium's probe reports it: the size of the
// paper, and the page area the sheet was opened on.
//
// The probe writes both from CXFA_LayoutProcessor::GetPage(i) — the size from
// GetPageSize, the page area's name from the view layout item's form node — so
// a sheet's page area is known even where that page area draws nothing at all.
// us-uscis__g-1055 has 91 such sheets, and reading the page area off the
// furniture on the paper would call every one of them unattributed.
type pdfiumSheet struct {
	w, h     float64
	pageArea string
}

// readPdfiumSheets reads the sheets out of one form's dump.
//
// The lines are "#page <i> <w> <h>" and "#pagearea <i> <class> <name>", which
// the probe writes before the boxes of that sheet.
func readPdfiumSheets(name string) ([]pdfiumSheet, error) {
	b, err := os.ReadFile(name)
	if err != nil {
		return nil, err
	}
	byIndex := map[int]pdfiumSheet{}
	most := -1
	for _, line := range strings.Split(string(b), "\n") {
		f := strings.Split(line, "\t")
		if len(f) != 4 {
			continue
		}
		i, err := strconv.Atoi(f[1])
		if err != nil || i < 0 {
			continue
		}
		s := byIndex[i]
		switch f[0] {
		case "#page":
			s.w, _ = strconv.ParseFloat(f[2], 64)
			s.h, _ = strconv.ParseFloat(f[3], 64)
		case "#pagearea":
			s.pageArea = f[3]
		default:
			continue
		}
		byIndex[i] = s
		if i > most {
			most = i
		}
	}
	out := make([]pdfiumSheet, most+1)
	for i := range out {
		out[i] = byIndex[i]
	}
	return out, nil
}

// TestSheetsAgainstPdfium checks the PAPER against pdfium: how big each sheet
// is, and which page area it was opened on.
//
//	XFACORPUS=/path/to/parts XFAPDFIUM=/path/to/dumps go test -run SheetsAgainstPdfium -v
//
// # Why this exists, and what it caught the day it was written
//
// Every other judge here compares a box: its x, and the sheet it landed on
// with its y. None of them compares the sheet ITSELF, and that is a whole
// class of error they cannot see. v0.18.0 found six forms laying every sheet
// but the first on a landscape page area 612 pt tall where the portrait one is
// 792, fixed it, moved 378 leaves onto correctly oriented sheets — and not one
// number in this file moved by a unit, because a leaf at the same (sheet, x,
// y) on a 612x792 sheet and on a 792x612 one counted as full agreement.
//
// # The size is the weak half of the question
//
// A sheet's size is a proxy for its page area and a poor one: 528 of the 559
// forms pdfium lays out give every page area of a form the same medium, so
// choosing the wrong one leaves the size right. Where the media DO differ the
// size sees it — that is what v0.18.0's six forms were — but that is 31 forms
// of 559.
//
// So this asks the question outright as well. Which page area a sheet was
// opened on is [placer.sheetAreas] here and GetPage(i)'s form node there, and
// neither is read off the geometry being judged.
//
// # What it cannot see
//
// Sheets are lined up by INDEX, and only as far as both sides have one. Nine
// forms disagree with pdfium on the NUMBER of sheets, which leaves 89 sheets
// compared against nothing: those are a pagination disagreement and this is
// not the instrument for one.
//
// A page area both sides agree on may still be a DIFFERENT OCCURRENCE of it —
// pdfium names the page area of the template, as this does, and a page set
// that runs twice reaches the same names again.
func TestSheetsAgainstPdfium(t *testing.T) {
	dir, dumps := os.Getenv("XFACORPUS"), os.Getenv("XFAPDFIUM")
	if dir == "" || dumps == "" {
		t.Skip("no XFACORPUS or no XFAPDFIUM")
	}
	names, err := filepath.Glob(filepath.Join(dir, "*.template.xml"))
	if err != nil || len(names) == 0 {
		t.Skipf("no templates in %s", dir)
	}
	sort.Strings(names)

	var forms, noDump int
	var sheets, sameSize, sameArea int
	var swapped int
	var countDiff, unpaired int
	var worst float64
	var sizeBad, areaBad []string

	for _, name := range names {
		form := filepath.Base(strings.TrimSuffix(name, ".template.xml"))
		theirs, err := readPdfiumSheets(filepath.Join(dumps, form+".tsv"))
		if err != nil {
			noDump++
			continue
		}
		tmpl := readNode(t, name, true)
		if tmpl == nil {
			continue
		}
		stem := strings.TrimSuffix(name, ".template.xml")
		f := Expand(tmpl, readNode(t, stem+".datasets.xml", false))
		l, p := placeForm(f, nil)
		forms++

		if len(l.Pages) != len(theirs) {
			countDiff++
			d := len(l.Pages) - len(theirs)
			if d < 0 {
				d = -d
			}
			unpaired += d
		}
		var badSize, badArea int
		var firstArea string
		for i := 0; i < len(l.Pages) && i < len(theirs); i++ {
			sheets++
			ours, them := l.Pages[i], theirs[i]
			// A tenth of a point. Both sides write four decimals of a float
			// arrived at differently, and seven forms of the corpus differ in
			// the fourth — 1008.0 against 1008.0001 — which is not a
			// disagreement about paper.
			dw := math.Abs(float64(ours.Width) - them.w)
			dh := math.Abs(float64(ours.Height) - them.h)
			if math.Max(dw, dh) < 0.1 {
				sameSize++
			} else {
				badSize++
				if math.Max(dw, dh) > worst {
					worst = math.Max(dw, dh)
				}
				if math.Abs(float64(ours.Width)-them.h) < 0.1 &&
					math.Abs(float64(ours.Height)-them.w) < 0.1 {
					swapped++
				}
			}
			if i < len(p.sheetAreas) && p.sheetAreas[i].Name == them.pageArea {
				sameArea++
			} else {
				badArea++
				if firstArea == "" && i < len(p.sheetAreas) {
					firstArea = fmt.Sprintf("sheet %d on %s where pdfium says %s",
						i, p.sheetAreas[i].Name, them.pageArea)
				}
			}
		}
		if badSize > 0 {
			sizeBad = append(sizeBad, fmt.Sprintf("%s: %d of %d sheets", form, badSize, len(l.Pages)))
		}
		if badArea > 0 {
			areaBad = append(areaBad, fmt.Sprintf("%s: %d of %d sheets, %s",
				form, badArea, len(l.Pages), firstArea))
		}
	}

	if sheets == 0 {
		t.Skip("no sheet of any form could be lined up against a dump")
	}
	t.Logf("%d forms judged, %d with no dump", forms, noDump)
	t.Logf("%d sheets lined up by index; %d forms disagree on the NUMBER of sheets, "+
		"leaving %d sheets compared against nothing", sheets, countDiff, unpaired)
	t.Logf("the same SIZE as pdfium: %d/%d (%.2f%%), of the rest %d have the two dimensions swapped, "+
		"and the largest disagreement in either is %.4f pt",
		sameSize, sheets, 100*float64(sameSize)/float64(sheets), swapped, worst)
	t.Logf("opened on the same PAGE AREA: %d/%d (%.2f%%)",
		sameArea, sheets, 100*float64(sameArea)/float64(sheets))
	for _, s := range sizeBad {
		t.Logf("  size:      %s", s)
	}
	for _, s := range areaBad {
		t.Logf("  pagearea:  %s", s)
	}
}
