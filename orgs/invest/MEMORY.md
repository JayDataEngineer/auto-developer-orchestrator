# Invest Org — Project Memory

**Canonical location**: `orgs/invest/` (in auto-developer-orchestrator repo)
**Deployed to**: `~/Documents/programs/dev/invest/` (via `bootstrap.sh`)

## Why this org exists

The kernel provides a generic agent loop. The **invest org** specializes it into a multi-asset investment research + paper-trading division. It is configured, not coded — YAML + Markdown + Python sandbox scripts. No Go code touches this org.

## Architecture

```
invest/
├── pux.yaml                # Org manifest (skills_dir, schedules)
├── MANIFESTO.md            # CTO overlay — multi-asset strategist loop
├── MEMORY.md               # THIS FILE — org-scoped agent memory
├── bootstrap.sh            # Idempotent deploy script
├── Dockerfile              # Standalone container build
├── docker-compose.yml      # Optional containerized run
├── requirements.txt        # Python deps (uv-managed)
├── roles/                  # 13 roles (3 heads + 10 specialists)
├── skills/                 # 13 SKILL.md files (8 baked + 5 queryable)
├── prompts/                # 5 scheduled prompt templates
├── sandbox/                # 14 Python scripts (data fetch, signals, trade, etc.)
└── config/
    └── watchlist.json      # Multi-asset watchlist (stocks + crypto + macro + FRED)
```

## Design Decisions

### 1. Multi-asset, not stocks-only (2026-06-19)
Original invest org was stocks-only via Alpaca paper. Extended to:
- **Stocks**: Alpaca paper, 9:30-16:00 ET weekdays
- **Crypto**: Alpaca crypto paper, 24/7/365 — separate `crypto_hourly` schedule
- **Macro**: FRED + yfinance read-only (^TNX, ^IRX, ^VIX, DX-Y.NYB, GC=F, CL=F)

**Why**: Correlations across asset classes matter. A crypto crash drags tech stocks; rising yields hurt both. Single-asset scanning misses the cross-asset signal.

### 2. Specialists don't get `read_skill` — skills are baked (2026-06-19)
Per `feedback_skills_backbone_vs_peeking.md`: only the CTO gets `read_skill`. Specialist role prompts bake in the core workflow inline. Large reference docs (TECHNICAL_ANALYSIS, FUNDAMENTAL_ANALYSIS, etc.) are duplicated into the relevant role's prompt.md.

**Why**: At scale, progressive disclosure doesn't work — specialists can't be trusted to load the right skill at the right time. Bake the backbone.

### 3. Skills layer (13 SKILL.md)
- **8 baked** (in role prompts): TECHNICAL_ANALYSIS, FUNDAMENTAL_ANALYSIS, MARKET_REGIME, MACRO_ANALYSIS, SEC_FILINGS, NEWS_SENTIMENT, SOCIAL_SENTIMENT, CRYPTO_ONCHAIN, RISK_MANAGEMENT, JOURNAL_PREDICTIONS
- **3 queryable** (CTO via read_skill): MULTI_ASSET_FUSION, OPTIONS_FLOW, CONTEXT_ENGINE_QUERY

### 4. Web MCP wired to 3 specialists
- `news-analyst` — news + social sentiment
- `filings-analyst` — SEC EDGAR via scrape/crawl
- `crypto-analyst` — on-chain + funding rates (mostly public APIs, web MCP for news)
- `researcher` (fallback) — generalist

The `web` MCP server (`backend/cmd/server/app.go:347-348`) provides: `research`, `search`, `scrape`, `extract`, `map`, `crawl`. This is the "special MCP server with all that information."

### 5. World is not ephemeral (2026-06-19)
Per `feedback_persistent_pipeline_state.md`:
- **Predictions** stored in SQLite (`journal.db`) — immutable once recorded
- **Signals** written to `/sandbox/signals.json` — current pending
- **Memos** in `/sandbox/workspace/memos/` — research reports by date
- **alt_data.py cache** wraps web MCP responses (1h TTL default)

The CTO consults [[CONTEXT_ENGINE_QUERY]] before any scan to avoid duplicate work.

### 6. IaC + self-bootstrap (2026-06-19)
Per `feedback_iac_self_bootstrap.md`:
- `bootstrap.sh` — idempotent deploy to target project (sha256 compare, only syncs changed files)
- `Dockerfile` + `docker-compose.yml` — optional standalone container
- Health check: every .py compiles, `fetch_data.py --help` exits 0

**Contract**: org is in repo, deployment target is filesystem. Bootstrap is the bridge.

## Operating Modes

| Mode | Confidence Threshold | Position Size | Use Case |
|------|---------------------|---------------|----------|
| **Lightning** | 0.5 | Default | Quick re-scan, technical-only |
| **Base** (default) | 0.6 | Default | Full pipeline, scheduled scans |
| **Conservative** | 0.75 | Halved | Uncertain regime, post-loss |

Modes are passed via prompt prefix (e.g., "Run a Conservative-mode morning scan").

## Required Environment Variables

- `ALPACA_API_KEY` — paper trading (set in shell, never commit)
- `ALPACA_SECRET_KEY` — paper trading
- `FRED_API_KEY` — macro data (free at fred.stlouisfed.org)

## Schedules

| Schedule | Cron (local) | Role | Purpose |
|----------|-------------|------|---------|
| morning_scan | `30 9 * * 1-5` | research-director | Pre-open full scan |
| midday_review | `0 13 * * 1-5` | research-director | Mid-session risk check |
| eod_scan | `30 15 * * 1-5` | research-director | Pre-close scan |
| eod_snapshot | `0 16 * * 1-5` | execution-manager | Record portfolio snapshot |
| eod_metrics | `30 16 * * 1-5` | execution-manager | Daily metrics |
| crypto_hourly | `7 * * * *` | research-director | 24/7 crypto-only scan |
| macro_weekly | `0 19 * * 0` | research-director | Sunday macro review |

## Lessons Learned

### More indicators ≠ better signals (Layer 7 vs Layer 8)
The invest project went through ~8 iterations of adding indicators. Adding the 9th oscillator didn't improve win rate — it added noise. The winning approach was **multi-signal fusion with weighted confidence** (see `TECHNICAL_ANALYSIS.md`), not "more indicators."

### Specialists need explicit stop conditions
Without stop conditions, the research-director loops forever analyzing edge cases. The MANIFESTO defines 4 stop conditions: critical risk alert, no actionable signals (composite < 0.5), market closed, ambiguous regime.

### The journal is the source of truth
Without recording predictions BEFORE execution, you can't measure accuracy. The reporter's first job every EOD is to evaluate open predictions and update the journal.

## Related Memory Files

- `~/Documents/programs/dev/invest/MEMORY.md` — invest project (deployed target) memory; points back here
- `~/.claude/projects/-home-ubuntu-Documents-programs-dev-auto-developer-orchestrator/memory/feedback_persistent_pipeline_state.md`
- `~/.claude/projects/-home-ubuntu-Documents-programs-dev-auto-developer-orchestrator/memory/feedback_org_level_cto.md`
- `~/.claude/projects/-home-ubuntu-Documents-programs-dev-auto-developer-orchestrator/memory/feedback_skills_backbone_vs_peeking.md`
- `~/.claude/projects/-home-ubuntu-Documents-programs-dev-auto-developer-orchestrator/memory/feedback_iac_self_bootstrap.md`
