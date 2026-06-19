# RESEARCH_WEB_WORKFLOW

The web-researcher's checklist. Loaded automatically into the web-researcher's prompt via skills_dir.

## Tool reference (web MCP)

| Tool | When | Cost |
|---|---|---|
| `mcp__web__research` | Default. Searches + scrapes top 3-5 results in one call. Use for most queries. | Moderate (3-5 page fetches per call) |
| `mcp__web__search` | When you only need titles+snippets (e.g., "does this topic have a name?"). | Cheap |
| `mcp__web__scrape` | When you have a specific URL and want clean markdown. | Cheap |
| `mcp__web__map` | When you need to discover URLs from a domain's sitemap. | Cheap |
| `mcp__web__crawl` | Deep crawl following links. Use sparingly — `max_depth=2, max_pages=20` defaults. | Expensive |
| `mcp__web__extract` | Structured JSON extraction (products, news, jobs). Use when scraping a known schema. | Cheap |
| `mcp__web__docs_list_sources` / `docs_fetch_docs` | When the topic is documented in a known llms.txt source. | Cheap |

All web tools accept `data:` URIs for inputs and return markdown.

## Workflow

### Step 1 — Decompose the question
Restate the user's research question in your own words. Then list 3-5 sub-questions. If you can't decompose, you don't understand the question — ask the CTO.

**Example:**
- User: "What's the deal with the White Lives Matter Montana Telegram channel?"
- Sub-questions:
  1. Who runs @WLM_USA_MONTANA and what's their stated platform?
  2. What do watchdog groups (ADL, SPLC) say about this channel?
  3. Has the channel been linked to real-world incidents?
  4. What's Telegram's policy on extremist channels and have they enforced it here?

### Step 2 — Search per sub-question
Call `mcp__web__research` with each sub-question as the `query` parameter. Default `max_results=3, depth=quick`. For load-bearing sub-questions, escalate to `depth=deep` and `max_results=5`.

```python
# Conceptual — the actual call is via the MCP tool interface
mcp__web__research(query="Who runs the @WLM_USA_MONTANA Telegram channel", max_results=3, depth="quick")
```

### Step 3 — Deepen on load-bearing sources
For each source that you'll cite, follow up with `mcp__web__scrape` on the URL. This gets you the full page text, not just the snippet. The snippet is often misleading.

### Step 4 — Cross-check
For each claim, find ≥2 independent sources. "Independent" means:
- Not both quoting the same press release
- Not both owned by the same parent company
- Not both citing each other in a circle

If you find only echo-chamber sources, note that in `_INDEX.md`.

### Step 5 — Record findings (markdown + SurrealDB source record)

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

**(b) SurrealDB source record** via `surreal_client.py save-source`:

```bash
python3 /sandbox/surreal_client.py save-source \
  --kind web \
  --url "https://example.com/article" \
  --title "Article Title" \
  --author "Author Name" \
  --published-at "2026-06-15" \
  --accessed-at "$(date -u +%Y-%m-%d)" \
  --content "$(cat artifacts/research/finding-slug.md)" \
  --topic-ids "topic:abc123" "topic:def456" \
  --person-ids "person:xyz789"
```

This atomically:
1. Embeds the content (1024-dim via Ollama mxbai-embed-large)
2. INSERTs a `source` record (idempotent on URL — re-runs UPDATE)
3. RELATEs each topic_id / person_id via `extracted_from` edge
4. Returns `source_id` for downstream use

**When to link topic_ids**: if your finding already maps to an existing topic, link it. If you discovered a new theme, defer linking — the synthesizer or CTO will create the topic later. Don't fabricate topic IDs.

**When to link person_ids**: only when the source directly discusses or quotes a person. If the person doesn't exist in the DB yet, create them via `upsert-person`:

```bash
# Creates person if missing; returns existing ID if found. Idempotent on canonical_name.
python3 /sandbox/surreal_client.py upsert-person \
  --name "Elon Musk" \
  --source-id "source:abc123" \
  --role "subject" \
  --notes "CEO of SpaceX, quoted in this article about Starship"
```

Only create persons for individuals the source is **about** or **quotes directly** — not every proper noun. Use your judgment.

### Step 6 — Build the index
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
- **Archive.org fallback** — if a page is 404, try `https://web.archive.org/web/*/<url>` via `mcp__web__scrape`.
