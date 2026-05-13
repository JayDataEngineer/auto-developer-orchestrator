Run a full regression test on this project.

Discovery:
1. Read the project docs (README, CLAUDE.md, any design docs)
2. Find what services exist, what ports they run on
3. Check if services are currently running
4. Find any existing regression lists (REG-NNN entries)

Then delegate:
- visual_auditor: Screenshot and analyze every visual surface (web UI, terminal UI, desktop app — whatever exists)
- interaction_tester: Run happy path + edge case tests on every interactive surface
- api_auditor: Probe every API endpoint, validate schemas, test error handling
- regression_hunter: Run every REG-NNN entry, verify all fixes are still in place

Compile all findings into a single report with severity ratings.
If any CRITICAL issues are found, flag them immediately.
