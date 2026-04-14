/**
 * Auto-Developer Orchestrator API Client
 * Typed wrapper for the Go backend REST API
 */

export interface ProjectResponse {
  projects: string[];
}

export interface Task {
  id: string;
  text: string;
  completed: boolean;
  status: 'completed' | 'in-progress' | 'pending' | 'cancelled';
}

export interface StatusResponse {
  gitState: string;
  workingTree: string;
  isAutoMode: boolean;
  agentStatus: string;
  lastCommit: string;
}

export interface ChecklistResponse {
  tasks: Task[];
}

export interface AIConfig {
  autoTask: boolean;
  autoTest: boolean;
  fullAutomationMode: boolean;
  postMergeTestGen: boolean;
  testGenPrompt: string;
  testTypes: {
    unit: boolean;
    e2e: boolean;
    integration: boolean;
    chaos: boolean;
    security: boolean;
    performance: boolean;
  };
}

export interface GitHubUser {
  connected: boolean;
  user?: {
    login: string;
    name: string;
    email: string;
    avatar_url: string;
  };
}

export interface DispatchResult {
  success: boolean;
  message: string;
  issueUrl: string;
  results?: Array<{ issueUrl: string }>;
}

export interface Repo {
  name: string;
  full_name: string;
  html_url: string;
  description: string;
  private: boolean;
  updated_at: string;
}

export interface ReposResponse {
  connected: boolean;
  repos?: Repo[];
}

export interface ActiveAgent {
  agentId: string;
  namespace: string; // OpenShell namespace for per-project isolation
  state: {
    model: string;
    streaming: boolean;
    input: number;
    output: number;
    cache: number;
  };
}

export interface ActiveProject {
  project: string;
  agents: ActiveAgent[];
}

export interface ActiveSessionsResponse {
  projects: ActiveProject[];
}

export interface ConversationSummary {
  project: string;
  agentId: string;
  lastMessage: string;
  lastAt: string;
  messageCount: number;
  title: string;
}

// Computer Use types
export interface LabeledElement {
  id: number;
  tag: string;
  text: string;
  role?: string;
  selector: string;
}

export interface PageInfo {
  url: string;
  title: string;
  elements: LabeledElement[];
  screenshot?: string; // base64 PNG
}

export interface ComputerUseAction {
  action: 'click' | 'type' | 'scroll' | 'navigate';
  element?: number;
  text?: string;
  url?: string;
  direction?: string;
  amount?: number;
  submit?: boolean;
}

export interface Artifact {
  id: string;
  agentId: string;
  type: 'plan' | 'todo' | 'notes';
  title: string;
  content: string;
  updatedAt: string;
}

export interface SchedulerJob {
  id: string;
  name: string;
  description?: string;
  project: string;
  agentId?: string;
  message: string;
  model?: string;
  scheduleType: 'cron' | 'every' | 'at' | 'manual';
  cronExpr?: string;
  timezone?: string;
  everySeconds?: number;
  atTime?: string;
  autoBranch?: boolean;
  autoMerge?: boolean;
  enabled: boolean;
  // Delivery (Phase 4)
  deliveryMode?: 'store' | 'webhook' | 'session';
  deliveryWebhookUrl?: string;
  // Failure alerts (Phase 3/4)
  failureAlertAfter?: number;
  failureAlertWebhookUrl?: string;
  status: 'idle' | 'running' | 'error' | 'disabled';
  lastRunAt?: string;
  lastRunStatus?: string;
  lastError?: string;
  nextRunAt?: string;
  consecutiveErrors: number;
  createdAt: string;
  updatedAt: string;
  // Metrics
  inputTokens?: number;
  outputTokens?: number;
  durationMs?: number;
  // Dependencies
  blocks?: string[];
  blockedBy?: string[];
  // Inbound webhook
  webhookToken?: string;
}

export interface SchedulerExecution {
  id: string;
  jobId: string;
  startedAt: string;
  endedAt?: string;
  status: 'running' | 'success' | 'error';
  error?: string;
  output?: string;
}

export interface SubAgentInfo {
  subAgentId: string;
  type: 'code' | 'explore' | 'web' | 'computer_use';
  status: 'pending' | 'running' | 'complete' | 'failed' | 'aborted';
  output: string;
  error?: string;
  inputTokens: number;
  outputTokens: number;
  cacheTokens: number;
  durationMs: number;
  toolCalls: number;
}

export interface FileEntry {
  name: string;
  is_dir: boolean;
  size?: number;
}

export interface RunLogEntry {
  ts: number;
  jobId: string;
  action: string;
  status?: string;
  error?: string;
  summary?: string;
  runAtMs?: number;
  durationMs?: number;
  nextRunAtMs?: number;
  model?: string;
  provider?: string;
}

export interface CreateJobRequest {
  name: string;
  description?: string;
  project: string;
  agentId?: string;
  message: string;
  model?: string;
  scheduleType: 'cron' | 'every' | 'at' | 'manual';
  cronExpr?: string;
  timezone?: string;
  everySeconds?: number;
  atTime?: string;
  autoBranch?: boolean;
  autoMerge?: boolean;
  enabled?: boolean;
  // Phase 4: Delivery
  deliveryMode?: 'store' | 'webhook' | 'session';
  deliveryWebhookUrl?: string;
  // Phase 3/4: Failure alerts
  failureAlertAfter?: number;
  failureAlertWebhookUrl?: string;
}

async function apiFetch<T>(path: string, options?: RequestInit): Promise<T> {
  const response = await fetch(path, {
    ...options,
    headers: {
      'Content-Type': 'application/json',
      ...options?.headers,
    },
  });

  if (!response.ok) {
    const error = await response.json().catch(() => ({ error: 'Unknown API error' }));
    throw new Error(error.error || `HTTP error ${response.status}`);
  }

  return response.json();
}

export const api = {
  projects: {
    list: () => apiFetch<ProjectResponse>('/api/projects'),
    register: (name: string, path?: string, repoUrl?: string) =>
      apiFetch<{ name: string }>('/api/projects/register', {
        method: 'POST',
        body: JSON.stringify({ name, path, repoUrl }),
      }),
  },
  status: {
    get: (project: string) =>
      apiFetch<StatusResponse>(`/api/status?project=${encodeURIComponent(project)}`),
    toggleMode: (project: string, mode: 'auto' | 'manual') =>
      apiFetch<{ success: boolean }>('/api/settings/mode', {
        method: 'POST',
        body: JSON.stringify({ mode, project }),
      }),
  },
  checklist: {
    get: (project: string) =>
      apiFetch<ChecklistResponse>(`/api/checklist?project=${encodeURIComponent(project)}`),
    update: (project: string, tasks: Task[]) =>
      apiFetch<{ success: boolean }>('/api/checklist/update', {
        method: 'POST',
        body: JSON.stringify({ tasks, project }),
      }),
  },
  config: {
    getAI: () => apiFetch<AIConfig>('/api/config/ai'),
    getGitHub: () => apiFetch<GitHubUser>('/api/github/user'),
    setAI: (config: AIConfig) =>
      apiFetch<{ success: boolean; aiConfig: AIConfig }>('/api/config/ai', {
        method: 'POST',
        body: JSON.stringify(config),
      }),
    connectGitHub: (token: string) =>
      apiFetch<{ success: boolean; error?: string }>('/api/config/github', {
        method: 'POST',
        body: JSON.stringify({ token }),
      }),
    getSystem: () => apiFetch<{ projectsDir: string }>('/api/config/system'),
    setSystem: (projectsDir: string) =>
      apiFetch<{ success: boolean; systemConfig: { projectsDir: string } }>('/api/config/system', {
        method: 'POST',
        body: JSON.stringify({ projectsDir }),
      }),
    getModels: () => apiFetch<{
      mainModel: { provider: string; modelId: string } | null;
      toolModel: { provider: string; modelId: string } | null;
    }>('/api/config/models'),
    setModels: (models: {
      mainModel?: { provider: string; modelId: string };
      toolModel?: { provider: string; modelId: string };
    }) =>
      apiFetch<{ success: boolean; mainModel: { provider: string; modelId: string }; toolModel: { provider: string; modelId: string } }>('/api/config/models', {
        method: 'PUT',
        body: JSON.stringify(models),
      }),
  },
  github: {
    getRepos: () => apiFetch<ReposResponse>('/api/github/repos'),
    getPRs: (owner: string, repo: string) =>
      apiFetch<{ prs: any[] }>(`/api/github/prs?owner=${encodeURIComponent(owner)}&repo=${encodeURIComponent(repo)}`),
    getStats: (owner: string, repo: string) =>
      apiFetch<{ stats: any }>(`/api/github/stats?owner=${encodeURIComponent(owner)}&repo=${encodeURIComponent(repo)}`),
    getBranches: (owner: string, repo: string) =>
      apiFetch<{ branches: any[] }>(`/api/github/branches?owner=${encodeURIComponent(owner)}&repo=${encodeURIComponent(repo)}`),
    getActivity: (owner: string, repo: string) =>
      apiFetch<{ events: any[] }>(`/api/github/activity?owner=${encodeURIComponent(owner)}&repo=${encodeURIComponent(repo)}`),
  },
  git: {
    clone: (url: string) =>
      apiFetch<{ message: string; projectName: string }>('/api/clone', {
        method: 'POST',
        body: JSON.stringify({ url }),
      }),
    merge: (project: string) =>
      apiFetch<{ summary: string }>('/api/merge', {
        method: 'POST',
        body: JSON.stringify({ project }),
      }),
  },
  pi: {
    prompt: (message: string, project: string, agentId: string = 'default', opts?: { model?: string; thinkingLevel?: string; autoBranch?: boolean }) => {
      return fetch('/api/pi/prompt', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ message, project, agentId, ...opts }),
      });
    },
    abort: (project: string, agentId: string = 'default') =>
      fetch(`/api/pi/abort?project=${encodeURIComponent(project)}&agentId=${encodeURIComponent(agentId)}`, { method: 'POST' }).then(() => {}),
    getState: (project: string, agentId: string = 'default') =>
      apiFetch<any>(`/api/pi/state?project=${encodeURIComponent(project)}&agentId=${encodeURIComponent(agentId)}`),
    getMessages: (project: string, agentId: string = 'default') =>
      apiFetch<any>(`/api/pi/messages?project=${encodeURIComponent(project)}&agentId=${encodeURIComponent(agentId)}`),
    getModels: (project: string, agentId: string = 'default') =>
      apiFetch<any>(`/api/pi/models?project=${encodeURIComponent(project)}&agentId=${encodeURIComponent(agentId)}`),
    setModel: (project: string, provider: string, modelId: string, agentId: string = 'default') =>
      fetch('/api/pi/model', {
        method: 'PUT',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ project, provider, modelId, agentId }),
      }).then(() => {}),
    compact: (project: string, agentId: string = 'default') =>
      fetch(`/api/pi/compact?project=${encodeURIComponent(project)}&agentId=${encodeURIComponent(agentId)}`, { method: 'POST' }).then(() => {}),
    getActiveSessions: () =>
      apiFetch<ActiveSessionsResponse>('/api/pi/active'),
    getHistory: () =>
      apiFetch<{ conversations: ConversationSummary[] }>('/api/pi/history'),
    deleteConversation: (project: string, agentId: string) =>
      apiFetch<{ deleted: boolean }>(`/api/pi/conversation?project=${encodeURIComponent(project)}&agentId=${encodeURIComponent(agentId)}`, {
        method: 'DELETE',
      }),
    renameConversation: (project: string, agentId: string, title: string) =>
      apiFetch<{ renamed: boolean }>(`/api/pi/conversation/rename?project=${encodeURIComponent(project)}&agentId=${encodeURIComponent(agentId)}`, {
        method: 'PUT',
        body: JSON.stringify({ title }),
      }),
    spawnAgent: (project: string, agentId?: string) =>
      apiFetch<{ success: boolean; agentId: string }>('/api/pi/agent/spawn', {
        method: 'POST',
        body: JSON.stringify({ project, agentId }),
      }),
    destroyAgent: (project: string, agentId: string) =>
      apiFetch<{ success: boolean }>('/api/pi/agent/destroy', {
        method: 'POST',
        body: JSON.stringify({ project, agentId }),
      }),
    respond: (project: string, agentId: string, requestId: string, action: 'approve' | 'deny' | 'answer', message?: string) =>
      apiFetch<{ success: boolean }>('/api/pi/respond', {
        method: 'POST',
        body: JSON.stringify({ project, agentId, requestId, action, message }),
      }),
  },
  x11: {
    mouse: (sandboxId: string, action: 'click' | 'move', x: number, y: number, button?: number) =>
      apiFetch<{ success: boolean }>(`/api/sandbox/${encodeURIComponent(sandboxId)}/x11/mouse`, {
        method: 'POST',
        body: JSON.stringify({ action, x, y, button }),
      }),
    keyboard: (sandboxId: string, action: 'type' | 'key', params: { text?: string; key?: string }) =>
      apiFetch<{ success: boolean }>(`/api/sandbox/${encodeURIComponent(sandboxId)}/x11/keyboard`, {
        method: 'POST',
        body: JSON.stringify({ action, ...params }),
      }),
    screenshot: (sandboxId: string, format: 'json' | 'png' = 'json') =>
      format === 'png'
        ? fetch(`/api/sandbox/${encodeURIComponent(sandboxId)}/x11/screenshot?format=png`).then(r => {
            if (!r.ok) throw new Error('X11 screenshot failed');
            return r.blob();
          })
        : apiFetch<{ image: string }>(`/api/sandbox/${encodeURIComponent(sandboxId)}/x11/screenshot`),
    resolution: (sandboxId: string) =>
      apiFetch<{ width: string; height: string }>(`/api/sandbox/${encodeURIComponent(sandboxId)}/x11/resolution`),
    activeWindow: (sandboxId: string) =>
      apiFetch<{ windowId: string; windowName: string }>(`/api/sandbox/${encodeURIComponent(sandboxId)}/x11/active-window`),
  },
  computerUse: {
    enable: (sandboxId: string, opts?: { signal?: AbortSignal }) =>
      apiFetch<{ enabled: boolean; sandboxId: string; cdpPort: number }>(`/api/sandbox/${encodeURIComponent(sandboxId)}/computer-use/enable`, {
        method: 'POST',
        signal: opts?.signal,
      }),
    disable: (sandboxId: string) =>
      apiFetch<{ disabled: boolean }>(`/api/sandbox/${encodeURIComponent(sandboxId)}/computer-use/disable`, {
        method: 'POST',
      }),
    screenshot: (sandboxId: string, describe = true) =>
      apiFetch<{ image: string; description?: string; url?: string; title?: string }>(`/api/sandbox/${encodeURIComponent(sandboxId)}/computer-use/screenshot?describe=${describe}`),
    screenshotRaw: (sandboxId: string) =>
      fetch(`/api/sandbox/${encodeURIComponent(sandboxId)}/computer-use/screenshot?format=png`).then(r => {
        if (!r.ok) throw new Error('Screenshot failed');
        return r.blob();
      }),
    snapshot: (sandboxId: string) =>
      apiFetch<{ url: string; title: string; elements: LabeledElement[] }>(`/api/sandbox/${encodeURIComponent(sandboxId)}/computer-use/snapshot`),
    act: (sandboxId: string, action: ComputerUseAction) =>
      apiFetch<PageInfo>(`/api/sandbox/${encodeURIComponent(sandboxId)}/computer-use/act`, {
        method: 'POST',
        body: JSON.stringify(action),
      }),
  },
  artifacts: {
    create: (agentId: string, type: 'plan' | 'todo' | 'notes', title: string, content: string) =>
      apiFetch<Artifact>(`/api/pi/artifacts`, {
        method: 'POST',
        body: JSON.stringify({ agentId, type, title, content }),
      }),
    list: (agentId: string) =>
      apiFetch<{ artifacts: Artifact[] }>(`/api/pi/artifacts?agentId=${encodeURIComponent(agentId)}`),
  },
  scheduler: {
    list: () =>
      apiFetch<{ jobs: SchedulerJob[] }>('/api/scheduler'),
    get: (id: string) =>
      apiFetch<SchedulerJob>(`/api/scheduler/${encodeURIComponent(id)}`),
    create: (job: CreateJobRequest) =>
      apiFetch<{ success: boolean; job: SchedulerJob }>('/api/scheduler', {
        method: 'POST',
        body: JSON.stringify(job),
      }),
    update: (id: string, job: Partial<CreateJobRequest>) =>
      apiFetch<{ success: boolean; job: SchedulerJob }>(`/api/scheduler/${encodeURIComponent(id)}`, {
        method: 'PUT',
        body: JSON.stringify(job),
      }),
    delete: (id: string) =>
      apiFetch<{ success: boolean }>(`/api/scheduler/${encodeURIComponent(id)}`, {
        method: 'DELETE',
      }),
    trigger: (id: string) =>
      apiFetch<{ success: boolean; message: string }>(`/api/scheduler/${encodeURIComponent(id)}/trigger`, {
        method: 'POST',
      }),
    executions: (id: string) =>
      apiFetch<{ executions: SchedulerExecution[] }>(`/api/scheduler/${encodeURIComponent(id)}/executions`),
    runs: (id: string, limit = 50) =>
      apiFetch<{ runs: RunLogEntry[] }>(`/api/scheduler/${encodeURIComponent(id)}/runs?limit=${limit}`),
    canStart: (id: string) =>
      apiFetch<{ canStart: boolean }>(`/api/scheduler/${encodeURIComponent(id)}/canStart`),
    setDeps: (id: string, blocks?: string[], blockedBy?: string[]) =>
      apiFetch<{ success: boolean }>(`/api/scheduler/${encodeURIComponent(id)}/deps`, {
        method: 'POST',
        body: JSON.stringify({ blocks, blockedBy }),
      }),
  },
  subAgents: {
    spawn: (parentAgentId: string, type: SubAgentInfo['type'] = 'code', projectDir?: string, message?: string) =>
      apiFetch<SubAgentInfo>('/api/pi/subagent/spawn', {
        method: 'POST',
        body: JSON.stringify({ parentAgentId, type, projectDir, message }),
      }),
    status: (subAgentId: string) =>
      apiFetch<SubAgentInfo>(`/api/pi/subagent/status?subAgentId=${encodeURIComponent(subAgentId)}`),
    result: (subAgentId: string) =>
      apiFetch<SubAgentInfo>(`/api/pi/subagent/result?subAgentId=${encodeURIComponent(subAgentId)}`),
    abort: (subAgentId: string) =>
      apiFetch<{ success: boolean }>('/api/pi/subagent/abort', {
        method: 'POST',
        body: JSON.stringify({ subAgentId }),
      }),
    list: (parentAgentId: string) =>
      apiFetch<{ subAgents: SubAgentInfo[] }>(`/api/pi/subagent/list?parentAgentId=${encodeURIComponent(parentAgentId)}`),
  },
  cli: {
    ls: (path: string) =>
      apiFetch<{ entries: FileEntry[] }>(`/api/cli/ls?path=${encodeURIComponent(path)}`),
    read: (path: string) =>
      apiFetch<{ content: string }>(`/api/cli/cat?path=${encodeURIComponent(path)}`),
    commands: () =>
      apiFetch<{ commands: string[] }>('/api/cli/commands'),
  },
};
