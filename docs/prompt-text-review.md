# Prompt Text Review — Redundancy, Verbosity, Docstring Duplication

> **Status:** audit snapshot (2026-07), covering the org tree at its pre-fold
> location. The tree now lives under `profiles/` and the path abbreviations
> below (`general/AGENTS.md`, `specialists/_demo/AGENTS.md`, `browser.md`, …)
> mean `profiles/…` — **re-verify line references before acting on them.**
> The text-quality findings (Patterns 1–4) remain valid for the surviving
> `profiles/` files; the one tooling change that matters at the fold: the
> browser family migrated to the native `sandbox_browser` MCP server, so the
> browser.md / web-agent.md frontmatter and "30 tools by ref" claims are stale
> — re-check those two files specifically before cutting anything.

A file-by-file, line-referenced pass over **every prompt in the system** (11 org
prompts + 27 subagent definitions), against three problems:

- **(a)** Redefining what the base (`general/AGENTS.md`, flows via `extends:`)
  already gives the CTO.
- **(b)** Verbosity — internal duplication, meta-commentary, redundant framing.
- **(c)** Describing **tool/agent mechanics in prose** when LangChain injects the
  **docstring** to define the tool — and injects the agent **`description:`** to
  define the agent to its caller. The base already says this
  (`general/AGENTS.md:21-22`: *"Each tool's own description says when + how to
  use it — read it; don't re-derive behavior from here."*).

Goal: **clean + minimal. Models should know what to do from the smallest focused
prompt.**

---

## The two prompt audiences (VERIFIED in code — the governing fact)

There are **two distinct system prompts**, and the (a)/(b)/(c) rules apply
differently to each. This reframes part of the agent-level analysis.

**1. The CTO / supervisor prompt** = `general/AGENTS.md` base (flows first via
`extends:`) + the org's overlay. **This audience GETS the base** — so org-level
restatements of Operating Principles / Orchestrator pattern ARE redundant.
→ **Org-level (a) findings below are valid.**

**2. The subagent prompt** = deepagents' 2-line `DEFAULT_SUBAGENT_PROMPT`
(`deepagents/middleware/subagents.py:244` in the workspace venv — *"you have a
number of standard tools; the caller sees only your final message; ensure your
final response contains the complete answer"*) **plus the agent's `.md` body.**
**Subagents do NOT receive the org base.** The `.md` body is the subagent's
entire prompt beyond those 2 lines. So an agent body restating "verify, read
files back" is **not** redundant with the base (the subagent never saw it) — but
it *is* redundant across the agents that each copy the same paragraph.
→ **Agent-level findings are reclassified below (mostly (c)/(b), not (a)).**

**The routing signal (the headline, from your note):** every subagent's
`description:` frontmatter is joined into `"- {name}: {description}"` and
injected **verbatim** into the **`task` tool's own description**
(`subagents.py:590,594`) — that is what the CTO reads to pick which subagent to
delegate to. **The agent `description:` is the exact analog of a tool's
docstring.** Implications:
- It must answer *"when do I pick THIS agent?"* — nothing more.
- Verbosity in ONE description adds tokens to **every** CTO turn (it's always
  in the task-tool description), so trimming compounds across the roster.
- The `.md` body (the subagent's own instructions) should **not** restate the
  description — different audiences, both in context.

---

## TL;DR — four systemic patterns are ~80% of the bloat

### Pattern 1 (a) — the duplicated "Operating Rules" block [ORG-level]
**8 of 10 specialists carry a near-verbatim 4–6 line "Operating Rules" block**
that mostly restates the base:

| Repeated rule | Already in base? |
|---|---|
| "Plan first. Restate the task. Identify the deliverable." | ⚠️ implied, **not stated** |
| "Verify, don't assert. Read files back." | ✅ "Verify or die" (`general/AGENTS.md:39-40`) |
| "Fail loudly. Surface errors verbatim." | ✅ "No fallbacks" (`general/AGENTS.md:41-42`) |
| "Be terse. Deliverable, not play-by-play." | ⚠️ implied, **not stated** |
| "Do trivial work yourself; delegate the rest." | ✅ orchestrator anti-patterns (`general/AGENTS.md:135-142`) |

**Fix (highest leverage):** (1) add two lines to the base Operating Principles
— *Plan first*; *Be terse* — so they're stated once; (2) strip every
specialist's "Operating Rules" down to **org-specific rules only** (smp "never
post without approval", twitter "verify session first", game-studio "Ray is the
execution layer", etc.). Net **−30 to −40 lines**, zero info lost.

### Pattern 2 (c) — tool mechanics re-described despite docstrings [BOTH]
Native tool mechanics (`execute` runs a shell command; `read_file`/`grep`/`glob`
locate; `browser_drag` html5-vs-physics) re-stated in prose despite the tool
docstrings the model already sees. Worst offender: `browser.md` "Advanced
interactions" (~40 lines re-documenting 30 browser tools by name).

### Pattern 3 (b) — internal / cross-file duplication [BOTH]
Files say the same thing 2–3× *within themselves* (video-renderer
Pitfalls≈Troubleshooting≈Rules; telegram Honesty Rules≈Operating Rules) or across
siblings (the same native-tool paragraph in 4 agents; the VNCCS pattern in 3
game-studio agents).

### Pattern 4 (c′) — agent `description:` is the routing signal; 3 are too long [AGENT-level, NEW]
Per the Mechanism section, `description:` is injected into the task-tool
description the CTO sees **every turn**. Most are crisp; **3 enumerate
capabilities instead of routing:**
- **web-search.md** (~85 words / 5 sentences) — could be ~1-2.
- **browser.md** (~70 words, lists ~14 capabilities) — "browses/interacts with
  live web via Chrome; returns structured findings."
- **explorer.md** (~60 words; "first step of the happy path" is framing the CTO
  already has) — trim to role + output.

The other ~24 are well-sized (see audit below).

---

## Org-level prompts (11 files) — CTO audience, gets the base

### `general/AGENTS.md` (the base — 142 lines) — reference, mostly good
- **Add** "Plan first" + "Be terse" to Operating Principles (enables Pattern 1).
- **Minor (b):** `### The default roster (general)` hand-writes
  explorer/researcher/browser/web-search with descriptions — but deepagents
  **also** injects those `description:` strings into the task-tool description.
  The CTO sees roster blurbs twice. Trim the base version to one line of
  *orchestration framing* ("explorer maps territory first; hand its report to
  workers"), let the task-tool carry the per-agent description.
- L7-10 ("This is the base org prompt… a streamlined collection of elements") is
  meta-commentary about the architecture — useful to a repo reader, mild runtime
  noise. *(judgment call)*
- L20-22 correctly enforces the (c) principle — keep as the anchor.

### `specialists/_demo/AGENTS.md` (29 lines) — almost entirely base-restatement (a)
- L8-21 "Operating rules" → every rule is in the base. **Cut to nothing.**
- L23-29 "Delegation" → **verbatim duplicate** of base delegation mechanics
  (`general/AGENTS.md:57-60`). Cut entirely.
- Could be ~5 lines: "You are the CTO of a demo org. Do the trivial parts,
  delegate the rest to the roster."

### `specialists/orchestrator/AGENTS.md` (27 lines) — mostly good, minor (a)
- L17-27 "Operating Rules": 3/4/5 (Verify/Fail loudly/Be terse) restate base.
  Keep rule 1 (Plan → task-planner) + rule 2 ("Inherited agents are
  first-class" — genuinely useful extends-behavior note).

### `specialists/invest/AGENTS.md` (120 lines)
- L88-96 "Operating Rules" → **all 4 lines restate the base.** Cut the section.
- Mission / "What This Org Is Not" / Bootstrap — org-specific, keep.

### `specialists/video-production/AGENTS.md` (144 lines) — internal duplication (b)
- L95-109 "Operating Rules": 1-4 restate base. **Rule 5 ("treat every request as
  finished deliverable") duplicates Mission L10-12; rule 6 ("source-grounded
  claims") duplicates Standards L16.** Cut 1-4 (base), cut 5-6 (already above).
- L14-24 "Standards" vs L123-131 "Delivery Standards" overlap heavily — merge.

### `specialists/social-media-pipeline/AGENTS.md` (102 lines)
- L84-95 "Operating Rules": 1/2/4/5 restate base. **Keep only rule 3 ("never post
  without explicit approval")** — the org-defining rule.

### `specialists/twitter-agent/AGENTS.md` (125 lines) — stale cruft + (b)
- L101-111 "Operating Rules": 1/3/4/5 restate base. Keep rule 2 ("verify session
  before posting").
- **L61-62 "VNC is mentioned in old prompts… ignore it"** — stale guidance about
  prompts that no longer exist. **Delete.**
- **L92-99 "Schedules"** — meta-commentary about what the harness "doesn't carry
  forward." Implement or delete; as-is it's noise.

### `specialists/telegram-agent/AGENTS.md` (117 lines) — duplicated section (b)
- **L82-94 "Honesty Rules" ≈ L96-103 "Operating Rules."** Consolidate into ONE
  org-specific rules section (check session first / don't log full bodies /
  idempotent notes / rate limits / confirm unknown handles). Generic lines → base.

### `specialists/deep-research-engine/AGENTS.md` (210 lines)
- L169-181 "Operating Rules": 1/4/5/6 restate base. Keep rule 2 ("query DB
  first") + rule 3 ("do trivial gathering yourself") — both org-specific (and
  rule 2 already in Principles L95; the two sections overlap).
- Temporal Awareness, Tool Failure Resilience, Sandbox scripts/endpoints —
  org-specific and valuable. Keep.

### `specialists/game-studio/AGENTS.md` (192 lines)
- L148-164 "Operating Rules": 1/3/4/5 restate base. Keep rule 2 ("do trivial work
  yourself") + rule 6 ("Ray is the execution layer").
- L177-186 "Directives (preserved from prior MANIFESTO)" overlaps Operating Rules
  (Ray-is-execution-layer at both L163 + L183) + Path Discipline. Dedup.

### `specialists/coder/AGENTS.md` (160 lines) — internal repetition (b), mild (c)
- No "Operating Rules" block (good). But:
- **"Never retry the same edit/command more than twice" stated 3×**: L37
  (RECOVER), L149-150 (Error recovery), L151 (→ ESCALATE). State once.
- L18 "Should work is banned" restates base. Cut. L156-160 "Voice" = base "Be
  terse" once added. Cut/fold.
- L62-83 "Tool use" — per-tool mechanic blurbs duplicate docstrings **(c)**;
  **keep the sequencing rule** ("locate→read→edit→verify"; "smallest edit") —
  that's cross-tool workflow, the legitimate carve-out.
- Operating modes / risk tiers / quality standards — org-specific and strong. Keep.

---

## Subagent definitions (27 files) — NO base; 2-line default + the `.md` body

> **Reclassification (per Mechanism):** subagents get no base, so agent-body
> duplication is mostly **(c) docstring-duplication** and **(b) intra-roster
> duplication**, NOT (a) base-restatement. The `description:` frontmatter is the
> routing signal (Pattern 4). Keep descriptions crisp; let the body carry role +
> workflow, leaning on tool docstrings for mechanics.

### `description:` audit (the routing signal — Pattern 4)

| Agent | Verdict |
|---|---|
| task-planner, invest-researcher, invest-trader, dre-synthesizer, dre-auditor, dre-writer, video-scriptwriter, video-renderer, smp-writer, twitter-drafter, telegram-drafter, code-worker, web-agent, coder-explorer, game-studio-{creative,renderer,technical-artist,gameplay-programmer,qa-tester,narrative-designer,docs-writer}, researcher | ✅ **crisp** — 1-2 sentences, role + output. Leave. |
| **web-search.md** | ❌ too long (~85 words). Trim to role + when-to-pick + how it differs from browser/researcher. |
| **browser.md** | ❌ too long (~70 words, enumerates ~14 capabilities). Trim to "browses/interacts with the live web via a persistent Chrome; returns structured findings, never raw HTML." |
| **explorer.md** | ❌ too long (~60 words; "first step of the happy path" is CTO framing). Trim to role + output. |
| game-studio-design-researcher | ⚠️ borderline ("under-400-word reports" is output spec, fine; keep). |

### The native-tool re-explanation ×4 (b intra-roster + c) — `explorer.md`, `researcher.md`, `coder-explorer.md`, `code-worker.md`
Each opens with ~the same paragraph: workspace at `/sandbox/workspace/`; tools
are `execute`/`read_file`/`glob`/`grep`/`pux_sandbox_python`; read-only in intent.
- **Tool mechanics** ("`execute` runs a shell command", "`read_file` reads…",
  "`glob`/`grep` locate") = **docstrings the subagent already sees → cut (c).**
- **"Read-only in intent / no writes"** = the agent's **role** → keep (scopes it).
- **Workspace path `/sandbox/workspace/`** = operational fact the subagent needs
  (it's *not* in the base for them; the constant still lives in
  `src/sandbox/exec.py`). Keep one line where the agent does path work — but
  verify the native tool docstrings don't already state the mount; if they do,
  cut from all bodies.
- **Fix:** one line each: *"Read-only investigator. Workspace at
  `/sandbox/workspace/`. No writes/edits."* (−~4 lines × 4 files.)

### `browser.md` (137 lines) — biggest (c) offender [body, valid]
The frontmatter declares **30 browser tools by `ref:`** (L5-35) — each with its
own docstring the subagent sees — then the body re-describes them:
- L44-61 "autopilot loop" — SoM cross-tool contract is legit (keep "act→observe
  →decide", "numbers are the handles", "recompute after page change").
- L63-81 "Heuristics" — mixed; keep the cross-tool bits ("prefer SoM index",
  "refresh after page change"), cut the tool-mechanic bits ("downloads take a
  direct file URL", `save_session`/`restore_session`).
- **L83-121 "Advanced interactions" — ~40 lines of pure tool mechanics**
  (`browser_drag` strategies, `browser_hover`, `browser_press` key list,
  `browser_click_at`, `browser_scroll_into_view`, `browser_a11y`, `browser_iframe`).
  **The clearest (c) violation in the system.** Cut to nothing (or 2 lines
  pointing at the docstrings). 137 → ~70 lines, no capability lost.

> ⚠️ At the 2026-08 fold the browser tools moved behind the `sandbox_browser`
> MCP server — **re-check the current `browser.md` frontmatter** (likely
> `capabilities: [{kind: mcp, ref: sandbox_browser}]` + far fewer declared
> tools) before applying the L5-35/L83-121 cuts; the body-level duplication
> finding stands regardless.

### `web-agent.md` (101 lines) — duplicates browser.md (b + c)
- L37-41 + L62-75 re-explain the same SoM/scroll/selector guidance as
  `browser.md`. web-agent's unique value is the *verify/assert/test-report*
  framing (L42-60, L77-91) — keep that, cut the duplicated browser mechanics.

### `game-studio-design-researcher.md` (55 lines) — (c) + duplicate section
- **Two "Tools" sections:** L37-41 and L49-55, both describing the same
  `research`/`scrape`/`crawl` + `analyze_image`/etc. MCP tools. Merge to one;
  defer mechanics to docstrings. L49-55 "same as Pi's research tools" — stale
  cross-era reference.

### `game-studio-technical-artist.md` (148 lines) — heavy internal + cross-file dup (b)
- **VNCCS pattern in 3 agent files**: here L112-119, `game-studio-creative.md`
  L80-93, `game-studio-renderer.md` L94-107. Pick one canonical home (renderer or
  a skill), point the others at it.
- L63-78 "Architecture" ASCII + L80-86 "Key Files" restate the Workflow (L19-43).
- L129-136 "Rules" duplicates L51-55 "Boundaries" (don't modify nodes/scenes).
- L122-127 "Quality Gate" lists MCP tools by name — borderline (c); keep the
  *intent* ("self-review via vision, reject artifacts").
- **Path drift:** references `departments/art/configs/` while creative/renderer
  use `art/configs/` — reconcile.

### Other game-studio agents — mostly tight
- `creative.md` L95-106 "Quality Gates" duplicates the manifest-schema example
  L59-62. `qa-tester`, `gameplay-programmer`, `docs-writer` reference skills
  correctly and are well-scoped. `narrative-designer.md` L45-49 "Tools" is
  docstring territory; keep the persona/tone content.

### `dre-synthesizer.md` / `dre-auditor.md` / `dre-writer.md`
- Each opens with a "Path Discipline" stanza (`/sandbox/workspace/`) — keep ONE
  line (subagent needs it), but it's repeated; verify tool docstrings.
- `dre-writer.md` L113-127 "Anti-patterns" (15 lines) overlaps per-channel rules
  (L30-39) + self-edit pass (L74-79). Tighten.
- `dre-auditor`'s 7-check SQL is the org's core IP — keep verbatim.

### `invest-researcher.md` / `invest-trader.md`
- Describe backbone **scripts** (`fetch_data.py`, `trade.py`) + their stdout
  contract — **NOT (c)** (scripts have no docstring the model sees; naming them
  is legitimate). Keep. L55-58 "Path Discipline" → one line / defer.

### `video-renderer.md` (157 lines)
- **L111-127 "Pitfalls" ≈ L128-137 "Troubleshooting" ≈ L138-146 "Rules"** — three
  sections, same content (venv-first, MathTex, OOM, sync drift, Kokoro). Collapse
  to ONE (−~20 lines). `video-scriptwriter.md` is tight.

### `smp-writer.md` / `twitter-drafter.md` / `telegram-drafter.md`
- Well-scoped drafting agents. Minor (b): each ends with Output + Stop Conditions
  + Anti-patterns that overlap ("you draft, CTO posts" appears in Anti-patterns
  AND the opener). One mention each.

---

## Recommended consolidation summary

| Action | Where | Approx savings |
|---|---|---|
| Add "Plan first" + "Be terse" to base; strip from 8 orgs | `general/AGENTS.md` + 8 specialists | −30 to −40 lines |
| Cut `_demo` Operating rules + Delegation (all base) | `_demo/AGENTS.md` | −18 lines |
| Collapse `browser.md` "Advanced interactions" → docstrings | `browser.md` | −40 lines |
| Trim 3 verbose `description:` (web-search/browser/explorer) | 3 agent frontmatters | −~150 words/CTO-turn |
| Replace 4× native-tool paragraphs with one line each | explorer/researcher/coder-explorer/code-worker | −16 lines |
| Collapse video-renderer Pitfalls≈Troubleshooting≈Rules → one | `video-renderer.md` | −20 lines |
| Dedup VNCCS across 3 game-studio agents → one canonical | game-studio agents | −20 lines |
| Cut stale VNC/schedules meta-commentary | `twitter-agent/AGENTS.md` | −10 lines |

**Rough total: −170 to −200 lines + ~150 words/turn off the task-tool
description, no capability lost** — every cut is a base-restatement, a
docstring-duplication, an internal duplicate, or an over-long routing signal.

---

## Judgment calls left for your manual pass
- Voice/tone of org-specific prose (coder's operating modes, narrative-designer's
  persona, dre's principles) — strong writing, your call on trimming.
- Whether endpoint/export blocks (dre, game-studio) move to a skill vs. stay in
  the prompt — a packaging decision, not a text-cleanup one.
- The base's meta-commentary about the architecture (L7-10) — useful to repo
  readers, mild runtime noise.

---

## Open question for you
The deep pass (this doc) is done per the "teamwork" split — your turn to do the
manual rewrite. Want me to **execute Batch 1** (the unambiguous mechanical cuts:
add the 2 base lines, strip the 8 "Operating Rules" blocks to org-specific-only,
trim the 3 verbose `description:`s) and prove it green with the guards + kit
suites before you take over for voice work? Or keep this purely as a reference
doc and you drive all edits?
