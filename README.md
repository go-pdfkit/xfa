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

`Place` lays a form out on one page under **positioned** layout. A box's place
is its own `x` and `y` added to those of every container above it, down from
the content area's origin, with `anchorType` and `rotate` resolved. Under
`position` pdf.js computes the same thing — `style.left` and `style.top` from
the node's own `x` and `y` (`html_utils.js:112-123`) — and delegates to the
browser's flexbox only for the flow layouts, so this is a reference followed
rather than a gap invented.

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
| **placed** | **13 331** |
| reported unplaced | 69 047 |
| — under a flow layout | 68 891 |
| — needing text measurement | 73 |
| — on another page area | 74 |
| — anchored, with no size of their own | 9 |

### The number is not the one a count over the templates predicts, and that is the finding

Counting the templates, **70 297** fields sit under an immediately enclosing
subform whose layout is `position`. A position-only engine does not reach them,
because **556 of the 560 outermost subforms are laid out `tb`**: almost every
positioned subform hangs below a flow one, and where a flow layout puts its
*second* child is the height of its first — the flow layout itself.

What is reached is the exact part of the flow rule: pdf.js accumulates from
nought, so the **first** child of a `tb`, `table`, `lr-tb` or `row` container
sits at the container's own origin (`layout.js:145-160`). That is the flow
algorithm's own answer for one child, not an approximation, and it is what lets
the slice reach a real form at all.

### Checked against pdf.js, not against itself

A box at coordinates this package computed proves nothing. `TestPlacementAgainstPdfjs`
compares them with pdf.js's own layout, run over the same templates and data:

| | | |
|---|---:|---:|
| boxes paired by name | 21 933 | |
| agree to within 1/100 pt | **21 881** | 99.76% |
| differ only by pdf.js's own two-decimal rounding | 47 | 0.21% |
| **disagree** | **5** | 0.02% |

All five differ in *width only*, never in place, and all five are `colSpan`
cells whose width comes from the parent table's `columnWidths` rather than
their own `w` (`html_utils.js:81-111`).

**What that check does not cover**: pdf.js hands flow layouts to the browser's
flexbox and emits no coordinates for them, so it and this package are blind in
the same place; borders, margins and insets, which pdf.js writes as CSS
`calc()`; text measurement; and anything under a rotated ancestor. It also
could not run on 77 of the 560 forms — 70 because pdf.js's `selectFont`
dereferences a null typeface when no font is supplied (`fonts.js:159`), and 7
because `PageSet[$getNextPage]` recurses until the stack runs out.

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
