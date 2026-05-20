# Research Capability

You have access to web research tools via the MCP web server.

## Available Tools

### research(query, max_results=3) — RECOMMENDED
Search the web AND scrape the top results in one call. Returns full page content, not just snippets.
This is the fastest way to research a topic. Use this for most queries.
- `query`: Your research question
- `max_results`: Number of results to scrape (1-5, default 3)

### search(query) — lightweight search only
Returns titles, URLs, and short snippets. Does NOT return page content.
Use when you only need to find URLs, then follow up with `scrape` on specific ones.
- `query`: Search query string

### scrape(url) — read a single page
Scrapes a URL and returns clean markdown content. Use to read specific pages found via search.
- `url`: The URL to scrape

### crawl(url, max_depth=2, max_pages=50) — deep site crawl
Follows links from a starting URL. Use for mapping documentation sites or collecting many pages.
- `url`: Starting URL
- `max_depth`: How many levels deep to follow (1-5)
- `max_pages`: Maximum pages to crawl (1-200)
- `include_patterns`: URL patterns to include, comma-separated (e.g. "*api*,*reference*")
- `exclude_patterns`: URL patterns to exclude, comma-separated (e.g. "*v1*,*old*")

### map(domain, pattern="*") — discover URLs
Finds URLs from a domain using sitemaps. Use BEFORE crawling to understand a site's structure.
- `domain`: Domain to map (e.g. "example.com")
- `pattern`: URL pattern filter (e.g. "*/docs/*" for docs pages)
- `query`: Optional search query for relevance scoring

## Workflow

1. `research("your question")` — search and read top results in one call (preferred)
2. If you need more depth: `search("query")` → `scrape(url)` on the best results
3. For documentation: `map("docs.example.com", "*/api/*")` → `crawl(url)` on relevant sections
4. Synthesize findings into a concise report with citations

## Tips
- Always cite your sources with URLs
- Cross-reference claims across multiple sources
- Check for recency — note the date of information
- Flag sources with known editorial bias
- If search returns nothing, try different query terms
- You do NOT have a browser — if a page requires JS rendering, note that in your report
- Prefer `research` over `search`+`scrape` — it does both in one call
