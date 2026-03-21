import React from 'react';
import { Cpu, Menu, BarChart3, FolderGit2, Plus, RefreshCw } from 'lucide-react';
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
  onRefreshProjects: () => void;
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
  onRefreshProjects,
  fullAutomationMode,
  projects,
  selectedProject,
  onProjectSelect
}) => {
  return (
    <header className="h-14 border-b border-white/10 bg-zinc-950 flex items-center justify-between px-4 lg:px-6 shrink-0">
      <div className="flex items-center gap-2 lg:gap-4">
        <button 
          onClick={onMenuToggle}
          className="lg:hidden p-2 text-zinc-400 hover:text-white"
        >
          <Menu size={20} />
        </button>
        <h1 className="text-[10px] lg:text-sm font-bold tracking-tighter uppercase text-zinc-400">Version 0.2.0-Alpha</h1>
        <div className="hidden lg:block h-4 w-[1px] bg-white/10" />
        
        {/* Project Selector */}
        <div className="flex items-center gap-2 bg-zinc-900 border border-white/5 rounded-full px-3 py-1">
          <FolderGit2 size={12} className="text-zinc-500" />
          <select 
            value={selectedProject}
            onChange={(e) => onProjectSelect(e.target.value)}
            className="bg-transparent text-[10px] font-bold uppercase tracking-widest text-zinc-300 outline-none cursor-pointer appearance-none"
          >
            {projects.length === 0 && <option value="">NO PROJECTS</option>}
            {projects.map(p => (
              <option key={p} value={p}>{p}</option>
            ))}
          </select>
        </div>

        <button 
          onClick={onCloneClick}
          className="p-1.5 bg-zinc-900 border border-white/5 rounded-full text-zinc-400 hover:text-white hover:bg-zinc-800 transition-all"
          title="Clone Repository"
        >
          <Plus size={14} />
        </button>

        <button 
          onClick={onRefreshProjects}
          className="p-1.5 bg-zinc-900 border border-white/5 rounded-full text-zinc-400 hover:text-white hover:bg-zinc-800 transition-all"
          title="Refresh Projects"
        >
          <RefreshCw size={14} />
        </button>

        <div className="hidden lg:block h-4 w-[1px] bg-white/10" />
        {fullAutomationMode && (
          <div className="flex items-center gap-2 px-3 py-1 bg-red-500/10 border border-red-500/20 rounded-full text-[10px] font-bold text-red-500 animate-pulse">
            <Cpu size={12} />
            AUTONOMOUS_LOOP_ACTIVE
          </div>
        )}
        <button 
          onClick={onCoverageClick}
          className="flex items-center gap-2 px-3 py-1 bg-primary/10 border border-primary/20 rounded-full text-[10px] font-bold text-primary hover:bg-primary/20 transition-colors"
        >
          <BarChart3 size={12} />
          COVERAGE: 91.2%
        </button>
        <div className="hidden lg:flex items-center gap-2 text-xs font-mono text-zinc-500">
          <span className={cn(
            "w-2 h-2 rounded-full", 
            status?.agentStatus === 'running' ? 'bg-emerald-500 animate-pulse' : 'bg-amber-500'
          )} />
          {status?.agentStatus === 'running' ? 'SYS_ONLINE' : 'AGENT_PAUSED'}
        </div>
        <div className="hidden lg:block h-4 w-[1px] bg-white/10" />
        <div className="hidden lg:flex items-center gap-2 text-[10px] font-mono text-zinc-600">
          <span className="uppercase">Branch:</span>
          <span className="text-zinc-400">{status?.workingTree || 'unknown'}</span>
          <span className="ml-2 uppercase">Commit:</span>
          <span className="text-zinc-400">{status?.lastCommit || '-------'}</span>
        </div>
      </div>

      {/* Mode Toggle Switch */}
      <div className="flex items-center bg-zinc-900 p-1 rounded-full border border-white/5 shadow-inner">
        <button 
          onClick={onToggleMode}
          className={cn(
            "px-4 py-1.5 rounded-full text-[10px] font-bold uppercase tracking-widest transition-all",
            !status?.isAutoMode ? "bg-zinc-800 text-white shadow-xl" : "text-zinc-500 hover:text-zinc-300"
          )}
        >
          Manual
        </button>
        <button 
          onClick={onToggleMode}
          className={cn(
            "px-4 py-1.5 rounded-full text-[10px] font-bold uppercase tracking-widest transition-all",
            status?.isAutoMode ? "bg-primary text-black shadow-lg shadow-primary/20" : "text-zinc-500 hover:text-zinc-300"
          )}
        >
          Full Auto
        </button>
      </div>

      <div className="flex items-center gap-4">
        <div className="flex items-center gap-2 text-xs font-mono text-zinc-500">
          <Cpu size={14} />
          <span>14.2%</span>
        </div>
      </div>
    </header>
  );
};
