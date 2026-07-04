# FACT_CHECK_ARTICLES

Use when the user asks to "fact-check", "verify", "is this true?", or otherwise audit a published article (Substack, blog, news). The article may have text + images + embedded media that all need cross-referencing.

## Goal

For each load-bearing factual claim in the article, return one of: **VERIFIED** (multiple independent sources confirm), **CONTESTED** (sources disagree), **UNVERIFIABLE** (no primary source found), or **MISLEADING** (technically true but framed to mislead). Quote the claim, link the sources, show your work.

## Workflow

### Step 1 — Scrape the article

Fetch the article URL with the web-mcp single-page fetch tool. If it returns thin content (JS-rendered, paywalled, or image-heavy Substack layout), fall back to a research-mcp combined search using the article's title + author + key phrase to triangulate the claims via other coverage.

### Step 2 — OCR every image

Articles often bury claims in screenshots, embedded tweets, infographics, and document scans. The scraper misses these. **For every `<img>` in the article**, use the right media-mcp tool for the image kind:

- **Documents, screenshots, scans** (tweets, court filings, charts with labels) → OCR-flavored tool. Extract verbatim text.
- **Photos** (people, places, scenes) → caption-flavored tool. Note identifiable people, locations, date clues.
- **Mixed** → run both, in that order.

Add the extracted text/transcript to your claim list. Images often contain the most-checkable claims — and they're the first thing a sloppy fact-checker skips.

### Step 3 — Extract claims

From the article text + OCR'd images, build a claim list. Each claim:
- Verbatim quote (or close paraphrase if long)
- What would need to be true for it to be factual
- Type (statistic, identification, causal claim, quote attribution, dated event)

Skip pure opinions ("Eric is a grifter") — those aren't fact-checkable. Focus on:
- Specific numbers ($X raised, N employees, X% market share)
- Dated events ("in March 2024, he...")
- Identifications ("founder of X", "advisor to Y")
- Quote attributions ("he said...")
- Legal/regulatory claims

### Step 4 — Verify each claim

For each claim, run a research-mcp combined search with the claim in question form.

Pull at least 2 independent sources. First-party = the org's own page, a filing, an interview. Third-party = news coverage, regulatory records. **A single Substack quoting another Substack is not verification.**

If the combined search doesn't surface anything useful, fall back to:
- A lightweight search (titles+snippets only) on the key entity + key term
- A single-page scrape on a specific result URL

For named individuals, check LinkedIn / company About pages / SEC filings / court records (PACER) / charity registry (Form 990). Many "founder/CEO" claims are verifiable in 1 lookup.

### Step 5 — Verdicts

For each claim, output:
```
CLAIM: "<verbatim quote>" (paragraph N)
VERDICT: VERIFIED | CONTESTED | UNVERIFIABLE | MISLEADING
SOURCES: [URL1, URL2, ...]
NOTE: <one sentence on what you found; if MISLEADING, explain the framing issue>
```

### Step 6 — Overall assessment

Close with:
- Total claims checked, breakdown by verdict
- Strongest finding (most clear-cut verification or refutation)
- Weakest point (where the article's evidence chain breaks down)
- Bias note: is the article's framing consistently one-sided? Does it omit obvious counter-evidence?

## Pitfalls

1. **Don't grade the article on whether you agree with it** — grade it on whether its factual claims hold up.
2. **Don't trust aggregators** — a claim echoed across 5 blogs that all cite the same broken source is still 1 source.
3. **Don't OCR photos of people and call the output a "claim"** — that's image content, not a fact the author asserted.
4. **If the article links to its own sources, follow them** — they may not actually support the claim (very common).
5. **Date checks are easy wins** — if the article says "last week" and the cited event was 8 months ago, flag it.
6. **Quote attributions need the original source** — finding a tweet/video/podcast where the person actually said it. Paraphrased "he said X" with no link is unverifiable.
7. **Image-blind reading is the #1 failure mode** — most fact-checks miss the screenshots. Be the agent that reads the images.
