# Shell Capability

## Available Functions

bash(command): Run a shell command in the sandbox
  Use for system operations, package management, service configuration.

file_read(path): Read a file
file_write(path, content): Write a file
file_edit(path, old_string, new_string): Edit a file by exact replacement
file_glob(pattern): Find files by name pattern
file_grep(pattern, path): Search file contents

## Tips
- Check current directory and environment before running commands
- Use absolute paths when possible
- Pipe commands for complex operations: grep | awk | sort
- Check exit codes and stderr for errors
