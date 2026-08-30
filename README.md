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
