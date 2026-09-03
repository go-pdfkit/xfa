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

`Place` lays a form out on one page. Under a **positioned** layout a box's
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

**Nothing is dropped.** Every field and every draw of the expanded form comes
back in exactly one of `Page.Boxes` and `Layout.Unplaced`, and each unplaced
one carries the reason in words. A layout that reaches part of a form is
useful; one that silently drops the rest is not, because nothing downstream
can tell a form that was laid out from a form that was half laid out.

Measured over the same 560 packages:

| | |
|---|---:|
| fields in the templates | 82 386 |
| fields in the expanded forms | 82 378 |
| **placed** | **15 400** |
| draws placed with them | 28 494 |
| a place computed, past the bottom of the one page | 21 094 |
| waiting on the height of one leaf inside the stack | 40 435 |
| under a layout this does not follow | 2 826 |
| a height written as `=0mm`, which is not a length | 2 459 |
| other | 164 |

### The wall is not the arithmetic

Slice 1 placed 13 331 fields and predicted that stacking over literal heights
would take that to 62 973. It takes it to **15 400**, and the two things that
stand in the way are not heights that need adding up.

**Text measurement, which was measured at half of one per cent and is not.** Of
the corpus's ~234 000 fields and draws, only 8 466 draws and 598 fields lack a
height a template writes. But a stack is a chain: one child whose height cannot
be said leaves every sibling below it in that container with nowhere to begin.
Under four per cent of the leaves hold up **40 435** fields — half of them.
The earlier count asked whether a *field* needs measuring; what decides a stack
is whether anything above it in the same container does, and most of what is in
a stack is `draw`.

**Pagination.** 21 094 fields have a place computed for them and fall past the
bottom of the one content area this lays out. pdf.js does not refuse them: it
fails the container and carries what is left onto the next page
(`template.js:5502-5600`). That is a slice of its own, and it is worth about
what it says.

Taking a container's *written* height as its height, rather than the taller of
that and its contents, was tried and measured rather than argued about. It
computes a place for **80 821** of the 82 378 fields — nearly all of them — and
is plausibly the rule behind the 62 973 estimate. It was rejected: it is not
pdf.js's rule, and over the 2 323 containers where both quantities can be had,
the written height is the wrong one 63 times, by 14 pt on average. Reaching
further by guessing is not reaching further. Under that rule only 14 410 fields
land on the one page anyway — fewer than the 15 400 above, because more of the
stack advances and so more of it runs off the bottom.

### Checked against pdf.js, which cannot see this

A box at coordinates this package computed proves nothing, and the check slice 1
used cannot reach slice 2: **pdf.js emits no coordinates for a child of a flow
layout.** It writes them into a `display: flex; flex-direction: column` div
(`web/xfa_layer_builder.css:255-263`) and lets the browser stack them, so its
output says where the *container* is and nothing about where the second child
went.

It does emit the accumulation itself, as a number. A subform's `style.height`
is `Math.max(extra.height + marginV, this.h || 0)` (`template.js:5222`), and
`extra.height` is the sum this package computes. So the check is on the heights:

| | | |
|---|---:|---:|
| container heights **this package computes**, paired with pdf.js's | 7 072 | |
| agree to within 1/100 pt | **7 072** | 100.00% |
| disagree | **0** | 0.00% |
| more paired, but written outright in the template — no check in agreeing | 3 736 | |
| this package cannot measure, so has no number to compare | 2 264 | |

Thirteen of those disagreed before the check accounted for one rule: pdf.js
writes the row's height back over every cell of a row (`layout.js:135-143`), so
what it emits for a cell is the row's height and not the cell's. Comparing the
row's height with it checks the stretch instead, which is what pdf.js is
saying there.

Placement is still compared where pdf.js emits one: 199 boxes, all agreeing.
For the 23 540 it placed by flexbox, all that can be checked is one-sided —
none came out above or to the left of the container it belongs to, and 21 459
of them sit exactly at that container's origin, which is where a flow puts its
first child.

**What none of this covers**: where inside a container the children ended up
(two orderings come to the same total); borders, margins and insets on
positioned layouts, which pdf.js writes as CSS `calc()`; text measurement; and
anything under a rotated ancestor. It also could not run on 77 of the 560
forms — 70 because pdf.js's `selectFont` dereferences a null typeface when no
font is supplied (`fonts.js:159`), and 7 because `PageSet[$getNextPage]`
recurses until the stack runs out.

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
