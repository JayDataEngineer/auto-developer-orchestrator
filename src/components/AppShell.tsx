import React, { useState, useCallback, useEffect, useRef } from 'react';
import { cn } from '../lib/utils';
import { useOrchestrator } from '../hooks/useOrchestrator';
import { useComputerUse } from '../hooks/useComputerUse';
import { usePuxAgentContext } from '../contexts/PuxAgentContext';
import { HistorySidebar } from './HistorySidebar';
import { AgentTab } from './AgentTab';
import { PuxAgentView } from './PuxAgentView';
import { ComputerViewer } from './ComputerViewer';
import { InputBar } from './InputBar';
import { TaskBoardTab } from './TaskBoardTab';
import { ToastProvider } from './ui/Toast';
import { Button } from './ui/button';
import { Tooltip, TooltipContent, TooltipTrigger, TooltipProvider } from './ui/tooltip';
import { Separator } from './ui/separator';
import { Select, SelectTrigger, SelectValue, SelectContent, SelectItem } from './ui/select';
import { useResizable } from '../hooks/useResizable';
import {
  Zap, Settings, LayoutGrid, Monitor,
  PanelLeftClose, PanelLeftOpen,
} from 'lucide-react';
import { GitHubConnectModal } from './GitHubConnectModal';
import { api } from '../lib/api';
import { PuxModel, ToolCall } from '../lib/pux-events';

type TabId = 'agent' | 'tasks' | 'desktop';

const TABS: { id: TabId; label: string; icon: React.ReactNode }[] = [
  { id: 'agent', label: 'Code', icon: <Zap size={10} /> },
  { id: 'tasks', label: 'Automate', icon: <LayoutGrid size={10} /> },
  { id: 'desktop', label: 'Pilot', icon: <Monitor size={10} /> },
];

function AppShellInner() {
  const addLog = useCallback((_msg: string, _type?: any) => {}, []);
  const { state, actions } = useOrchestrator(addLog);
  const { state: puxState, sendPrompt, abort, compact, switchModel, reset } = usePuxAgentContext();
  const [activeTab, setActiveTab] = useState<TabId>('agent');
  const [showGitHubModal, setShowGitHubModal] = useState(false);
  const [selectedProject, setSelectedProject] = useState<string | null>(null);

  // Sidebar state
  const [sidebarCollapsed, setSidebarCollapsed] = useState(false);
  const [activeAgentId, setActiveAgentId] = useState('default');
  const [streamingState, setStreamingState] = useState<{
    isStreaming: boolean;
    runningTool: ToolCall | undefined;
    thinking: string;
  }>({ isStreaming: false, runningTool: undefined, thinking: '' });

  const { projects, githubUser } = state;
  const safeProjects = projects ?? [];
  const { refreshProjectData } = actions;

  // Computer use — shared across tabs
  const cu = useComputerUse();

  // Resolve sandbox ID
  const [resolvedSandboxId, setResolvedSandboxId] = useState<string | null>(null);
  useEffect(() => {
    if (!selectedProject) { setResolvedSandboxId(null); return; }
    let cancelled = false;
    const resolve = async () => {
      try {
        const sandboxes = await api.sandbox.list();
        if (cancelled) return;
        if (sandboxes.length === 0) { setResolvedSandboxId(`sandbox-${selectedProject}`); return; }
        const match = sandboxes.find((s: any) =>
          s.id === selectedProject || s.id === `sandbox-${selectedProject}` || s.projectPath?.includes(selectedProject)
        );
        setResolvedSandboxId(match ? match.id : sandboxes[0].id);
      } catch {
        if (!cancelled) setResolvedSandboxId(`sandbox-${selectedProject}`);
      }
    };
    resolve();
    return () => { cancelled = true; };
  }, [selectedProject]);

  // Auto-enable computer use on Pilot tab
  const enableRef = useRef(cu.enableComputerUse);
  enableRef.current = cu.enableComputerUse;
  useEffect(() => {
    if (activeTab !== 'desktop') return;
    if (!resolvedSandboxId) return;
    if (cu.enabled && cu.sandboxId === resolvedSandboxId) return;
    if (cu.loading) return;
    enableRef.current(resolvedSandboxId);
  }, [activeTab, resolvedSandboxId, cu.enabled, cu.sandboxId, cu.loading]);

  // Resizable left sidebar (Code tab only)
  const {
    width: sidebarWidth,
    isDragging: sidebarDragging,
    handleProps: sidebarHandleProps,
  } = useResizable({ defaultWidth: 224, minWidth: 180, maxWidth: 400, side: 'right' });

  // Resizable VNC viewer (Pilot tab)
  const {
    height: vncHeight,
    isDragging: vncDragging,
    handleProps: vncHandleProps,
  } = useResizable({ defaultWidth: 320, minWidth: 150, maxWidth: 800, side: 'bottom' } as any);

  // Auto-select first project
  useEffect(() => {
    if (!selectedProject && safeProjects.length > 0) setSelectedProject(safeProjects[0]);
  }, [safeProjects, selectedProject]);

  const handleProjectChange = useCallback((project: string) => setSelectedProject(project), []);

  const handleStreamingStateChange = useCallback((s: { isStreaming: boolean; runningTool: ToolCall | undefined; thinking: string }) => {
    setStreamingState(s);
  }, []);

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
            setTimeout(() => { document.querySelector<HTMLInputElement>('[data-prompt-input]')?.focus(); }, 50);
            break;
        }
      }
    };
    window.addEventListener('keydown', handler);
    return () => window.removeEventListener('keydown', handler);
  }, []);

  // ── Shared agent state for InputBar in Pilot tab ──
  const [models, setModels] = useState<PuxModel[]>([]);
  const [toolModel, setToolModel] = useState<string | null>(null);
  const [autoBranch, setAutoBranch] = useState(false);
  const [autoMerge, setAutoMerge] = useState(false);

  useEffect(() => {
    api.config.getModels().then((cfg) => {
      if (cfg.toolModel?.modelId) setToolModel(cfg.toolModel.modelId);
    }).catch(() => {});
  }, []);

  useEffect(() => {
    if (selectedProject) api.pux.getModels(selectedProject).then((data: any) => {
      const list = Array.isArray(data) ? data : data?.models ?? [];
      setModels(Array.isArray(list) ? list : []);
    }).catch(() => {});
  }, [selectedProject]);

  const showLeftSidebar = activeTab === 'agent' && !sidebarCollapsed;

  return (
    <div className="flex flex-col h-screen bg-background text-foreground font-sans selection:bg-primary/20 overflow-hidden">
      {/* Top bar */}
      <div className="h-10 border-b border-border flex items-center px-2 shrink-0 bg-sidebar backdrop-blur-md gap-1">
        {/* Left sidebar toggle (Code tab only) */}
        {activeTab === 'agent' && (
          <Tooltip>
            <TooltipTrigger asChild>
              <Button
                variant="ghost"
                size="icon-xs"
                onClick={() => setSidebarCollapsed(!sidebarCollapsed)}
                className={!sidebarCollapsed ? 'text-primary bg-primary/10' : ''}
              >
                {sidebarCollapsed ? <PanelLeftOpen size={12} /> : <PanelLeftClose size={12} />}
                {!sidebarCollapsed && <span>History</span>}
              </Button>
            </TooltipTrigger>
            <TooltipContent>{sidebarCollapsed ? 'Show History' : 'Hide History'}</TooltipContent>
          </Tooltip>
        )}

        {/* Tab switcher */}
        <div className="flex items-center gap-1">
          {TABS.map((tab, i) => (
            <Tooltip key={tab.id}>
              <TooltipTrigger asChild>
                <Button
                  variant="ghost"
                  size="xs"
                  onClick={() => setActiveTab(tab.id)}
                  className={activeTab === tab.id ? 'text-primary bg-primary/10' : ''}
                >
                  {tab.icon}
                  {tab.label}
                </Button>
              </TooltipTrigger>
              <TooltipContent>{tab.label} <span className="kbd">Ctrl+{i+1}</span></TooltipContent>
            </Tooltip>
          ))}
        </div>

        <Separator orientation="vertical" className="h-4" />

        {/* Project selector */}
        <Select value={selectedProject || ''} onValueChange={handleProjectChange}>
          <SelectTrigger className="w-48 bg-transparent border-0">
            <SelectValue placeholder="No projects" />
          </SelectTrigger>
          <SelectContent>
            {safeProjects.map(p => (
              <SelectItem key={p} value={p}>{p}</SelectItem>
            ))}
          </SelectContent>
        </Select>

        <div className="flex-1" />

        <div className="flex items-center gap-1.5 text-xs font-semibold text-muted-foreground">
          <Zap size={9} className="text-primary" />
          <span className="text-primary">Pux</span>
        </div>

        <div className="flex-1" />

        {/* Settings */}
        <Tooltip>
          <TooltipTrigger asChild>
            <Button variant="ghost" size="xs" onClick={() => window.dispatchEvent(new CustomEvent('open-github-settings'))}>
              <Settings size={12} />
            </Button>
          </TooltipTrigger>
          <TooltipContent>Settings</TooltipContent>
        </Tooltip>
      </div>

      {/* Main content */}
      <div className="flex-1 overflow-hidden flex">
        {/* Left sidebar — Code tab only */}
        {showLeftSidebar && (
          <div style={{ width: sidebarWidth }} className="relative shrink-0 border-r border-border overflow-hidden bg-sidebar">
            <HistorySidebar
              projects={safeProjects}
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
            {/* Drag handle */}
            <div
              {...sidebarHandleProps}
              className={cn(
                'absolute top-0 right-0 bottom-0 w-1.5 cursor-col-resize z-10 touch-none',
                sidebarDragging ? 'bg-primary/30' : 'hover:bg-muted'
              )}
            />
          </div>
        )}

        {/* Center: tab content */}
        <div className="flex-1 min-w-0 overflow-hidden">
          {activeTab === 'agent' && (
            <AgentTab
              selectedProject={selectedProject}
              projects={safeProjects}
              activeAgentId={activeAgentId}
              onActiveAgentIdChange={setActiveAgentId}
              onStreamingStateChange={handleStreamingStateChange}
            />
          )}

          {activeTab === 'tasks' && (
            <TaskBoardTab currentProject={selectedProject} />
          )}

          {activeTab === 'desktop' && (
            <div className="h-full flex flex-col">
              {/* VNC viewer — resizable */}
              <div style={{ height: vncHeight }} className="shrink-0 relative border-b border-border">
                <ComputerViewer
                  sandboxId={resolvedSandboxId}
                  enabled={cu.enabled}
                  loading={cu.loading}
                  error={cu.error}
                />
                {/* Drag handle */}
                <div
                  {...vncHandleProps}
                  className={cn(
                    'absolute bottom-0 left-0 right-0 h-1.5 cursor-row-resize z-10 touch-none',
                    vncDragging ? 'bg-primary/30' : 'hover:bg-muted'
                  )}
                />
              </div>

              {/* Chat area — PuxAgentView without InputBar */}
              <div className="flex-1 overflow-hidden">
                <PuxAgentView
                  selectedProject={selectedProject || undefined}
                  selectedAgentId={activeAgentId}
                  projects={safeProjects}
                  hideInputBar
                  onStreamingStateChange={handleStreamingStateChange}
                />
              </div>

              {/* Input bar — wired to shared PuxAgentContext */}
              <InputBar
                isStreaming={puxState.isStreaming}
                disabled={puxState.isStreaming || !selectedProject}
                model={puxState.model}
                models={models}
                selectedProject={selectedProject || undefined}
                selectedAgentId={activeAgentId}
                isZenMode={false}
                autoBranch={autoBranch}
                autoMerge={autoMerge}
                toolModel={toolModel}
                onSend={(text: string) => {
                  if (!selectedProject || puxState.isStreaming) return;
                  sendPrompt(text, selectedProject, { agentId: activeAgentId, model: puxState.model || 'or-free', thinkingLevel: 'medium', autoBranch, autoMerge });
                }}
                onAbort={() => { if (selectedProject) abort(selectedProject, activeAgentId); }}
                onReset={reset}
                onSwitchModel={(provider: string, modelId: string) => { if (selectedProject) switchModel(selectedProject, provider, modelId, activeAgentId); }}
                onCompact={() => { if (selectedProject) compact(selectedProject, activeAgentId); }}
                onAutoBranchChange={setAutoBranch}
                onAutoMergeChange={setAutoMerge}
                onSetToolModel={async (provider: string, modelId: string) => {
                  try { await api.config.setModels({ toolModel: { provider, modelId } }); setToolModel(modelId); } catch {}
                }}
              />
            </div>
          )}
        </div>
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
      <TooltipProvider delayDuration={300}>
        <AppShellInner />
      </TooltipProvider>
    </ToastProvider>
  );
}
