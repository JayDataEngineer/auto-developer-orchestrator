# DOCS_AUTHORING

How to write docs for the Tech Noir docs-site at `~/Documents/programs/creative/tech-noir/docs-site/`.

## Stack

- **Next.js 15** (App Router) + React 19
- **MDX** via `@next/mdx` — pages are `.mdx` files
- **Tailwind CSS 4** (new `@theme` syntax, NOT v3 `theme.extend`)
- Custom search via `Fuse.js` (no Algolia, no Pagefind)
- Custom design system — NOT shadcn, NOT Nextra

## Where Pages Live

```
docs-site/src/app/docs/<category>/<page>/page.mdx
```

Examples:
- `src/app/docs/pipeline/forge/page.mdx`
- `src/app/docs/design/player-movement/page.mdx`
- `src/app/docs/architecture/decisions/page.mdx`

## Design Tokens (RESPECT THESE)

Read `docs-site/src/app/globals.css` for the source of truth. Key rules:

| Token | Value | Use |
|-------|-------|-----|
| Border radius | **0 everywhere** | Never write `rounded-*` classes. Square corners only. |
| Headline font | Space Grotesk | `<h1>`, `<h2>`, page titles |
| Body font | Inter | Default body text |
| Mono font | Fira Code | Code blocks, inline code |
| Primary palette | Custom surface variants | Use CSS variables — don't hardcode hex |
| Utility class | `blueprint-grid` | Diagram backgrounds (subtle grid) |
| Utility class | `glass-header` | Sticky headers with backdrop blur |
| Utility class | `ghost-border` | Callout borders (low-opacity) |

## Frontmatter

The existing site does NOT use frontmatter. Titles come from `<h1>` in the MDX body. Don't add `---title:...---` blocks — they won't break anything but they're inconsistent with the rest of the site.

## MDX Patterns That Work

### Basic page

```mdx
# Forge Art Pipeline

Intro paragraph.

## How It Works

Body content with **bold** and `inline code`.

```python
# Code blocks work
forge_client.generate("prompt")
```

| Column | Value |
|--------|-------|
| A      | 1     |
```

### Embedding React components

Existing `mdx-components.tsx` provides styled versions of standard HTML tags. You can also import components:

```mdx
import { Callout } from '@/components/Callout'

<Callout type="warning">
Don't run Forge without health check first.
</Callout>
```

Check `docs-site/src/components/` for what's available before inventing new components.

## Workflow for Documenting a Feature

1. **Read the source.** Use `file_read` and `file_glob` to understand the code. Don't guess — read.
2. **Read 2-3 existing docs pages** to absorb the tone and structure.
3. **Propose an outline** in `/sandbox/workspace/docs/outline.md` — sections + 1-line summary of each.
4. **Write the MDX** at the target path. Respect design tokens.
5. **Update `Search.tsx`** (`docs-site/src/components/Search.tsx`) — add the new page to the `docsIndex` array with title + category + path. Without this, the page is unreachable via search.
6. **Verify** by checking the file parses (no MDX syntax errors).

## What NOT to Do

- Don't add frontmatter (`---` blocks)
- Don't use shadcn components or `class:` trailing modifiers
- Don't import from `nextra/components` — this isn't Nextra
- Don't add rounded corners (the site explicitly zeros them via `* { border-radius: 0 }`)
- Don't add a sidebar entry manually — sidebar is auto-derived from the file tree
- Don't use `@/app/*` imports; use `@/components/*` for shared UI

## Tone

Match the existing pages: terse, technical, no marketing fluff. The audience is engineers and artists on the studio team. Headers in Title Case. Code blocks tagged with language.

Bad: "Welcome to the amazing Forge pipeline! In this comprehensive guide, we'll explore..."
Good: "Forge drives art generation on the Ray cluster. Health-check before each call."

## When in Doubt

Read an existing page that does the thing you're trying to do. The site is consistent — match the pattern.
