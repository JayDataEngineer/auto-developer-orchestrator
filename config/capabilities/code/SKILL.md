# Code Capability

## Available Functions

bash(command): Run a shell command
  Use for installing dependencies, running tests, building, git operations.
  Commands run in the project workspace.

file_read(path): Read a file's contents
  ALWAYS read a file before editing it.
  Supports absolute paths and relative paths from workspace root.

file_write(path, content): Write a file (creates or overwrites)
  Use for creating new files only.
  For existing files, prefer file_edit.

file_edit(path, old_string, new_string): Edit a file by replacing exact text
  old_string must match exactly — include surrounding context for uniqueness.
  This is the preferred way to modify existing files.

file_glob(pattern): Find files by name pattern
  Returns matching file paths. Use **/*.ts for recursive search.

file_grep(pattern, path): Search file contents
  Returns matching lines with file paths and line numbers.

## Workflow
1. file_read() to understand existing code
2. file_edit() to make targeted changes
3. bash() to run tests and verify

## Tips
- Always read before editing — never guess at file contents
- Use file_edit, not file_write, for existing files — it's safer
- Run tests after changes to verify nothing broke
- For multi-file changes, plan all edits first, then execute
