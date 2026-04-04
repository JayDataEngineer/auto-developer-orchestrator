import React, { useState } from 'react';
import { cn } from './lib/utils';
import { HistorySidebar } from './components/HistorySidebar';
import { RightPanel } from './components/RightPanel';
import { PiAgentView } from './components/PiAgentView';
import { AIConfigModal } from './components/AIConfigModal';
import { CloneModal } from './components/CloneModal';
import { AddProjectModal } from './components/AddProjectModal';
import { GitHubConnectModal } from './components/GitHubConnectModal';
import { ErrorBoundary } from './components/ErrorBoundary';
import { useOrchestrator } from './hooks/useOrchestrator';
import { useArtifacts } from './hooks/useArtifacts';
import { SchedulerView } from './components/SchedulerView';
import { api } from './lib/api';
import {
  Box, Settings, Plus, GitBranch, Github, Zap, ChevronDown, ExternalLink, Clock,
} from 'lucide-react';

export default function App() {
  const { state, actions } = useOrchestrator(() => {});
  const [activeModal, setActiveModal] = useState<string | null>(null);
  const [settingsOpen, setSettingsOpen] = useState(false);
  const [activeAgentId, setActiveAgentId] = useState('default');

  const { projects, selectedProject, githubUser, isZenMode } = state;
  const { setSelectedProject, refreshProjectData, setIsZenMode } = actions;

  const artifactsHook = useArtifacts(selectedProject ? `${selectedProject}:${activeAgentId}` : null);
  const sandboxId = selectedProject ? `sandbox-${selectedProject}-${activeAgentId}` : null;

  return (
    <ErrorBoundary>
      <div className="flex h-screen bg-black text-slate-100 font-sans selection:bg-primary/20 overflow-hidden">
        {/* Left: History Sidebar */}
        {!isZenMode && (
          <HistorySidebar
            sessions={[]}
            activeSessionId={activeAgentId}
            onSelectSession={setActiveAgentId}
            onNewChat={() => setActiveAgentId('default')}
          />
        )}

        {/* Center column */}
        <div className="flex-1 flex flex-col min-w-0 overflow-hidden">
          {/* Slim toolbar header */}
          <div className="h-10 border-b border-white/5 flex items-center px-4 shrink-0 bg-black/50 backdrop-blur-md gap-3">
            {/* Project selector */}
            <div className="relative">
              <select
                value={selectedProject}
                onChange={e => setSelectedProject(e.target.value)}
                className="appearance-none bg-transparent text-[10px] font-mono uppercase tracking-widest text-muted-foreground pr-4 cursor-pointer focus:outline-none"
              >
                {projects.map(p => (
                  <option key={p} value={p} className="bg-zinc-900 text-white">{p}</option>
                ))}
              </select>
              <ChevronDown size={10} className="absolute right-0 top-1/2 -translate-y-1/2 text-muted-foreground pointer-events-none" />
            </div>

            <div className="w-px h-4 bg-white/10" />

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
                      onClick={() => { setActiveModal('aiConfig'); setSettingsOpen(false); }}
                      className="w-full text-left px-3 py-2 text-[10px] font-mono uppercase tracking-widest text-muted hover:bg-white/5 hover:text-zinc-300 flex items-center gap-2"
                    >
                      <Zap size={10} /> AI Config
                    </button>
                    <button
                      onClick={() => { setActiveModal('addProject'); setSettingsOpen(false); }}
                      className="w-full text-left px-3 py-2 text-[10px] font-mono uppercase tracking-widest text-muted hover:bg-white/5 hover:text-zinc-300 flex items-center gap-2"
                    >
                      <Plus size={10} /> Add Project
                    </button>
                    <button
                      onClick={() => { setActiveModal('clone'); setSettingsOpen(false); }}
                      className="w-full text-left px-3 py-2 text-[10px] font-mono uppercase tracking-widest text-muted hover:bg-white/5 hover:text-zinc-300 flex items-center gap-2"
                    >
                      <GitBranch size={10} /> Clone Repo
                    </button>
                    <button
                      onClick={() => { setActiveModal('githubConnect'); setSettingsOpen(false); }}
                      className="w-full text-left px-3 py-2 text-[10px] font-mono uppercase tracking-widest text-muted hover:bg-white/5 hover:text-zinc-300 flex items-center gap-2"
                    >
                      <Github size={10} /> GitHub {githubUser?.connected ? '(Connected)' : ''}
                    </button>
                    <button
                      onClick={() => { setActiveModal('scheduler'); setSettingsOpen(false); }}
                      className="w-full text-left px-3 py-2 text-[10px] font-mono uppercase tracking-widest text-muted hover:bg-white/5 hover:text-zinc-300 flex items-center gap-2"
                    >
                      <Clock size={10} /> Scheduled Jobs
                    </button>
                  </div>
                </>
              )}
            </div>

            {/* Zen toggle */}
            <button
              onClick={() => setIsZenMode(!isZenMode)}
              className="text-[9px] font-mono uppercase tracking-widest text-muted hover:text-zinc-300 transition-colors"
            >
              {isZenMode ? 'Exit Full' : 'Full'}
            </button>
          </div>

          {/* Agent chat view (center) */}
          <PiAgentView
            selectedProject={selectedProject}
            selectedAgentId={activeAgentId}
            projects={projects}
            isZenMode={isZenMode}
            onZenToggle={() => setIsZenMode(!isZenMode)}
          />
        </div>

        {/* Right: Browser / Artifacts Panel */}
        {!isZenMode && (
          <RightPanel
            agentId={selectedProject ? `${selectedProject}:${activeAgentId}` : null}
            sandboxId={sandboxId}
            artifacts={artifactsHook.artifacts}
            artifactsLoading={artifactsHook.loading}
          />
        )}

        {/* Modals */}
        <AIConfigModal isOpen={activeModal === 'aiConfig'} onClose={() => { setActiveModal(null); refreshProjectData(); }} />
        <CloneModal isOpen={activeModal === 'clone'} onClose={() => setActiveModal(null)} onClone={(url) => api.git.clone(url).then(refreshProjectData)} />
        <AddProjectModal isOpen={activeModal === 'addProject'} onClose={() => setActiveModal(null)} onAdd={(name, path, url) => api.projects.register(name, path, url).then(refreshProjectData)} />
        <GitHubConnectModal isOpen={activeModal === 'githubConnect'} onClose={() => setActiveModal(null)} onConnect={(token) => api.config.connectGitHub(token).then(refreshProjectData)} />
        {activeModal === 'scheduler' && (
          <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/60 backdrop-blur-sm">
            <div className="w-[700px] max-h-[80vh] border border-white/10 bg-zinc-950 shadow-2xl flex flex-col">
              <SchedulerView projects={projects} onClose={() => setActiveModal(null)} />
            </div>
          </div>
        )}
      </div>
    </ErrorBoundary>
  );
}
