import React, { useState } from 'react';
import { ChevronRight, Box, Loader, Check } from 'lucide-react';
import { cn } from '../../lib/utils';
import { SubAgentInfo } from '../../lib/api';

export function SubAgentCard({ agent }: { agent: SubAgentInfo }) {
  const [open, setOpen] = useState(agent.status === 'running');
  const statusIcon = agent.status === 'running' ? <Loader size={10} className="animate-spin text-blue-400" /> :
    agent.status === 'complete' ? <Check size={10} className="text-green-400" /> :
    <span className="text-xs text-muted-foreground/60">{agent.status}</span>;

  const toolCount = agent.toolCalls ?? 0;

  return (
    <div className="border border-blue-900/30 bg-blue-950/20">
      <button
        onClick={() => setOpen(!open)}
        className="w-full flex items-center gap-2 px-3 py-2 text-left"
      >
        <Box size={11} className="text-blue-400" />
        <span className="text-xs font-semibold text-blue-300">
          {agent.type} sub-agent
        </span>
        <span className="text-xs font-mono text-muted-foreground/60 truncate">
          {agent.subAgentId ? agent.subAgentId.slice(0, 30) + '...' : 'unknown'}
        </span>
        {toolCount > 0 && (
          <span className="text-xs font-mono text-muted-foreground/60">
            {toolCount} tools
          </span>
        )}
        <div className="flex-1" />
        {statusIcon}
        {agent.status === 'running' && (
          <span className="text-xs text-blue-400 font-mono">running</span>
        )}
        {agent.status === 'complete' && (
          <span className="text-xs text-green-400 font-mono">done</span>
        )}
        <ChevronRight size={10} className={cn("text-muted-foreground transition-transform", open && "rotate-90")} />
      </button>
      {open && agent.output && (
        <div className="px-3 pb-2 border-t border-blue-900/20">
          <pre className="text-xs font-mono text-blue-200/60 whitespace-pre-wrap max-h-40 overflow-auto">
            {agent.output}
          </pre>
        </div>
      )}
    </div>
  );
}
