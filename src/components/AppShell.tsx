import React, { useState, useCallback, useEffect, useRef } from 'react';
import { cn } from '../lib/utils';
import { useOrchestrator } from '../hooks/useOrchestrator';
import { useComputerUse } from '../hooks/useComputerUse';
import { HistorySidebar } from './HistorySidebar';
import { RightPanel } from './RightPanel';
import { AgentTab } from './AgentTab';
import { PiAgentView } from './PiAgentView';
import { ComputerUseTab } from './ComputerUseTab';
import { TaskBoardTab } from './TaskBoardTab';
import { ToastProvider } from './ui/Toast';
import { useResizable } from '../hooks/useResizable';
import { useArtifacts } from '../hooks/useArtifacts';
import {
  Zap, Settings, ChevronDown, LayoutGrid, Monitor, MessageSquare,
  PanelLeftClose, PanelLeftOpen, PanelRightClose, PanelRightOpen, Globe,
  FolderOpen, Plus
} from 'lucide-react';
import { GitHubConnectModal } from './GitHubConnectModal';
import { api, ConversationSummary } from '../lib/api';
import { ToolCall } from '../lib/pi-events';

type TabId = 'agent' | 'tasks' | 'desktop';

const TABS: { id: TabId; label: string; icon: React.ReactNode }[] = [
  { id: 'agent', label: 'Agent', icon: <Zap size={10} /> },
  { id: 'tasks', label: 'Tasks', icon: <LayoutGrid size={10} /> },
  { id: 'desktop', label: 'Desktop', icon: <Monitor size={10} /> },
];

// Per-tab labels for the sidebar toggle buttons
const LEFT_LABELS: Record<TabId, string> = {
  agent: 'Chats',
  tasks: '',
  desktop: 'Agent',
};
const RIGHT_LABELS: Record<TabId, string> = {
  agent: 'Artifacts',
  tasks: '',
  desktop: 'Browser',
};

function DesktopChatSection({
  projects,
  selectedProject,
  activeAgentId,
  onSelectProject,
  onSelectConversation,
  onNewConversation,
}: {
  projects: string[];
  selectedProject: string | null;
  activeAgentId: string;
  onSelectProject: (p: string) => void;
  onSelectConversation: (project: string, agentId: string) => void;
  onNewConversation: () => void;
}) {
  const [conversations, setConversations] = useState<ConversationSummary[]>([]);
  const [sandboxName, setSandboxName] = useState('');
  const [sandboxCreating, setSandboxCreating] = useState(false);

  // Fetch conversations for selected project
  useEffect(() => {
    if (!selectedProject) {
      setConversations([]);
      return;
    }
    let cancelled = false;
    const fetchConvos = async () => {
      try {
        const data = await api.pi.getHistory();
        if (!cancelled) {
          const filtered = (data.conversations || []).filter(
            (c: ConversationSummary) => c.project === selectedProject
          );
          setConversations(filtered);
        }
      } catch {
        if (!cancelled) setConversations([]);
      }
    };
    fetchConvos();
    const interval = setInterval(fetchConvos, 10000);
    return () => { cancelled = true; clearInterval(interval); };
  }, [selectedProject]);

  const createSandbox = useCallback(async () => {
    if (!sandboxName.trim()) return;
    setSandboxCreating(true);
    try {
      // Register as a local project via the API
      const res = await fetch('/api/projects/register', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ name: sandboxName.trim(), path: `/tmp/sandbox-${sandboxName.trim()}` }),
      });
      if (res.ok) {
        onSelectProject(sandboxName.trim());
        setSandboxName('');
      }
    } catch {
      // ignore
    } finally {
      setSandboxCreating(false);
    }
  }, [sandboxName, onSelectProject]);

  return (
    <div>
      {/* Section header */}
      <div className="px-3 py-2 border-b border-white/5 flex items-center gap-2">
        <MessageSquare size={10} className="text-muted-foreground" />
        <span className="text-xs font-black uppercase tracking-[0.2em] text-muted-foreground">
          Chats
        </span>
        <div className="flex-1" />
        <button
          onClick={onNewConversation}
          className="p-1 hover:bg-white/5 text-muted hover:text-zinc-300 transition-colors"
          title="New conversation"
        >
          <Plus size={10} />
        </button>
      </div>

      {/* Project selector */}
      <div className="px-3 py-2 border-b border-white/5">
        <div className="flex items-center gap-1 mb-1.5">
          <FolderOpen size={9} className="text-zinc-500" />
          <span className="text-[10px] font-mono uppercase tracking-widest text-zinc-500">Project</span>
        </div>
        <select
          value={selectedProject || ''}
          onChange={e => onSelectProject(e.target.value)}
          className="w-full bg-zinc-900 border border-white/10 rounded px-2 py-1 text-xs font-mono text-zinc-200 outline-none focus:border-primary/40"
        >
          <option value="">Select project...</option>
          {projects.map(p => (
            <option key={p} value={p}>{p}</option>
          ))}
        </select>

        {/* Create sandbox project */}
        <div className="mt-2 flex gap-1">
          <input
            value={sandboxName}
            onChange={e => setSandboxName(e.target.value)}
            placeholder="New sandbox name..."
            className="flex-1 bg-zinc-900 border border-white/10 rounded px-2 py-1 text-xs font-mono text-zinc-200 outline-none focus:border-primary/40"
            onKeyDown={e => { if (e.key === 'Enter') createSandbox(); }}
          />
          <button
            onClick={createSandbox}
            disabled={!sandboxName.trim() || sandboxCreating}
            className="px-2 py-1 text-xs font-mono bg-primary text-black rounded hover:bg-primary/80 disabled:opacity-30"
          >
            {sandboxCreating ? '...' : '+'}
          </button>
        </div>
      </div>

      {/* Conversation list */}
      <div className="max-h-48 overflow-y-auto">
        {conversations.length === 0 ? (
          <div className="px-3 py-3 text-xs font-mono text-zinc-700 text-center">
            {selectedProject ? 'No conversations yet' : 'Select a project'}
          </div>
        ) : (
          conversations.map(conv => {
            const isActive = conv.project === selectedProject && conv.agentId === activeAgentId;
            const title = conv.title || conv.lastMessage?.slice(0, 40) || conv.agentId;
            return (
              <button
                key={`${conv.project}-${conv.agentId}`}
                onClick={() => onSelectConversation(conv.project, conv.agentId)}
                className={cn(
                  "w-full text-left px-3 py-1.5 flex flex-col gap-0.5 transition-colors border-l-2",
                  isActive
                    ? "bg-primary/10 text-primary border-primary"
                    : "text-zinc-500 hover:bg-white/5 hover:text-zinc-300 border-transparent"
                )}
              >
                <span className="text-xs font-mono truncate">{title}</span>
                <span className="text-[10px] font-mono text-zinc-600">
                  {conv.messageCount} msgs
                </span>
              </button>
            );
          })
        )}
      </div>
    </div>
  );
}

function AppShellInner() {
  const addLog = useCallback((_msg: string, _type?: any) => {}, []);
  const { state, actions } = useOrchestrator(addLog);
  const [activeTab, setActiveTab] = useState<TabId>('agent');
  const [showGitHubModal, setShowGitHubModal] = useState(false);
  const [selectedProject, setSelectedProject] = useState<string | null>(null);

  // Shared sidebar collapse state — toggle buttons work on ALL tabs
  const [sidebarCollapsed, setSidebarCollapsed] = useState(false);
  const [rightPanelCollapsed, setRightPanelCollapsed] = useState(false);
  const [showSettings, setShowSettings] = useState(false);
  const [activeAgentId, setActiveAgentId] = useState('default');
  const [streamingState, setStreamingState] = useState<{
    isStreaming: boolean;
    runningTool: ToolCall | undefined;
    thinking: string;
  }>({ isStreaming: false, runningTool: undefined, thinking: '' });

  const { projects, githubUser } = state;
  const { refreshProjectData } = actions;

  // Single shared computer use instance
  const cu = useComputerUse();

  // Resolve the real sandbox ID from the API
  const [resolvedSandboxId, setResolvedSandboxId] = useState<string | null>(null);
  useEffect(() => {
    if (!selectedProject) {
      setResolvedSandboxId(null);
      return;
    }

    let cancelled = false;
    const resolve = async () => {
      try {
        const res = await fetch('/api/sandbox/');
        if (!res.ok || cancelled) return;
        const data = await res.json();
        const sandboxes = Array.isArray(data) ? data : (data.sandboxes || []);
        if (cancelled) return;
        if (sandboxes.length === 0) {
          setResolvedSandboxId(`sandbox-${selectedProject}`);
          return;
        }
        const match = sandboxes.find((s: any) =>
          s.id === selectedProject ||
          s.id === `sandbox-${selectedProject}` ||
          s.projectPath?.includes(selectedProject)
        );
        setResolvedSandboxId(match ? match.id : sandboxes[0].id);
      } catch {
        if (!cancelled) {
          setResolvedSandboxId(`sandbox-${selectedProject}`);
        }
      }
    };

    resolve();
    return () => { cancelled = true; };
  }, [selectedProject]);

  // Auto-enable computer use only when the Desktop tab is active
  const enableRef = useRef(cu.enableComputerUse);
  enableRef.current = cu.enableComputerUse;
  useEffect(() => {
    if (activeTab !== 'desktop') return;
    if (!resolvedSandboxId) return;
    if (cu.enabled && cu.sandboxId === resolvedSandboxId) return;
    if (cu.loading) return; // Don't retry while already enabling
    enableRef.current(resolvedSandboxId);
  }, [activeTab, resolvedSandboxId, cu.enabled, cu.sandboxId, cu.loading]);



  // Artifacts hook
  const artifactsHook = useArtifacts(selectedProject ? `${selectedProject}:${activeAgentId}` : null);

  // Resizable left sidebar
  const {
    width: sidebarWidth,
    isDragging: sidebarDragging,
    handleProps: sidebarHandleProps,
  } = useResizable({ defaultWidth: 224, minWidth: 180, maxWidth: 400, side: 'right' });

  // Resizable right panel
  const {
    width: rightPanelWidth,
    isDragging: rightPanelDragging,
    handleProps: rightPanelHandleProps,
  } = useResizable({ defaultWidth: 384, minWidth: 280, maxWidth: 600, side: 'left' });

  // Auto-select first project
  useEffect(() => {
    if (!selectedProject && projects.length > 0) {
      setSelectedProject(projects[0]);
    }
  }, [projects, selectedProject]);

  const handleProjectChange = useCallback((project: string) => {
    setSelectedProject(project);
  }, []);

  const handleStreamingStateChange = useCallback((state: { isStreaming: boolean; runningTool: ToolCall | undefined; thinking: string }) => {
    setStreamingState(state);
  }, []);

  // Listen for GitHub settings open event from child components
  useEffect(() => {
    const handler = () => setShowGitHubModal(true);
    window.addEventListener('open-github-settings', handler);
    return () => window.removeEventListener('open-github-settings', handler);
  }, []);

  // Keyboard shortcuts
  useEffect(() => {
    const handler = (e: KeyboardEvent) => {
      if (e.ctrlKey || e.metaKey) {
        switch (e.key) {
          case '1': e.preventDefault(); setActiveTab('agent'); break;
          case '2': e.preventDefault(); setActiveTab('tasks'); break;
          case '3': e.preventDefault(); setActiveTab('desktop'); break;
          case 'k':
            e.preventDefault();
            setActiveTab('agent');
            setTimeout(() => {
              const input = document.querySelector<HTMLInputElement>('[data-prompt-input]');
              input?.focus();
            }, 50);
            break;
        }
      }
    };
    window.addEventListener('keydown', handler);
    return () => window.removeEventListener('keydown', handler);
  }, []);

  const leftLabel = LEFT_LABELS[activeTab];
  const rightLabel = RIGHT_LABELS[activeTab];

  return (
    <div className="flex flex-col h-screen bg-black text-slate-100 font-sans selection:bg-primary/20 overflow-hidden">
      {/* Top bar */}
      <div className="h-10 border-b border-white/5 flex items-center px-2 shrink-0 bg-black/50 backdrop-blur-md gap-1">
        {/* Left sidebar toggle — label changes per tab */}
        {leftLabel && (
          <button
            onClick={() => setSidebarCollapsed(!sidebarCollapsed)}
            className={cn(
              'flex items-center gap-1 px-2 py-1.5 text-xs font-mono uppercase tracking-widest transition-colors rounded',
              !sidebarCollapsed
                ? 'text-primary bg-primary/10'
                : 'text-muted hover:text-muted-foreground hover:bg-white/5'
            )}
            title={sidebarCollapsed ? `Show ${leftLabel}` : `Hide ${leftLabel}`}
          >
            {sidebarCollapsed ? <PanelLeftOpen size={12} /> : <PanelLeftClose size={12} />}
            {!sidebarCollapsed && <span>{leftLabel}</span>}
          </button>
        )}

        {/* Tab switcher */}
        <div className="flex items-center gap-1">
          {TABS.map(tab => (
            <button
              key={tab.id}
              onClick={() => setActiveTab(tab.id)}
              className={cn(
                'flex items-center gap-1.5 px-3 py-1.5 text-sm font-mono uppercase tracking-widest transition-colors rounded',
                activeTab === tab.id
                  ? 'text-primary bg-primary/10'
                  : 'text-muted hover:text-muted-foreground hover:bg-white/5'
              )}
            >
              {tab.icon}
              {tab.label}
            </button>
          ))}
        </div>

        <div className="w-px h-4 bg-white/10" />

        {/* Project selector */}
        <div className="relative">
          <select
            value={selectedProject || ''}
            onChange={e => handleProjectChange(e.target.value)}
            className="appearance-none bg-transparent text-sm font-mono uppercase tracking-widest text-muted-foreground pr-4 cursor-pointer focus:outline-none"
          >
            {projects.length === 0 && <option value="">No projects</option>}
            {projects.map(p => (
              <option key={p} value={p} className="bg-zinc-900 text-white">{p}</option>
            ))}
          </select>
          <ChevronDown size={10} className="absolute right-0 top-1/2 -translate-y-1/2 text-muted-foreground pointer-events-none" />
        </div>

        <div className="flex-1" />

        <div className="flex items-center gap-1.5 text-xs font-mono uppercase tracking-widest text-muted-foreground">
          <Zap size={9} className="text-primary" />
          <span className="text-primary">PI</span>
        </div>

        <div className="flex-1" />

        {/* Right sidebar toggle — label changes per tab */}
        {rightLabel && (
          <button
            onClick={() => setRightPanelCollapsed(!rightPanelCollapsed)}
            className={cn(
              'flex items-center gap-1 px-2 py-1.5 text-xs font-mono uppercase tracking-widest transition-colors rounded',
              !rightPanelCollapsed
                ? 'text-primary bg-primary/10'
                : 'text-muted hover:text-muted-foreground hover:bg-white/5'
            )}
            title={rightPanelCollapsed ? `Show ${rightLabel}` : `Hide ${rightLabel}`}
          >
            {!rightPanelCollapsed && <span>{rightLabel}</span>}
            {rightPanelCollapsed ? <PanelRightOpen size={12} /> : <PanelRightClose size={12} />}
          </button>
        )}

        {/* Settings */}
        <button
          onClick={() => {
            if (rightPanelCollapsed) setRightPanelCollapsed(false);
            setShowSettings(!showSettings);
          }}
          className={cn(
            'flex items-center gap-1.5 px-2 py-1.5 text-xs font-mono uppercase tracking-widest transition-colors rounded',
            showSettings
              ? 'text-primary bg-primary/10'
              : 'text-muted hover:text-muted-foreground hover:bg-white/5'
          )}
          title="Settings"
        >
          <Settings size={12} />
          <span className="hidden md:inline">Settings</span>
        </button>
      </div>

      {/* Main content: left sidebar | center tab | right sidebar */}
      <div className="flex-1 overflow-hidden flex">
        {/* Left sidebar — content changes per tab */}
        {!sidebarCollapsed && leftLabel && (
          <div style={{ width: sidebarWidth }} className="relative shrink-0 border-r border-white/5 overflow-hidden">
            {activeTab === 'agent' && (
              <HistorySidebar
                projects={projects}
                activeProject={selectedProject || undefined}
                activeAgentId={activeAgentId}
                onSelectSession={(project: string, agentId: string) => {
                  setSelectedProject(project);
                  setActiveAgentId(agentId);
                  setActiveTab('agent');
                }}
                onNewChat={() => {
                  setActiveAgentId('default');
                  setActiveTab('agent');
                }}
              />
            )}
            {activeTab === 'desktop' && (
              <div className="absolute inset-0 bg-black">
                <PiAgentView
                  selectedProject={selectedProject || undefined}
                  selectedAgentId={activeAgentId}
                  projects={projects}
                  isZenMode={false}
                />
              </div>
            )}
            {/* Drag handle */}
            <div
              {...sidebarHandleProps}
              className={cn(
                'absolute top-0 right-0 bottom-0 w-1 cursor-col-resize z-10 transition-colors',
                sidebarDragging ? 'bg-primary/30' : 'hover:bg-white/10'
              )}
            />
          </div>
        )}

        {/* Center: tab content */}
        <div className="flex-1 min-w-0 overflow-hidden">
          {activeTab === 'agent' && (
            <AgentTab
              selectedProject={selectedProject}
              projects={projects}
              activeAgentId={activeAgentId}
              onActiveAgentIdChange={setActiveAgentId}
              onStreamingStateChange={handleStreamingStateChange}
            />
          )}
          {activeTab === 'tasks' && (
            <TaskBoardTab />
          )}
          {activeTab === 'desktop' && (
            <ComputerUseTab
              selectedProject={selectedProject}
              sandboxId={resolvedSandboxId}
              cu={cu}
            />
          )}
        </div>

        {/* Right sidebar — content changes per tab */}
        {!rightPanelCollapsed && rightLabel && (
          <div style={{ width: rightPanelWidth }} className="relative shrink-0 border-l border-white/5">
            {/* Drag handle */}
            <div
              {...rightPanelHandleProps}
              className={cn(
                'absolute top-0 left-0 bottom-0 w-1 cursor-col-resize z-10 transition-colors',
                rightPanelDragging ? 'bg-primary/30' : 'hover:bg-white/10'
              )}
            />
            {activeTab === 'agent' && (
              <RightPanel
                agentId={selectedProject ? `${selectedProject}:${activeAgentId}` : null}
                sandboxId={resolvedSandboxId}
                artifacts={artifactsHook.artifacts}
                artifactsLoading={artifactsHook.loading}
                streamingState={streamingState}
                cu={cu}
                showSettings={showSettings}
                onShowSettingsChange={setShowSettings}
              />
            )}
            {activeTab === 'desktop' && (
              <div className="h-full flex flex-col bg-black">
                {/* Browser header */}
                <div className="h-10 border-b border-white/5 flex items-center px-3 gap-2 shrink-0 bg-black/50">
                  <Globe size={12} className="text-primary" />
                  <span className="text-xs font-black uppercase tracking-[0.2em] text-muted-foreground">
                    Browser
                  </span>
                  <div className="flex-1" />
                  {cu.enabled ? (
                    <span className="text-xs font-mono text-primary">Active</span>
                  ) : (
                    <span className="text-xs font-mono text-zinc-600">Inactive</span>
                  )}
                </div>

                <div className="flex-1 overflow-y-auto custom-scrollbar">
                  {/* Chat selection section */}
                  <DesktopChatSection
                    projects={projects}
                    selectedProject={selectedProject}
                    activeAgentId={activeAgentId}
                    onSelectProject={handleProjectChange}
                    onSelectConversation={(project, agentId) => {
                      setSelectedProject(project);
                      setActiveAgentId(agentId);
                    }}
                    onNewConversation={() => setActiveAgentId('default')}
                  />

                  {/* Divider */}
                  <div className="border-t border-white/5" />

                  {/* Browser tools section */}
                  <div className="p-3 space-y-3">
                    {!cu.enabled && (
                      <div className="border border-white/5 p-3 space-y-2">
                        <p className="text-xs font-mono text-muted-foreground">
                          Enable browser automation for the sandbox.
                        </p>
                        <button
                          onClick={() => resolvedSandboxId && cu.enableComputerUse(resolvedSandboxId)}
                          disabled={!resolvedSandboxId || cu.loading}
                          className="px-3 py-1.5 bg-primary text-black text-xs font-black uppercase tracking-widest hover:bg-primary/80 disabled:opacity-30 transition-colors"
                        >
                          {cu.loading ? 'Starting...' : 'Enable Computer Use'}
                        </button>
                        {cu.error && (
                          <p className="text-xs font-mono text-red-400">{cu.error}</p>
                        )}
                      </div>
                    )}

                    {cu.enabled && (
                      <>
                        <div className="border border-white/5 p-3 space-y-2">
                          <span className="text-xs font-mono text-zinc-400">Navigate</span>
                          <form
                            onSubmit={(e) => {
                              e.preventDefault();
                              const url = (e.target as any).url.value;
                              if (url) cu.navigate(url);
                            }}
                            className="flex gap-1"
                          >
                            <input
                              name="url"
                              placeholder="https://"
                              className="flex-1 bg-zinc-900 border border-white/10 rounded px-2 py-1 text-xs font-mono text-zinc-200 outline-none focus:border-primary/40"
                            />
                            <button type="submit" className="px-2 py-1 text-xs font-mono bg-primary text-black rounded hover:bg-primary/80">
                              Go
                            </button>
                          </form>
                        </div>

                        <div className="border border-white/5 p-3 space-y-2">
                          <span className="text-xs font-mono text-zinc-400">Screenshot</span>
                          <button
                            onClick={() => cu.takeScreenshot()}
                            className="w-full px-3 py-1.5 text-xs font-mono uppercase tracking-widest text-primary border border-primary/20 hover:border-primary/40 hover:bg-primary/5 transition-colors"
                          >
                            Capture
                          </button>
                          {cu.screenshot && (
                            <img
                              src={`data:image/png;base64,${cu.screenshot}`}
                              alt="Desktop"
                              className="w-full border border-white/10 rounded"
                            />
                          )}
                        </div>

                        {cu.url && (
                          <div className="border border-white/5 p-3 space-y-1">
                            <span className="text-xs font-mono text-zinc-400">Current page</span>
                            <p className="text-xs font-mono text-primary break-all">{cu.url}</p>
                            {cu.title && (
                              <p className="text-xs font-mono text-zinc-500">{cu.title}</p>
                            )}
                          </div>
                        )}

                        <button
                          onClick={() => cu.disableComputerUse()}
                          className="w-full px-3 py-1.5 text-xs font-mono uppercase tracking-widest text-red-400/70 hover:text-red-400 border border-red-400/20 hover:border-red-400/40 transition-colors"
                        >
                          Disable Computer Use
                        </button>
                      </>
                    )}
                  </div>
                </div>
              </div>
            )}
          </div>
        )}
      </div>

      {/* GitHub modal */}
      {showGitHubModal && (
        <GitHubConnectModal
          isOpen
          onClose={() => setShowGitHubModal(false)}
          onConnect={(token) => api.config.connectGitHub(token).then(refreshProjectData)}
        />
      )}
    </div>
  );
}

export function AppShell() {
  return (
    <ToastProvider>
      <AppShellInner />
    </ToastProvider>
  );
}
