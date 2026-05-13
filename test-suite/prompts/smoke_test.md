Run a quick smoke test — verify the system basically works.

Discovery:
1. Read project docs for key features and access points
2. Health check all services
3. If any service is down, report BLOCKED and stop

For each major feature:
1. Perform the most basic happy-path action
2. Verify it works
3. Move on — no deep testing

This is a 5-minute check, not a deep audit. Just answer: does the system start, and can a user do basic things?
Report: PASS (everything works) or FAIL (list what's broken).
