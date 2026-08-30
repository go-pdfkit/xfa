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
