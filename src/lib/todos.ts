// Todo List & Scratch Pad Tools
// Pi doesn't have built-in write_todos or scratch pad tools.
// These are exposed to the agent via the system prompt.

import { api } from './api';

// ─── Todo List ──────────────────────────────────────────────

export interface TodoItem {
  id: string;
  text: string;
  status: 'pending' | 'in_progress' | 'done' | 'cancelled';
  notes?: string;
}

export interface TodoList {
  items: TodoItem[];
  updatedAt: string;
}

// Save a todo list for an agent
export async function saveTodos(agentId: string, items: TodoItem[]): Promise<TodoList> {
  const content = items.map(item => {
    const checkbox = item.status === 'done' ? 'x' : item.status === 'in_progress' ? '>' : ' ';
    return `- [${checkbox}] ${item.text}${item.notes ? ` — ${item.notes}` : ''}`;
  }).join('\n');

  await api.artifacts.create(agentId, 'todo', 'Tasks', content);
  return { items, updatedAt: new Date().toISOString() };
}

// Load todos for an agent
export async function loadTodos(agentId: string): Promise<TodoList> {
  const artifacts = await api.artifacts.list(agentId);
  const todoArtifact = artifacts.artifacts?.find(a => a.type === 'todo');
  if (!todoArtifact) {
    return { items: [], updatedAt: new Date().toISOString() };
  }

  // Parse markdown checkboxes
  const items: TodoItem[] = [];
  const lines = todoArtifact.content.split('\n').filter(l => l.trim().startsWith('- ['));
  for (const line of lines) {
    const match = line.match(/- \[([ x>])\] (.+)/);
    if (match) {
      const statusMap: Record<string, TodoItem['status']> = { 'x': 'done', '>': 'in_progress', ' ': 'pending' };
      items.push({
        id: crypto.randomUUID(),
        text: match[2],
        status: statusMap[match[1]] || 'pending',
      });
    }
  }

  return { items, updatedAt: todoArtifact.updatedAt || new Date().toISOString() };
}

// ─── Scratch Pad ──────────────────────────────────────────────

// Save scratch pad notes for an agent
export async function saveScratchPad(agentId: string, content: string): Promise<void> {
  await api.artifacts.create(agentId, 'notes', 'Scratch Pad', content);
}

// Load scratch pad notes for an agent
export async function loadScratchPad(agentId: string): Promise<string> {
  const artifacts = await api.artifacts.list(agentId);
  const notesArtifact = artifacts.artifacts?.find(a => a.type === 'notes' && a.title === 'Scratch Pad');
  return notesArtifact?.content || '';
}

// ─── System Prompt Sections ──────────────────────────────────

export const TODOS_SYSTEM_PROMPT = `# Todo List

You can track tasks using the todo list. Use this to plan complex tasks and track progress.

## Todo Commands

Save todos:
\`\`\`bash
curl -X POST http://localhost:3847/api/pi/artifacts -d '{"agentId":"YOUR_AGENT_ID","type":"todo","title":"Tasks","content":"- [ ] Task 1\n- [ ] Task 2\n- [x] Task 3"}'
\`\`\`

Load todos:
\`\`\`bash
curl "http://localhost:3847/api/pi/artifacts?agentId=YOUR_AGENT_ID"
\`\`\`

## Checkbox Format
- \`- [ ]\` = pending task
- \`- [>]\` = in progress
- \`- [x]\` = completed

Keep your todo list updated as you work. Review it before starting complex tasks.`;

export const SCRATCHPAD_SYSTEM_PROMPT = `# Scratch Pad

You have a scratch pad for temporary notes, thoughts, and observations. Use it to:
- Store information you discover that might be useful later
- Track decisions you've made and why
- Note down patterns or conventions you observe in the codebase
- Record errors you've encountered and how you resolved them

Save notes:
\`\`\`bash
curl -X POST http://localhost:3847/api/pi/artifacts -d '{"agentId":"YOUR_AGENT_ID","type":"notes","title":"Scratch Pad","content":"## Observations\\n- ..."}'
\`\`\`

Load notes:
\`\`\`bash
curl "http://localhost:3847/api/pi/artifacts?agentId=YOUR_AGENT_ID"
\`\`\``;
