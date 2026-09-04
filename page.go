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
// and PageArea[$getNextPage] (:4061-4075): ca-cra__rc1-fill-11-25e,
// ca-cra__t2042-fill-24e, ca-cra__t2042-fill-25e, us-ssa__ha-4631,
// us-ssa__ssa-372, us-ssa__ssa-5062 and us-ssa__ssa-766. Every one of them is
// a page set with no <occur> over page areas that write max="1".
//
// The branch that restarts a usable page set (:4222-4227) resets its own
// indices and calls itself, and comes back to the same place when every page
// area below it has spent its own <occur> — because it does NOT clean them on
// the way. The branch below it, which does clean them (:4230-4231), is never
// reached, since a page set with no <occur> is usable for ever. So neither
// branch can make progress and the stack goes.
//
// Guarding the recursion is not the same thing as answering it, and slice 3
// did the first. What restarting a page set MEANS is that its page areas are
// offered again — see [pager.cleanKids], where both references say so — and
// once the restart cleans them it terminates because it has a page to give,
// rather than because a counter stopped it. The guard stays for the one shape
// that can still come back on unchanged state: a page set holding only nested
// page sets that yield nothing. And a page set holding neither a page area nor
// a page set says there is no next page rather than looping.
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
	// Every page set under the outermost subform is indexed, not only the one
	// the form starts from. pdf.js walks real parent pointers, so a break that
	// names a page area of a second page set by its id lands somewhere it can
	// still ask for a next page; here the parents have to be recorded first.
	for _, k := range root.Kids {
		if k.Kind == "pageSet" {
			if p.top == nil {
				p.top = k
			}
			p.index(k)
		}
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
		// The sequence must hold it: a page area named by id can be anywhere
		// in the form, and one that is not under a page set has no page set to
		// ask for the sheet after it.
		if t, idx := p.resolve(root, target); t != nil && t.Kind == "pageArea" && idx < 0 && p.within[t] != nil {
			area, consumed = t, br
		}
	}
	p.used[area] = 1
	set := p.within[area]
	// pdf.js writes pageSetIndex: 0 here (template.js:5487) and pageIndex at
	// the page area it chose. The page index is right; the page SET index is
	// not, and it says a nested page set has been offered when nothing has.
	// The sheet in hand came from a page area of this set, so none of its
	// nested sets has been reached, and -1 is where "none of them" is written
	// everywhere else in this machine.
	//
	// pdfium settles it. Once a page area is spent it looks at the siblings
	// AFTER it inside its own page set — FindPageAreaFromPageSet(parent,
	// cur_page_area_, ...) in GetNextAvailPageArea
	// (cxfa_viewlayoutprocessor.cpp:1444-1447) — and
	// FindPageAreaFromPageSet_Ordered walks those siblings in document order,
	// descending into a nested page set as it meets one (:1249-1258). So the
	// first nested set is offered before the enclosing set starts over, and
	// with pageSetIndex at 0 it would be skipped until after a restart.
	//
	// It changed no answer while a restart did not clean the page areas below
	// it, because the restart offered the nested set on its second pass. It
	// does now. No form of the corpus nests a page set — 560 forms, 560 page
	// sets — so this is measured by [TestTheSequenceDescendsIntoANestedPageSet]
	// and by nothing else.
	st := &setState{numberOfUse: 1, pageSetIndex: -1}
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
	// the page set again — and a page set that starts again offers its page
	// areas again, which is why cleaning them is part of the restart and not
	// of some later branch. See [pager.cleanKids].
	//
	// The guard stays. It is the one thing that can come back here on state no
	// different from the state it left — a page set holding only empty nested
	// sets restarts, offers them, and is asked again — so it is done at most
	// once for one request. See the note on [pager].
	if p.restarted[set] {
		return nil
	}
	p.restarted[set] = true
	if p.setUsable(set) {
		st.numberOfUse++
		st.pageIndex, st.pageSetIndex = -1, -1
		p.cleanKids(set)
		return p.afterSet(set)
	}
	if parent := p.within[set]; parent != nil {
		return p.afterSet(parent)
	}
	// A page set whose own <occur> is spent, with no page set above it to ask,
	// is the end of the form. pdf.js's answer here is its second infinite
	// loop: it cleans the page areas below and calls itself
	// (template.js:4230-4231), but $cleanPage leaves the set's own [$extra]
	// alone, so pageIndex is still at the end of the list and $isUsable is
	// still false, and the call arrives back at this line unchanged.
	//
	// pdfium answers it. GetNextAvailPageArea walks up from the current page
	// area's page set and stops at the root — if (pPageSet == page_set_node_)
	// break — and returns nullptr (cxfa_viewlayoutprocessor.cpp:1450-1470),
	// with FindPageAreaFromPageSet_Ordered having already refused the set on
	// iMax <= iPageSetCount (:1196-1215). So a spent page set really is the
	// end, and this is the one branch that still says [noNextPage].
	//
	// It is the branch that keeps the SET's <occur> meaning something: were it
	// to start the set over regardless, no page set could ever be bounded. One
	// page set of the corpus writes an <occur> at all, and it writes no max.
	return nil
}

// cleanKids forgets how many times each page area BELOW a page set has been
// used, leaving the page sets' own counts alone. It is exactly pdf.js's
// $cleanPage (template.js:4160-4167): PageSet[$cleanPage] recurses into its
// page areas and its nested page sets, PageArea[$cleanPage] deletes that page
// area's [$extra] (:4058-4060), and no page set deletes its own — which is
// what makes a set's <occur> a bound on the whole run while a page area's is
// not.
//
// # This is what a page area's <occur max> bounds, and it is not the form
//
// A page area's max caps how many sheets it makes IN ONE RUN of the page set
// holding it. Starting the set again starts them again. pdfium says so
// outright: FindPageAreaFromPageSet_Ordered walks the set from its first child
// and assigns cur_page_count_ = 1 on the page area it settles on
// (cxfa_viewlayoutprocessor.cpp:1240-1244) — the count GetNextAvailPageArea
// tests the max against (:1414-1425) — while the guard on repeating the whole
// set reads the SET's own max against page_set_map_ (:1196-1215), which
// nothing resets. So an uncapped page set holding capped page areas yields
// sheets for ever, and a capped one stops.
//
// pdf.js means the same thing and cannot reach it. Its $cleanPage is in the
// LAST branch of PageSet[$getNextPage] (template.js:4230-4231), below the
// branch that restarts a usable set (:4222-4227) — and a set with no <occur>
// is usable for ever ($isUsable, :4169-4174, whose first clause is !this.occur).
// So the restart fires, offers a page area whose own max is spent, is handed
// back the same question, and recurses until the stack is gone. That is not a
// corner: 559 of the 560 forms in the corpus write a page set with no <occur>
// at all.
//
// It is the recursion this package guarded in slice 3 rather than answered,
// and the guard was the right shape and the wrong result: it terminated by
// saying the form had run out of pages. Seven forms — ca-cra__rc1-fill-11-25e,
// ca-cra__t2042-fill-24e, ca-cra__t2042-fill-25e, us-ssa__ha-4631,
// us-ssa__ssa-372, us-ssa__ssa-5062 and us-ssa__ssa-766 — are the ones pdf.js
// dies on, and every one of them is a page set with no <occur> over page areas
// that write max="1". They terminate here because the restart now offers a
// page area that can be used, not because a counter stopped it.
func (p *pager) cleanKids(set *FormNode) {
	for _, k := range set.Kids {
		switch k.Kind {
		case "pageArea":
			delete(p.used, k)
		case "pageSet":
			p.cleanKids(k)
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

// occurMax reads how many times a page area or a page set may be used, and
// whether anything bounds it at all.
//
// An <occur> that writes no max bounds nothing, and a written min does not
// stand in for one. That is not what Occur[$clean] reads like — it says a
// written min with no max pins the max to the min (template.js:3925-3932) —
// but that branch cannot fire on an attribute nobody wrote. The constructor
// tests attributes.max !== "" (:3896-3903), and a MISSING attribute is
// undefined rather than the empty string: _mkAttributes builds the object from
// the attributes the parser actually saw (parser.js:79-111). So getInteger's
// default of -1 is taken in the constructor, $clean's this.max === "" is
// already false, and only an attribute written as max="" ever reaches the
// branch. No page area or page set of the corpus writes one.
//
// It is not read this way because pdf.js arrives there by an accident of its
// own. pdfium asks the same question with the default suppressed —
// TryInteger(XFA_Attribute::Max, /*bUseDefault=*/false), in
// GetNextAvailPageArea (cxfa_viewlayoutprocessor.cpp:1414-1423) and in
// FindPageAreaFromPageSet_Ordered (:1202-1215) — and takes -1 where the
// attribute is absent. The two references agree on the number, by different
// routes, which is why it is the number rather than one reference's quirk.
//
// It decides one form outright. us-ssa__ssa-3371-bk writes <occur min="1"/> on
// its only page area; read as a max of one it gives a single sheet, and
// pdf.js's own dump for it is NINE.
func occurMax(n *FormNode) (int, bool) {
	o := n.Template.Child("occur")
	if o == nil {
		return 0, false
	}
	s := o.Get("max")
	if s == "" {
		return 0, false
	}
	return wholeOr(s, -1), true
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
