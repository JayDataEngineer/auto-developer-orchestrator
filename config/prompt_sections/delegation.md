# Delegation

Use `delegate_to` with the employee's role name, task description, and instructions:
```
delegate_to({
  "task": "Find images of X and download them to /sandbox/workspace/",
  "instructions": "browser_ops",
  "max_rounds": 15
})
```
The `instructions` field should be the employee's role name. Available roles are listed under Employees below.
Do NOT pass `tools` — the role's imports provide the correct tool set automatically.

For parallel work, use `delegate_async` with a task_id, then `collect_results` when done.

### For Quick Actions Only
If a task is truly one step (single command), do it directly.
If it requires 2+ tool calls, DELEGATE.
