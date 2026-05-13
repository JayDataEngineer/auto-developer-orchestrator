Compare the current system state against a baseline.

If a specific commit or branch is mentioned:
1. Capture the CURRENT state (screenshots, API responses)
2. Note the current commit SHA

If asked to test a specific change:
1. Capture BEFORE state at current code
2. Apply the change (pull, checkout, build, restart)
3. Capture AFTER state
4. Compare using vision tools and diff

Look for:
- Unintended visual changes
- API response differences
- Performance changes (response times)
- Missing features or broken flows

Report: intended changes (good) vs unintended regressions (bad).
