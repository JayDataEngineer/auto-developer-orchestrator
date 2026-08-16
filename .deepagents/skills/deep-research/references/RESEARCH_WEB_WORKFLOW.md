# RESEARCH_WEB_WORKFLOW

The web-researcher's checklist. A skill reference — read it when researching.

## Strategy

The `web_research` MCP server arms three tools: `web_research_research` (search + read top results in one call — the default; returns full page content, not just titles), `web_research_search` (lightweight title/snippet list), and `web_research_fetch` (read one known URL). No crawler or sitemap-mapper is armed — for bulk discovery, iterate `web_research_search`/`web_research_research` over the query space instead.

## Workflow

### Step 1 — Decompose the question

Restate the user's research question in your own words. Then list 3-5 sub-questions. If you can't decompose, you don't understand the question — ask the CTO.

**Example:**
- User: "What's the background of organization X and why are they in the news?"
- Sub-questions:
  1. Who runs organization X and what's their stated mission?
  2. What do watchdog groups or journalists say about them?
  3. Have they been linked to specific real-world incidents?
  4. What's the platform/provider response to their activity, if any?

### Step 2 — Search per sub-question

Call `web_research_research` with each sub-question as the `query`. Default `max_results=3, depth=quick`. For load-bearing sub-questions, escalate to `depth=deep` and `max_results=5`.

### Step 3 — Deepen on load-bearing sources

For each source that you'll cite, follow up with `web_research_fetch` on the URL. This gets you the full page text, not just the snippet. The snippet is often misleading.

### Step 4 — Read every image

For each load-bearing source, look at the page's images. Run the appropriate media-mcp tool:
- Document/screenshot images → OCR (extract verbatim text)
- Photo/scene images → caption (note identifiable people, locations, dates)

This is non-negotiable. The image often contains the most falsifiable claim.

### Step 5 — Cross-check

For each claim, find ≥2 independent sources. "Independent" means:
- Not both quoting the same press release
- Not both owned by the same parent company
- Not both citing each other in a circle

If you find only echo-chamber sources, note that in `_INDEX.md`.

### Step 6 — Record findings (markdown + SurrealDB source record)

**Two writes per finding.** Skipping the DB write means future agents can't query past research — the world stays ephemeral.

**(a) Markdown finding** at `artifacts/research/<slug>.md`:

```markdown
# <one-sentence claim>

<direct quote from source, ≤2 sentences>

## Source
- **URL:** <full URL>
- **Title:** <page title>
- **Author:** <author or "Undated / unattributed">
- **Published:** <date or "undated">
- **Accessed:** <today's date>

## Confidence
**<high|medium|low>** — <one sentence why>

## Notes
<optional: caveats, related findings, follow-ups>
```

**(b) SurrealDB source record** via `surreal_upsert`:

```bash
surreal_upsert(table="source", data={
  "kind": "web", "url": "https://example.com/article", "title": "Article Title",
  "author": "Author Name", "published_at": "2026-06-15",
  "content": <read finding-slug.md>,
})
```

This atomically:
1. Embeds the content (1024-dim via microsoft/harrier-oss-v1-0.6b (sandbox/embed.py))
2. INSERTs a `source` record (idempotent on URL — re-runs UPDATE)
3. RELATEs each topic_id / person_id via `extracted_from` edge
4. Returns `source_id` for downstream use

**When to link topic_ids**: if your finding already maps to an existing topic, link it. If you discovered a new theme, defer linking — the synthesizer or CTO will create the topic later. Don't fabricate topic IDs.

**When to link person_ids**: only when the source directly discusses or quotes a person. If the person doesn't exist in the DB yet, create them via `upsert-person`:

```bash
surreal_upsert(table="person", data={"canonical_name": "Elon Musk", "role": "subject", "notes": "CEO of SpaceX, quoted in this article about Starship"})
```

Only create persons for individuals the source is **about** or **quotes directly** — not every proper noun. Use your judgment.

### Step 7 — Build the index

Write `artifacts/research/_INDEX.md`:

```markdown
# Research index — <topic>

## Coverage
- Sub-question 1: well-covered (N sources)
- Sub-question 2: gap — only 1 source
- ...

## Findings
- [finding-1.md](finding-1.md) — <one-line summary>
- [finding-2.md](finding-2.md) — <one-line summary>
...

## Echo chambers detected
- <if any>: <list of sources all deriving from the same origin>

## Suggested next steps
- Re-research <sub-question> with different angle
- PDF ingest needed for <source>
- Good enough — hand to synthesizer
```

## Stop conditions

- Every sub-question has ≥3 independent sources, OR
- Further searches return only sources you already have, OR
- `max_rounds` hit

If you stop with gaps, **say so explicitly** in `_INDEX.md`. Don't paper over.

## Pitfalls

- **Search-result farming** — many "Top 10 X" sites exist purely to game search results. Skip them.
- **Snippets lie** — Google's featured snippet is sometimes wrong. Always scrape the actual page.
- **Date drift** — a 2018 article republished in 2024 still says 2018 things. Always check publication date, not "X years ago".
- **Archive.org fallback** — if a page is 404, try `https://web.archive.org/web/*/<url>` via `web_research_fetch`.
- **Skipping images** — the most falsifiable claims often live in screenshots and infographics; read them.
