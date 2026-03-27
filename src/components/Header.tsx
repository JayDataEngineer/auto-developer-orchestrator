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
  onCLIClick,
  onAgentsClick,
  fullAutomationMode,
  projects,
  selectedProject,
  onProjectSelect
}) => {
  return (
    <header className="h-20 bg-black border-b border-border flex items-center justify-between px-6 lg:px-10 shrink-0 z-50">
      <div className="flex items-center gap-4 lg:gap-6">
        <button 
          onClick={onMenuToggle}
          className="lg:hidden p-2 text-zinc-500 hover:text-white transition-colors"
        >
          <Menu size={24} />
        </button>
        <div className="flex flex-col">
          <h1 className="text-[12px] font-bold tracking-[0.3em] uppercase text-white leading-none mb-1.5">Orchestrator <span className="text-primary opacity-80">v0.2.5</span></h1>
          <div className="flex items-center gap-2">
            <span className="text-[9px] font-bold text-primary tracking-[0.2em] uppercase font-mono px-2 py-0.5 border border-primary/40 bg-primary/5">Deep Research Architect</span>
          </div>
        </div>
        <div className="hidden lg:block h-6 w-[1px] bg-border" />
        
        {/* Project Selector */}
        <div className="flex items-center gap-3 bg-secondary border border-border px-4 py-1.5 transition-all hover:border-primary group">
          <FolderGit2 size={14} className="text-zinc-500 group-hover:text-primary transition-colors" />
          <select 
            value={selectedProject}
            onChange={(e) => onProjectSelect(e.target.value)}
            className="bg-transparent text-[10px] font-bold uppercase tracking-widest text-zinc-300 outline-none cursor-pointer appearance-none min-w-[120px] font-mono"
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
            className="p-2 bg-secondary border border-border text-zinc-400 hover:text-white hover:border-primary transition-all"
            title="Clone Repository"
          >
            <Plus size={16} />
          </button>

          <button 
            onClick={onAddExistingClick}
            className="flex items-center gap-2 px-4 py-2 bg-secondary border border-border text-[9px] font-bold uppercase tracking-[0.2em] text-zinc-400 hover:text-white hover:border-primary transition-all group"
            title="Add Existing Local Repository"
          >
            <FolderGit2 size={16} className="group-hover:text-primary transition-colors" />
            <span className="hidden xl:inline">Add Existing</span>
          </button>

          <button
            onClick={onRefreshProjects}
            className="p-2 bg-secondary border border-border text-zinc-400 hover:text-white hover:border-primary transition-all"
            title="Refresh Projects"
          >
            <RefreshCw size={16} />
          </button>

          <button
            onClick={onCLIClick}
            className="flex items-center gap-2 px-3 py-2 bg-secondary border border-border text-[10px] font-bold uppercase tracking-widest text-zinc-400 hover:text-white hover:border-primary transition-all"
            title="Open CLI Terminal"
          >
            <Terminal size={14} />
            CLI
          </button>

          <button
            onClick={onAgentsClick}
            className="flex items-center gap-2 px-3 py-2 bg-secondary border border-border text-[10px] font-bold uppercase tracking-widest text-zinc-400 hover:text-white hover:border-primary transition-all"
            title="Open AI Agents"
          >
            <Bot size={14} />
            Agents
          </button>
        </div>

        <div className="hidden lg:block h-6 w-[1px] bg-border" />
        {fullAutomationMode && (
          <div className="flex items-center gap-2 px-4 py-1.5 bg-red-500/10 border border-red-500/30 text-[10px] font-bold text-red-500 glow-primary">
            <Cpu size={14} />
            AUTONOMOUS_LOOP_ACTIVE
          </div>
        )}
        <button 
          onClick={onCoverageClick}
          className="flex items-center gap-2 px-4 py-2 bg-primary/5 border border-primary/30 text-[9px] font-bold text-primary hover:bg-primary/15 transition-all font-mono tracking-widest"
        >
          <BarChart3 size={14} />
          STAT_COVERAGE::91.2%
        </button>
        
        <div className="hidden lg:flex items-center gap-3 text-[9px] font-mono text-zinc-500 font-bold uppercase tracking-widest">
          <span className={cn(
            "w-2.5 h-2.5 rounded-none", 
            status?.agentStatus === 'running' ? 'bg-success shadow-[0_0_10px_rgba(0,255,0,0.5)]' : 'bg-amber-500/50'
          )} />
          <span className={cn(status?.agentStatus === 'running' ? 'text-success' : 'text-amber-500/70')}>
            {status?.agentStatus === 'running' ? 'SYS_ONLINE' : 'AGENT_PAUSED'}
          </span>
        </div>

        <div className="hidden lg:flex items-center gap-3 text-[9px] font-mono text-zinc-500 font-bold uppercase tracking-widest border-l border-border pl-6">
          <Bot size={14} className="text-primary glow-primary" />
          <span className="text-white">JULES_CORE:</span>
          <span className="text-emerald-400">ENCRYPTED // ONLINE</span>
        </div>
      </div>

      <div className="flex items-center gap-6">
        {/* Mode Toggle Switch */}
        <div className="flex items-center bg-secondary p-1 border border-border shadow-2xl">
          <button 
            onClick={onToggleMode}
            className={cn(
              "px-5 py-2 text-[10px] font-bold uppercase tracking-widest transition-all duration-200",
              !status?.isAutoMode ? "bg-white text-black" : "text-zinc-500 hover:text-white"
            )}
          >
            Manual
          </button>
          <button 
            onClick={onToggleMode}
            className={cn(
              "px-5 py-2 text-[10px] font-bold uppercase tracking-widest transition-all duration-200",
              status?.isAutoMode ? "bg-primary text-white glow-primary" : "text-zinc-500 hover:text-white"
            )}
          >
            Full Auto
          </button>
        </div>

        <div className="hidden lg:flex items-center gap-2 text-[10px] font-mono text-zinc-500 bg-secondary px-3 py-1.5 border border-border">
          <Cpu size={14} className="text-primary" />
          <span className="text-white font-bold tracking-tighter">14.2%</span>
        </div>
      </div>
    </header>
  );
};
