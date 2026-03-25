/**
 * Deep Agent for generating TODO lists from codebase analysis
 *
 * This agent analyzes a codebase and automatically generates a TODO_FOR_JULES.md
 * file with technical tasks to feed to Jules (Google's AI coding agent).
 *
 * Configuration via .env:
 * - DEEP_AGENT_MODEL: Model name (default: "gpt-4o")
 * - DEEP_AGENT_BASE_URL: API base URL (default: "https://api.openai.com/v1")
 * - OPENAI_API_KEY: API key for OpenAI-compatible API
 *
 * All AI access goes through LangChain's deepagents package.
 */

import { createDeepAgent, LocalShellBackend } from "deepagents";
import { ChatOpenAI } from "@langchain/openai";
import * as fs from "fs";
import * as path from "path";

// Get configuration from environment variables
const DEEP_AGENT_MODEL = process.env.DEEP_AGENT_MODEL || "gpt-4o";
const DEEP_AGENT_BASE_URL = process.env.DEEP_AGENT_BASE_URL || "https://api.openai.com/v1";
const OPENAI_API_KEY = process.env.OPENAI_API_KEY;

// Create OpenAI-compatible chat model
const chatModel = new ChatOpenAI({
  model: DEEP_AGENT_MODEL,
  apiKey: OPENAI_API_KEY,
  configuration: {
    baseURL: DEEP_AGENT_BASE_URL,
  },
});

// System prompt for TODO generation
const todoGeneratorPrompt = `You are an expert software architect and technical lead. Your job is to analyze a codebase and generate a comprehensive TODO list for improvement.

## Your Task

1. Analyze the codebase structure by exploring the files.
2. Identify technical debt, missing features, bugs, and architectural improvements.
3. Use the \`write_todos\` tool to maintain a structured list of 5-10 actionable tasks.
4. Also write your findings to a file called \`TODO_FOR_JULES.md\` for persistence.

## Task Guidelines

- Tasks should be specific, technical, and actionable.
- Mix of small (refactoring), medium (feature additions), and large (architectural) tasks.
- Include: bug fixes, performance optimizations, security audits, and testing gaps.
- Prioritize high-impact improvements.

## Capabilities

- You have an **Explorer Subagent** specifically designed for deep code exploration. Delegate broad analysis or searching tasks to it if needed.
- You have filesystem tools to read and list files.

## Process

1. First, explore the project structure using \`ls\` and \`read_file\`, or delegate to the explorer subagent.
2. Review key files: \`package.json\`, main source files, and configuration.
3. Use \`write_todos\` to record each task as you identify it.
4. Finally, write the summary to \`TODO_FOR_JULES.md\`.

Remember: This TODO list will be executed by Jules (an AI coding agent), so be specific and technical!`;

// Define the Explorer Subagent
export const explorerSubagent = createDeepAgent({
  name: "explorer",
  model: chatModel,
  systemPrompt: "You are a code exploration expert. Your job is to deeply analyze codebase structures, find patterns, and identify technical issues. Use your tools to search, read, and understand the code thoroughly.",
  backend: new LocalShellBackend({
    rootDir: process.cwd(),
    inheritEnv: true,
  }),
});

// Create the deep agent with filesystem access and subagents
export const todoAgent = createDeepAgent({
  name: "my_deep_agent",
  model: chatModel,
  systemPrompt: todoGeneratorPrompt,
  subagents: [explorerSubagent as any],
  backend: new LocalShellBackend({
    rootDir: process.cwd(),
    inheritEnv: true,
  }),
});

/**
 * Task interface matching deepagents TodoListMiddleware
 */
export interface AgentTodo {
  id: string;
  content: string;
  status: "pending" | "in_progress" | "completed" | "cancelled";
  priority?: "high" | "medium" | "low";
}

// Helper function to generate TODOs and return structured state
export async function generateTODOs(projectPath: string, prompt?: string) {
  const result = await (todoAgent as any).invoke({
    messages: [["user", `Analyze the codebase at "${projectPath}" and generate technical improvement tasks using the write_todos tool. ${prompt ? `Guidance: ${prompt}` : ''}`]]
  } as any);

  // Extract structured todos from the agent's state channel
  const todos = (result as any).todos as AgentTodo[] || [];
  
  // Also ensure the markdown file exists for compatibility
  const todoPath = path.join(projectPath, "TODO_FOR_JULES.md");
  let markdownContent = "";
  if (fs.existsSync(todoPath)) {
    markdownContent = fs.readFileSync(todoPath, "utf-8");
  } else if (todos.length > 0) {
    // Fallback: generate markdown if file wasn't created but todos are in state
    markdownContent = todos.map(t => `- [ ] ${t.content}`).join('\n');
    fs.writeFileSync(todoPath, markdownContent);
  }

  return {
    todos,
    markdown: markdownContent
  };
}
