import React, { useState, useCallback, useEffect, useRef } from 'react';
import { cn } from '../lib/utils';
import { useOrchestrator } from '../hooks/useOrchestrator';
import { useComputerUse } from '../hooks/useComputerUse';
import { HistorySidebar } from './HistorySidebar';
import { RightPanel } from './RightPanel';
import { AgentTab } from './AgentTab';
import { ComputerUseTab } from './ComputerUseTab';
import { TaskBoardTab } from './TaskBoardTab';
import { ToastProvider } from './ui/Toast';
import { useResizable } from '../hooks/useResizable';
import { useArtifacts } from '../hooks/useArtifacts';
import {
  Zap, Settings, ChevronDown, LayoutGrid, Monitor,
  PanelLeftClose, PanelLeftOpen, PanelRightClose, PanelRightOpen
} from 'lucide-react';
import { GitHubConnectModal } from './GitHubConnectModal';
import { api } from '../lib/api';
import { ToolCall } from '../lib/pi-events';

type TabId = 'agent' | 'tasks' | 'desktop';

const TABS: { id: TabId; label: string; icon: React.ReactNode }[] = [
  { id: 'agent', label: 'Agent', icon: <Zap size={10} /> },
  { id: 'tasks', label: 'Tasks', icon: <LayoutGrid size={10} /> },
  { id: 'desktop', label: 'Desktop', icon: <Monitor size={10} /> },
];

function AppShellInner() {
  const addLog = useCallback((_msg: string, _type?: any) => {}, []);
  const { state, actions } = useOrchestrator(addLog);
  const [activeTab, setActiveTab] = useState<TabId>('agent');
  const [showGitHubModal, setShowGitHubModal] = useState(false);
  const [selectedProject, setSelectedProject] = useState<string | null>(null);

  // Shared sidebar state — works on ALL tabs
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

  // Single shared computer use instance — one state, one sandbox
  const cu = useComputerUse();

  // Resolve the real sandbox ID from the API (shared across all tabs)
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
          // No sandboxes exist yet — use fallback ID, backend will create it
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

  // Auto-enable computer use when sandbox is resolved
  const enableRef = useRef(cu.enableComputerUse);
  enableRef.current = cu.enableComputerUse;
  useEffect(() => {
    if (!resolvedSandboxId) return;
    if (cu.enabled && cu.sandboxId === resolvedSandboxId) return;
    enableRef.current(resolvedSandboxId);
  }, [resolvedSandboxId, cu.enabled, cu.sandboxId]);

  // Artifacts hook — shared across all tabs
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

  return (
    <div className="flex flex-col h-screen bg-black text-slate-100 font-sans selection:bg-primary/20 overflow-hidden">
      {/* Top bar */}
      <div className="h-10 border-b border-white/5 flex items-center px-2 shrink-0 bg-black/50 backdrop-blur-md gap-1">
        {/* History panel toggle — always visible */}
        <button
          onClick={() => setSidebarCollapsed(!sidebarCollapsed)}
          className={cn(
            'flex items-center gap-1 px-2 py-1.5 text-[9px] font-mono uppercase tracking-widest transition-colors rounded',
            !sidebarCollapsed
              ? 'text-primary bg-primary/10'
              : 'text-muted hover:text-muted-foreground hover:bg-white/5'
          )}
          title={sidebarCollapsed ? 'Show History' : 'Hide History'}
        >
          {sidebarCollapsed ? <PanelLeftOpen size={12} /> : <PanelLeftClose size={12} />}
          {!sidebarCollapsed && <span>History</span>}
        </button>

        {/* Tab switcher */}
        <div className="flex items-center gap-1">
          {TABS.map(tab => (
            <button
              key={tab.id}
              onClick={() => setActiveTab(tab.id)}
              className={cn(
                'flex items-center gap-1.5 px-3 py-1.5 text-[10px] font-mono uppercase tracking-widest transition-colors rounded',
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
            className="appearance-none bg-transparent text-[10px] font-mono uppercase tracking-widest text-muted-foreground pr-4 cursor-pointer focus:outline-none"
          >
            {projects.length === 0 && <option value="">No projects</option>}
            {projects.map(p => (
              <option key={p} value={p} className="bg-zinc-900 text-white">{p}</option>
            ))}
          </select>
          <ChevronDown size={10} className="absolute right-0 top-1/2 -translate-y-1/2 text-muted-foreground pointer-events-none" />
        </div>

        <div className="flex-1" />

        <div className="flex items-center gap-1.5 text-[9px] font-mono uppercase tracking-widest text-muted-foreground">
          <Zap size={9} className="text-primary" />
          <span className="text-primary">PI</span>
        </div>

        <div className="flex-1" />

        {/* Artifacts panel toggle — always visible */}
        <button
          onClick={() => setRightPanelCollapsed(!rightPanelCollapsed)}
          className={cn(
            'flex items-center gap-1 px-2 py-1.5 text-[9px] font-mono uppercase tracking-widest transition-colors rounded',
            !rightPanelCollapsed
              ? 'text-primary bg-primary/10'
              : 'text-muted hover:text-muted-foreground hover:bg-white/5'
          )}
          title={rightPanelCollapsed ? 'Show Artifacts' : 'Hide Artifacts'}
        >
          {!rightPanelCollapsed && <span>Artifacts</span>}
          {rightPanelCollapsed ? <PanelRightOpen size={12} /> : <PanelRightClose size={12} />}
        </button>

        {/* Settings — opens right panel settings view */}
        <button
          onClick={() => {
            if (rightPanelCollapsed) setRightPanelCollapsed(false);
            setShowSettings(!showSettings);
          }}
          className={cn(
            'flex items-center gap-1.5 px-2 py-1.5 text-[9px] font-mono uppercase tracking-widest transition-colors rounded',
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

      {/* Main content: History sidebar | center tab | Artifacts panel */}
      <div className="flex-1 overflow-hidden flex">
        {/* Left: History Sidebar (resizable) */}
        {!sidebarCollapsed && (
          <div style={{ width: sidebarWidth }} className="relative shrink-0 border-r border-white/5">
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
            {/* Drag handle on right edge */}
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
            <TaskBoardTab selectedProject={selectedProject} />
          )}
          {activeTab === 'desktop' && (
            <ComputerUseTab
              selectedProject={selectedProject}
              sandboxId={resolvedSandboxId}
              cu={cu}
            />
          )}
        </div>

        {/* Right: Artifacts Panel (resizable) */}
        {!rightPanelCollapsed && (
          <div style={{ width: rightPanelWidth }} className="relative shrink-0 border-l border-white/5">
            {/* Drag handle on left edge */}
            <div
              {...rightPanelHandleProps}
              className={cn(
                'absolute top-0 left-0 bottom-0 w-1 cursor-col-resize z-10 transition-colors',
                rightPanelDragging ? 'bg-primary/30' : 'hover:bg-white/10'
              )}
            />
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
