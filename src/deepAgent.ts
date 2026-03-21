/**
 * Deep Agent for generating TODO lists from codebase analysis
 * 
 * This agent analyzes a codebase and automatically generates a TODO_FOR_JULES.md
 * file with technical tasks to feed to Jules (Google's AI coding agent).
 */

import { createDeepAgent, LocalShellBackend } from "deepagents";
import { ChatAnthropic } from "@langchain/anthropic";
import * as fs from "fs";
import * as path from "path";

// System prompt for TODO generation
const todoGeneratorPrompt = `You are an expert software architect and technical lead. Your job is to analyze a codebase and generate a comprehensive TODO list for improvement.

## Your Task

1. Analyze the codebase structure by exploring the files
2. Identify technical debt, missing features, bugs, and improvements
3. Generate a TODO_FOR_JULES.md file with 5-10 actionable tasks

## Output Format

Write your findings to a file called \`TODO_FOR_JULES.md\` in the following format:

\`\`\`markdown
- [ ] Task 1 description
- [ ] Task 2 description
- [ ] Task 3 description
\`\`\`

## Task Guidelines

- Tasks should be specific and actionable
- Mix of small, medium, and large tasks
- Include: bug fixes, feature additions, refactoring, testing, documentation
- Prioritize high-impact improvements
- Write clear, concise task descriptions

## Process

1. First, explore the project structure using ls and read_file
2. Review key files: package.json, main source files, config files
3. Identify patterns, issues, and opportunities
4. Write your TODO list to TODO_FOR_JULES.md

Remember: This TODO list will be executed by Jules (an AI coding agent), so be specific and technical!`;

// Create the deep agent with filesystem access
export const todoAgent = createDeepAgent({
  model: new ChatAnthropic({
    model: "claude-sonnet-4-20250514",
    temperature: 0,
  }),
  systemPrompt: todoGeneratorPrompt,
  backend: new LocalShellBackend({
    rootDir: process.cwd(),
    inheritEnv: true,
  }),
});

// Helper function to generate TODOs for a project
export async function generateTODOs(projectPath: string): Promise<string> {
  const result = await todoAgent.invoke({
    messages: [{
      role: "user",
      content: `Analyze the codebase at "${projectPath}" and generate a TODO_FOR_JULES.md file with technical improvement tasks.`,
    }],
  });

  // Extract the TODO list from the generated file
  const todoPath = path.join(projectPath, "TODO_FOR_JULES.md");
  if (fs.existsSync(todoPath)) {
    const content = fs.readFileSync(todoPath, "utf-8");
    return content;
  }

  throw new Error("TODO_FOR_JULES.md was not created");
}
