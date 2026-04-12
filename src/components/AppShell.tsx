import React, { useState, useCallback, useEffect } from 'react';
import { cn } from '../lib/utils';
import { useOrchestrator } from '../hooks/useOrchestrator';
import { AgentTab } from './AgentTab';
import { ComputerUseTab } from './ComputerUseTab';
import { TaskBoardTab } from './TaskBoardTab';
import { SchedulerView } from './SchedulerView';
import { ToastProvider } from './ui/Toast';
import {
  Zap, Settings, ChevronDown, LayoutGrid, Monitor, Clock,
  PanelLeftClose, PanelLeftOpen, PanelRightClose, PanelRightOpen
} from 'lucide-react';
import { GitHubConnectModal } from './GitHubConnectModal';
import { api } from '../lib/api';

type TabId = 'agent' | 'tasks' | 'desktop' | 'scheduler';

const TABS: { id: TabId; label: string; icon: React.ReactNode }[] = [
  { id: 'agent', label: 'Agent', icon: <Zap size={10} /> },
  { id: 'tasks', label: 'Tasks', icon: <LayoutGrid size={10} /> },
  { id: 'desktop', label: 'Desktop', icon: <Monitor size={10} /> },
  { id: 'scheduler', label: 'Scheduler', icon: <Clock size={10} /> },
];

function AppShellInner() {
  const addLog = useCallback((_msg: string, _type?: any) => {}, []);
  const { state, actions } = useOrchestrator(addLog);
  const [activeTab, setActiveTab] = useState<TabId>('agent');
  const [showGitHubModal, setShowGitHubModal] = useState(false);
  const [selectedProject, setSelectedProject] = useState<string | null>(null);

  // Panel collapse state — only used by Agent tab
  const [sidebarCollapsed, setSidebarCollapsed] = useState(false);
  const [rightPanelCollapsed, setRightPanelCollapsed] = useState(false);

  const { projects, githubUser } = state;
  const { refreshProjectData } = actions;

  // Auto-select first project
  useEffect(() => {
    if (!selectedProject && projects.length > 0) {
      setSelectedProject(projects[0]);
    }
  }, [projects, selectedProject]);

  const handleProjectChange = useCallback((project: string) => {
    setSelectedProject(project);
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
          case '4': e.preventDefault(); setActiveTab('scheduler'); break;
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

  const isAgentTab = activeTab === 'agent';

  return (
    <div className="flex flex-col h-screen bg-black text-slate-100 font-sans selection:bg-primary/20 overflow-hidden">
      {/* Top bar */}
      <div className="h-10 border-b border-white/5 flex items-center px-2 shrink-0 bg-black/50 backdrop-blur-md gap-1">
        {/* History panel toggle — only on Agent tab */}
        {isAgentTab && (
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
        )}

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

        {/* Artifacts panel toggle — only on Agent tab */}
        {isAgentTab && (
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
        )}

        {/* GitHub / Settings */}
        <button
          onClick={() => setShowGitHubModal(true)}
          className="flex items-center gap-1.5 text-muted hover:text-zinc-300 transition-colors"
          title="GitHub Settings"
        >
          <Settings size={14} />
          <span className="text-[8px] font-mono uppercase tracking-widest hidden md:inline">
            {githubUser?.connected ? 'Connected' : 'GitHub'}
          </span>
        </button>
      </div>

      {/* Tab content */}
      <div className="flex-1 overflow-hidden">
        {activeTab === 'agent' && (
          <AgentTab
            selectedProject={selectedProject}
            projects={projects}
            refreshProjectData={refreshProjectData}
            sidebarCollapsed={sidebarCollapsed}
            onSidebarToggle={() => setSidebarCollapsed(!sidebarCollapsed)}
            rightPanelCollapsed={rightPanelCollapsed}
            onRightPanelToggle={() => setRightPanelCollapsed(!rightPanelCollapsed)}
          />
        )}
        {activeTab === 'tasks' && (
          <TaskBoardTab selectedProject={selectedProject} />
        )}
        {activeTab === 'desktop' && (
          <ComputerUseTab
            selectedProject={selectedProject}
            projects={projects}
            refreshProjectData={refreshProjectData}
          />
        )}
        {activeTab === 'scheduler' && (
          <SchedulerView projects={projects} />
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
