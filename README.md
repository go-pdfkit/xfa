# xfa — go-pdfkit

[![License](https://img.shields.io/badge/license-BSD--3--Clause-blue)](LICENSE)

**Read the form inside a PDF whose pages are blank.** Pure Go, no cgo.

A PDF form comes in two kinds and only one of them is a PDF form. Of 2 240 real
government forms, 560 carry an XFA package — and **546 of those are static**: a
second, proprietary description of a form already drawn on the pages, which
[`go-pdfkit/forms`](https://github.com/go-pdfkit/forms) reads and fills without
help.

The other **fourteen** are dynamic. Their pages hold a panel reading *"Please
wait... your PDF viewer may not be able to display this type of document"*, and
the form exists only as XML. Adobe removed the format from PDF 2.0; no browser
and no other reader lays one out. Somebody handed such a file has a document
that looks blank and is not.

## What it does not do, and why that is a decision

It does not run the forms' scripts. That is measured, not assumed: of **5 744**
scripts across those fourteen forms, **5 131** are handlers for `enter`, `exit`,
`change` and `click` — they fire when somebody types. Only **188** run at load,
so the form a reader first sees does not depend on them.

FormCalc, XFA's other scripting language, is not read either: every script in
the corpus declares itself as JavaScript. The reference implementation
(pdf.js) spends 37 KB on FormCalc that this does not need.

## Joining a field to its value

`Bind` takes a parsed template and parsed datasets and hands back each field
with the value bound to it — name, path, data path, value — so a caller can
fill in a form without knowing XFA.

That is not name-matching. A form binds explicitly through `<bind>` elements,
and where it does not, each container consumes the next unclaimed data node of
its own name in document order. The two trees are named differently on purpose:
the template names a field for the layout, the data names it for the record. In
one real form the field at `FormulaireGf.Page1.MEP.NumeroCerfa` takes its value
from the **attribute** at
`FormulaireGf.MetierGf.DemandePensionAscendants/@Cerfa`.

Measured over all **560** XFA packages in the corpus:

| | |
|---|---:|
| templates read | 560 |
| `<bind>` elements | 26 573 |
| — `match="none"` | 11 875 |
| — `match="global"` | 11 544 |
| — no `match` (means `once`) | 2 589 |
| — `match="dataRef"`, every one with a `ref` | 565 |
| fields placed | 80 482 |
| fields bound to a data node | 60 865 |
| **`<bind>` expressions holding a construct this does not read** | **0** |

Every `ref` in the corpus is a plain path, a `$record` path, a `$` path, or one
of those with `[*]`. None of SOM's hard parts — FormCalc subexpressions
`.[…]`, JavaScript predicates `.(…)`, negative indices — occurs even once.

### Where this deliberately differs from pdf.js

The reference implementation is Mozilla's `src/core/xfa/bind.js`, and this
follows it closely. Four differences, each deliberate:

- **It invents no data nodes.** pdf.js, meeting a `ref` that matches nothing,
  creates the nodes the expression describes so a person can type into them.
  This reads documents rather than editing them, so the field is reported
  unbound — the same answer, since an invented node is empty.
- **It does not repeat a container to satisfy `occur`'s `initial`.** Those
  clones carry no data; they change the layout, not the values.
- **It descends into `pageSet` and `pageArea`.** pdf.js binds nothing there.
  Of 82 386 template fields in the corpus, **628** sit under a page area across
  177 forms — and **one form has no fields anywhere else**, so dropping them
  silently would lose it whole.
- **It stops a global search that has begun to repeat.** pdf.js's global lookup
  does not skip what it has already handed back, so `match="global"` under an
  unbounded `occur` asks for the same node for ever.

It is also linear where pdf.js is quadratic: a search for a name under a data
node resumes where the last one stopped, rather than beginning again.

## Where a field goes on the page

`Expand` joins the two trees and returns the form as a document holds it — one
node per occurrence, so a table row written once and filled three times is
three nodes. Layout runs over that and never over the template, which is what
pdf.js's binder does too (`bind.js:61`).

`Place` lays a form out on paper. Under a **positioned** layout a box's
place is its own `x` and `y` added to those of every container above it, down
from the content area's origin, with `anchorType` and `rotate` resolved. Under
a **flow** layout the coordinates are thrown away and the children are stacked,
and this follows two of the six:

- **`tb` and `table`** stack downwards. Each child begins where the one above
  it ends (`layout.js:144-158`, `extra.height += h`), and a container's own
  height is the taller of what it holds and what the template writes for it
  (`template.js:5222`) — so the heights are arrived at from the leaves upwards,
  out of the literal `w` and `h` the template already carries. A container's
  margin is outside what it holds: it moves the children in and adds all four
  insets to the height reported upwards.
- **`row`** cuts its cells from the `columnWidths` of the table above it
  (`html_utils.js:81-106`), a cell spanning `colSpan` of them, and stretches
  every cell to the height of the tallest.

`lr-tb` wraps its children onto lines, which needs a line-breaking rule;
`rl-tb` and `rl-row` fill from the right, which needs a width. Only the first
child of an `lr-tb` is placed.

A **leaf's own height** is often not written either, and then it is its text:
broken into lines at the width it has, a line count times a line height. See
[Measuring text](#measuring-text).

**Nothing is dropped.** Every field and every draw of the form's body comes
back in exactly one of `Page.Boxes` and `Layout.Unplaced`, and each unplaced
one carries the reason in words. A layout that reaches part of a form is
useful; one that silently drops the rest is not, because nothing downstream
can tell a form that was laid out from a form that was half laid out. A page
area's own furniture is the exception, and it is not a leak: a letterhead
belongs to the sheet, so it is drawn once on every sheet that page area makes.

### Where it runs off the bottom, it turns the page

Which page comes next is **not** "another of the same". It is decided by a
state machine — the page set's `relation`, each page area's `<occur>`, the
parity of the page number, and the explicit `<breakBefore>` and `<breakAfter>`
the template writes — and this follows pdf.js's (`template.js:4064-4236`,
`5418-5657`) rather than assuming. Every container of the chain being flowed
begins again at the top of the new content area, which is what pdf.js arrives
at by re-entering the whole tree with a new space.

Breaks are not a corner: **435 of the 560 forms carry a `<breakBefore>`**, and
2 155 of the 2 561 in the corpus are `startNew="1"`, which means "a fresh sheet
here, once".

### Splitting a container is a chain question, not a node question

A container that *may* be split is broken across the boundary: what it placed
before the break stays where it is, and the rest of it begins again at the top
of the next content area — which is what pdf.js's saved generator and
`failingNode` do (`layout.js:38-53`).

Whether it may is **a property of the whole chain above it**, and
`Subform[$isSplittable]` (`template.js:4940-4975`) asks the container above
before it looks at the container itself. Four things must all hold:

1. the container above it is splittable — the recursion ends at the
   `<template>` element, which answers yes (`template.js:5401-5403`), and every
   other kind of node inherits the base answer, no (`xfa_object.js:214-216`).
   The one that matters is `<area>`: it holds body content, it is not a
   subform, and `$getSubformParent` does not skip it — so **nothing inside an
   area is ever split**;
2. its own layout is neither `position` nor anything containing `row`;
3. its `<keep intact>` is `none`. 1 355 elements in the corpus carry
   `keep intact="contentArea"`, a designer saying "do not let this row land
   half on one page and half on the next". An `exclGroup` has the same
   predicate **without** this clause (`template.js:2405-2429`), read there
   rather than assumed;
4. if the container above it has a layout ending in `-tb` and has already put
   something on the line in hand, it is not splittable. That clause is not code
   here and the doc comment says why: no container this package flows through
   ends in `-tb`, so it cannot change an answer, and it needs `numberInLine`,
   which the `lr-tb` slice will bring.

### What moves whole is still put on the paper

A container that may not be split is not dropped for being too tall.
`checkDimensions` returns true outright while the sheet has had nothing that
moves in one piece (`layout.js:266-268`), and the one that claims that pass has
`noLayoutFailure` switched on before its own check runs
(`setFirstUnsplittable`, `template.js:313-319`), so it cannot fail either — and
nor can anything inside it, because the flag is cleared only on the way out of
that same node.

Everything after it on the sheet **is** checked, and what does not fit sends
the whole chain to the next content area, which is what splitting the
containers above it *means*. There it is first in its turn, and goes down
whatever its height. So a container taller than a whole content area comes out
one per sheet, hanging over the bottom, exactly as pdf.js draws it. The pass is
handed out once per **sheet**, not once per content area: pdf.js clears
`firstUnsplittable` in the page loop, before the loop over that sheet's content
areas (`template.js:5536-5538`).

### The recursion pdf.js dies on is not in the branch it looks to be in

pdf.js exhausts its stack on **7 of the 560 forms**, going round between
`PageSet[$getNextPage]` and `PageArea[$getNextPage]`. The branch it is usually
blamed on is the last one:

```js
this[$cleanPage]();
return this[$getNextPage]();
```

`$cleanPage` clears the `[$extra]` of every page area and page set *below* the
set and not the set's own, so the call returns to state no different from the
state it left.

**The defect is not in that one branch.** The branch above it does the same
thing — a page set whose `<occur>` still allows another run resets its own
indices and calls itself, and comes back to the same place when every page area
below it has spent its own `<occur>`. A faithful port has to guard *every* way
a page set can start itself again, not the one named after cleaning. Both
branches were found by porting the machine and running it over the corpus, not
by reading it: the second one was a stack overflow in this package's own tests.

**Guarding a recursion is not the same as answering it**, and the guard alone
was the wrong answer — see [Getting more sheets](#getting-more-sheets-when-a-page-set-runs-out)
below, which is what the restart was for.

Measured over the same 560 packages:

| | |
|---|---:|
| fields in the body of the expanded forms | 81 750 |
| **placed** | **71 230** |
| draws placed with them | 130 846 |
| sheets they came to | 2 737 |
| a place computed and nowhere left to put it | 6 208 |
| under a layout this does not follow (`lr-tb`) | 4 303 |
| anchored by a corner, with no size of its own | 9 |

## Measuring text

Half the corpus was held up by one thing, and it was not a font.

Only **8 466 draws and 598 fields** of the corpus's ~234 000 leaves write no
height — under four per cent. But a stack is a chain: where a `tb` container's
second child begins **is** the height of the first, so one leaf nobody can
measure leaves every sibling below it, and every sibling of every container
above it, with nowhere to begin. Those few thousand leaves held up **44 246
fields, more than half the corpus.**

### The regime this measures in is the reference's own

pdf.js measures with the fonts of the PDF the XFA package came in, and has a
written-down fallback for when it has none:

- `TextMeasure.addString` with no font — *"When we have no font in the pdf,
  just use the font size as default width"* (`text.js:211-219`): **one em per
  character**, a line **1.2 ems** tall, and a first line **one em** tall.
- `getMetrics` with no font (`fonts.js:173-179`): the constants
  `{ lineHeight: 12, lineGap: 2, lineNoGap: 10 }`.

This package implements that regime and nothing else, which is why `go.mod`
still has no dependencies. It is not a stand-in for something better: it is
what the reference runs on a form whose fonts it cannot resolve, so the two
sets of numbers are directly comparable rather than merely plausible.

**The size is not the template's, and that is pdf.js's doing.** `FontInfo` asks
the font finder for the typeface (`text.js:42`) and, when it does not have it,
replaces the *whole* of `xfaFont` with the default's (`:43-46, 55-83`) —
typeface Courier, size 10, letter spacing 0. A `<font size="14pt">` beside a
typeface nobody has is discarded along with it. So is a `<span
style="font-size:14pt">`, and so is `line-height`. Honouring the written size
would disagree with the reference on nearly every leaf.

When real advances are wanted, they belong in `go-opentype/opentype`, which
already exposes glyph advances and units per em. Filling `glyph.w` from a face
is the whole change; the line breaking and the arithmetic above it do not move.

### Rich text is walked in the order the markup writes it

2 255 of the unsized draws hold `<exData contentType="text/html">` rather than
a string, and **32 109 of the corpus's `<p>` elements hold text both before and
after a `<span>`**. So character data inside a rich text is kept as ordered
`#text` children rather than joined and trimmed, which is what pdf.js's
`XmlObject` does with it (`xfa_object.js:865-887`).

The normalisation is pdf.js's, applied run by run rather than once over the
joined text, because that is where pdf.js applies it: a newline is **removed**,
runs of whitespace collapse to one space unless the element writes
`xfa-spacerun:yes` — **44 099 spans of the corpus do** — and `<body>` and
`<html>` drop whitespace-only text altogether.

*The defect not to copy*: `<b>` and `<i>` call `measure.pushFont`
(`xhtml.js:377, 467`), and `TextMeasure` has no such method. A rich text
carrying either throws inside pdf.js's own measurement. No unsized leaf of the
corpus writes one.

### Two things decided it, and neither is about fonts

**The last no-break space of a run becomes an ordinary one.** pdf.js does this
to every text node of the document (`parser.js:61-63`), with the comment
*"normally by definition a &nbsp is unbreakable but in real life Acrobat can
break strings on &nbsp"*. It decides line breaking outright: `Line 2 of
Form\u00a0AB(S11)` in a 63-point column is four lines with the space and three
without it. Until it was applied outside rich text, **31 container heights
disagreed with pdf.js**, all of them in tables of `ca-cra` and `us-irs` forms.

**A leaf whose text gives no height is not left unmeasured.** `computeBbox`
(`html_utils.js:290-324`) is called on every draw and every field before it
returns, and where the height is still unwritten it fills it in from `minH` —
or from nought under a positioned parent that writes a height of its own. The
container above stacks *that*. 7 475 of the unsized draws write a `minH`.

The one case this refuses is `maxH` above nought, where pdf.js answers with the
**room** the leaf has rather than with anything the template wrote. A height is
arrived at before the room it will go in is known — that is why the measurement
is a pass of its own — and no corpus leaf that needs it writes one.

### What the measurement is worth

**25 585 more fields**, 29 123 to 54 708.

| what blocks a field, before and after | slice 3 | slice 4 |
|---|---:|---:|
| **placed** | **29 123** | **54 708** |
| waiting on a height only measuring text would give | 44 246 | 0 |
| below a height written as `=0mm` | 3 649 | 16 936 |
| under `lr-tb` | 3 124 | 4 303 |
| a place computed and nowhere left to put it | 1 608 | 5 794 |

The three blockers that *grew* grew because they were behind the wall: a field
below an unmeasurable leaf was reported for the leaf, and is now reported for
whatever is genuinely in its way. **`h="=0mm"` is now the largest single
thing between here and a whole corpus** — and the section below settles what
it means.

## A measure written as a calculation

XFA lets a length be written as a calculation, with a leading `=`. The corpus
writes exactly one — **`h="=0mm"`, 955 times over 101 of its 560 forms**, on
draws named `Line` — and that one shape held up **16 936 fields**, because a
stack cannot say where its next child begins while a height above it is
unreadable.

**The tempting move is to copy pdf.js, and it would be right by accident.** It
places all of them: `getMeasurement`'s pattern `/([+-]?\d+\.?\d*)(.*)/` is
unanchored, so it matches the `0mm` **inside** the string having never noticed
the `=` at all (`utils.js:83-87`). That is a property of a regular expression,
not a statement about XFA — the same shape as the `px` this package refused in
its first slice.

The authority is pdfium's `CXFA_Measurement`, Foxit's implementation and the
closest to Adobe's own. `SetString` strips a leading `=` **deliberately**, and
then parses what is left leniently (`cxfa_measurement.cpp`):

- blanks before the number are skipped, and only spaces are — `FXSYS_wcstof`
  skips `' '` and nothing else (`fx_extension.cpp:41-45`), so a tab stops the
  number before it starts;
- the value is the longest floating-point number **beginning** what is left;
- a value that is not finite is nought, which is what `isfinite` is there for;
- the unit is the whole of the rest, matched exactly and with its case.

So `="0mm"` really is nought. The two implementations agree on the answer and
only one of them agrees for a reason, which is why the tests here are a port of
`cxfa_measurement_unittest.cpp` rather than an agreement count with pdf.js.

**An expression is not evaluated, and that is the rule rather than a gap.**
`="Foo.h * 2"` has nothing numeric beginning it, so under pdfium's rule it is
nought in a unit pdfium does not know — and a length in a unit it does not know
is nought points, because `ToUnitInternal` has no arm for one and `ToUnit`
turns "cannot convert" into nought. Nought is the reference's **answer** for an
expression, not a shortfall standing in for one. A script engine behind this
would disagree with pdfium on every form that writes one.

Two answers therefore differ from the ones a plain measure gets, and neither is
written in the corpus: a calculated number with **no** unit is nought (`="5"`
is not five points, where `"5"` is), and the unit is matched with its case
(`="5MM"` is nought, where `"5MM"` is nine tenths of an inch).

### What it is worth: 16 522 more fields, and 16 936 was not the prediction

| | slice 4 | slice 5 |
|---|---:|---:|
| **placed** | **54 708** | **71 230** |
| below a height written as `=0mm` | 16 936 | **0** |
| a place computed and nowhere left to put it | 5 794 | 6 208 |
| under `lr-tb` | 4 303 | 4 303 |
| anchored by a corner, with no size of its own | 9 | 9 |

Counted field by field against slice 4's own dispositions, the 16 936 whose
first blocker was `=0mm` reconcile exactly:

| what became of a field blocked by `=0mm` | |
|---|---:|
| **placed** | **16 687** |
| taller than a whole content area, and cannot be broken | 249 |

**189 fields that slice 4 placed are now reported unplaced**, and this is the
honest direction. Each sits in a container whose height slice 4 could not
compute: the stack placed the child it had reached and stopped, so part of the
container went on the sheet. Now the container measures, and it measures
**taller than a whole content area** — so it cannot be placed at all without
splitting it across a sheet, which this slice does not do. Losing them is what
it looks like when a masked blocker comes out from behind a wall. 128 of them
are one form, `us-opm__sf144a`.

The arithmetic: 16 687 placed + 24 more freed elsewhere − 189 reported =
**16 522**, and 54 708 + 16 522 = 71 230.

### The judge had a fault of its own, and it was the same shape as the last one

Four draws of `us-ssa__ss-5-ar-inst` are written **`w="-0.106in"`** — a
negative width — and became reachable for the first time in this slice. The
check called all four defects: boxes placed *left of the container holding
them*.

They are not. A negative extent is not a box reaching left of where it was put:
pdfium normalises a widget's rectangle before using it — `CFX_RectF::Normalize`
moves the origin by the extent and takes its magnitude
(`cxfa_fffield.cpp:293`, `cxfa_ffwidget.cpp:288`) — and so does this package.
pdf.js does not, because CSS cannot: it emits `width:-0.11px`, which a browser
ignores. The judge was comparing **our normalised left edge against pdf.js's
unnormalised origin**, which is the same box counted two ways. Normalising both
sides puts the count back to **0**.

It is slice 4's fault in a new place: a comparison at fault announcing itself
as a disagreement about the subject.

## Splitting, and 4 260 fields that were never a splitting problem

### What it is worth: 4 260 more fields, and none of them wanted splitting

| | slice 5 | slice 6 |
|---|---:|---:|
| **placed** | **71 230** | **75 490** |
| taller than a whole content area, and cannot be broken | 4 214 | **0** |
| past the last sheet the page set gives | 1 887 | 1 887 |
| no room inside a container that moves in one piece | 107 | 61 |
| under `lr-tb` | 4 303 | 4 303 |
| anchored by a corner, with no size of its own | 9 | 9 |
| sheets | 2 737 | 2 864 |

**The disagreement first.** "Most of the 6 208 want a container broken across a
sheet" is not what they wanted. All 4 214 of the largest bucket are
**positioned subforms**, and a positioned layout fails clause 2 of
`$isSplittable`: no rule anywhere would ever have split one of them. They are
whole-page subforms — `form1.Page1`, `topmostSubform.Page5` — six to thirty
points taller than the content area they are drawn for, one per printed sheet,
which is how LiveCycle writes a multi-page form.

What was refusing them was not the absence of a splitter. It was a fit-check
this package applied and pdf.js does not: **the first thing on a sheet that
moves in one piece is never measured against anything.** Because the first
whole-page subform was rejected, the flow never turned the page at all, and
every later one was rejected against the same sheet. `us-irs__fw9` came out on
one sheet with 321 fields unplaced, against pdf.js's six sheets.

The reconciliation is exact: 4 214 too-tall placed + 46 freed from
`noRoomInside` (inside the first such container of a sheet, pdf.js checks
nothing either) = **4 260**, and 71 230 + 4 260 = **75 490**. No count went
down anywhere this time.

**1 887 past the last sheet did not move**, which is worth saying: turning more
pages did not exhaust any page set that was not exhausted before.

### Checked against pdf.js, which CAN see all of this

The judge from slices 2 and 3 is extended rather than replaced, and one thing
it did before was wrong.

**Boxes are now paired by their whole chain of names, not by their own.** Six
subforms of `us-irs__fw9` are called `Bullet1`, in three lists on three sheets;
while most of them had no height they were never compared, and once they all
had one they became six entries of one list with nothing to make the two lists
line up. Pairing on the chain turns a mispairing into an **unpaired** box
rather than into a disagreement — and it moved 8 090 unpairable boxes down to
5 133 while moving 2 139 boxes out of the "pdf.js emits a place" column and
into the "pdf.js placed it by flexbox" one, where they belong.

| container heights | | |
|---|---:|---:|
| heights **this package computes**, paired with pdf.js's | 7 764 | |
| **agree to within 1/100 pt** | **7 764** | **100.00%** |
| disagree | 0 | 0.00% |
| more paired but written outright in the template — no check in agreeing | 5 289 | |

| where the boxes went | | |
|---|---:|---:|
| boxes where pdf.js emits a real place | 176 | |
| **agree exactly** | **176** | **100.00%** |
| boxes pdf.js placed by flexbox | 161 820 | |
| ... at the flow container's own origin, where a first child goes | 157 108 | |
| ... below or to the right of it, where the rest go | 4 712 | |
| ... **above or to the left of it, which would be outside it** | **0** | **0.00%** |

| which sheet they went on | | |
|---|---:|---:|
| forms where this package placed **every** element of the body | 473 | |
| of those, agreeing with pdf.js on the number of sheets | **469** | 99.2% |
| boxes paired on those forms | 149 433 | |
| **on the same sheet as pdf.js put them** | **149 433** | **100.00%** |
| on another sheet | 0 | 0.00% |
| forms where fewer elements were placed, and so fewer sheets used | 5 | |
| ... the same number of sheets | 5 | |
| ... **MORE** sheets, which would be a defect | **0** | 0.00% |

**The pairing itself was re-audited, not just the rate.** Splitting changes
which boxes are comparable, so the question is what the judge never gets to
compare. Of 156 574 body boxes on the 469 agreeing forms: 149 433 paired,
6 680 unnamed and so never keyed, 461 keyed but absent from pdf.js's dump —
**460 of those 461 are `presence="hidden"`**, which pdf.js emits as
`display:none` and the dump does not carry — and **0** dropped because the two
sides produced a different number of boxes for one key. That last zero is the
one that matters: splitting did not create a single new mispairing.

Four forms disagree on the sheet count. Two (`us-opm__sf813`, `us-opm__sf39a`)
are the same two as before and are not pagination: they have a **positioned**
outermost subform, and pdf.js's `checkDimensions` position arm
(`layout.js:355-364`) sends children reaching past the bottom of the content
area onto a second sheet, which this package does not fit-check at all. Two
(`us-uscis__i-600a`, `us-uscis__i-821`) are newly visible: they were nowhere
near fully placed before — `i-600a` came out on **one** sheet against pdf.js's
fourteen — and now come out on thirteen. In both, every box agrees up to a
`<breakBefore targetType="pageArea" startNew="1"/>` and is one sheet behind
after it. That is a question about breaks, not about splitting, and it is not
diagnosed to the line here.

**What none of this covers**: where inside a container the children ended up
(two orderings come to the same total, and no height tells them apart);
borders, margins and insets on positioned layouts; anything under a rotated
ancestor; where a break inside a container that moves whole would have sent
the page; and real per-glyph
advances, which nothing here has and which the comparison is therefore blind
to in both directions. It also could not run on 77 of the 560 forms — 70
because pdf.js's `selectFont` dereferences a null typeface when no font is
supplied (`fonts.js:159`), which is precisely the line `getMetrics` documents
as its no-font answer, and 7 for the recursion above.

## Lengths

XFA's units are `mm`, `pt`, `in`, `cm`, `pc`, `mp`, `em` and `%` — the list
Foxit's implementation recognises in pdfium's `CXFA_Measurement`, which is the
implementation closest to Adobe's own. **`px` is not one of them.** pdf.js reads
it as one point and this package used to read it as 72/96 of one; both were
HTML habits carried into a format that does not have the unit. A length in `px`
is now refused rather than guessed at, and the element carrying it is reported
unplaced. No `x`, `y`, `w` or `h` in the corpus is written in it.

A length may also be written as a **calculation**, with a leading `=`. That is
read by pdfium's rule, which strips the `=` and parses the rest leniently and
without ever failing — see the section above.

`Node.Measure` answers three ways, not two: the attribute is absent, or it is a
length, or it is there and unreadable. XFA's "unspecified" and its "zero" are
different things — a field with no width has no width until its text is
measured, which is not the same as a field nought wide — and pdf.js loses the
distinction in `measureToString`, which turns any string into `"0px"`.

## Getting more sheets when a page set runs out

`<occur>` on a page area does not bound a form, and reading it as if it did left
**1 887 fields on 10 forms** off the paper.

### A page area's max is a bound on ONE RUN of the set holding it

Restarting a page set offers its page areas again. Both references say so and
neither quite carries it out.

**pdfium** says it in a line: `FindPageAreaFromPageSet_Ordered` walks a set from
its first child and sets `cur_page_count_ = 1` on the page area it settles on
(`cxfa_viewlayoutprocessor.cpp:1240-1244`) — and `cur_page_count_` is the
counter `GetNextAvailPageArea` tests the max against (`:1414-1425`). What bounds
the whole run is the **set's** own max, read against `page_set_map_`
(`:1196-1215`), which nothing resets. So an uncapped page set over capped page
areas yields sheets for ever, and a capped one stops.

**pdf.js** means the same and cannot reach it. Its `$cleanPage`
(`template.js:4160-4167`) is exactly that reset, but it sits in the LAST branch
of `PageSet[$getNextPage]` (`:4230-4231`), below the branch that restarts a
usable set (`:4222-4227`) — and a set with no `<occur>` is usable for ever
(`$isUsable`, `:4169-4174`, whose first clause is `!this.occur`). **559 of the
560 corpus forms write a page set with no `<occur>` at all.** So the restart
fires, hands back a page area whose own max is spent, and recurses until the
stack is gone. That is the seven-form crash above, and it is this branch.

Cleaning the page areas on the restart is the whole change. The sequence then
terminates because it has a page to give, not because a counter stopped it.

### An absent `max` is unbounded, whatever the `min` says

The second half is one line of `occurMax`. `Occur[$clean]` reads as though a
written `min` with no `max` pins the max to the min (`template.js:3925-3932`) —
but that branch cannot fire on an attribute nobody wrote. The constructor tests
`attributes.max !== ""` (`:3896-3903`), and a **missing** attribute is
`undefined` rather than `""` (`_mkAttributes`, `parser.js:79-111`), so
`getInteger`'s default of `-1` is taken and `$clean`'s `this.max === ""` is
already false. Only `max=""` written out reaches it, and nothing in the corpus
writes one.

That is pdf.js arriving somewhere by accident, so it is not read from pdf.js.
pdfium asks the same question with the default suppressed —
`TryInteger(XFA_Attribute::Max, /*bUseDefault=*/false)` — and takes `-1` where
the attribute is absent. **The two agree on the number by different routes.**

It decides one form outright: `us-ssa__ssa-3371-bk` writes `<occur min="1"/>`
on its only page area. Read as a max of one it gives a single sheet and 127
fields fall off; pdf.js's own dump for it is **nine** sheets, and this package
now puts it on nine.

### What it is worth

| | slice 7 | slice 8 |
|---|---:|---:|
| **placed** | **79 851** | **81 738** |
| past the last sheet the page set gives | 1 887 | **0** |
| anchored by a corner, with no size of its own | 9 | 9 |
| no room inside a container that moves in one piece | 3 | 3 |
| sheets | 3 040 | 3 088 |

Reconciled field by field against `main`, 81 750 dispositions paired on
`(form, path, occurrence)` with the same multiset of keys: **1 887 placed, 0
lost, and 0 fields placed both times on a different sheet.**

### Where the reference could see it, and where it could not

Of the 1 887, **1 760 are on forms pdf.js cannot lay out at all** — the seven it
recurses to death on, plus `ca-cra__t2121-fill-24e` and `-25e`, which have the
same page set and die of its font defect first. The remaining **127** are
`us-ssa__ssa-3371-bk`, which pdf.js does lay out, and which is therefore the
only external check this change has. It is a good one: the form goes from one
sheet to nine, its 190 body boxes all pair, and **every one lands on the sheet
pdf.js put it on**.

| which sheet they went on | slice 7 | slice 8 |
|---|---:|---:|
| forms where this package placed **every** element | 476 | **477** |
| of those, agreeing with pdf.js on the number of sheets | 472 | **473** |
| boxes paired on those forms | 150 792 | **150 982** |
| **on the same sheet as pdf.js** | 100.00% | **100.00%** |
| forms placing fewer elements and using **MORE** sheets | 2 | 2 |

Container heights: **7 794 of 7 794 agree to 1/100 pt**, none unmeasurable.
Placement: **176 of 176** exact, and **0** of the 163 382 flexbox boxes above or
left of their container.

**The pairing re-audited, because more sheets means more boxes.** Of 162 353
body boxes on the 473 agreeing forms: 150 982 paired, 6 781 unnamed and so never
keyed, 4 590 keyed but absent from pdf.js's dump (1 900 `presence="hidden"`),
and **0 dropped because the two sides counted a key differently**. The same
audit on `main` gives 6 781, 4 590 and 1 900 — *identical* — so the newly
comparable form contributed 190 boxes and every one of them paired.

### One correction that came with it

pdf.js starts a form with `pageSetIndex: 0` (`template.js:5487`) on the page set
the first sheet came from, which says a nested page set has been offered when
none has. It changed no answer while the restart did not clean, because the
restart offered the nested set on its second pass. It does now, so it is `-1`
here. pdfium looks at the siblings *after* the spent page area, descending into
a nested set as it meets one (`cxfa_viewlayoutprocessor.cpp:1444-1447`,
`:1249-1258`). **No corpus form nests a page set** — 560 forms, 560 page sets —
so this is measured by one unit test and by nothing else.

### What it does not settle

The four sheet-count disagreements are the same four, and none is this. Neither
`us-opm__sf813` nor `us-opm__sf39a` (the positioned fit check,
`layout.js:355-364`) nor `us-uscis__i-600a` nor `us-uscis__i-821` (one sheet
behind after a `breakBefore`) writes an `<occur>` on any page area, so neither
half of this change can touch them — checked before assuming it. The two forms
using more sheets than the reference, `us-uscis__i-956` and `i-956g`, are
likewise unchanged.

