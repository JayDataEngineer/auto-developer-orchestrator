import React, { useState, useCallback } from 'react';
import { cn } from '../lib/utils';
import { useOrchestrator } from '../hooks/useOrchestrator';
import { AgentTab } from './AgentTab';
import { ComputerUseTab } from './ComputerUseTab';
import { Zap, Settings, ChevronDown } from 'lucide-react';
import { GitHubConnectModal } from './GitHubConnectModal';
import { SchedulerView } from './SchedulerView';
import { api } from '../lib/api';

type TabId = 'agent' | 'computer-use';

const TABS: { id: TabId; label: string }[] = [
  { id: 'agent', label: 'Agent' },
  { id: 'computer-use', label: 'Computer Use' },
];

export function AppShell() {
  const { state, actions } = useOrchestrator(() => {});
  const [activeTab, setActiveTab] = useState<TabId>('agent');
  const [settingsOpen, setSettingsOpen] = useState(false);
  const [activeModal, setActiveModal] = useState<string | null>(null);
  const [selectedProject, setSelectedProject] = useState<string | null>(null);

  const { projects, githubUser } = state;
  const { refreshProjectData } = actions;

  // Auto-select first project if none selected
  React.useEffect(() => {
    if (!selectedProject && projects.length > 0) {
      setSelectedProject(projects[0]);
    }
  }, [projects, selectedProject]);

  const handleProjectChange = useCallback((project: string) => {
    setSelectedProject(project);
  }, []);

  return (
    <div className="flex flex-col h-screen bg-black text-slate-100 font-sans selection:bg-primary/20 overflow-hidden">
      {/* Top bar */}
      <div className="h-10 border-b border-white/5 flex items-center px-4 shrink-0 bg-black/50 backdrop-blur-md gap-3">
        {/* Tab switcher */}
        <div className="flex items-center gap-1">
          {TABS.map(tab => (
            <button
              key={tab.id}
              onClick={() => setActiveTab(tab.id)}
              className={cn(
                'px-3 py-1.5 text-[10px] font-mono uppercase tracking-widest transition-colors rounded',
                activeTab === tab.id
                  ? 'text-primary bg-primary/10'
                  : 'text-muted hover:text-muted-foreground hover:bg-white/5'
              )}
            >
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

        {/* Settings dropdown */}
        <div className="relative">
          <button
            onClick={() => setSettingsOpen(!settingsOpen)}
            className="flex items-center gap-1.5 text-muted hover:text-zinc-300 transition-colors"
          >
            <Settings size={14} />
          </button>
          {settingsOpen && (
            <>
              <div className="fixed inset-0 z-40" onClick={() => setSettingsOpen(false)} />
              <div className="absolute right-0 top-full mt-1 w-56 border border-white/10 bg-zinc-950 shadow-2xl z-50">
                <button
                  onClick={() => { setActiveModal('githubConnect'); setSettingsOpen(false); }}
                  className="w-full text-left px-3 py-2 text-[10px] font-mono uppercase tracking-widest text-muted hover:bg-white/5 hover:text-zinc-300 flex items-center gap-2"
                >
                  <Zap size={10} /> GitHub {githubUser?.connected ? '(Connected)' : '(Connect)'}
                </button>
                <button
                  onClick={() => { setActiveModal('scheduler'); setSettingsOpen(false); }}
                  className="w-full text-left px-3 py-2 text-[10px] font-mono uppercase tracking-widest text-muted hover:bg-white/5 hover:text-zinc-300 flex items-center gap-2"
                >
                  <Zap size={10} /> Scheduled Jobs
                </button>
              </div>
            </>
          )}
        </div>
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
        {activeTab === 'computer-use' && (
          <ComputerUseTab
            selectedProject={selectedProject}
            projects={projects}
            refreshProjectData={refreshProjectData}
          />
        )}
      </div>

      {/* Modals */}
      {activeModal === 'githubConnect' && (
        <GitHubConnectModal isOpen onClose={() => setActiveModal(null)} onConnect={(token) => api.config.connectGitHub(token).then(refreshProjectData)} />
      )}
      {activeModal === 'scheduler' && (
        <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/60 backdrop-blur-sm">
          <div className="w-[700px] max-h-[80vh] border border-white/10 bg-zinc-950 shadow-2xl flex flex-col">
            <SchedulerView projects={projects} onClose={() => setActiveModal(null)} />
          </div>
        </div>
      )}
    </div>
  );
}
