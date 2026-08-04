#!/usr/bin/env bash
# In-the-wild org runner — drives `pux direct` against the SHIPPED DEFAULT
# provider (OpenRouter → mimo-v2.5, Parasail backend). The OpenRouter key is
# read from the project .env (or the ambient environment); it lives in the
# process ENV only, never on the argv (so it can't be echoed back in an
# argparse error). Reusable across every org run so each live test is identical
# provider wiring.
#
#   scripts/wild-run.sh --org general --task "..."
#   scripts/wild-run.sh --org deep-research-engine --task "..." --recursion-limit 80
set -euo pipefail

cd "$(dirname "$0")/.."

# Load the project .env so OPENROUTER_API_KEY is in the environment. Harmless
# if .env is absent (the key may already be ambient in CI / prod shells).
if [ -f .env ]; then
  set -a
  # shellcheck disable=SC1091
  . ./.env
  set +a
fi

: "${OPENROUTER_API_KEY:?OPENROUTER_API_KEY is required — set it in .env or the environment.}"
# base_url + model id are the shipped models.yaml defaults (OpenRouter +
# xiaomi/mimo-v2.5 via Parasail) — left unset so the run tests the EXACT config
# a cloner gets.

exec uv run pux direct "$@"
