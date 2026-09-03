package xfa

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

type dumpBox struct {
	Path string  `json:"path"`
	Name string  `json:"name"`
	Kind string  `json:"kind"`
	X    float64 `json:"x"`
	Y    float64 `json:"y"`
	W    float64 `json:"w"`
	H    float64 `json:"h"`
}

func TestDumpAll(t *testing.T) {
	dir := os.Getenv("XFACORPUS")
	names, _ := filepath.Glob(filepath.Join(dir, "*.template.xml"))
	sort.Strings(names)
	for _, name := range names {
		tmpl := readNode(t, name, true)
		stem := strings.TrimSuffix(name, ".template.xml")
		form := Expand(tmpl, readNode(t, stem+".datasets.xml", false))
		l := Place(form)
		out := []dumpBox{}
		for _, p := range l.Pages {
			for _, b := range p.Boxes {
				out = append(out, dumpBox{b.Path, b.Node.Name, b.Kind,
					float64(b.X), float64(b.Y), float64(b.W), float64(b.H)})
			}
		}
		j, _ := json.Marshal(out)
		os.WriteFile("/Users/Shared/xfaslice/mine/"+filepath.Base(stem)+".json", j, 0o644)
	}
}
