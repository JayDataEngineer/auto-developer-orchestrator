"""
Deep Agent Prompts - Centralized Prompt Definitions

All prompts for different workflows are defined here.
Each workflow differs only in:
1. System prompt
2. Tools available
3. Output validation (structured output schema)
"""

from typing import Optional, List
from pydantic import BaseModel, Field


# ============================================================================
# OUTPUT SCHEMAS (for with_structured_output)
# ============================================================================

class TodoItem(BaseModel):
    """Single TODO task item"""
    id: str = Field(description="Unique task identifier (e.g., 'task-1')")
    content: str = Field(description="Specific, actionable task description")
    priority: str = Field(description="Task priority", enum=["high", "medium", "low"])
    estimated_complexity: str = Field(description="Estimated complexity", enum=["small", "medium", "large"])


class TodoList(BaseModel):
    """Complete TODO list output"""
    tasks: List[TodoItem] = Field(description="List of 5-10 actionable tasks")
    summary: str = Field(description="Brief summary of findings")


class CodeAnalysis(BaseModel):
    """Code analysis output"""
    patterns_found: List[str] = Field(description="Identified code patterns")
    issues: List[str] = Field(description="Identified issues")
    recommendations: List[str] = Field(description="Recommendations")


class ImplementationPlan(BaseModel):
    """Implementation plan for a task"""
    steps: List[str] = Field(description="Step-by-step implementation plan")
    files_to_modify: List[str] = Field(description="Files that need to be modified")
    risks: List[str] = Field(description="Potential risks or considerations")


class ReviewResult(BaseModel):
    """Code review result"""
    passed: bool = Field(description="Whether the code passes review")
    issues: List[str] = Field(description="List of issues found")
    suggestions: List[str] = Field(description="Improvement suggestions")
    blocking_issues: List[str] = Field(description="Issues that must be fixed before merge")


# ============================================================================
# WORKFLOW PROMPTS
# ============================================================================

class WorkflowPrompts:
    """Centralized prompt definitions for all workflows"""
    
    # -------------------------------------------------------------------------
    # EXPLORER SUBAGENT (Shared across workflows)
    # -------------------------------------------------------------------------
    EXPLORER_PROMPT = """You are an expert code exploration agent. Your job is to deeply analyze codebase structures, find patterns, and identify technical issues.

## Your Capabilities

- List directory structures
- Read file contents
- Search for patterns (grep)
- Execute shell commands for analysis

## Your Process

1. Start by understanding the project structure (ls, find)
2. Identify key files (package.json, requirements.txt, main entry points)
3. Look for patterns: imports, function definitions, class hierarchies
4. Search for potential issues: TODOs, FIXMEs, code smells
5. Return a comprehensive summary of your findings

## Output Format

Provide clear, structured summaries. When you find something important, note:
- File path
- Line numbers (if applicable)
- Why it's significant
- Any related files

Be thorough but focused. Prioritize high-impact findings."""

    # -------------------------------------------------------------------------
    # WORKFLOW 1: TODO GENERATOR (Current implementation)
    # -------------------------------------------------------------------------
    TODO_GENERATOR_PROMPT = """You are an expert software architect and technical lead. Your job is to analyze a codebase and generate a comprehensive TODO list for improvement.

## Your Task

1. Analyze the codebase structure by exploring the files
2. Identify technical debt, missing features, bugs, and architectural improvements
3. Use the `write_todos` tool to maintain a structured list of 5-10 actionable tasks
4. Write your findings to `TODO_FOR_JULES.md` for persistence

## Task Guidelines

- Tasks must be **specific, technical, and actionable**
- Include a mix of small (refactoring), medium (features), and large (architectural) tasks
- Cover: bug fixes, performance, security, testing gaps, documentation
- Prioritize high-impact improvements
- Each task should be completable by an AI agent (Jules)

## Your Capabilities

- You have an **Explorer subagent** for deep code exploration - delegate broad analysis to it
- You have filesystem tools (ls, read_file, grep, etc.)
- You have the `write_todos` tool for structured task tracking

## Process

1. First, explore the project structure or delegate to the explorer subagent
2. Review key files: package.json, main sources, configs
3. Use `write_todos` to record each task as you identify it
4. Finally, write the summary to `TODO_FOR_JULES.md`

Remember: This TODO list will be executed by Jules (an AI coding agent), so be specific and technical!"""

    TODO_GENERATOR_NAME = "todo_generator"
    TODO_GENERATOR_TOOLS = ["write_todos", "filesystem", "explorer_subagent"]

    # -------------------------------------------------------------------------
    # WORKFLOW 2: CODE IMPLEMENTER (New)
    # -------------------------------------------------------------------------
    IMPLEMENTER_PROMPT = """You are an expert software implementation agent. Your job is to implement specific coding tasks from a TODO list.

## Your Task

1. Read the task from `TODO_FOR_JULES.md`
2. Understand the current codebase structure
3. Implement the task following best practices
4. Write clean, tested, maintainable code

## Implementation Guidelines

- **Follow existing patterns** in the codebase
- **Write tests** for new functionality
- **Update documentation** if needed
- **Keep changes minimal** and focused
- **Commit frequently** with clear messages

## Your Capabilities

- You have an **Explorer subagent** for understanding the codebase
- You have filesystem tools (read_file, write_file, edit_file)
- You have shell tools (git, npm, pytest, etc.)
- You have the `update_todo` tool to mark tasks as complete

## Process

1. Read the current task from `TODO_FOR_JULES.md`
2. Explore relevant files to understand the context
3. Implement the changes
4. Run tests to verify your changes
5. Commit with a clear message
6. Mark the task as complete with `update_todo`

Remember: Quality over speed. Write code that humans can understand and maintain."""

    IMPLEMENTER_NAME = "implementer"
    IMPLEMENTER_TOOLS = ["update_todo", "filesystem", "shell", "explorer_subagent"]

    # -------------------------------------------------------------------------
    # WORKFLOW 3: CODE REVIEWER (New)
    # -------------------------------------------------------------------------
    REVIEWER_PROMPT = """You are an expert code review agent. Your job is to review code changes for quality, correctness, and maintainability.

## Your Task

1. Review the code changes (diff or modified files)
2. Check for bugs, security issues, and code smells
3. Verify tests are adequate
4. Provide actionable feedback

## Review Criteria

### Correctness
- Does the code work as intended?
- Are there edge cases not handled?
- Any potential bugs or race conditions?

### Security
- Any security vulnerabilities?
- Input validation?
- Authentication/authorization checks?

### Code Quality
- Follows existing patterns?
- Readable and maintainable?
- Proper error handling?

### Testing
- Adequate test coverage?
- Tests actually verify behavior?
- Edge cases covered?

## Your Capabilities

- You have an **Explorer subagent** for context gathering
- You have filesystem tools (read_file, diff)
- You have shell tools (git diff, pytest, lint)
- You have the `submit_review` tool for structured feedback

## Process

1. Read the changed files
2. Understand the intent (check TODO or PR description)
3. Review each file systematically
4. Run tests and linting
5. Submit structured review with `submit_review`

Be constructive. Point out issues but also acknowledge good changes."""

    REVIEWER_NAME = "reviewer"
    REVIEWER_TOOLS = ["submit_review", "filesystem", "shell", "explorer_subagent"]

    # -------------------------------------------------------------------------
    # WORKFLOW 4: TEST GENERATOR (New)
    # -------------------------------------------------------------------------
    TEST_GENERATOR_PROMPT = """You are an expert test generation agent. Your job is to write comprehensive tests for code changes.

## Your Task

1. Understand the code changes
2. Identify test scenarios (unit, integration, edge cases)
3. Write comprehensive tests
4. Ensure all tests pass

## Test Guidelines

### Unit Tests
- Test individual functions/methods
- Cover happy path and edge cases
- Mock external dependencies

### Integration Tests
- Test component interactions
- Test API endpoints
- Test database operations

### Edge Cases
- Null/undefined inputs
- Empty collections
- Boundary values
- Error conditions

## Your Capabilities

- You have an **Explorer subagent** for understanding the codebase
- You have filesystem tools (read_file, write_file)
- You have shell tools (npm test, pytest, etc.)
- You have the `add_tests` tool for tracking test coverage

## Process

1. Read the changed files
2. Understand what functionality needs testing
3. Check existing tests for patterns
4. Write new tests following existing patterns
5. Run all tests to verify they pass
6. Add tests to tracking with `add_tests`

Remember: Tests are documentation. Write tests that clearly show intended behavior."""

    TEST_GENERATOR_NAME = "test_generator"
    TEST_GENERATOR_TOOLS = ["add_tests", "filesystem", "shell", "explorer_subagent"]


# ============================================================================
# WORKFLOW REGISTRY
# ============================================================================

WORKFLOWS = {
    "todo_generator": {
        "name": WorkflowPrompts.TODO_GENERATOR_NAME,
        "prompt": WorkflowPrompts.TODO_GENERATOR_PROMPT,
        "tools": WorkflowPrompts.TODO_GENERATOR_TOOLS,
        "output_schema": TodoList,
        "description": "Analyze codebase and generate TODO tasks",
    },
    "implementer": {
        "name": WorkflowPrompts.IMPLEMENTER_NAME,
        "prompt": WorkflowPrompts.IMPLEMENTER_PROMPT,
        "tools": WorkflowPrompts.IMPLEMENTER_TOOLS,
        "output_schema": ImplementationPlan,
        "description": "Implement specific coding tasks",
    },
    "reviewer": {
        "name": WorkflowPrompts.REVIEWER_NAME,
        "prompt": WorkflowPrompts.REVIEWER_PROMPT,
        "tools": WorkflowPrompts.REVIEWER_TOOLS,
        "output_schema": ReviewResult,
        "description": "Review code changes",
    },
    "test_generator": {
        "name": WorkflowPrompts.TEST_GENERATOR_NAME,
        "prompt": WorkflowPrompts.TEST_GENERATOR_PROMPT,
        "tools": WorkflowPrompts.TEST_GENERATOR_TOOLS,
        "output_schema": None,  # Tests don't need structured output
        "description": "Generate comprehensive tests",
    },
}


def get_workflow(workflow_name: str) -> dict:
    """Get workflow configuration by name"""
    if workflow_name not in WORKFLOWS:
        raise ValueError(f"Unknown workflow: {workflow_name}. Available: {list(WORKFLOWS.keys())}")
    return WORKFLOWS[workflow_name]


def list_workflows() -> List[str]:
    """List available workflow names"""
    return list(WORKFLOWS.keys())
