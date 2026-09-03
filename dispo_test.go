package xfa

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

func TestDispo(t *testing.T) {
	dir := os.Getenv("XFACORPUS")
	out := os.Getenv("XFADISPO")
	if dir == "" || out == "" {
		t.Skip("no")
	}
	names, _ := filepath.Glob(filepath.Join(dir, "*.template.xml"))
	sort.Strings(names)
	all := map[string]map[string]string{}
	for _, name := range names {
		tmpl := readNode(t, name, true)
		if tmpl == nil {
			continue
		}
		stem := strings.TrimSuffix(name, ".template.xml")
		form := Expand(tmpl, readNode(t, stem+".datasets.xml", false))
		l := Place(form)
		m := map[string]string{}
		n := 0
		form.Root.Walk(func(k *FormNode) {
			if k.Kind == "field" || k.Kind == "draw" {
				n++
			}
		})
		_ = n
		for _, p := range l.Pages {
			for _, b := range p.Boxes {
				if b.Kind == "field" {
					m[b.Path] = "PLACED"
				}
			}
		}
		for _, u := range l.Unplaced {
			if u.Kind == "field" {
				if _, ok := m[u.Path]; !ok {
					m[u.Path] = u.Why
				}
			}
		}
		all[filepath.Base(stem)] = m
	}
	f, _ := os.Create(out)
	json.NewEncoder(f).Encode(all)
	f.Close()
}
