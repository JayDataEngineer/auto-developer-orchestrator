import React from 'react';
import { PuxAgentView } from './PuxAgentView';
import { ToolCall } from '../lib/pux-events';

interface AgentTabProps {
  selectedProject: string | null;
  projects: string[];
  activeAgentId: string;
  onActiveAgentIdChange: (id: string) => void;
  onStreamingStateChange: (state: { isStreaming: boolean; runningTool: ToolCall | undefined; thinking: string }) => void;
}

export function AgentTab({
  selectedProject,
  projects,
  activeAgentId,
  onStreamingStateChange,
}: AgentTabProps) {
  return (
    <div className="flex flex-col h-full bg-background text-foreground overflow-hidden">
      <PuxAgentView
        selectedProject={selectedProject || undefined}
        selectedAgentId={activeAgentId}
        projects={projects}
        isZenMode={false}
        onZenToggle={() => {}}
        onStreamingStateChange={onStreamingStateChange}
      />
    </div>
  );
}
