import React, { useState, useCallback, useEffect, useRef } from 'react';
import { cn } from '../lib/utils';
import { useOrchestrator } from '../hooks/useOrchestrator';
import { AgentTab } from './AgentTab';
import { ComputerUseTab } from './ComputerUseTab';
import { TaskBoardTab } from './TaskBoardTab';
import { FileBrowserPanel } from './FileBrowserPanel';
import { SubAgentPanel } from './SubAgentPanel';
import { SchedulerView } from './SchedulerView';
import { ToastProvider, useToastContext } from './ui/Toast';
import {
  Zap, Settings, ChevronDown, LayoutGrid, Monitor, Clock,
  FolderTree, Cpu
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

  // Keyboard shortcuts: Ctrl+1-4 switch tabs, Ctrl+K focuses agent prompt
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
            // Focus the prompt input after a tick
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
      <div className="h-10 border-b border-white/5 flex items-center px-4 shrink-0 bg-black/50 backdrop-blur-md gap-3">
        {/* Tab switcher with icons */}
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

        {/* GitHub connect button */}
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
