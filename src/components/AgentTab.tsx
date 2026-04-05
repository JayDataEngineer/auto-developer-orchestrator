import React, { useState } from 'react';
import { cn } from '../lib/utils';
import { HistorySidebar } from './HistorySidebar';
import { RightPanel } from './RightPanel';
import { PiAgentView } from './PiAgentView';
import { useArtifacts } from '../hooks/useArtifacts';
import { ChevronLeft, ChevronRight } from 'lucide-react';

interface AgentTabProps {
  selectedProject: string | null;
  projects: string[];
  refreshProjectData: () => void;
}

export function AgentTab({ selectedProject, projects, refreshProjectData }: AgentTabProps) {
  const [activeAgentId, setActiveAgentId] = useState('default');
  const [sidebarCollapsed, setSidebarCollapsed] = useState(false);
  const [rightPanelCollapsed, setRightPanelCollapsed] = useState(false);

  const artifactsHook = useArtifacts(selectedProject ? `${selectedProject}:${activeAgentId}` : null);
  const sandboxId = selectedProject ? `sandbox-${selectedProject}-${activeAgentId}` : null;

  return (
    <div className="flex h-full bg-black text-slate-100 overflow-hidden">
      {/* Left: History Sidebar (collapsible) */}
      {!sidebarCollapsed && (
        <HistorySidebar
          projects={projects}
          activeProject={selectedProject || undefined}
          activeAgentId={activeAgentId}
          onSelectSession={(project: string, agentId: string) => {
            setActiveAgentId(agentId);
          }}
          onNewChat={() => setActiveAgentId('default')}
        />
      )}

      {/* Collapse toggle */}
      <button
        onClick={() => setSidebarCollapsed(!sidebarCollapsed)}
        className={cn(
          'absolute z-20 flex items-center justify-center w-4 h-12 bg-zinc-900 border border-white/5 text-zinc-500 hover:text-zinc-300 transition-colors',
          sidebarCollapsed ? 'left-0' : 'left-56'
        )}
        style={{ top: 'calc(2.5rem + 0.5rem)' }}
      >
        {sidebarCollapsed ? <ChevronRight size={10} /> : <ChevronLeft size={10} />}
      </button>

      {/* Center: Agent chat */}
      <div className="flex-1 flex flex-col min-w-0 overflow-hidden">
        <PiAgentView
          selectedProject={selectedProject || undefined}
          selectedAgentId={activeAgentId}
          projects={projects}
          isZenMode={false}
          onZenToggle={() => {}}
        />
      </div>

      {/* Right: Browser / Artifacts Panel (collapsible) */}
      {!rightPanelCollapsed && (
        <RightPanel
          agentId={selectedProject ? `${selectedProject}:${activeAgentId}` : null}
          sandboxId={sandboxId}
          artifacts={artifactsHook.artifacts}
          artifactsLoading={artifactsHook.loading}
        />
      )}

      {/* Right panel collapse toggle */}
      <button
        onClick={() => setRightPanelCollapsed(!rightPanelCollapsed)}
        className={cn(
          'absolute z-20 flex items-center justify-center w-4 h-12 bg-zinc-900 border border-white/5 text-zinc-500 hover:text-zinc-300 transition-colors',
          rightPanelCollapsed ? 'right-0' : 'right-96'
        )}
        style={{ top: 'calc(2.5rem + 0.5rem)' }}
      >
        {rightPanelCollapsed ? <ChevronLeft size={10} /> : <ChevronRight size={10} />}
      </button>
    </div>
  );
}
