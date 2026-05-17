# Staff Memos (Artifact Handoff)

Employees can write artifacts via `yield_artifact` — saved to `/sandbox/workspace/memos/` and persisted to the artifact store.
For multi-step pipelines (research -> code -> review):
1. First employee writes their output as an artifact
2. Tell the next employee to read it: "Read `/sandbox/workspace/memos/report-<topic>.md` and implement it"
3. This avoids carrying large outputs in your context — the file IS the handoff
