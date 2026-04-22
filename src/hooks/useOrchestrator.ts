import { useState, useEffect, useCallback } from 'react';
import { api, ProjectResponse, Task, StatusResponse, AIConfig, GitHubUser } from '../lib/api';
import { PuxSSEEvent } from '../lib/pux-events';
import { readSSEStream } from './useSSEStream';

export type ModalType = 'review' | 'aiConfig' | 'coverage' | 'clone' | 'addProject' | 'user' | 'githubConnect' | null;

/**
 * Drain an SSE response stream, logging tool calls and errors.
 * Prevents response body leaks when the orchestrator dispatches tasks.
 */
async function drainSSEResponse(response: Response, addLog: (msg: string, type?: any) => void) {
  if (!response.body) return;
  await readSSEStream(response, (event: PuxSSEEvent) => {
    if (event.type === 'tool_execution_start') {
      addLog(`PUX_AGENT: Running tool ${(event.data as any).toolName}...`, 'INFO');
    } else if (event.type === 'error') {
      addLog(`PUX_AGENT_ERROR: ${(event.data as any).error}`, 'ERROR');
    } else if (event.type === 'pr_created') {
      addLog(`PUX_AGENT: PR #${(event.data as any).number} created - ${(event.data as any).url}`, 'SUCCESS');
    } else if (event.type === 'commit_created') {
      addLog(`PUX_AGENT: Committed "${(event.data as any).message}" to ${(event.data as any).branch}`, 'INFO');
    }
  });
}

export const useOrchestrator = (addLog: (msg: string, type?: any) => void) => {
  const [activeTab, setActiveTab] = useState<'terminal' | 'activity' | 'github' | 'agents' | 'manifesto'>('terminal');
  const [activeModal, setActiveModal] = useState<ModalType>(null);
  const [projects, setProjects] = useState<string[]>([]);
  const [selectedProject, setSelectedProject] = useState<string>('');
  const [tasks, setTasks] = useState<Task[]>([]);
  const [status, setStatus] = useState<StatusResponse | null>(null);
  const [aiConfig, setAiConfig] = useState<AIConfig | null>(null);
  const [githubUser, setGithubUser] = useState<GitHubUser | null>(null);
  const [isGeneratingChecklist, setIsGeneratingChecklist] = useState(false);
  const [isDispatching, setIsDispatching] = useState(false);
  const [isSidebarOpen, setIsSidebarOpen] = useState(false);
  const [isZenMode, setIsZenMode] = useState(false);

  // Initial data fetch
  useEffect(() => {
    const init = async () => {
      try {
        const [projRes, aiRes, githubRes] = await Promise.all([
          api.projects.list(),
          api.config.getAI(),
          api.config.getGitHub()
        ]);
        
        setProjects(projRes.projects);
        setAiConfig(aiRes);
        setGithubUser(githubRes);

        if (projRes.projects.length > 0) {
          setSelectedProject(projRes.projects[0]);
        }
      } catch (e) {
        addLog(`Initialization failed: ${e instanceof Error ? e.message : String(e)}`, 'ERROR');
      }
    };
    init();
  }, [addLog]);

  // Project-specific data fetch
  const refreshProjectData = useCallback(async () => {
    if (!selectedProject) return;
    try {
      const [statusRes, checklistRes] = await Promise.all([
        api.status.get(selectedProject),
        api.checklist.get(selectedProject)
      ]);
      setStatus(statusRes);
      setTasks(checklistRes.tasks || []);
    } catch (e) {
      addLog(`Failed to refresh project data: ${e instanceof Error ? e.message : String(e)}`, 'ERROR');
    }
  }, [selectedProject, addLog]);

  useEffect(() => {
    refreshProjectData();
  }, [refreshProjectData]);

  // Actions
  const handleToggleMode = async () => {
    if (!selectedProject || !status) return;
    const newMode = status.isAutoMode ? 'manual' : 'auto';
    try {
      await api.status.toggleMode(selectedProject, newMode);
      refreshProjectData();
    } catch (e) {
      addLog(`Failed to toggle mode: ${e instanceof Error ? e.message : String(e)}`, 'ERROR');
    }
  };

  const handleDispatch = async (taskId: string) => {
    if (!selectedProject) return;
    const task = tasks.find(t => t.id === taskId);
    if (!task) return;
    addLog(`PUX_AGENT: Dispatching task "${task.text}" to Pux agent...`, 'SYSTEM');
    setIsDispatching(true);
    try {
      const response = await api.pux.prompt(
        `Implement the following task in the current project:\n\nTask: ${task.text}\n\nProject: ${selectedProject}`,
        selectedProject
      );
      if (!response.ok) throw new Error('Pux prompt failed');
      addLog(`PUX_AGENT: Task dispatched, streaming response...`, 'INFO');
      // Consume the SSE stream so it isn't leaked
      await drainSSEResponse(response, addLog);
      addLog(`PUX_AGENT: Task completed successfully`, 'SUCCESS');
      refreshProjectData();
    } catch (e) {
      addLog(`PUX_AGENT_ERROR: ${e instanceof Error ? e.message : String(e)}`, 'ERROR');
    } finally {
      setIsDispatching(false);
    }
  };

  const handleDispatchAll = async () => {
    if (!selectedProject) return;
    const pendingTasks = tasks.filter(t => t.status === 'pending');
    if (pendingTasks.length === 0) {
      addLog('PUX_AGENT: No pending tasks to dispatch.', 'INFO');
      return;
    }
    setIsDispatching(true);

    const taskList = pendingTasks.map((t, i) => `${i + 1}. ${t.text}`).join('\n');
    addLog(`PUX_AGENT: Dispatching ${pendingTasks.length} tasks to Pux agent...`, 'SYSTEM');

    try {
      const response = await api.pux.prompt(
        `Implement the following tasks in the current project, one by one:\n\n${taskList}\n\nProject: ${selectedProject}`,
        selectedProject
      );
      if (!response.ok) throw new Error('Pux prompt failed');
      addLog(`PUX_AGENT: ${pendingTasks.length} tasks dispatched, streaming response...`, 'INFO');
      await drainSSEResponse(response, addLog);
      addLog(`PUX_AGENT: All tasks completed.`, 'SUCCESS');
    } catch (e) {
      addLog(`PUX_AGENT_ERROR: ${e instanceof Error ? e.message : String(e)}`, 'ERROR');
    } finally {
      setIsDispatching(false);
    }
  };

  const handleGenerateChecklist = async (prompt?: string) => {
    if (!selectedProject) return;
    setIsGeneratingChecklist(true);
    addLog('DEEP AGENT: Analyzing codebase and generating prioritized task list...', 'SYSTEM');
    if (prompt) addLog(`Guidance: "${prompt}"`, 'INFO');

    try {
      const res = await fetch('/api/ai/agent-checklist', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ prompt, project: selectedProject })
      });

      if (!res.ok) throw new Error('Checklist generation failed');
      if (!res.body) throw new Error('No response body');

      const reader = res.body.getReader();
      const decoder = new TextDecoder();
      let buffer = '';

      while (true) {
        const { value, done } = await reader.read();
        if (done) break;

        buffer += decoder.decode(value, { stream: true });
        const parts = buffer.split('\n\n');
        buffer = parts.pop() || '';

        for (const part of parts) {
          if (!part.trim()) continue;
          for (const line of part.split('\n')) {
            if (line.startsWith('data: ')) {
              try {
                const data = JSON.parse(line.slice(6));
                if (data.event === "on_tool_start") {
                  addLog(`DEEP AGENT: Running tool ${data.name}...`, 'INFO');
                } else if (data.event === "log") {
                   addLog(data.message, 'INFO');
                } else if (data.event === "error") {
                   addLog(data.message, 'ERROR');
                }
              } catch (e) { console.error("Error parsing SSE chunk", e); }
            }
          }
        }
      }

      // Flush remaining buffer
      if (buffer.trim()) {
        for (const line of buffer.split('\n')) {
          if (line.startsWith('data: ')) {
            try {
              const data = JSON.parse(line.slice(6));
              if (data.event === "on_tool_start") {
                addLog(`DEEP AGENT: Running tool ${data.name}...`, 'INFO');
              } else if (data.event === "log") {
                 addLog(data.message, 'INFO');
              } else if (data.event === "error") {
                 addLog(data.message, 'ERROR');
              }
            } catch (e) { console.error("Error parsing SSE chunk", e); }
          }
        }
      }

      addLog('DEEP AGENT: Analysis complete. Refreshing task list...', 'SUCCESS');
      await refreshProjectData();
    } catch (e) {
      addLog(`Failed to generate checklist: ${e instanceof Error ? e.message : String(e)}`, 'ERROR');
    } finally {
      setIsGeneratingChecklist(false);
    }
  };

  return {
    state: {
      activeTab, projects, selectedProject, tasks, status, aiConfig,
      githubUser, isGeneratingChecklist, isDispatching, activeModal,
      isSidebarOpen, isZenMode
    },
    actions: {
      setActiveTab, setSelectedProject, setActiveModal,
      setIsSidebarOpen, setIsZenMode,
      handleToggleMode, handleDispatch, handleDispatchAll, handleGenerateChecklist,
      refreshProjectData,
      setGithubUser, setAiConfig, // For modal callbacks
    }
  };
};
