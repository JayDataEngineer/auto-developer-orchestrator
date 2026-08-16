#!/usr/bin/env bash
# scripts/dre_pipeline.sh — THE ONE COMMAND.
#
# Full DRE pipeline: raw Telegram export → intelligence report + audit.
# Deterministic (code) + LLM (agent) stages chained into a single invocation.
#
# Usage:
#   scripts/dre_pipeline.sh data/telegram-dump/ChatExport_2026-03-13
#   scripts/dre_pipeline.sh                          # uses $DATA_DIR env
#   scripts/dre_pipeline.sh --skip-preprocess <dir>  # DB already ingested
#
# Output: artifacts/run-YYYY-MM-DD/{brief.md, audit_report.md, entities/}
set -euo pipefail

REPO="$(cd "$(dirname "$0")/.." && pwd)"
cd "$REPO"

# --- Args -----------------------------------------------------------------
SKIP_PREPROCESS=0
DATA_DIR=""
while [[ $# -gt 0 ]]; do
    case "$1" in
        --skip-preprocess) SKIP_PREPROCESS=1; shift ;;
        --help|-h)
            echo "Usage: $0 [--skip-preprocess] <data_dir>"; exit 0 ;;
        *) DATA_DIR="$1"; shift ;;
    esac
done
DATA_DIR="${DATA_DIR:-$DATA_DIR}"
DATA_DIR="${DATA_DIR:-data/telegram-dump/ChatExport_2026-03-13}"
if [[ ! -d "$DATA_DIR" ]]; then
    echo "ERROR: data dir not found: $DATA_DIR" >&2; exit 1
fi
DATA_DIR="$(cd "$DATA_DIR" && pwd)"
export DATA_DIR

RUN_DIR="artifacts/run-$(date +%Y-%m-%d)"
SB="profiles/specialists/deep-research-engine/sandbox"
mkdir -p "$RUN_DIR"

echo "╔══════════════════════════════════════════════════════════════╗"
echo "║  DRE FULL PIPELINE                                           ║"
echo "║  data:   $DATA_DIR"
echo "║  output: $RUN_DIR"
echo "╚══════════════════════════════════════════════════════════════╝"

# --- Infrastructure check -------------------------------------------------
echo "[infra] Checking services..."
# SurrealDB credentials from env (dev default: root:root — the SurrealDB
# out-of-box default for localhost). Production overrides via .env.
SURREAL_USER="${SURREAL_USER:-root}"
SURREAL_PASS="${SURREAL_PASS:-root}"
curl -sf -o /dev/null -H 'surreal-ns: research' -H 'surreal-db: main' \
     -u "${SURREAL_USER}:${SURREAL_PASS}" --data-binary 'RETURN 1' http://127.0.0.1:8000/sql \
  || { echo "ERROR: SurrealDB not responding on :8000"; exit 1; }
echo "[infra] SurrealDB OK"

# --- Step 0: Deterministic preprocessing (faces, OCR, VLM, audio) --------
if [[ "$SKIP_PREPROCESS" -eq 0 ]]; then
    echo ""
    echo "══ Step 0: Deterministic preprocessing ══════════════════════"
    python3 scripts/preprocess_pipeline.py --data "$DATA_DIR" --run-dir "$RUN_DIR"
else
    echo "[step 0] SKIPPED (--skip-preprocess)"
fi

# --- Step 0.5: Recluster → ingest → resolve → dossiers -------------------
echo ""
echo "══ Step 0.5: Identity clustering, graph ingest, resolution, dossiers ══"
python3 "$SB/recluster.py" "$RUN_DIR" 0.80 0.30
python3 "$SB/pipeline_ingest.py" --run-dir "$RUN_DIR" --skip-embeddings
python3 "$SB/resolve_identities.py" "$RUN_DIR"
python3 "$SB/build_entity_dossiers.py" "$RUN_DIR"

# --- Steps 1-5: LLM agent (gather → synthesize → audit → publish) -------
echo ""
echo "══ Steps 1-5: DRE agent (synthesis + audit) ══════════════════"
TASK="The deterministic pipeline is COMPLETE. DO NOT run any preprocessing scripts.
Entity dossiers are in ${RUN_DIR}/entities/ (read the index at ${RUN_DIR}/entities/index.md).
SurrealDB graph has 364 items, 67 persons, 61 cluster dossiers.

Your ONLY job now:
1. Delegate to dre-synthesizer: produce the intelligence report and write it
   to ${RUN_DIR}/brief.md. Read the entity dossiers + query SurrealDB for
   the graph. Every claim must be cited to a source item.
2. Delegate to dre-auditor: produce the quality audit and write it to
   ${RUN_DIR}/audit_report.md. Verify embedding coverage, transcript
   completeness, cross-modal linking, grounding.
3. Read BOTH files back to verify they exist and are substantive (>2KB each).
4. Report the file paths and sizes.

This is deep research — take as many steps as needed. Do NOT write files
anywhere except ${RUN_DIR}/."

bin/pux direct \
    --org deep-research-engine \
    --task "$TASK" \
    --data "$DATA_DIR" \
    --recursion-limit 200

# --- Verify ---------------------------------------------------------------
echo ""
echo "══ Verification ══════════════════════════════════════════════"
for f in brief.md audit_report.md; do
    if [[ -f "$RUN_DIR/$f" ]]; then
        SZ=$(wc -c < "$RUN_DIR/$f")
        echo "  ✓ $RUN_DIR/$f ($SZ bytes)"
    else
        echo "  ✗ $RUN_DIR/$f MISSING"
    fi
done
echo ""
echo "Done. Artifacts in: $RUN_DIR"
