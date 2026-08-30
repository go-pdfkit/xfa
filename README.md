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
