import React, { useState, useCallback, useEffect } from 'react';
import { cn } from '../lib/utils';
import { usePiAgent } from '../hooks/usePiAgent';
import { usePiSessionManager } from '../hooks/usePiSessionManager';
import { PiSessionCard } from './PiSessionCard';
import { PiAgentView } from './PiAgentView';
import { Send, GitBranch, Zap, ArrowLeft } from 'lucide-react';

interface PiDashboardViewProps {
  selectedProject?: string;
  projects: string[];
  onProjectSelect?: (project: string) => void;
}

// One hook per project — rendered at the top level to satisfy React's rules of hooks.
function ProjectSessionRow({
  project,
  manager,
  expandedProject,
  onExpand,
}: {
  project: string;
  manager: ReturnType<typeof usePiSessionManager>;
  expandedProject: string | null;
  onExpand: (project: string | null) => void;
}) {
  const hook = usePiAgent();

  // Register this hook with the session manager
  useEffect(() => {
    manager.registerSession(project, hook);
    return () => manager.unregisterSession(project);
  }, [project]); // eslint-disable-line react-hooks/exhaustive-deps

  // Hydrate on mount
  useEffect(() => {
    hook.hydrateState(project);
  }, [project]); // eslint-disable-line react-hooks/exhaustive-deps

  const isExpanded = expandedProject === project;

  return (
    <PiSessionCard
      project={project}
      state={hook.state}
      isExpanded={isExpanded}
      onClick={() => onExpand(isExpanded ? null : project)}
    />
  );
}

export const PiDashboardView: React.FC<PiDashboardViewProps> = ({
  selectedProject,
  projects,
  onProjectSelect,
}) => {
  const manager = usePiSessionManager(projects);
  const [expandedProject, setExpandedProject] = useState<string | null>(null);
  const [promptText, setPromptText] = useState('');
  const [promptProject, setPromptProject] = useState<string>(selectedProject || projects[0] || '');
  const [autoBranch, setAutoBranch] = useState(false);
  const [activeCount, setActiveCount] = useState(0);

  // Update prompt project when selected project changes
  useEffect(() => {
    if (selectedProject) setPromptProject(selectedProject);
  }, [selectedProject]);

  // Poll active count
  useEffect(() => {
    const interval = setInterval(() => {
      setActiveCount(manager.activeCount());
    }, 1000);
    return () => clearInterval(interval);
  }, [manager]);

  const handleSend = useCallback(() => {
    if (!promptText.trim() || !promptProject) return;
    manager.sendPrompt(promptProject, promptText.trim(), { autoBranch });
    setPromptText('');
  }, [promptText, promptProject, autoBranch, manager]);

  const handleKeyDown = useCallback((e: React.KeyboardEvent) => {
    if (e.key === 'Enter' && !e.shiftKey) {
      e.preventDefault();
      handleSend();
    }
  }, [handleSend]);

  const handleBack = useCallback(() => {
    setExpandedProject(null);
  }, []);

  // Split mode: expanded project on right, grid on left
  const isSplitMode = expandedProject !== null;

  return (
    <div className="flex h-full bg-black overflow-hidden">
      {/* Grid panel (full width in grid mode, sidebar in split mode) */}
      <div
        className={cn(
          "flex flex-col overflow-hidden transition-all duration-300",
          isSplitMode ? "w-72 border-r border-white/5" : "flex-1"
        )}
      >
        {/* Header */}
        <div className="h-12 border-b border-white/5 flex items-center px-4 shrink-0 bg-black/50 backdrop-blur-md gap-3">
          <div className="flex items-center gap-2 text-[10px] font-mono tracking-widest uppercase font-bold">
            <Zap size={12} className="text-primary" />
            <span className="text-primary">PI</span>
            <span className="text-zinc-700">CODING AGENT</span>
          </div>
          <div className="flex-1" />
          {/* Auto-branch toggle */}
          <button
            onClick={() => setAutoBranch(!autoBranch)}
            className={cn(
              "flex items-center gap-1.5 px-2 py-1 text-[8px] font-mono uppercase tracking-widest border transition-all",
              autoBranch
                ? "border-primary/30 text-primary bg-primary/5"
                : "border-white/5 text-zinc-600 hover:text-zinc-400"
            )}
          >
            <GitBranch size={9} />
            Auto-branch
          </button>
          {/* Active count */}
          {activeCount > 0 && (
            <div className="flex items-center gap-1.5">
              <div className="w-1.5 h-1.5 rounded-full bg-primary animate-pulse" />
              <span className="text-[8px] font-mono text-primary">{activeCount} active</span>
            </div>
          )}
        </div>

        {/* Cards grid */}
        <div className={cn(
          "flex-1 overflow-y-auto p-3 custom-scrollbar",
          !isSplitMode && "p-6"
        )}>
          <div className={cn(
            "grid gap-3",
            isSplitMode
              ? "grid-cols-1"
              : "grid-cols-1 sm:grid-cols-2 lg:grid-cols-3 xl:grid-cols-4"
          )}>
            {projects.map(project => (
              <ProjectSessionRow
                key={project}
                project={project}
                manager={manager}
                expandedProject={expandedProject}
                onExpand={setExpandedProject}
              />
            ))}
            {projects.length === 0 && (
              <div className="col-span-full flex flex-col items-center justify-center py-16 text-center">
                <p className="text-xs text-zinc-600 font-mono">No projects registered</p>
              </div>
            )}
          </div>
        </div>

        {/* Prompt bar (only in grid mode) */}
        {!isSplitMode && (
          <div className="p-4 border-t border-white/5">
            <div className="flex gap-2">
              <select
                value={promptProject}
                onChange={(e) => setPromptProject(e.target.value)}
                className="bg-zinc-900 border border-white/5 rounded px-2 py-2 text-[10px] text-white font-mono outline-none focus:border-primary/40"
              >
                {projects.map(p => (
                  <option key={p} value={p}>{p}</option>
                ))}
              </select>
              <div className="flex-1 relative">
                <input
                  type="text"
                  value={promptText}
                  onChange={(e) => setPromptText(e.target.value)}
                  onKeyDown={handleKeyDown}
                  placeholder="Describe a task..."
                  className="w-full bg-zinc-900 border border-white/5 rounded px-3 py-2 text-[11px] text-white placeholder-zinc-700 outline-none focus:border-primary/40 font-mono"
                />
              </div>
              <button
                onClick={handleSend}
                disabled={!promptText.trim() || !promptProject}
                className="px-3 py-2 bg-primary text-black rounded hover:bg-primary/80 disabled:opacity-20 transition-all"
              >
                <Send size={14} />
              </button>
            </div>
          </div>
        )}
      </div>

      {/* Expanded detail panel */}
      {isSplitMode && (
        <div className="flex-1 flex flex-col min-w-0">
          <div className="h-12 border-b border-white/5 flex items-center px-4 shrink-0 bg-black/50 backdrop-blur-md">
            <button
              onClick={handleBack}
              className="flex items-center gap-2 text-[9px] font-mono text-zinc-500 hover:text-zinc-300 uppercase tracking-widest transition-colors"
            >
              <ArrowLeft size={12} />
              Back to grid
            </button>
          </div>
          <div className="flex-1 overflow-hidden">
            <PiAgentView
              selectedProject={expandedProject}
              projects={projects}
            />
          </div>
        </div>
      )}
    </div>
  );
};
