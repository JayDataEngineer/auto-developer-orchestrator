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
  fullAutomationMode: boolean;
  testGenPrompt: string;
  model: string;
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

export interface ActiveAgent {
  agentId: string;
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
    connectGitHub: (token: string) =>
      apiFetch<{ success: boolean; error?: string }>('/api/config/github', {
        method: 'POST',
        body: JSON.stringify({ token }),
      }),
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
  },
};
