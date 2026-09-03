// Copyright (c) 2026, the go-pdfkit/xfa authors
// All rights reserved.
//
// SPDX-License-Identifier: BSD-3-Clause

package xfa

import "strings"

// A pager is the sequence of sheets a form asks for.
//
// It is a state machine rather than an overflow test, which is the thing about
// XFA pagination that has to be got right first: which page comes next is not
// "the same one again". It depends on where in the document one is, on whether
// the page number is odd or even, and on the explicit breaks the template
// writes. pdf.js keeps that state on the template root — pageNumber,
// currentPageArea, currentContentArea (template.js:5418-5433) — and asks the
// page set for the next page at the bottom of its loop (:5656). This is the
// same machine with the same state, driven the same way.
//
// # Where it deliberately differs, and why it has to
//
// pdf.js exhausts its stack on seven of the five hundred and sixty forms in
// the corpus, recursing between PageSet[$getNextPage] (template.js:4176-4236)
// and PageArea[$getNextPage] (:4061-4075). The last branch of the page set is
//
//	this[$cleanPage]();
//	return this[$getNextPage]();
//
// and $cleanPage (:4160-4167) deletes the [$extra] of every page area and page
// set BELOW it and not its own — so pageIndex and pageSetIndex are still at
// the end of their lists, every test fails again, and the call repeats for
// ever on unchanged state. It is a real defect on real files.
//
// The defect is not in that one branch. The branch above it does the same
// thing — a page set whose <occur> still allows another run resets its own
// indices and calls itself, and comes back to the same place when every page
// area below it has spent its own <occur>. That is the shape a faithful port
// has to guard: EVERY way a page set can start itself again, not the one that
// happens to be named after cleaning. So a page set may restart at most once
// per request for a page, and a page set holding neither a page area nor a
// page set says so rather than looping. Both were found by porting the
// machine and running it, not by reading it.
type pager struct {
	// areas is the page set the form starts from: the outermost subform's
	// first, which is the one pdf.js reads (template.js:5443).
	top *FormNode
	// within maps a page area or a nested page set to the page set holding
	// it, which is how $getParent is reached without a parent pointer.
	within map[*FormNode]*FormNode
	// sets is the [$extra] of each page set, and used the numberOfUse of each
	// page area.
	sets map[*FormNode]*setState
	used map[*FormNode]int
	// number is pdf.js's [$extra].pageNumber, which the parity of a duplex or
	// simplex page set is read from (template.js:4207-4209).
	number int
	// restarted guards the one branch that can return to unchanged state, so
	// that a page set which exhausts itself twice in the same call says there
	// is no next page instead of recursing.
	restarted map[*FormNode]bool
}

// A setState is what a page set remembers between pages: how many times it has
// been run through, and how far along its page areas and its nested page sets
// it has got.
type setState struct {
	numberOfUse             int
	pageIndex, pageSetIndex int
}

// newPager reads the page sets under a form's outermost subform.
func newPager(root *FormNode) *pager {
	p := &pager{
		within:    map[*FormNode]*FormNode{},
		sets:      map[*FormNode]*setState{},
		used:      map[*FormNode]int{},
		number:    1,
		restarted: map[*FormNode]bool{},
	}
	p.top = firstOfKind(root, "pageSet")
	if p.top != nil {
		p.index(p.top)
	}
	return p
}

// index records which page set each page area and nested page set sits in.
func (p *pager) index(set *FormNode) {
	for _, k := range set.Kids {
		switch k.Kind {
		case "pageArea":
			p.within[k] = set
		case "pageSet":
			p.within[k] = set
			p.index(k)
		}
	}
}

// state is a page set's [$extra], created on first use as pdf.js creates it
// (template.js:4177-4181).
func (p *pager) state(set *FormNode) *setState {
	st, ok := p.sets[set]
	if !ok {
		st = &setState{numberOfUse: 1, pageIndex: -1, pageSetIndex: -1}
		p.sets[set] = st
	}
	return st
}

// first is the page the form starts on, and the state that goes with it.
//
// pdf.js takes pageAreas[0] unless a break before the outermost subform names
// another (template.js:5446-5488). The break is consumed there — it does not
// fire again when the subform is laid out — but only when its target really
// resolves to a page area.
func (p *pager) first(root *FormNode) (*FormNode, *FormNode) {
	if p.top == nil {
		return nil, nil
	}
	areas := kidsOfKind(p.top, "pageArea")
	if len(areas) == 0 {
		return nil, nil
	}
	area, consumed := areas[0], (*FormNode)(nil)
	if br, target := openingBreak(root); br != nil {
		if t, idx := p.resolve(root, target); t != nil && t.Kind == "pageArea" && idx < 0 {
			area, consumed = t, br
		}
	}
	p.used[area] = 1
	set := p.within[area]
	st := &setState{numberOfUse: 1, pageSetIndex: 0}
	for i, a := range kidsOfKind(set, "pageArea") {
		if a == area {
			st.pageIndex = i
		}
	}
	p.sets[set] = st
	return area, consumed
}

// openingBreak is the break pdf.js looks for before it has chosen a page: one
// on the outermost subform, or on the first subform inside it, written either
// as a <breakBefore> or as the deprecated <break before=...>
// (template.js:5446-5465). It returns the element and the target it names.
func openingBreak(root *FormNode) (*FormNode, string) {
	for _, n := range []*FormNode{root, firstOfKind(root, "subform")} {
		if n == nil {
			continue
		}
		if b := n.Template.Child("breakBefore"); b != nil {
			return n, b.Get("target")
		}
		if b := n.Template.Child("break"); b != nil && b.Get("beforeTarget") != "" {
			return n, b.Get("beforeTarget")
		}
	}
	return nil, ""
}

// next is the page area to use after this one.
//
// It is PageArea[$getNextPage] (template.js:4064-4075): a page set that runs
// its pages in order hands the same page area back for as long as its <occur>
// allows, and otherwise asks the page set for the next one.
func (p *pager) next(from *FormNode) *FormNode {
	p.restarted = map[*FormNode]bool{}
	return p.afterArea(from)
}

func (p *pager) afterArea(from *FormNode) *FormNode {
	set := p.within[from]
	if set == nil {
		return nil
	}
	if ordered(set) && p.areaUsable(from) {
		p.used[from]++
		return from
	}
	return p.afterSet(set)
}

// afterSet is PageSet[$getNextPage] (template.js:4176-4236).
func (p *pager) afterSet(set *FormNode) *FormNode {
	st := p.state(set)
	if !ordered(set) {
		return p.byParity(set)
	}
	areas := kidsOfKind(set, "pageArea")
	if st.pageIndex+1 < len(areas) {
		st.pageIndex++
		return p.afterArea(areas[st.pageIndex])
	}
	nested := kidsOfKind(set, "pageSet")
	if st.pageSetIndex+1 < len(nested) {
		st.pageSetIndex++
		return p.afterSet(nested[st.pageSetIndex])
	}
	if len(areas) == 0 && len(nested) == 0 {
		// It can never yield a page, so saying "start again" would be a loop
		// rather than an answer.
		return nil
	}
	// Everything below has been offered and refused. What is left is to start
	// the page set again — which is the ONE thing that can come back here on
	// state no different from the state it left, and so the one thing that has
	// to be done at most once. See the note on [pager]: pdf.js does it without
	// a guard and exhausts its stack on seven forms of the corpus.
	if p.restarted[set] {
		return nil
	}
	p.restarted[set] = true
	if p.setUsable(set) {
		st.numberOfUse++
		st.pageIndex, st.pageSetIndex = -1, -1
		return p.afterSet(set)
	}
	if parent := p.within[set]; parent != nil {
		return p.afterSet(parent)
	}
	p.clean(set)
	return p.afterSet(set)
}

// clean forgets how much of a page set has been used, so that the sequence
// begins again. It is pdf.js's $cleanPage (template.js:4160-4167) with the
// page set's own state reset as well as its children's.
func (p *pager) clean(set *FormNode) {
	delete(p.sets, set)
	for _, k := range set.Kids {
		switch k.Kind {
		case "pageArea":
			delete(p.used, k)
		case "pageSet":
			p.clean(k)
		}
	}
}

// byParity picks a page area of a duplex or simplex page set by the parity and
// the position of the page number (template.js:4206-4235).
//
// pdf.js counts position from a page number that its own loop has already
// incremented past nought, so "first" never matches and every page takes the
// "rest" arm. That is faithfully reproduced: it is what the reference does,
// and a form laid out against it has been designed around it.
func (p *pager) byParity(set *FormNode) *FormNode {
	areas := kidsOfKind(set, "pageArea")
	if len(areas) == 0 {
		return nil
	}
	// The position is always "rest". pdf.js reads it as pageNumber === 0 ?
	// "first" : "rest" (template.js:4209) from a page number its own loop sets
	// to one before the first sheet and increments before ever asking for a
	// next page — so "first" is a branch of the reference that cannot be
	// reached, and a branch here that could not be reached either would be a
	// claim nothing tests.
	parity := "odd"
	if p.number%2 == 0 {
		parity = "even"
	}
	for _, want := range [][2]string{
		{parity, "rest"}, {"any", "rest"}, {"any", "any"},
	} {
		for _, a := range areas {
			if oddOrEven(a) == want[0] && pagePosition(a) == want[1] {
				return a
			}
		}
	}
	return areas[0]
}

// areaUsable is PageArea[$isUsable] (template.js:4043-4054): a page area with
// no <occur> may be used for ever, and one with a max may be used that many
// times.
func (p *pager) areaUsable(a *FormNode) bool {
	n, ok := p.used[a]
	if !ok {
		p.used[a] = 0
		return true
	}
	max, has := occurMax(a)
	return !has || max == -1 || n < max
}

// setUsable is PageSet[$isUsable] (template.js:4169-4175).
func (p *pager) setUsable(s *FormNode) bool {
	max, has := occurMax(s)
	return !has || max == -1 || p.state(s).numberOfUse < max
}

// occurMax reads how many times a page area or a page set may be used.
//
// pdf.js resolves the defaults in Occur[$clean] (template.js:3918-3934), and
// they are not the ones a field's <occur> takes: under a page area or a page
// set min defaults to nought and max to unbounded, and a written min with no
// max pins max to it.
func occurMax(n *FormNode) (int, bool) {
	o := n.Template.Child("occur")
	if o == nil {
		return 0, false
	}
	if s := o.Get("max"); s != "" {
		return wholeOr(s, -1), true
	}
	if s := o.Get("min"); s != "" {
		return wholeOr(s, 0), true
	}
	return -1, true
}

// ordered says a page set runs its pages one after another, which is the
// default: getStringOption's option list begins with "orderedOccurrence"
// (template.js:4144-4148) and an absent or unrecognised value yields the first
// entry.
func ordered(set *FormNode) bool {
	switch set.Template.Get("relation") {
	case "duplexPaginated", "simplexPaginated":
		return false
	default:
		return true
	}
}

// oddOrEven and pagePosition are read the same way, with "any" and "any"
// leading their option lists (template.js:3990-4005).
func oddOrEven(a *FormNode) string {
	switch v := a.Template.Get("oddOrEven"); v {
	case "even", "odd":
		return v
	default:
		return "any"
	}
}

func pagePosition(a *FormNode) string {
	switch v := a.Template.Get("pagePosition"); v {
	case "first", "last", "rest":
		return v
	default:
		return "any"
	}
}

// kidsOfKind are a node's children of one kind, in order.
func kidsOfKind(n *FormNode, kind string) []*FormNode {
	var out []*FormNode
	for _, k := range n.Kids {
		if k.Kind == kind {
			out = append(out, k)
		}
	}
	return out
}

// resolve finds what a break's target names, among the page areas and the
// content areas of the form. The second return is which content area of it was
// named, or -1 for the page area itself.
//
// A target is written in SOM, which this package parses for data references
// (som.go). Reaching the template rather than the data needs much less: every
// target in the corpus is an id, a name, or a dotted path of names ending in a
// page area or a content area. "#name" is an id and "#contentArea" is the
// content area of whatever came before it, which is how a template writes "the
// body of this page".
//
// An unresolved target is not an error and not a guess: pdf.js returns false
// from handleBreak (template.js:342-347), so the break simply does not fire.
func (p *pager) resolve(root *FormNode, target string) (*FormNode, int) {
	if target == "" || p.top == nil {
		return nil, -1
	}
	if strings.HasPrefix(target, "#") && target != "#contentArea" {
		return byID(root, strings.TrimPrefix(target, "#")), -1
	}
	at := p.top
	for _, step := range strings.Split(target, ".") {
		if at == nil || step == "" {
			return nil, -1
		}
		if step == "#contentArea" || strings.HasPrefix(step, "#") {
			// Only a content area can be reached this way: it is the one
			// element of a page area that layout needs and that the expanded
			// form does not carry, because nothing is laid out IN a content
			// area that is not laid out in its page area.
			if step != "#contentArea" || at.Kind != "pageArea" {
				return nil, -1
			}
			return at, 0
		}
		if next, idx := namedKid(at, step); idx >= 0 {
			return next, idx
		} else if next != nil {
			at = next
		} else {
			return nil, -1
		}
	}
	return at, -1
}

// byID is the element carrying that id, anywhere in the form.
func byID(root *FormNode, id string) *FormNode {
	var found *FormNode
	root.Walk(func(k *FormNode) {
		if found == nil && k.Template.Get("id") == id {
			found = k
		}
	})
	return found
}

// namedKid is the child of that name. Where the name is a content area's, it
// answers the page area holding it and which of its content areas it is.
func namedKid(n *FormNode, name string) (*FormNode, int) {
	for _, k := range n.Kids {
		if k.Name == name {
			return k, -1
		}
	}
	if n.Kind == "pageArea" {
		for i, k := range n.Template.Children("contentArea") {
			if k.Get("name") == name {
				return n, i
			}
		}
	}
	return nil, -1
}

// A breakSpec is a <breakBefore> or a <breakAfter>, or the deprecated <break>
// written as one.
//
// pdf.js does the same rewriting at the top of Subform[$toHTML]
// (template.js:4980-5017): a <break> becomes a BreakBefore and a BreakAfter,
// APPENDED to the arrays, so a subform carrying both keeps its own element as
// the one that fires. <break before="pageEven"> and "pageOdd" have no place in
// the option list a breakBefore's targetType is read from, so they become
// "auto" and do nothing — which is pdf.js's own answer for them, and this
// package's, rather than a rule invented here.
type breakSpec struct {
	targetType string
	target     string
	startNew   bool
}

// breakBeforeOf and breakAfterOf are the break that fires as a container is
// reached, and the one that fires once it has been laid out.
func breakBeforeOf(n *FormNode) (breakSpec, bool) {
	if b := n.Template.Child("breakBefore"); b != nil {
		return breakSpec{targetTypeOf(b.Get("targetType")), b.Get("target"), b.Get("startNew") == "1"}, true
	}
	if b := n.Template.Child("break"); b != nil {
		if b.Get("before") != "" || b.Get("beforeTarget") != "" {
			return breakSpec{targetTypeOf(b.Get("before")), b.Get("beforeTarget"), b.Get("startNew") == "1"}, true
		}
	}
	return breakSpec{}, false
}

func breakAfterOf(n *FormNode) (breakSpec, bool) {
	if b := n.Template.Child("breakAfter"); b != nil {
		return breakSpec{targetTypeOf(b.Get("targetType")), b.Get("target"), b.Get("startNew") == "1"}, true
	}
	if b := n.Template.Child("break"); b != nil {
		if b.Get("after") != "" || b.Get("afterTarget") != "" {
			return breakSpec{targetTypeOf(b.Get("after")), b.Get("afterTarget"), b.Get("startNew") == "1"}, true
		}
	}
	return breakSpec{}, false
}

// targetTypeOf reads a break's targetType. Its option list is "auto",
// "contentArea", "pageArea" (template.js:1046-1050), and anything else — a
// <break before="pageOdd"> among them — is the first entry.
func targetTypeOf(v string) string {
	switch v {
	case "contentArea", "pageArea":
		return v
	default:
		return "auto"
	}
}

// A breakTo says where a break sends the layout: which page area, which of its
// content areas, and whether a fresh sheet is started whatever.
type breakTo struct {
	area  *FormNode
	index int
	page  bool
}

// fire works out what a break does from where the layout is, and says whether
// it does anything at all.
//
// This is pdf.js's handleBreak (template.js:328-400) with its two arms kept
// apart. The thing worth noticing in it is how little a break usually does: a
// <breakBefore targetType="pageArea"> with no target and no startNew is a
// no-op, and Adobe's designer writes thousands of them. What is NOT a no-op is
// startNew="1", which is two thousand of the two and a half thousand in the
// corpus: it starts a fresh sheet of the page area in hand, exactly once.
func (p *pager) fire(root *FormNode, b breakSpec, cur *FormNode, slot int) (breakTo, bool) {
	if b.targetType == "auto" {
		return breakTo{}, false
	}
	var target *FormNode
	idx := -1
	if b.target != "" {
		if target, idx = p.resolve(root, b.target); target == nil {
			return breakTo{}, false
		}
	}
	if b.targetType == "pageArea" {
		if target == nil || target.Kind != "pageArea" || idx >= 0 {
			target = nil
		}
		switch {
		case b.startNew:
			if target == nil {
				target = cur
			}
			return breakTo{area: target, page: true}, true
		case target != nil && target != cur:
			return breakTo{area: target, page: true}, true
		}
		return breakTo{}, false
	}
	// A content area, named as "PageArea.ContentArea" or "PageArea.#contentArea".
	if idx < 0 {
		target = nil
	}
	switch {
	case b.startNew && target == nil:
		// "Start the next container": the content area after this one, which
		// is the next sheet where a page area holds only one.
		return breakTo{index: slot + 1}, true
	case b.startNew:
		if target == cur && slot < idx {
			return breakTo{index: idx}, true
		}
		return breakTo{area: target, index: idx, page: true}, true
	case target != nil && !(target == cur && idx == slot):
		if target == cur {
			return breakTo{index: idx}, true
		}
		return breakTo{area: target, index: idx, page: true}, true
	}
	return breakTo{}, false
}

// use records that a break's target page area is being gone to, and says
// whether it may be: a page area whose <occur> is spent is not reached by
// naming it (template.js:5648-5654).
func (p *pager) use(a *FormNode) bool {
	if !p.areaUsable(a) {
		return false
	}
	p.used[a]++
	return true
}
