#!/bin/bash
# run_scan.sh — Full morning scan pipeline wrapper
# Sets env vars once, runs each step, saves output to files.
# Usage: bash /sandbox/run_scan.sh [--full|--quick|--execute]
#   --full    = all 11 steps (default)
#   --quick   = fetch data + signal fusion + regime only (no trades/risk)
#   --execute = skip data fetch, just run trade/risk steps on existing signals

set -euo pipefail

MODE="${1:---full}"
DATA_DIR="/workspace/data"
mkdir -p "$DATA_DIR"

# Export env vars for all scripts
export MARKET_DATA_FILE="$DATA_DIR/market_data.json"
export SIGNALS_FILE="$DATA_DIR/signals.json"
export JOURNAL_FILE="$DATA_DIR/journal.json"
export JOURNAL_ARCHIVE="$DATA_DIR/journal_archive.json"
export REGIME_HISTORY="$DATA_DIR/regime_history.json"
export REGIME_CONFIG="/sandbox/regime_config.json"
export WALKFORWARD_REPORT="$DATA_DIR/walkforward_report.json"
export SIGNALS_CONFIG="/sandbox/signals_config.json"
export RISK_CONFIG="/sandbox/risk_config.json"
export ALPACA_API_KEY="${ALPACA_API_KEY:-PKRSCFAUIFMGNE4LQTBG5GAFXD}"
export ALPACA_SECRET_KEY="${ALPACA_SECRET_KEY:-4uAsxThg7vGadJ6VWnYgVUjryML2TwMLGeM4QLVgTVvQ}"

REPORT="$DATA_DIR/scan_report.txt"
> "$REPORT"

log() { echo "[$(date +%H:%M:%S)] $1" | tee -a "$REPORT"; }

# Data fetch + analysis steps (skip for --execute)
if [ "$MODE" != "--execute" ]; then
    # STEP 0: Evaluate past predictions
    log "=== STEP 0: Evaluate past predictions ==="
    python3 /sandbox/journal.py evaluate >> "$REPORT" 2>&1 || log "  (evaluate skipped)"
    python3 /sandbox/journal.py stats >> "$REPORT" 2>&1 || log "  (stats skipped)"

    # STEP 1: Fetch market data
    log "=== STEP 1: Fetch market data ==="
    rm -f "$MARKET_DATA_FILE"
    python3 /sandbox/fetch_data.py > "$MARKET_DATA_FILE" 2>/dev/null
    if python3 -c "import json; json.load(open('$MARKET_DATA_FILE'))" 2>/dev/null; then
        log "  Saved market data ($(wc -c < "$MARKET_DATA_FILE") bytes)"
    else
        log "  ERROR: market_data.json invalid"
        exit 1
    fi

    # STEP 2.5: Multi-signal fusion
    log "=== STEP 2.5: Signal fusion ==="
    python3 /sandbox/signals.py rank >> "$REPORT" 2>&1 || log "  (rank failed)"
    python3 /sandbox/signals.py consensus >> "$REPORT" 2>&1 || log "  (consensus failed)"

    # STEP 2.6: Walk-forward validation
    log "=== STEP 2.6: Walk-forward validation ==="
    python3 /sandbox/walkforward.py report >> "$REPORT" 2>&1 || log "  (no report yet)"

    # STEP 2.7: Regime detection
    log "=== STEP 2.7: Regime detection ==="
    python3 /sandbox/regime.py detect >> "$REPORT" 2>&1
    python3 /sandbox/regime.py adjust > "$DATA_DIR/regime_params.json" 2>/dev/null || log "  (adjust failed)"

    if [ "$MODE" = "--quick" ]; then
        log "=== Quick scan complete. Data in $DATA_DIR ==="
        exit 0
    fi
fi

# Trade + risk steps (--full or --execute)
if [ "$MODE" = "--execute" ]; then
    log "=== Execute mode: running trade/risk on existing data ==="
fi

# STEP 6: Record predictions (signals.json must exist from agent)
log "=== STEP 6: Record predictions ==="
if [ -f "$SIGNALS_FILE" ]; then
    python3 /sandbox/journal.py record-signals >> "$REPORT" 2>&1 || log "  (record-signals failed)"
else
    log "  (no signals.json yet - skip recording)"
fi

# STEP 7: Risk assessment
log "=== STEP 7: Risk assessment ==="
python3 /sandbox/risk.py assess >> "$REPORT" 2>&1 || log "  (assess failed)"

# STEP 9: Execute trades (uses signals.json)
log "=== STEP 9: Execute trades ==="
if [ -f "$SIGNALS_FILE" ]; then
    python3 /sandbox/trade.py >> "$REPORT" 2>&1 || log "  (trade failed)"
else
    log "  (no signals.json - skip trades)"
fi

# STEP 10: Place stops
log "=== STEP 10: Place stops ==="
python3 /sandbox/risk.py orders >> "$REPORT" 2>&1 || log "  (orders failed)"

# STEP 10.5: Stops review
log "=== STEP 10.5: Stops review ==="
python3 /sandbox/risk.py stops >> "$REPORT" 2>&1 || log "  (stops failed)"

log "=== Full scan complete. Report: $REPORT ==="
log "Data files:"
ls -la "$DATA_DIR" >> "$REPORT" 2>&1
