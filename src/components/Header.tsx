import React from 'react';
import { Cpu, Menu, BarChart3, FolderGit2, Plus, RefreshCw, Terminal, Bot } from 'lucide-react';
import { cn } from '../lib/utils';

interface HeaderProps {
  status: {
    agentStatus: string;
    isAutoMode: boolean;
    gitState: string;
    workingTree: string;
    lastCommit: string;
  } | null;
  onToggleMode: () => void;
  onMenuToggle: () => void;
  onCoverageClick: () => void;
  onCloneClick: () => void;
  onAddExistingClick: () => void;
  onRefreshProjects: () => void;
  onCLIClick: () => void;
  onAgentsClick: () => void;
  fullAutomationMode: boolean;
  projects: string[];
  selectedProject: string;
  onProjectSelect: (project: string) => void;
}

export const Header: React.FC<HeaderProps> = ({ 
  status, 
  onToggleMode, 
  onMenuToggle, 
  onCoverageClick, 
  onCloneClick,
  onAddExistingClick,
  onRefreshProjects,
  fullAutomationMode,
  projects,
  selectedProject,
  onProjectSelect
}) => {
  return (
    <header className="h-16 glass border-b border-white/5 flex items-center justify-between px-6 lg:px-8 shrink-0 z-50">
      <div className="flex items-center gap-4 lg:gap-6">
        <button 
          onClick={onMenuToggle}
          className="lg:hidden p-2 text-zinc-400 hover:text-white transition-colors"
        >
          <Menu size={24} />
        </button>
        <div className="flex flex-col">
          <h1 className="text-[10px] font-bold tracking-[0.2em] uppercase text-primary/80 leading-none mb-1">Orchestrator v0.2.5</h1>
          <span className="text-[8px] font-medium text-zinc-500 tracking-[0.1em] uppercase">Deep Research Architech</span>
        </div>
        <div className="hidden lg:block h-6 w-[1px] bg-white/5" />
        
        {/* Project Selector */}
        <div className="flex items-center gap-3 glass-dark border border-white/5 rounded-xl px-4 py-1.5 transition-all hover:border-white/10 group">
          <FolderGit2 size={14} className="text-zinc-500 group-hover:text-primary transition-colors" />
          <select 
            value={selectedProject}
            onChange={(e) => onProjectSelect(e.target.value)}
            className="bg-transparent text-[11px] font-bold uppercase tracking-widest text-zinc-300 outline-none cursor-pointer appearance-none min-w-[120px]"
          >
            {projects.length === 0 && <option value="">NO PROJECTS</option>}
            {projects.map(p => (
              <option key={p} value={p}>{p}</option>
            ))}
          </select>
        </div>

        <div className="flex items-center gap-2">
          <button 
            onClick={onCloneClick}
            className="p-2 glass-dark border border-white/5 rounded-xl text-zinc-400 hover:text-white hover:border-primary/30 transition-all hover:scale-105"
            title="Clone Repository"
          >
            <Plus size={16} />
          </button>

          <button 
            onClick={onAddExistingClick}
            className="flex items-center gap-2 px-3 py-2 glass-dark border border-white/5 rounded-xl text-[10px] font-bold uppercase tracking-widest text-zinc-400 hover:text-white hover:border-primary/30 transition-all hover:scale-105"
            title="Add Existing Local Repository"
          >
            <FolderGit2 size={16} />
            <span className="hidden xl:inline">Add Existing</span>
          </button>

          <button
            onClick={onRefreshProjects}
            className="p-2 glass-dark border border-white/5 rounded-xl text-zinc-400 hover:text-white hover:border-primary/30 transition-all hover:scale-105"
            title="Refresh Projects"
          >
            <RefreshCw size={16} />
          </button>

          <button
            onClick={onCLIClick}
            className="flex items-center gap-2 px-3 py-2 glass-dark border border-white/5 rounded-xl text-[10px] font-bold uppercase tracking-widest text-zinc-400 hover:text-white hover:border-primary/30 transition-all hover:scale-105"
            title="Open CLI Terminal"
          >
            <Terminal size={14} />
            CLI
          </button>

          <button
            onClick={onAgentsClick}
            className="flex items-center gap-2 px-3 py-2 glass-dark border border-white/5 rounded-xl text-[10px] font-bold uppercase tracking-widest text-zinc-400 hover:text-white hover:border-primary/30 transition-all hover:scale-105"
            title="Open AI Agents"
          >
            <Bot size={14} />
            Agents
          </button>
        </div>

        <div className="hidden lg:block h-6 w-[1px] bg-white/5" />
        {fullAutomationMode && (
          <div className="flex items-center gap-2 px-4 py-1.5 bg-red-500/5 border border-red-500/20 rounded-xl text-[10px] font-bold text-red-400 glow-primary animate-pulse">
            <Cpu size={14} />
            AUTONOMOUS_LOOP_ACTIVE
          </div>
        )}
        <button 
          onClick={onCoverageClick}
          className="flex items-center gap-2 px-4 py-1.5 bg-primary/5 border border-primary/20 rounded-xl text-[10px] font-bold text-primary hover:bg-primary/10 transition-all hover:scale-105"
        >
          <BarChart3 size={14} />
          COVERAGE: 91.2%
        </button>
        
        <div className="hidden lg:flex items-center gap-3 text-[11px] font-mono text-zinc-500">
          <span className={cn(
            "w-2.5 h-2.5 rounded-full", 
            status?.agentStatus === 'running' ? 'bg-emerald-500 glow-primary animate-pulse' : 'bg-amber-500/50'
          )} />
          <span className={cn(status?.agentStatus === 'running' ? 'text-emerald-500' : 'text-amber-500/70')}>
            {status?.agentStatus === 'running' ? 'SYS_ONLINE' : 'AGENT_PAUSED'}
          </span>
        </div>
      </div>

      <div className="flex items-center gap-6">
        {/* Mode Toggle Switch */}
        <div className="flex items-center glass-dark p-1 rounded-xl border border-white/5 shadow-2xl">
          <button 
            onClick={onToggleMode}
            className={cn(
              "px-5 py-2 rounded-lg text-[10px] font-bold uppercase tracking-widest transition-all duration-300",
              !status?.isAutoMode ? "bg-zinc-800 text-white shadow-lg" : "text-zinc-500 hover:text-zinc-300"
            )}
          >
            Manual
          </button>
          <button 
            onClick={onToggleMode}
            className={cn(
              "px-5 py-2 rounded-lg text-[10px] font-bold uppercase tracking-widest transition-all duration-300",
              status?.isAutoMode ? "bg-primary text-white shadow-lg shadow-primary/20 glow-primary" : "text-zinc-500 hover:text-zinc-300"
            )}
          >
            Full Auto
          </button>
        </div>

        <div className="hidden lg:flex items-center gap-2 text-xs font-mono text-zinc-500 glass-dark px-3 py-1.5 rounded-xl border border-white/5">
          <Cpu size={14} className="text-primary/70" />
          <span className="text-white/80">14.2%</span>
        </div>
      </div>
    </header>
  );
};
