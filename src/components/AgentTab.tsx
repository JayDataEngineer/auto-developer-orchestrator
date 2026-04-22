import React from 'react';
import { PiAgentView } from './PiAgentView';
import { ToolCall } from '../lib/pi-events';

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
      <PiAgentView
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
