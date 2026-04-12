import React from 'react';
import { Box, GitBranch, Zap } from 'lucide-react';

export function FleetBar({ project, branch, model, streaming }: {
  project?: string;
  branch?: string | null;
  model?: string | null;
  streaming?: boolean;
}) {
  return (
    <div className="w-full border-b border-white/5 flex items-center gap-4 px-6 py-1.5 bg-black/30 text-xs font-mono uppercase tracking-widest shrink-0">
      {project && (
        <span className="flex items-center gap-1.5 text-muted-foreground">
          <Box size={9} />
          {project}
        </span>
      )}
      {branch && (
        <span className="flex items-center gap-1.5 text-muted-foreground">
          <GitBranch size={9} />
          {branch}
        </span>
      )}
      {model && (
        <span className="flex items-center gap-1.5 text-muted-foreground">
          <Zap size={9} />
          {model}
        </span>
      )}
      {streaming && (
        <span className="flex items-center gap-1.5 text-primary">
          <div className="w-1.5 h-1.5 rounded-full bg-primary animate-pulse" />
          Live
        </span>
      )}
    </div>
  );
}
