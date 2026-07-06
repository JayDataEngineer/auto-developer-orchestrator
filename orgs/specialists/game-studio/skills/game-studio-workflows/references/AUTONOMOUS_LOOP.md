# AUTONOMOUS_LOOP

The studio-director's cycle contract. Drives N iterations of build → QA → decide.

## BOOTSTRAP (first run only, idempotent)

```bash
python3 /sandbox/surreal_client.py init-schema
```

Creates the `studio` namespace, `game-studio` database, and required tables/indexes. Safe to re-run — `DEFINE` is idempotent. Skip once it returns `{"ok": true}`.

## START

```bash
TASK_ID=$(python3 /sandbox/surreal_client.py start-task \
    --prompt "<user's verbatim goal>" \
    | jq -r .task_id)
```

Store `$TASK_ID` for the COMPLETE step at the end.

## PLAN

Lookback: query the last 5 task_runs in this namespace:

```bash
python3 /sandbox/surreal_client.py list-tasks --limit 5 | jq .
```

Read what was tried, what worked, what didn't. Decide:
- **Resume** — continue from the last yielded cycle (increment cycle number)
- **Restart** — fresh cycle 1 (last run aborted or user changed goal)
- **Branch** — fork from a specific past cycle (rare; user explicitly asks)

Write the plan to `/sandbox/workspace/plan.md` so sub-agents can read it.

## DELEGATE-CYCLE (repeat N times)

For each cycle (default N=3, user-set via prompt):

```python
# 1. Build (art + gameplay). The `task` tool is synchronous — each call
#    returns when its specialist finishes, so run them back-to-back, not
#    in parallel.
task(
    subagent_type="game-studio-technical-artist",
    description=f"Cycle {n} art: <art goal from plan>. Write assets to /sandbox/workspace/art/cycle-{n}/. Use FORGE_WORKFLOW skill. Save manifest to art/cycle-{n}/manifest.json."
)
task(
    subagent_type="game-studio-gameplay-programmer",
    description=f"Cycle {n} integration: <gameplay goal>. Save scene changes. Use GODOT_VIA_MCP skill if bridge is up; godot CLI otherwise. Update /sandbox/workspace/game/cycle-{n}/changelog.md."
)

# 2. QA
task(
    subagent_type="game-studio-qa-tester",
    description=f"QA cycle {n}. Screenshot the viewport via godot_client (or godot CLI headless fallback). Write vibe.json per MEDIA_QA skill. Output: /sandbox/workspace/qa/cycle-{n}/vibe.json."
)

# 3. Read vibe.json
vibe = json.load(open(f"/sandbox/workspace/qa/cycle-{n}/vibe.json"))
if vibe["recommendation"] == "yield":
    break  # done early
elif vibe["recommendation"] == "abort":
    return  # surface failure
# else: iterate, next cycle
```

## COMPLETE

```bash
python3 /sandbox/surreal_client.py complete-task \
    --id "$TASK_ID" \
    --delegated-to "game-studio-technical-artist" "game-studio-gameplay-programmer" "game-studio-qa-tester" \
    --artifacts /sandbox/workspace/art/ /sandbox/workspace/game/ /sandbox/workspace/qa/ \
    --status <success|failed|partial>
```

Then write `/sandbox/workspace/summary.md`:
- Cycle count completed
- Top 3 changes per cycle
- Final vibe score
- Files touched (list paths)

## Stop Conditions (HARD)

| Trigger | Action |
|---------|--------|
| 3 cycles complete | Yield with current state |
| vibe score ≥ 4/5 | Yield early |
| 2 consecutive cycle failures | Abort |
| Total runtime > 15 min | Yield partial with note |
| `GODOT_MCP_DOWN` + CLI also fails | Yield what's done, note "qa incomplete" |
| `FORGE_DOWN` for >1 cycle | Yield partial art |

Don't iterate past 3 cycles hoping for perfection — diminishing returns. The user can re-kickoff with a refined goal.

## Cycle Boundaries

Between cycles, the studio-director:
1. Reads `vibe.json` from cycle N
2. Updates `/sandbox/workspace/plan.md` with focus for cycle N+1 (e.g., "boost contrast", "fix player sprite clipping")
3. Re-delegates with the refined goal

The plan evolves — that's the point. Don't redo cycle N's exact task in N+1.

## Failure Modes

| Failure | Recovery |
|---------|----------|
| task to game-studio-technical-artist times out | Skip art cycle, game-studio-gameplay-programmer runs with existing assets |
| a specialist returns no result | Retry the task call once, then assume it failed |
| game-studio-qa-tester can't screenshot (Godot down + CLI failing) | Skip QA, log "qa_skipped: godot unreachable", continue |
| SurrealDB write fails | Continue anyway; log to `/sandbox/workspace/surreal_errors.log`. Don't abort the loop over logging. |

## Logging Discipline

Every cycle boundary, append to `/sandbox/workspace/cycle_log.md`:

```markdown
## Cycle N (timestamp)
- delegated to: game-studio-technical-artist (art/char_02.png), game-studio-gameplay-programmer (player.gd +12 -3)
- qa: vibe.json scores art=3 tone=4 tech=4, recommendation=iterate
- next focus: char_02 contrast
```

This is the human-readable audit trail. The user will read it after the loop completes.
