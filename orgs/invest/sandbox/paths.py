"""Path discovery for invest org sandbox scripts.

Auto-discovers all paths relative to this file's location, so scripts work:
  - On host: invoked as `python3 sandbox/X.py` from project root, or with absolute paths
  - In Docker sandbox: invoked as `python3 /sandbox/X.py` (legacy mount layout still works)

Env overrides (all optional):
  INVEST_PROJECT_DIR  default = parent of sandbox/ dir
  INVEST_DATA_DIR     default = $PROJECT_DIR/data
  INVEST_CONFIG_DIR   default = $PROJECT_DIR/config
  INVEST_CACHE_DIR    default = $PROJECT_DIR/.cache

Plus per-file overrides (SignalsFile, JournalFile, etc.) — see each script.

Usage in a sibling script:
    from paths import SCRIPT_DIR, DATA_DIR, CONFIG_DIR, SIGNALS_FILE
    # or
    import paths
    with open(paths.SIGNALS_FILE) as f: ...
"""
from __future__ import annotations
import os

# This file lives at <project>/sandbox/paths.py
SCRIPT_DIR = os.path.dirname(os.path.abspath(__file__))

# Project root is the parent of sandbox/
_PROJECT_FROM_SCRIPT = os.path.dirname(SCRIPT_DIR)

# Project dir: env override wins, else derived from script location.
# Special case: if running inside /sandbox/ (Docker mount), PROJECT_DIR is /workspace.
if os.path.basename(SCRIPT_DIR) == "sandbox" and SCRIPT_DIR == "/sandbox":
    _DEFAULT_PROJECT = "/workspace"
else:
    _DEFAULT_PROJECT = _PROJECT_FROM_SCRIPT

PROJECT_DIR = os.environ.get("INVEST_PROJECT_DIR", _DEFAULT_PROJECT)

DATA_DIR = os.environ.get("INVEST_DATA_DIR", os.path.join(PROJECT_DIR, "data"))
CONFIG_DIR = os.environ.get("INVEST_CONFIG_DIR", os.path.join(PROJECT_DIR, "config"))
CACHE_DIR = os.environ.get("INVEST_CACHE_DIR", os.path.join(PROJECT_DIR, ".cache"))
MEMOS_DIR = os.environ.get("INVEST_MEMOS_DIR", os.path.join(PROJECT_DIR, "workspace", "memos"))

# Ensure data dir exists (scripts assume they can write here).
try:
    os.makedirs(DATA_DIR, exist_ok=True)
except OSError:
    pass  # read-only filesystem or permission issue — script will fail loudly on write

# Canonical file locations (each can be overridden by env, but defaults are sane).
MARKET_DATA_FILE = os.environ.get("MARKET_DATA_FILE", os.path.join(DATA_DIR, "market_data.json"))
SIGNALS_FILE = os.environ.get("SIGNALS_FILE", os.path.join(DATA_DIR, "signals.json"))
JOURNAL_FILE = os.environ.get("JOURNAL_FILE", os.path.join(DATA_DIR, "journal.json"))
JOURNAL_ARCHIVE = os.environ.get("JOURNAL_ARCHIVE", os.path.join(DATA_DIR, "journal_archive.json"))
JOURNAL_DB = os.environ.get("JOURNAL_DB", os.path.join(DATA_DIR, "journal.db"))
REGIME_HISTORY = os.environ.get("REGIME_HISTORY", os.path.join(DATA_DIR, "regime_history.json"))
REGIME_CONFIG = os.environ.get("REGIME_CONFIG", os.path.join(CONFIG_DIR, "regime_config.json"))
SIGNALS_CONFIG = os.environ.get("SIGNALS_CONFIG", os.path.join(CONFIG_DIR, "signals_config.json"))
RISK_CONFIG = os.environ.get("RISK_CONFIG", os.path.join(CONFIG_DIR, "risk_config.json"))
WALKFORWARD_CONFIG = os.environ.get("WALKFORWARD_CONFIG", os.path.join(CONFIG_DIR, "walkforward_config.json"))
WALKFORWARD_REPORT = os.environ.get("WALKFORWARD_REPORT", os.path.join(DATA_DIR, "walkforward_report.json"))
ALPHA_RESULTS = os.environ.get("ALPHA_RESULTS", os.path.join(DATA_DIR, "alpha_results.json"))
HISTORICAL_RESULTS = os.environ.get("HISTORICAL_RESULTS", os.path.join(DATA_DIR, "historical_results.json"))
WATCHLIST_FILE = os.environ.get("WATCHLIST_FILE", os.path.join(CONFIG_DIR, "watchlist.json"))

# Backtest outputs (snapshots, predictions, scores). Lives INSIDE the org
# workspace so artifacts persist to host via the bind-mount. Override via env
# only when running outside the org (e.g., local dev against /tmp/).
BACKTEST_DIR = os.environ.get("INVEST_BACKTEST_DIR", os.path.join(DATA_DIR, "backtest"))
BACKTEST_SNAPSHOT_FILE = os.path.join(BACKTEST_DIR, "snapshot_{date}.json")
BACKTEST_PREDICTIONS_FILE = os.environ.get("BACKTEST_PREDICTIONS_FILE", os.path.join(BACKTEST_DIR, "predictions.json"))
BACKTEST_SCORES_FILE = os.environ.get("BACKTEST_SCORES_FILE", os.path.join(BACKTEST_DIR, "scores.json"))
# walkthrough_progress.json lives at DATA_DIR (not BACKTEST_DIR) so it matches
# what walk_progress.py expects. Both backtest.py auto-tracking and the
# walk_progress.py CLI write to the same file.
WALKTHROUGH_PROGRESS_FILE = os.environ.get(
    "WALKTHROUGH_PROGRESS_FILE",
    os.path.join(DATA_DIR, "walkthrough_progress.json"),
)

# Default watchlist fallbacks (used by fetch_data.py if WATCHLIST_FILE missing).
DEFAULT_WATCHLIST_STOCKS = ["AAPL", "MSFT", "NVDA", "GOOGL", "META", "TSLA", "AMZN"]
DEFAULT_WATCHLIST_CRYPTO = ["BTC", "ETH", "SOL"]


def sibling(name: str) -> str:
    """Return absolute path to a sibling script in the same sandbox/ dir."""
    return os.path.join(SCRIPT_DIR, name)


def data_file(name: str) -> str:
    """Return absolute path to a file in the data dir."""
    return os.path.join(DATA_DIR, name)


def config_file(name: str) -> str:
    """Return absolute path to a file in the config dir."""
    return os.path.join(CONFIG_DIR, name)


def print_paths() -> None:
    """Print all resolved paths — useful for debugging."""
    print(f"SCRIPT_DIR        = {SCRIPT_DIR}")
    print(f"PROJECT_DIR       = {PROJECT_DIR}")
    print(f"DATA_DIR          = {DATA_DIR}")
    print(f"CONFIG_DIR        = {CONFIG_DIR}")
    print(f"CACHE_DIR         = {CACHE_DIR}")
    print(f"MEMOS_DIR         = {MEMOS_DIR}")
    print(f"MARKET_DATA_FILE  = {MARKET_DATA_FILE}")
    print(f"SIGNALS_FILE      = {SIGNALS_FILE}")
    print(f"JOURNAL_FILE      = {JOURNAL_FILE}")
    print(f"WATCHLIST_FILE    = {WATCHLIST_FILE}")
    print(f"BACKTEST_DIR      = {BACKTEST_DIR}")


if __name__ == "__main__":
    print_paths()
