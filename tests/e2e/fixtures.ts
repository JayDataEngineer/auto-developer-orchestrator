/**
 * Playwright E2E Test Fixtures
 *
 * Provides a custom test fixture that mocks ALL backend API endpoints
 * so tests can run without the Go backend. Includes helpers for SSE
 * stream simulation to test agent chat flows.
 */
import { test as base, expect, type Page, type Route } from '@playwright/test';

// ─── Mock Data ────────────────────────────────────────────────────

export const MOCK_PROJECTS = [
  { name: 'test-project', path: '/home/user/projects/test-project' },
  { name: 'demo-app', path: '/home/user/projects/demo-app' },
  { name: 'sample-repo', path: '/home/user/projects/sample-repo' },
];

export const MOCK_TASKS = [
  {
    id: 'task-1',
    title: 'Fix login bug',
    description: 'Login fails on mobile',
    status: 'pending',
    projectDir: 'test-project',
    parentAgent: 'default',
    createdAt: Date.now(),
    updatedAt: Date.now(),
  },
  {
    id: 'task-2',
    title: 'Add dark mode',
    status: 'in_progress',
    projectDir: 'test-project',
    parentAgent: 'default',
    createdAt: Date.now(),
    updatedAt: Date.now(),
    durationMs: 120000,
    inputTokens: 5000,
    outputTokens: 2000,
  },
  {
    id: 'task-3',
    title: 'Write tests',
    status: 'completed',
    projectDir: 'test-project',
    parentAgent: 'default',
    createdAt: Date.now(),
    updatedAt: Date.now(),
  },
  {
    id: 'task-4',
    title: 'Deploy to prod',
    status: 'failed',
    projectDir: 'test-project',
    parentAgent: 'default',
    error: 'Build failed',
    createdAt: Date.now(),
    updatedAt: Date.now(),
  },
];

export const MOCK_SCHEDULER_JOBS = [
  {
    id: 'job-1',
    name: 'Daily tests',
    project: 'test-project',
    message: 'Run all tests',
    scheduleType: 'cron',
    cronExpr: '0 9 * * *',
    enabled: true,
    status: 'idle',
    consecutiveErrors: 0,
    createdAt: new Date().toISOString(),
    updatedAt: new Date().toISOString(),
  },
];

export const MOCK_MODELS = {
  models: [
    { provider: 'litellm', id: 'or-free', name: 'OR Free' },
    { provider: 'litellm', id: 'fast', name: 'Fast' },
    { provider: 'litellm', id: 'smart', name: 'Smart' },
  ],
};

// ─── SSE Stream Builder ───────────────────────────────────────────

interface SSEEvent {
  type: string;
  data: Record<string, unknown>;
}

/**
 * Build a full SSE response body from a sequence of events.
 * Simulates a realistic agent conversation with text, tool calls, etc.
 */
export function buildSSEStream(events: SSEEvent[]): string {
  return events.map(e => {
    const json = JSON.stringify(e.data);
    return `event: ${e.type}\ndata: ${json}\n\n`;
  }).join('');
}

/** Pre-built SSE stream: simple text reply with no tool calls */
export const SSE_SIMPLE_REPLY: SSEEvent[] = [
  { type: 'agent_start', data: {} },
  { type: 'text_delta', data: { text: 'I will ' } },
  { type: 'text_delta', data: { text: 'help you with that.' } },
  { type: 'agent_end', data: { input: 100, output: 50, cache: 0 } },
];

/** Pre-built SSE stream: reply with a bash tool call */
export const SSE_WITH_TOOL_CALL: SSEEvent[] = [
  { type: 'agent_start', data: {} },
  { type: 'thinking_delta', data: { text: 'Let me check the current files...' } },
  { type: 'tool_execution_start', data: { toolName: 'bash', toolId: 'tool-1', args: { command: 'ls -la' } } },
  { type: 'tool_execution_end', data: { toolId: 'tool-1', toolName: 'bash', result: 'total 42\ndrwxr-xr-x 5 user user 4096 Jan 1 src\n-rw-r--r-- 1 user user 1204 Jan 1 package.json', error: '' } },
  { type: 'text_delta', data: { text: 'Here are the files in your project.' } },
  { type: 'agent_end', data: { input: 200, output: 150, cache: 10 } },
];

/** Pre-built SSE stream: reply with multiple tool calls (read + edit + bash) */
export const SSE_WITH_MULTIPLE_TOOLS: SSEEvent[] = [
  { type: 'agent_start', data: {} },
  { type: 'thinking_delta', data: { text: 'I need to read the file first...' } },
  { type: 'tool_execution_start', data: { toolName: 'read', toolId: 'tool-1', args: { filePath: '/src/App.tsx' } } },
  { type: 'tool_execution_end', data: { toolId: 'tool-1', toolName: 'read', result: 'export default function App() { return <div>Hello</div> }', error: '' } },
  { type: 'tool_execution_start', data: { toolName: 'bash', toolId: 'tool-2', args: { command: 'npm test' } } },
  { type: 'tool_execution_end', data: { toolId: 'tool-2', toolName: 'bash', result: 'Tests: 12 passed, 0 failed', error: '' } },
  { type: 'text_delta', data: { text: 'All tests pass. The code looks good.' } },
  { type: 'agent_end', data: { input: 500, output: 300, cache: 50 } },
];

/** Pre-built SSE stream: tool call with error */
export const SSE_WITH_TOOL_ERROR: SSEEvent[] = [
  { type: 'agent_start', data: {} },
  { type: 'tool_execution_start', data: { toolName: 'bash', toolId: 'tool-1', args: { command: 'npm run build' } } },
  { type: 'tool_execution_end', data: { toolId: 'tool-1', toolName: 'bash', result: '', error: 'Build failed: exit code 1' } },
  { type: 'text_delta', data: { text: 'The build failed. Let me fix the error.' } },
  { type: 'agent_end', data: { input: 300, output: 100, cache: 0 } },
];

/** Pre-built SSE stream: tool call that never ends (simulates hanging/infinite load) */
export const SSE_HANGING_TOOL: SSEEvent[] = [
  { type: 'agent_start', data: {} },
  { type: 'tool_execution_start', data: { toolName: 'bash', toolId: 'tool-hang', args: { command: 'curl https://example.com' } } },
  // NO tool_execution_end — this simulates the infinite spinner bug
];

/** Pre-built SSE stream: tool call with missing name (empty string) */
export const SSE_MISSING_TOOL_NAME: SSEEvent[] = [
  { type: 'agent_start', data: {} },
  { type: 'tool_execution_start', data: { toolName: '', toolId: 'tool-unnamed', args: {} } },
  // No end event either — worst case scenario
];

/** Pre-built SSE stream: agent creates a PR (pr_created event) */
export const SSE_WITH_PR_CREATED: SSEEvent[] = [
  { type: 'agent_start', data: {} },
  { type: 'text_delta', data: { text: 'I have created the pull request.' } },
  { type: 'agent_end', data: { input: 200, output: 100, cache: 0 } },
  { type: 'pr_created', data: { url: 'https://github.com/test-org/test-repo/pull/42', number: 42, title: 'Fix login bug' } },
];

/** Pre-built SSE stream: error event from agent */
export const SSE_WITH_ERROR_EVENT: SSEEvent[] = [
  { type: 'agent_start', data: {} },
  { type: 'text_delta', data: { text: 'Starting...' } },
  { type: 'error', data: { error: 'Model rate limit exceeded' } },
];

/** Pre-built SSE stream: sub-agent spawning via bash command */
export const SSE_WITH_SUBAGENT_SPAWN: SSEEvent[] = [
  { type: 'agent_start', data: {} },
  { type: 'tool_execution_start', data: { toolName: 'bash', toolId: 'tool-spawn', args: { command: 'curl /api/pi/subagent/spawn -d \'{"type":"code"}\'' } } },
  { type: 'tool_execution_end', data: { toolId: 'tool-spawn', toolName: 'bash', result: '{"subAgentId":"sub-code-1234567890","status":"running"}', error: '' } },
  { type: 'text_delta', data: { text: 'Sub-agent spawned successfully.' } },
  { type: 'agent_end', data: { input: 300, output: 150, cache: 0 } },
];

/** Pre-built SSE stream: full flow with thinking + tool call + text */
export const SSE_WITH_THINKING_AND_TOOLS: SSEEvent[] = [
  { type: 'agent_start', data: {} },
  { type: 'thinking_delta', data: { text: 'I need to analyze the codebase structure first...' } },
  { type: 'tool_execution_start', data: { toolName: 'bash', toolId: 'tool-full-1', args: { command: 'find src -type f -name "*.ts"' } } },
  { type: 'tool_execution_end', data: { toolId: 'tool-full-1', toolName: 'bash', result: 'src/index.ts\nsrc/App.tsx\nsrc/utils.ts', error: '' } },
  { type: 'tool_execution_start', data: { toolName: 'read', toolId: 'tool-full-2', args: { filePath: '/src/App.tsx' } } },
  { type: 'tool_execution_end', data: { toolId: 'tool-full-2', toolName: 'read', result: 'export default function App() { return <div>Hello</div> }', error: '' } },
  { type: 'text_delta', data: { text: 'I found 3 TypeScript files in the project. The main App component renders a simple div.' } },
  { type: 'agent_end', data: { input: 400, output: 250, cache: 20 } },
];

/** Pre-built SSE stream: agent creates a commit */
export const SSE_WITH_COMMIT_CREATED: SSEEvent[] = [
  { type: 'agent_start', data: {} },
  { type: 'text_delta', data: { text: 'I have committed the changes.' } },
  { type: 'commit_created', data: { message: 'feat: add user authentication', branch: 'feat/auth' } },
  { type: 'agent_end', data: { input: 150, output: 80, cache: 0 } },
];

/** Pre-built SSE stream: agent creates a branch */
export const SSE_WITH_BRANCH_CREATED: SSEEvent[] = [
  { type: 'agent_start', data: {} },
  { type: 'branch_created', data: { branch: 'feature/new-ui' } },
  { type: 'text_delta', data: { text: 'Created new branch for the UI work.' } },
  { type: 'agent_end', data: { input: 100, output: 60, cache: 0 } },
];

/** Pre-built SSE stream: agent requests approval before a risky action */
export const SSE_WITH_APPROVAL_REQUEST: SSEEvent[] = [
  { type: 'agent_start', data: {} },
  { type: 'text_delta', data: { text: 'I will post this tweet now.' } },
  { type: 'approval_request', data: { requestId: 'req-1', type: 'tool_confirm', toolName: 'bash', message: 'Post tweet via Twitter API', risk: 'high' } },
  // No agent_end — stream stays open waiting for approval
];

/** Pre-built SSE stream: agent asks the user a question */
export const SSE_WITH_QUESTION: SSEEvent[] = [
  { type: 'agent_start', data: {} },
  { type: 'text_delta', data: { text: 'I need some information.' } },
  { type: 'question_asked', data: { requestId: 'req-2', type: 'question', message: 'Which Twitter account should I use?', risk: 'low' } },
];

// ─── Route Mocking Helpers ────────────────────────────────────────

interface MockConfig {
  /** SSE events to serve for the next /api/pi/prompt call */
  sseEvents?: SSEEvent[];
  /** Pi tasks to return */
  tasks?: typeof MOCK_TASKS;
  /** Scheduler jobs to return */
  jobs?: typeof MOCK_SCHEDULER_JOBS;
}

/**
 * Install all API route mocks on the page. Every backend endpoint returns
 * valid mock data so the frontend can fully render.
 */
export async function mockApiRoutes(page: Page, config: MockConfig = {}) {
  const tasks = config.tasks ?? MOCK_TASKS;
  const jobs = config.jobs ?? MOCK_SCHEDULER_JOBS;
  const sseEvents = config.sseEvents;

  // ── Core data endpoints (called on app init) ──

  await page.route('**/api/projects', async route => {
    await route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify({ projects: MOCK_PROJECTS }),
    });
  });

  await page.route('**/api/config/ai', async route => {
    await route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify({
        autoTask: true,
        autoTest: true,
        fullAutomationMode: false,
        postMergeTestGen: false,
        testGenPrompt: '',
        testTypes: { unit: true, e2e: false, integration: false, chaos: false, security: false, performance: false },
      }),
    });
  });

  await page.route('**/api/github/user', async route => {
    await route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify({ connected: false }),
    });
  });

  // ── Project data (called when project is selected) ──

  await page.route('**/api/status**', async route => {
    await route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify({
        gitState: 'clean',
        workingTree: 'clean',
        isAutoMode: false,
        agentStatus: 'idle',
        lastCommit: 'abc123 Initial commit',
        project: 'test-project',
      }),
    });
  });

  await page.route('**/api/checklist**', async route => {
    await route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify({
        tasks: [
          { id: 't1', text: 'Setup project', completed: true, status: 'completed' },
          { id: 't2', text: 'Add features', completed: false, status: 'pending' },
        ],
      }),
    });
  });

  // ── Pux API endpoints (new API paths used by the redesigned UI) ──

  await page.route('**/api/pux/models**', async route => {
    await route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify(MOCK_MODELS),
    });
  });

  await page.route('**/api/pux/conversations**', async route => {
    await route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify([]),
    });
  });

  await page.route('**/api/pux/conversation**', async route => {
    await route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify({ success: true }),
    });
  });

  await page.route('**/api/pux/history**', async route => {
    await route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify({ messages: [] }),
    });
  });

  await page.route('**/api/pux/model**', async route => {
    await route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify({ success: true }),
    });
  });

  await page.route('**/api/pux/defaults**', async route => {
    await route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify({ logic: '', worker: '' }),
    });
  });

  await page.route('**/api/pux/providers**', async route => {
    await route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify({ providers: [] }),
    });
  });

  await page.route('**/api/pux/agent-status**', async route => {
    await route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify({ agents: {} }),
    });
  });

  await page.route('**/api/pux/mcp-servers**', async route => {
    await route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify({ servers: [] }),
    });
  });

  await page.route('**/api/pux/decision**', async route => {
    await route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify({ success: true }),
    });
  });

  // ── Legacy Pi Agent endpoints (kept for backward compatibility) ──

  await page.route('**/api/pi/state**', async route => {
    await route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify({ model: 'or-free', streaming: false, input: 0, output: 0, cache: 0 }),
    });
  });

  await page.route('**/api/pi/messages**', async route => {
    await route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify([]),
    });
  });

  await page.route('**/api/pi/models**', async route => {
    await route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify(MOCK_MODELS),
    });
  });

  await page.route('**/api/pi/model', async route => {
    await route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify({ success: true }),
    });
  });

  await page.route('**/api/pi/compact**', async route => {
    await route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify({ success: true }),
    });
  });

  await page.route('**/api/pi/abort**', async route => {
    await route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify({ success: true }),
    });
  });

  await page.route('**/api/pi/active', async route => {
    await route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify({ projects: [] }),
    });
  });

  await page.route('**/api/pi/history', async route => {
    await route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify({ conversations: [] }),
    });
  });

  // ── Pi Tasks endpoints ──

  await page.route('**/api/pi/tasks/list**', async route => {
    await route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify({ tasks }),
    });
  });

  await page.route('**/api/pi/tasks/', async route => {
    if (route.request().method() === 'POST') {
      const body = await route.request().postDataJSON();
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({
          ...body,
          id: 'new-task-' + Date.now(),
          status: 'pending',
          projectDir: body?.projectDir || 'test-project',
          parentAgent: body?.parentAgent || 'default',
          createdAt: Date.now(),
          updatedAt: Date.now(),
        }),
      });
    } else {
      await route.fulfill({ status: 404, body: 'Not found' });
    }
  });

  // ── Scheduler endpoints ──

  await page.route('**/api/scheduler', async route => {
    if (route.request().method() === 'GET') {
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({ jobs }),
      });
    } else if (route.request().method() === 'POST') {
      const body = await route.request().postDataJSON();
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({
          success: true,
          job: { ...body, id: 'job-' + Date.now(), status: 'idle', consecutiveErrors: 0, createdAt: new Date().toISOString(), updatedAt: new Date().toISOString() },
        }),
      });
    }
  });

  await page.route('**/api/scheduler/*', async route => {
    if (route.request().method() === 'DELETE') {
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({ success: true }),
      });
    } else {
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify(jobs[0]),
      });
    }
  });

  // ── Workers endpoints (used by Agents tab) ──

  await page.route('**/api/workers/**', async route => {
    await route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify([]),
    });
  });

  // ── Artifacts endpoint ──

  await page.route('**/api/pi/artifacts**', async route => {
    await route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify({ artifacts: [] }),
    });
  });

  // ── Sandbox / Computer Use endpoints ──

  await page.route('**/api/sandbox/**', async route => {
    const url = route.request().url();
    if (url.includes('/computer-use/enable')) {
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({ enabled: true, sandboxId: 'sandbox-test-project', cdpPort: 9222 }),
      });
    } else if (url.includes('/desktop-mode')) {
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({ enabled: true, sandboxId: 'sandbox-test-project', vncPort: 6080, displayNum: 99 }),
      });
    } else if (url.includes('/viewer')) {
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({ url: 'http://localhost:6080/vnc.html', sandboxId: 'sandbox-test-project', status: 'running' }),
      });
    } else {
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({ sandboxes: [] }),
      });
    }
  });

  // ── CLI endpoints ──

  await page.route('**/api/cli/**', async route => {
    const url = route.request().url();
    if (url.includes('/ls')) {
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({
          entries: [
            { name: 'src', is_dir: true },
            { name: 'package.json', is_dir: false, size: 1024 },
            { name: 'README.md', is_dir: false, size: 512 },
          ],
        }),
      });
    } else if (url.includes('/cat')) {
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({ content: '// sample file content' }),
      });
    } else {
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({ commands: ['ls', 'cat', 'grep', 'find'] }),
      });
    }
  });

  // ── SSE Prompt endpoint (the big one) ──
  // Mock both /api/pux/prompt (new) and /api/pi/prompt (legacy)

  const sseHandler = async (route: Route) => {
    const events = sseEvents || SSE_SIMPLE_REPLY;
    const body = buildSSEStream(events);

    await route.fulfill({
      status: 200,
      contentType: 'text/event-stream',
      headers: {
        'Cache-Control': 'no-cache',
        'Connection': 'keep-alive',
      },
      body,
    });
  };

  await page.route('**/api/pux/prompt', sseHandler);
  await page.route('**/api/pi/prompt', sseHandler);

  // ── Sub-agent endpoints ──

  await page.route('**/api/pi/subagent/**', async route => {
    await route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify({ subAgents: [] }),
    });
  });

  // ── Approval response endpoint ──

  await page.route('**/api/pi/respond', async route => {
    await route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify({ success: true }),
    });
  });

  // ── Session endpoints ──

  await page.route('**/api/pi/sessions**', async route => {
    await route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify({ sessions: [] }),
    });
  });
}

// ─── Custom Test Fixture ──────────────────────────────────────────

type E2EFixtures = {
  mockPage: Page;
};

/**
 * Extended test fixture that auto-mocks API routes before each test.
 * Usage:
 *   import { e2e } from './fixtures';
 *   e2e('my test', async ({ mockPage }) => { ... });
 */
export const e2e = base.extend<E2EFixtures>({
  mockPage: async ({ page }, use) => {
    await mockApiRoutes(page);
    await use(page);
  },
});

export { expect };
