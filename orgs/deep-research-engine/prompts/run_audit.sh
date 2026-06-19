#!/usr/bin/env bash
# Deep Research Engine — 6 success criteria audit.
# Companion to skills/AUDIT_QUALITY_GATES.md. Runs all 6 checks and prints PASS/FAIL per check.
#
# Usage:
#   SURREAL_PASSWORD=root bash prompts/run_audit.sh
#
# All queries go through Caddy on http://localhost:8000/surreal/sql
# with headers surreal-ns: research + surreal-db: main.
set -euo pipefail

URL=http://localhost:8000/surreal/sql
NS=(-H "Accept: application/json" -H "surreal-ns: research" -H "surreal-db: main")
USERPASS="root:${SURREAL_PASSWORD:-root}"

q() { curl -sX POST "$URL" "${NS[@]}" -u "$USERPASS" -d "$1" | jq -r '.[0].result'; }

echo "=== Deep Research Engine Audit ==="

# 1. transcripts_complete
MISSING=$(q 'SELECT count() AS n FROM item WHERE type IN ["voice","video"] AND (count(->transcribed_by->transcript) = 0 OR array::len(->transcribed_by->transcript.text) = 0) GROUP ALL' | jq -r '.[0].n // 0')
TOTAL=$(q 'RETURN count(SELECT id FROM item WHERE type IN ["voice","video"])')
PASS=$((TOTAL - MISSING))
if [ "$MISSING" -eq 0 ]; then
    echo "1. transcripts_complete: $PASS/$TOTAL PASS"
else
    echo "1. transcripts_complete: $PASS/$TOTAL FAIL ($MISSING missing)"
fi

# 2. sender_names_clean
POLLUTED=$(q "RETURN count(SELECT id FROM item WHERE string::matches(sender, '\\\\d{2}\\\\.\\\\d{2}\\\\.\\\\d{4}'))")
if [ "${POLLUTED:-0}" -eq 0 ]; then
    echo "2. sender_names_clean: polluted=$POLLUTED PASS"
else
    echo "2. sender_names_clean: polluted=$POLLUTED FAIL"
fi

# 3. sender_extraction_worked
UNKNOWN=$(q 'RETURN count(SELECT id FROM item WHERE sender = "Unknown")')
ITEMS=$(q 'RETURN count(SELECT id FROM item)')
PCT=$(python3 -c "print(round(100 * ${UNKNOWN:-0} / max(${ITEMS:-1},1),1))")
if [ "${PCT%.*}" -lt 5 ]; then
    echo "3. sender_extraction_worked: unknown=$UNKNOWN/$ITEMS ${PCT}% PASS"
else
    echo "3. sender_extraction_worked: unknown=$UNKNOWN/$ITEMS ${PCT}% FAIL"
fi

# 4. topic_discovery_ran
TOPICS=$(q 'RETURN count(SELECT id FROM topic)')
if [ "${TOPICS:-0}" -ge 5 ]; then
    echo "4. topic_discovery_ran: topics=$TOPICS PASS"
else
    echo "4. topic_discovery_ran: topics=$TOPICS FAIL"
fi

# 5. person_clusters_exist
PERSONS=$(q 'RETURN count(SELECT id FROM person)')
if [ "${PERSONS:-0}" -ge 3 ]; then
    echo "5. person_clusters_exist: persons=$PERSONS PASS"
else
    echo "5. person_clusters_exist: persons=$PERSONS FAIL"
fi

# 6. cross_modal_linking_worked
LINKED=$(q 'RETURN count(SELECT id FROM person WHERE face_centroid != NONE AND voice_centroid != NONE)')
if [ "${LINKED:-0}" -ge 1 ]; then
    echo "6. cross_modal_linking_worked: linked=$LINKED PASS"
else
    echo "6. cross_modal_linking_worked: linked=$LINKED FAIL"
fi
