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
| **placed** | **29 123** |
| draws placed with them | 49 329 |
| sheets they came to | 1 237 |
| waiting on the height of one leaf inside the stack | 46 819 |
| a place computed and nowhere left to put it | 1 608 |
| under a layout this does not follow | 2 924 |
| a height written as `=0mm`, which is not a length | 3 649 |
| other | 127 |

### The prediction was 36 494, and the gap is the finding

Slice 2 placed 15 400 fields and reported that 21 094 more had a place computed
that fell past the bottom of the one content area it laid out — so "the
arithmetic reaches 36 494" once the page turns. It reaches **29 123**.

Of those 21 094 (20 736 counted by path), pagination placed **14 077 — 68% of
them.** The other third were never behind pagination at all:

| what became of a field that fell past the bottom | |
|---|---:|
| **placed, once the sheet turned** | **14 077** |
| held up by a leaf above it with no written height, further down the same stack | 5 156 |
| taller than a whole content area, and cannot be broken | 885 |
| the form ran out of pages | 405 |
| no room inside a container that moves whole | 167 |
| other | 46 |

**The page-1 cutoff was masking text measurement.** A field reported as "past
the bottom" was one whose place had been computed *on the assumption that
everything above it on that page had a height*; once the page turns and the
stack goes on, a quarter of them turn out to sit below a leaf that writes none.
Which is the same shape of error as the two before it: a question asked about
one node — does this one's place fall past the bottom? — where the structure is
a chain.

### Checked against pdf.js, which CAN see this

pdf.js emits one `<div class="xfaPage">` per sheet, with every element inside
the one it belongs to. That is said outright, in the structure of the output
rather than in a style, so page **count** and per-page **membership** are
comparable even where a coordinate is not.

It has to be narrowed, because pdf.js measures text and this package does not:
on a form where a leaf writes no height pdf.js lays out elements this leaves
unplaced, and more elements need more sheets. So the count is compared strictly
only on the forms where every element of the body was placed.

| | | |
|---|---:|---:|
| forms where this package placed **every** element of the body | 199 | |
| of those, agreeing with pdf.js on the number of sheets | **198** | 99.5% |
| boxes paired on those forms | 23 006 | |
| **on the same sheet as pdf.js put them** | **23 006** | 100.00% |
| on another sheet | **0** | 0.00% |
| forms where fewer elements were placed, and so fewer sheets used | 221 | |
| ... the same number of sheets | 63 | |
| ... **MORE** sheets, which would be a defect | **0** | 0.00% |

The one disagreement, `us-opm__sf813`, is not pagination: its outermost subform
is **positioned**, and pdf.js's `checkDimensions` position arm
(`layout.js:355-364`) sends children that reach past the bottom of the content
area onto a second sheet. This package does not fit-check a positioned layout
at all — it did not in slice 1 either — so it draws them where their
coordinates say, off the bottom of the one sheet.

The heights and the placements are still checked, and both still agree exactly:

| | | |
|---|---:|---:|
| container heights this package computes, paired with pdf.js's | 7 072 | |
| agree to within 1/100 pt | **7 072** | 100.00% |
| boxes where pdf.js emits a real place | 450 | |
| agree exactly | **450** | 100.00% |
| boxes pdf.js placed by flexbox, none above or left of its container | 51 312 | |

**What none of this covers**: where inside a container the children ended up
(two orderings come to the same total); borders, margins and insets on
positioned layouts, which pdf.js writes as CSS `calc()`; text measurement;
anything under a rotated ancestor; and breaking one container in two across a
sheet. It also could not run on 77 of the 560 forms — 70 because pdf.js's
`selectFont` dereferences a null typeface when no font is supplied
(`fonts.js:159`), and 7 for the recursion above.

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
