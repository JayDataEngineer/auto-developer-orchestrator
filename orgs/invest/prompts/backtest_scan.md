You are running a historical backtest walk for the Investment Division.

## Purpose
Time-travel test: for each date in the walk, fetch a historical market snapshot, generate signals, record them, and (at the end) evaluate all predictions against actual subsequent price action.

**Tightly segmented, focused, fast.** Each date is its own transaction. The user must see progress after every date.

## Inputs
The prompt message contains:
- `dates`: comma-separated list of YYYY-MM-DD (e.g., "2026-04-01,2026-04-08,2026-04-15")

If only a single `date` is given, run one date and skip the walk machinery.

## Pre-flight (once)
```bash
python3 sandbox/walk_progress.py init --dates "{dates}"
```

## Per-date loop
For each date in `{dates}`:

### 1. Start progress
```bash
python3 sandbox/walk_progress.py start-date --date {date}
```

### 2. Fetch historical snapshot + auto-generate research plan
```bash
python3 sandbox/backtest.py --date {date}
```
This writes:
- `data/backtest/snapshot_{date}.json` — prices + indicators as-of that date, plus `research_plan_path` pointer
- `data/research_plan_{date}.json` — SEC EDGAR URLs (date-scoped), Yahoo Finance news URLs, research queries (7-day lookback window)

Both are produced automatically — you do NOT need to invoke `historical_research.py` separately.

### 3. Delegate to research-director in Backtest mode
Use `delegate_to` (synchronous — you want the result before moving on):
> **Backtest mode**: analyze the snapshot at `data/backtest/snapshot_{date}.json`. Refresh signals via `python3 sandbox/signals.py rank` and `python3 sandbox/regime.py detect`. Read the research plan at `data/research_plan_{date}.json` — pick at most 2 tickers for a single `delegate_async` to news-analyst (or skip if you judge news won't change the call). Save signals to `data/signals.json`. Yield a 100-word context note as `backtest_{date}_note.md`. Hard cap: 8 rounds.

### 4. Record signals for backtest scoring
For each signal in `data/signals.json`:
```bash
python3 sandbox/backtest.py --record-signal "SYMBOL,ACTION,CONFIDENCE,{date}"
```
Each `--record-signal` call auto-updates `walkthrough_progress.json` with the running signal count for that date — no need to manually call `walk_progress.py complete-date`.

### 5. Mark date complete
```bash
python3 sandbox/walk_progress.py complete-date --date {date} --news {N} --filings {N}
```
Signals count is already tracked via `--record-signal`. Only `--news` and `--filings` need manual counts.

**Failure handling:** if any step errors, run:
```bash
python3 sandbox/walk_progress.py fail-date --date {date} --reason "short description"
```
…and continue to the next date. Do NOT abort the walk on a single date failure.

## Post-walk (once)
```bash
python3 sandbox/walk_progress.py summary
python3 sandbox/backtest.py --evaluate
python3 sandbox/backtest.py --report
python3 sandbox/historical.py run --months 3
python3 sandbox/historical.py compare
```

## Output
Return a walk report with:
1. **Walk Summary** — dates processed, signals per date, duration per date, total wall-clock
2. **Evaluation** — precision/recall at each horizon (1d, 5d, 21d), calibration curve
3. **Strategy Performance** — 3-month walk-forward: return, Sharpe, win rate vs SPY
4. **Per-date Highlights** — best call, worst call, surprise event detected via news
5. **Fresh Research Notes** — what the news-analyst surfaced for each date (one bullet per date)

## Path Discipline
- All sandbox scripts invoked as `python3 sandbox/X.py` from the project root
- Data files live at `data/`
- The walk_progress file lives at `data/walkthrough_progress.json` — query it any time with `python3 sandbox/walk_progress.py summary` to see ETA
