# Manual

The WalkieTalkie user manual, structured the same way as the [eBookEd](https://github.com/JohnDovey/eBookED) manual (frontmatter / chapters / backmatter, numbered files, generated index/spine).

Content is authored as **`.ebhtml`** files — plain HTML fragments, not Markdown. A chapter file looks like:

```
---
title: Getting Started
subtitle:
numberMode: Auto
numberOverride:
---
<h1 id="getting-started">Getting Started</h1>
<p>Body content as plain HTML...</p>
```

The `---` header (title/subtitle/numberMode/numberOverride) is only used on numbered chapter pages. Front matter and back matter pages (title page, copyright, TOC, about) are just a bare HTML fragment with no header.

This manual is ideally authored and exported using the **[eBookED](https://github.com/JohnDovey/eBookED)** app itself (open it as a project via File → New/Open Project, pointed at this `Manual/` directory) rather than hand-edited — that's what keeps the `.ebhtml` files, the generated index, and the EPUB/PDF/Word exports consistent.

## Layout

- `frontmatter/` — title page, introduction, table of contents
- `chapters/` — numbered chapter files, e.g. `001-Getting-Started.ebhtml`
- `backmatter/` — appendices, about, etc.
- `images/` — screenshots and diagrams referenced by manual pages
- `output/` — exported `.epub`, `.pdf`, and `.docx` builds of the manual

## Build outputs are committed

Unlike eBookED's own repo (which gitignores its `manual/output/`), **WalkieTalkie's `Manual/output/` is checked into version control, not gitignored.** Exports are generated and bundled with releases, so the `.epub`/`.pdf`/`.docx` files belong in the repo alongside the source `.ebhtml`.

Keep this manual up to date as the app develops — add or update a chapter whenever a user-facing feature changes.
