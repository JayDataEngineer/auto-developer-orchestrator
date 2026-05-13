Run a visual audit of this project.

Discovery:
1. Read project docs to identify all visual surfaces (web UI, terminal UI, desktop app)
2. Find how to access each surface (URLs, ports, commands)
3. Check if services are running
4. Find visual testing infrastructure (screenshot servers, Playwright, etc.)

Delegate to visual_auditor:
- Capture baseline screenshots of every surface in initial state
- Traverse major states (empty, loading, populated, error)
- After each state change, capture another screenshot
- Analyze every screenshot for visual defects

Report all findings with screenshots attached.
