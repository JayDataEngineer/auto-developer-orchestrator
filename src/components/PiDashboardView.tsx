import React, { useState, useCallback, useEffect } from 'react';
import { cn } from '../lib/utils';
import { usePiAgent } from '../hooks/usePiAgent';
import { usePiSessionManager } from '../hooks/usePiSessionManager';
import { PiSessionCard } from './PiSessionCard';
import { PiAgentView } from './PiAgentView';
import { Send, GitBranch, Zap, ArrowLeft, Plus, Wrench, ChevronDown, ChevronRight } from 'lucide-react';

interface PiDashboardViewProps {
  selectedProject?: string;
  projects: string[];
  onProjectSelect?: (project: string) => void;
  isZenMode?: boolean;
  onZenToggle?: () => void;
}

// One hook per agent — rendered at the top level to satisfy React's rules of hooks.
function AgentCard({
  project,
  agentId,
  agentIndex,
  manager,
  expandedKey,
  onExpand,
  onDestroy,
}: {
  project: string;
  agentId: string;
  agentIndex: number;
  manager: ReturnType<typeof usePiSessionManager>;
  expandedKey: string | null;
  onExpand: (key: string | null) => void;
  onDestroy: () => void;
}) {
  const hook = usePiAgent(agentId);

  // Register this hook with the session manager
  useEffect(() => {
    manager.registerSession(project, agentId, hook);
    return () => manager.unregisterSession(project, agentId);
  }, [project, agentId]); // eslint-disable-line react-hooks/exhaustive-deps

  // Hydrate on mount
  useEffect(() => {
    hook.hydrateState(project, agentId);
  }, [project, agentId]); // eslint-disable-line react-hooks/exhaustive-deps

  const key = `${project}::${agentId}`;
  const isExpanded = expandedKey === key;

  return (
    <PiSessionCard
      project={project}
      agentId={agentId}
      agentIndex={agentIndex}
      state={hook.state}
      isExpanded={isExpanded}
      onClick={() => onExpand(isExpanded ? null : key)}
      onDestroy={agentId !== 'default' ? onDestroy : undefined}
    />
  );
}

// Group of agents for one project
function ProjectAgentGroup({
  project,
  manager,
  expandedKey,
  onExpand,
}: {
  project: string;
  manager: ReturnType<typeof usePiSessionManager>;
  expandedKey: string | null;
  onExpand: (key: string | null) => void;
}) {
  const [agentIds, setAgentIds] = useState<string[]>(['default']);
  const [spawning, setSpawning] = useState(false);

  const handleSpawn = useCallback(async () => {
    if (spawning) return;
    setSpawning(true);
    try {
      const newAgentId = await manager.spawnAgent(project);
      setAgentIds(prev => [...prev, newAgentId]);
    } catch (err) {
      console.error('Failed to spawn agent:', err);
    } finally {
      setSpawning(false);
    }
  }, [project, manager, spawning]);

  const handleDestroy = useCallback((agentId: string) => {
    manager.destroyAgent(project, agentId);
    setAgentIds(prev => prev.filter(id => id !== agentId));
  }, [project, manager]);

  return (
    <div className="border border-white/5">
      {/* Project header */}
      <div className="flex items-center justify-between px-3 py-2 border-b border-white/5 bg-white/[0.02]">
        <span className="text-[10px] font-black uppercase tracking-widest text-white">
          {project}
        </span>
        <button
          onClick={handleSpawn}
          disabled={spawning || agentIds.length >= 5}
          className="flex items-center gap-1 px-2 py-1 text-[8px] font-mono text-muted hover:text-primary border border-white/5 hover:border-primary/30 transition-all disabled:opacity-30 disabled:cursor-not-allowed"
        >
          <Plus size={9} />
          New Agent
        </button>
      </div>
      {/* Agent cards */}
      <div className="grid grid-cols-1 gap-0">
        {agentIds.map((agentId, idx) => (
          <AgentCard
            key={`${project}::${agentId}`}
            project={project}
            agentId={agentId}
            agentIndex={idx + 1}
            manager={manager}
            expandedKey={expandedKey}
            onExpand={onExpand}
            onDestroy={() => handleDestroy(agentId)}
          />
        ))}
      </div>
    </div>
  );
}

export const PiDashboardView: React.FC<PiDashboardViewProps> = ({
  selectedProject,
  projects,
  onProjectSelect,
  isZenMode = false,
  onZenToggle,
}) => {
  const manager = usePiSessionManager(projects);
  const [expandedKey, setExpandedKey] = useState<string | null>(null);
  const [promptText, setPromptText] = useState('');
  const [promptProject, setPromptProject] = useState<string>(selectedProject || projects[0] || '');
  const [promptAgentId, setPromptAgentId] = useState<string>('default');
  const [autoBranch, setAutoBranch] = useState(false);
  const [showSidebar, setShowSidebar] = useState(true);
  const [activeCount, setActiveCount] = useState(0);

  // Derive available agents for the selected prompt project
  const [availableAgents, setAvailableAgents] = useState<string[]>(['default']);
  useEffect(() => {
    const agents = manager.getAgentsForProject(promptProject);
    if (agents.length > 0) {
      setAvailableAgents(agents.map(a => a.agentId));
    } else {
      setAvailableAgents(['default']);
    }
  }, [promptProject, manager, activeCount]); // re-evaluate when activeCount changes

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

  // Parse expanded key to get project and agentId
  const expandedProject = expandedKey ? expandedKey.split('::')[0] : null;
  const expandedAgentId = expandedKey ? expandedKey.split('::')[1] || 'default' : null;

  const handleSend = useCallback(() => {
    if (!promptText.trim() || !promptProject) return;
    manager.sendPrompt(promptProject, promptAgentId, promptText.trim(), { autoBranch });
    setPromptText('');
  }, [promptText, promptProject, promptAgentId, autoBranch, manager]);

  const handleKeyDown = useCallback((e: React.KeyboardEvent) => {
    if (e.key === 'Enter' && !e.shiftKey) {
      e.preventDefault();
      handleSend();
    }
  }, [handleSend]);

  const handleBack = useCallback(() => {
    setExpandedKey(null);
  }, []);

  // Split mode: expanded project on right, grid on left
  const isSplitMode = expandedKey !== null;

  return (
    <div className="flex-1 flex w-full h-full bg-black overflow-hidden relative">
      {/* Grid panel */}
      {(!isZenMode || !isSplitMode) && (
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
            <span className="text-muted">CODING AGENT</span>
          </div>
          <div className="flex-1" />
          {/* Auto-branch toggle */}
          <button
            onClick={() => setAutoBranch(!autoBranch)}
            className={cn(
              "flex items-center gap-1.5 px-2 py-1 text-[8px] font-mono uppercase tracking-widest border transition-all",
              autoBranch
                ? "border-primary/30 text-primary bg-primary/5"
                : "border-white/5 text-muted-foreground hover:text-muted-foreground"
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
              <ProjectAgentGroup
                key={project}
                project={project}
                manager={manager}
                expandedKey={expandedKey}
                onExpand={setExpandedKey}
              />
            ))}
            {projects.length === 0 && (
              <div className="col-span-full flex flex-col items-center justify-center py-16 text-center">
                <p className="text-xs text-muted-foreground font-mono">No projects registered</p>
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
                onChange={(e) => { setPromptProject(e.target.value); setPromptAgentId('default'); }}
                className="bg-zinc-900 border border-white/5 rounded px-2 py-2 text-[10px] text-white font-mono outline-none focus:border-primary/40"
              >
                {projects.map(p => (
                  <option key={p} value={p}>{p}</option>
                ))}
              </select>
              <select
                value={promptAgentId}
                onChange={(e) => setPromptAgentId(e.target.value)}
                className="bg-zinc-900 border border-white/5 rounded px-2 py-2 text-[10px] text-white font-mono outline-none focus:border-primary/40"
              >
                {availableAgents.map(aid => (
                  <option key={aid} value={aid}>{aid === 'default' ? '#1 (default)' : `#${availableAgents.indexOf(aid) + 1}`}</option>
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
    )}

      {/* Sidebar — visible in grid mode */}
      {!isSplitMode && !isZenMode && (
        <div className="w-72 border-l border-white/5 flex flex-col bg-black shrink-0">
          <button
            onClick={() => setShowSidebar(!showSidebar)}
            className="p-4 border-b border-white/5 flex items-center gap-3 text-left"
          >
            <Wrench size={12} className="text-muted-foreground" />
            <span className="text-[9px] font-black uppercase tracking-widest text-muted-foreground">
              Activity
            </span>
            <div className="flex-1" />
            {showSidebar ? <ChevronDown size={10} className="text-muted-foreground" /> : <ChevronRight size={10} className="text-muted-foreground" />}
          </button>
          {showSidebar && (
            <div className="flex-1 flex flex-col items-center justify-center p-6">
              <Wrench size={20} className="text-zinc-800 mb-3" />
              <p className="text-[9px] font-mono text-zinc-700 uppercase tracking-widest text-center">
                Select an agent to view activity
              </p>
            </div>
          )}
        </div>
      )}

      {/* Expanded detail panel */}
      {isSplitMode && expandedProject && expandedAgentId && (
        <div className="flex-1 flex flex-col min-w-0">
          {!isZenMode && (
            <div className="h-12 border-b border-white/5 flex items-center px-4 shrink-0 bg-black/50 backdrop-blur-md">
              <button
                onClick={handleBack}
                className="flex items-center gap-2 text-[9px] font-mono text-muted hover:text-zinc-300 uppercase tracking-widest transition-colors"
              >
                <ArrowLeft size={12} />
                Back to grid
              </button>
            </div>
          )}
          <div className="flex-1 overflow-hidden">
            <PiAgentView
              selectedProject={expandedProject}
              selectedAgentId={expandedAgentId}
              projects={projects}
              isZenMode={isZenMode}
              onZenToggle={onZenToggle}
            />
          </div>
        </div>
      )}
    </div>
  );
};
