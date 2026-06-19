# WRITE_SUBSTACK_ARTICLES

The substack-writer's checklist. Loaded automatically into the substack-writer's prompt via skills_dir.

## Anatomy of a Substack article

| Section | Words | Purpose |
|---|---|---|
| **Lede** | 100-300 | Hook the reader. Concrete, specific. No "In recent years…". |
| **Body** | 1200-2800 | The argument. Divided into 3-6 sections of 200-500 words each. |
| **Kicker** | 50-150 | Land the punch. Not a summary. |
| **Footnotes** | — | Every load-bearing claim cited. |

Target: **1500-3500 words total**. Sweet spot: 2000.

## Workflow

### Step 1 — Read the brief
Open `artifacts/brief.md`. Note the bottom line, the key claims, the conflicts, the open questions. Your article needs to take a position — Substack isn't Wikipedia.

### Step 2 — Decide your thesis
Write one sentence: "This article argues that ___." If you can't fill in the blank, you don't have an article yet. Tell the CTO.

**Bad theses:**
- "This article explores the topic of X." (No argument.)
- "X is a complex issue with many facets." (No argument.)
- "X is important." (No argument.)

**Good theses:**
- "The Telegram dump shows doxing operations rely on a single unverified source, and that source has a documented history of deception."
- "Three of the four named subjects in the chat cannot be independently verified, and that should change how the dump is reported."

### Step 3 — Outline
Write `artifacts/article_outline.md`:

```markdown
# Outline — <working title>

## Thesis
<one sentence>

## Lede
<concrete opening: a quote, a number, a scene>

## Section 1: <heading>
- Point A (cite [1])
- Point B (cite [3])

## Section 2: <heading>
- ...

## Kicker
<what's the last line?>

## Footnotes
- [1] ...
- [3] ...
```

Review the outline before drafting. Does each section earn its place? Cut anything that's "interesting but not load-bearing."

### Step 4 — Draft
Write `artifacts/article.md`. Markdown format. Footnotes as `[^N]` markers in text, defined at the bottom:

```markdown
# <headline>

<lede — 2-3 paragraphs, no sub-header>

## <section 1 heading>

<body — claim with [^1] marker>

...

## <last section>

<kicker paragraph>

---

[^1]: <author>, "<title>", <publication>, <date>, <URL>.
[^2]: ...
```

### Step 5 — Self-edit
Read the draft. Cut:

```bash
# Common AI-tells — grep for these and rewrite
grep -in "delve\|navigate the\|in today's\|it is worth noting\|needless to say\|in recent years\|it's important to note" artifacts/article.md
```

- First paragraph (often throat-clearing — start at paragraph 2)
- Adverbs: "really", "very", "quite", "rather"
- Passive voice when active is available
- Any sentence that doesn't advance the argument or provide necessary context

### Step 6 — Citation audit
Every load-bearing claim should have a footnote. To check:

```bash
# Count footnote markers vs footnote definitions
grep -oE '\[\^[0-9]+\]' artifacts/article.md | sort -u | wc -l   # markers
grep -oE '^\[\^[0-9]+\]:' artifacts/article.md | wc -l           # definitions
```

Numbers should match. Each definition should point to a real source from the brief.

### Step 7 — Headline options
Write 3 headline options. Substack headlines that work:

| Pattern | Example |
|---|---|
| Concrete noun + tension | "The Antifa Doxing Cell That Wasn't" |
| Question with non-obvious answer | "Why Does Flathead County Have So Many Militias?" |
| Specific number | "Three Dossiers, One Source" |
| Verbal noun + object | "Hunting the Wrong People" |

**Avoid:**
- "Thoughts on X"
- "Reflections on Y"
- "What X Tells Us About Y" (overused)
- Headlines that ask a question the article doesn't answer

## Stop conditions

- 1500-3500 words
- Clear single-sentence thesis (stated in the article, not just your head)
- Every load-bearing claim footnoted
- No AI-tell phrases
- 3 headline options provided

## Pitfalls

- **Length creep** — if you're at 4000 words, you haven't edited. Cut 30%.
- **Burying the lede** — first paragraph should grab. If it starts with "Recently," or "It is widely known," cut it and start at paragraph 2.
- **Summary ending** — Substack readers skip summaries. End on a specific image, a question, or a turn.
- **Footnote overkill** — don't footnote common knowledge ("The sky is blue[^1]"). Do footnote any specific number, quote, or contested claim.
- **Quote sandwich** — don't introduce a quote, give the quote, then explain the quote. Trust the reader; let the quote land.
- **Substack "house style"** — Substack rewards confident voice, not academic hedging. Take a position; you can footnote your uncertainty.
