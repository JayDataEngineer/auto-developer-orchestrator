import React, { useState, useCallback } from 'react';
import { cn } from '../lib/utils';
import { HistorySidebar } from './HistorySidebar';
import { RightPanel } from './RightPanel';
import { PiAgentView } from './PiAgentView';
import { useArtifacts } from '../hooks/useArtifacts';
import { useResizable } from '../hooks/useResizable';
import { ToolCall } from '../lib/pi-events';

interface AgentTabProps {
  selectedProject: string | null;
  projects: string[];
  refreshProjectData: () => void;
  sidebarCollapsed: boolean;
  onSidebarToggle: () => void;
  rightPanelCollapsed: boolean;
  onRightPanelToggle: () => void;
}

export function AgentTab({
  selectedProject,
  projects,
  refreshProjectData,
  sidebarCollapsed,
  onSidebarToggle,
  rightPanelCollapsed,
  onRightPanelToggle,
}: AgentTabProps) {
  const [activeAgentId, setActiveAgentId] = useState('default');
  const [streamingState, setStreamingState] = useState<{
    isStreaming: boolean;
    runningTool: ToolCall | undefined;
    thinking: string;
  }>({ isStreaming: false, runningTool: undefined, thinking: '' });

  const handleStreamingStateChange = useCallback((state: { isStreaming: boolean; runningTool: ToolCall | undefined; thinking: string }) => {
    setStreamingState(state);
  }, []);

  const artifactsHook = useArtifacts(selectedProject ? `${selectedProject}:${activeAgentId}` : null);
  const sandboxId = selectedProject ? `sandbox-${selectedProject}` : null;

  const {
    width: sidebarWidth,
    isDragging: sidebarDragging,
    handleProps: sidebarHandleProps,
  } = useResizable({ defaultWidth: 224, minWidth: 180, maxWidth: 400, side: 'right' });

  const {
    width: rightPanelWidth,
    isDragging: rightPanelDragging,
    handleProps: rightPanelHandleProps,
  } = useResizable({ defaultWidth: 384, minWidth: 280, maxWidth: 600, side: 'left' });

  return (
    <div className="flex h-full bg-black text-slate-100 overflow-hidden">
      {/* Left: History Sidebar (resizable) */}
      {!sidebarCollapsed && (
        <div style={{ width: sidebarWidth }} className="relative shrink-0 border-r border-white/5">
          <HistorySidebar
            projects={projects}
            activeProject={selectedProject || undefined}
            activeAgentId={activeAgentId}
            onSelectSession={(project: string, agentId: string) => {
              setActiveAgentId(agentId);
            }}
            onNewChat={() => setActiveAgentId('default')}
          />
          {/* Drag handle on right edge */}
          <div
            {...sidebarHandleProps}
            className={cn(
              'absolute top-0 right-0 bottom-0 w-1 cursor-col-resize z-10 transition-colors',
              sidebarDragging ? 'bg-primary/30' : 'hover:bg-white/10'
            )}
          />
        </div>
      )}

      {/* Center: Agent chat */}
      <div className="flex-1 flex flex-col min-w-0 overflow-hidden">
        <PiAgentView
          selectedProject={selectedProject || undefined}
          selectedAgentId={activeAgentId}
          projects={projects}
          isZenMode={false}
          onZenToggle={() => {}}
          onStreamingStateChange={handleStreamingStateChange}
        />
      </div>

      {/* Right: Artifacts Panel (resizable) */}
      {!rightPanelCollapsed && (
        <div style={{ width: rightPanelWidth }} className="relative shrink-0 border-l border-white/5">
          {/* Drag handle on left edge */}
          <div
            {...rightPanelHandleProps}
            className={cn(
              'absolute top-0 left-0 bottom-0 w-1 cursor-col-resize z-10 transition-colors',
              rightPanelDragging ? 'bg-primary/30' : 'hover:bg-white/10'
            )}
          />
          <RightPanel
            agentId={selectedProject ? `${selectedProject}:${activeAgentId}` : null}
            sandboxId={sandboxId}
            artifacts={artifactsHook.artifacts}
            artifactsLoading={artifactsHook.loading}
            streamingState={streamingState}
          />
        </div>
      )}
    </div>
  );
}
