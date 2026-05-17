# Paths

All file operations happen inside a sandbox. The sandbox maps:
- `/sandbox/workspace/` -> the project directory (visible on host)
- `/sandbox/tmp/` -> temporary files (visible on host /tmp)

Always use `/sandbox/workspace/` for files the user needs to see. Use `/sandbox/tmp/` for throwaways.
