/**
 * Simple Deep Agent Example - TODO Generator
 * 
 * This is the SIMPLEST way to use deepagents:
 * 1. Import createDeepAgent
 * 2. Set up a prompt
 * 3. BAM! Done!
 */

import { createDeepAgent, LocalShellBackend } from "deepagents";
import { ChatAnthropic } from "@langchain/anthropic";

// That's it! Just import and configure!
const todoAgent = createDeepAgent({
  model: new ChatAnthropic({
    model: "claude-sonnet-4-20250514",
    temperature: 0,
  }),
  systemPrompt: `You are an expert software architect. Analyze the codebase and generate a TODO_FOR_JULES.md file with 5-10 technical improvement tasks.

Write the TODO list in this format:
- [ ] Task description
- [ ] Another task
- [ ] Yet another task

Tasks should be specific and actionable for Jules (AI coding agent) to execute.`,
  backend: new LocalShellBackend({
    rootDir: process.cwd(),
    inheritEnv: true,
  }),
});

// Use it!
async function main() {
  console.log("🤖 Deep Agent generating TODOs...");
  
  const result = await (todoAgent as any).invoke({ messages: [["user", "Analyze this codebase and create a TODO_FOR_JULES.md with improvement tasks."]] } as any);
  
  console.log("✅ TODO generation complete!");
  console.log("📝 Generated tasks:", result.todos);
  console.log("📁 Created files:", Object.keys(result.files));
}

// Run if executed directly
if (import.meta.url === `file://${process.argv[1]}`) {
  main().catch(console.error);
}

export { todoAgent };
