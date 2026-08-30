// Copyright (c) 2026, the go-pdfkit/xfa authors
// All rights reserved.
//
// SPDX-License-Identifier: BSD-3-Clause

package xfa

import (
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
