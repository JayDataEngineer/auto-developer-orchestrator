#!/usr/bin/env bash
# In-the-wild org runner — drives `pux direct` against the SHIPPED DEFAULT
# provider (opencode-go Zen endpoint → mimo-v2.5), reading the api key from the
# local opencode auth.json at runtime. The key lives in the process ENV only,
# never on the argv (so it can't be echoed back in an argparse error). Reusable
# across every org run so each live test is identical provider wiring.
#
#   scripts/wild-run.sh --org general --task "..."
#   scripts/wild-run.sh --org deep-research-engine --task "..." --recursion-limit 80
set -euo pipefail

AUTH="${OPENCODE_AUTH:-$HOME/.local/share/opencode/auth.json}"
KEY=$(uv run --project "$(dirname "$0")/.." python -c \
  "import json,os; print(json.load(open(os.path.expanduser('$AUTH')))['opencode-go']['key'])")
export OPENCODE_API_KEY="$KEY"
# base_url + model id are the shipped models.yaml defaults (opencode Zen Go +
# mimo-v2.5) — left unset so the run tests the EXACT config a cloner gets.

cd "$(dirname "$0")/.."
exec uv run pux direct "$@"
