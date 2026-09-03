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

A container that would have to be **broken in two** for its parts to fit is not
broken. pdf.js keeps `[$extra].children`, a generator and a `failingNode` to do
that (`layout.js:38-53`); this does not. A container that *may* be split
(`Subform[$isSplittable]`, `template.js:4940-4975`) has its children
distributed across sheets instead, which is the same thing where the container
itself draws nothing. One that may not — a positioned layout, a row, or
anything with `keep intact` — moves whole, and is reported unplaced where it
fits no sheet at all. 1 355 elements in the corpus carry
`keep intact="contentArea"`, which is a designer saying "do not let this row
land half on one page and half on the next".

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
a page set can start itself again, not the one named after cleaning. So here a
page set may restart at most once per request for a page, cleaning really does
reset the set's own place in its list, and a page set holding neither a page
area nor a page set says so rather than looping. Both branches were found by
porting the machine and running it over the corpus, not by reading it: the
second one was a stack overflow in this package's own tests.

Measured over the same 560 packages:

| | |
|---|---:|
| fields in the body of the expanded forms | 81 750 |
| **placed** | **54 708** |
| draws placed with them | 106 181 |
| sheets they came to | 2 268 |
| below a height written as `=0mm`, which is not a length | 16 936 |
| under a layout this does not follow (`lr-tb`) | 4 303 |
| a place computed and nowhere left to put it | 5 794 |
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
thing between here and a whole corpus** — and pdf.js reads it as nought,
because `getMeasurement`'s pattern is unanchored and finds the `0mm` inside it
(`utils.js:83-87`). Whether that is Adobe's rule or an accident of a regular
expression is a reading for the next slice, not a guess for this one.

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
| heights **this package computes**, paired with pdf.js's | 7 758 | |
| **agree to within 1/100 pt** | **7 758** | **100.00%** |
| disagree | 0 | 0.00% |
| more paired but written outright in the template — no check in agreeing | 5 136 | |

| where the boxes went | | |
|---|---:|---:|
| boxes where pdf.js emits a real place | 176 | |
| **agree exactly** | **176** | **100.00%** |
| boxes pdf.js placed by flexbox | 117 813 | |
| ... at the flow container's own origin, where a first child goes | 115 498 | |
| ... below or to the right of it, where the rest go | 2 315 | |
| ... **above or to the left of it, which would be outside it** | **0** | **0.00%** |

| which sheet they went on | | |
|---|---:|---:|
| forms where this package placed **every** element of the body | 357 | |
| of those, agreeing with pdf.js on the number of sheets | **354** | 99.2% |
| boxes paired on those forms | 89 334 | |
| **on the same sheet as pdf.js put them** | **89 334** | **100.00%** |
| on another sheet | 0 | 0.00% |
| forms where fewer elements were placed, and so fewer sheets used | 86 | |
| ... the same number of sheets | 40 | |
| ... **MORE** sheets, which would be a defect | **0** | 0.00% |

The three sheet-count disagreements are not pagination and not text. Two
(`us-opm__sf813`, `us-opm__sf39a`) have a **positioned** outermost subform, and
pdf.js's `checkDimensions` position arm (`layout.js:355-364`) sends children
reaching past the bottom of the content area onto a second sheet; this package
does not fit-check a positioned layout at all.

**What none of this covers**: where inside a container the children ended up
(two orderings come to the same total, and no height tells them apart);
borders, margins and insets on positioned layouts; anything under a rotated
ancestor; breaking one container in two across a sheet; and real per-glyph
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

`Node.Measure` answers three ways, not two: the attribute is absent, or it is a
length, or it is there and unreadable. XFA's "unspecified" and its "zero" are
different things — a field with no width has no width until its text is
measured, which is not the same as a field nought wide — and pdf.js loses the
distinction in `measureToString`, which turns any string into `"0px"`.
