# INGEST_PDF_DOCUMENTS

The pdf-ingestor's checklist. Loaded automatically into the pdf-ingestor's prompt via skills_dir.

## Tool reference

PDFs are extracted via shell tools (`pdftotext`, `pdfinfo`, `pdftoppm`) + Python (`pypdf`, `pymupdf`) + media-mcp for OCR. The media-mcp tool names are auto-injected at runtime under the `mcp__media__` prefix — check your tool list for the exact names. The OCR-flavored tool is what you want for scanned pages; the caption/tagging-flavored tools are for figures and charts.

| Tool | When | Install |
|---|---|---|
| `pdftotext` (CLI) | Default text extraction. Fast, accurate for born-digital PDFs. | `poppler-utils` (usually pre-installed) |
| `pdftotext -layout` | Multi-column PDFs (academic papers, magazines). | Same |
| `pdfinfo` | Metadata: page count, author, creation date, encryption status. | Same |
| `pdftoppm` | Rasterize a PDF page to PNG. For OCR fallback. | Same |
| `pypdf` (Python) | When you need page-level access, bookmarks, annotations. | `pip install pypdf` |
| `pymupdf` (Python) | Tables, form fields, image extraction. | `pip install pymupdf` |
| `qpdf --decrypt` | Encrypted PDFs (if user has the right to access). | `apt install qpdf` |

## Workflow

### Step 1 — Inventory

For each PDF in the input path:

```bash
pdfinfo "<path>"
# Capture: Pages, Title, Author, CreationDate, ModDate, Encrypted
ls -la "<path>"  # File size
md5sum "<path>"  # De-dupe if user gives the same PDF twice
```

Write to `artifacts/pdf/<doc-slug>/_METADATA.md`:

```markdown
# <doc-slug> — metadata

- **Title:** <from pdfinfo or filename>
- **Author:** <from pdfinfo or "Unknown">
- **Published:** <CreationDate or "undated">
- **Pages:** <N>
- **File:** <path>
- **MD5:** <hash>
- **Encrypted:** yes/no
- **Scanned:** <yes/no — determined in step 2>
- **Primary / Secondary / Tertiary:** <pick one>
- **Known bias / context:** <e.g., "Industry-funded whitepaper" or "Court filing, defense perspective">
```

### Step 2 — Extract text

First try the fast path:

```bash
pdftotext -layout "<path>" "artifacts/pdf/<doc-slug>/raw.txt"
wc -l "artifacts/pdf/<doc-slug>/raw.txt"
# If line count is suspiciously low (<5 lines for a 20+ page PDF), it's probably scanned.
```

**If `raw.txt` is empty or garbage:**
1. Rasterize: `pdftoppm -r 200 "<path>" "artifacts/pdf/<doc-slug>/page" -png`
2. For each page PNG, call the OCR-flavored media-mcp tool with `mode=markdown`
3. Concatenate results into `raw.txt`

### Step 3 — Extract structure

Read `raw.txt`. Identify:
- **Section headers** — numbered ("1.", "1.1", "II.") or styled ("## Methodology")
- **Tables** — if `pdftotext -layout` mangled them, use pymupdf:

  ```python
  import pymupdf
  doc = pymupdf.open("<path>")
  for page in doc:
      tables = page.find_tables()
      for t in tables:
          print(t.extract())  # CSV-friendly
  ```

- **Figures/charts** — extract with `pymupdf`:

  ```python
  for page in doc:
      for img in page.get_images():
          xref = img[0]
          pix = pymupdf.Pixmap(doc, xref)
          pix.save(f"artifacts/pdf/<doc-slug>/figures/p{xref}.png")
  ```

  For each significant figure, optionally call a caption-flavored media-mcp tool to get a content description.

- **Citations / footnotes** — record verbatim. These become your "secondary sources" pointer.

### Step 4 — Pull findings (markdown + SurrealDB source record)

**Two writes per finding.** Skipping the DB write means future agents can't query past research — the world stays ephemeral.

**(a) Markdown finding** at `artifacts/pdf/<doc-slug>/<section-slug>.md`:

```markdown
# <section heading> (p. <N>)

<direct quote, ≤2 sentences, verbatim>

## Context
<1 sentence: how this fits the doc's overall argument>

## Type
**<primary|secondary|tertiary>** — <why>
```

**(b) SurrealDB source record** — one per PDF document (not per section). Run this once after extracting the document:

```bash
python3 /sandbox/surreal_client.py save-source \
  --kind pdf \
  --path "/abs/path/to/document.pdf" \
  --title "<title from pdfinfo>" \
  --author "<author from pdfinfo>" \
  --published-at "<CreationDate ISO-formatted>" \
  --content-file "artifacts/pdf/<doc-slug>/raw.txt" \
  --topic-ids "topic:abc123"
```

This atomically:
1. Embeds the extracted text (1024-dim via Ollama mxbai-embed-large, capped at 8k chars)
2. INSERTs a `source` record (idempotent on path)
3. RELATEs topic_ids via `extracted_from` edge

**For sections that reference existing topics or persons**, create additional extracted_from edges manually:

```bash
python3 /sandbox/surreal_client.py relate \
  --src "topic:abc123" --edge extracted_from --tgt "source:<source_id>"
```

### Step 5 — Build the index

Write `artifacts/pdf/_INDEX.md`:

```markdown
# PDF ingest index

## Documents
- [<doc-1>](_METADATA.md) — <one-line summary>, <N> findings
- [<doc-2>](_METADATA.md) — <one-line summary>, <N> findings

## Findings by document
### <doc-1>
- [section-A.md](<doc-1>/section-A.md) — <claim>
- [section-B.md](<doc-1>/section-B.md) — <claim>

## Primary sources
- <doc-1>, <doc-3>

## Secondary sources
- <doc-2>

## Quality notes
- <doc-2> is a scanned PDF; OCR was clean but verify any numbers against the figure on p. 14
```

## Stop conditions

- Every relevant section extracted with page numbers, OR
- All sections scanned but none relevant (report this — don't pad), OR
- `max_rounds` hit

## Pitfalls

- **Layout loss** — `pdftotext` without `-layout` interleaves columns. Always use `-layout` for multi-column PDFs.
- **CJK / non-Latin text** — `pdftotext` may produce mojibake. Fall back to OCR.
- **Form-field PDFs** — `pdftotext` may miss form field values. Use `pymupdf`'s `page.widgets()`.
- **Inline images vs image XObjects** — pymupdf's `get_images()` only catches XObjects. Inline images need `page.get_text("dict")` and walk the spans.
- **Encrypted PDFs** — `pdfinfo` will say `Encrypted: yes`. Don't try to brute-force. Report to CTO and let user decide.
- **Mega-PDF OCR cost** — a 500-page scanned PDF × OCR per page = expensive. Warn the CTO before starting.
