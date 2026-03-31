import React from 'react';
import { cn } from '../lib/utils';
import { PiAgentState } from '../hooks/usePiAgent';
import { GitBranch, Loader, Zap, AlertCircle, X } from 'lucide-react';

interface PiSessionCardProps {
  project: string;
  agentId: string;
  agentIndex: number;
  state: PiAgentState;
  isExpanded?: boolean;
  onClick?: () => void;
  onDestroy?: () => void;
}

function formatTokenCount(count: number): string {
  if (count >= 1000) return `${(count / 1000).toFixed(1)}k`;
  return String(count);
}

export const PiSessionCard: React.FC<PiSessionCardProps> = ({
  project,
  agentId,
  agentIndex,
  state,
  isExpanded = false,
  onClick,
  onDestroy,
}) => {
  const totalTokens = state.tokenUsage.input + state.tokenUsage.output;
  const activeTools = state.toolCalls.filter(tc => !tc.endTime).length;
  const isDefault = agentId === 'default';

  return (
    <button
      onClick={onClick}
      className={cn(
        "w-full text-left border transition-all p-4 space-y-3 relative group",
        isExpanded
          ? "border-primary bg-primary/5"
          : "border-white/5 bg-black hover:border-white/10",
      )}
    >
      {/* Destroy button for non-default agents */}
      {!isDefault && onDestroy && (
        <button
          onClick={(e) => { e.stopPropagation(); onDestroy(); }}
          className="absolute top-2 right-2 opacity-0 group-hover:opacity-100 p-1 text-muted-foreground hover:text-red-400 transition-all"
        >
          <X size={10} />
        </button>
      )}

      {/* Project name + agent index + status */}
      <div className="flex items-start justify-between gap-2">
        <div className="flex items-center gap-2">
          <span className="text-[11px] font-black uppercase tracking-widest text-white truncate">
            {project}
          </span>
          <span className="text-[9px] font-mono text-muted-foreground">
            #{agentIndex}
          </span>
        </div>
        <div className="flex items-center gap-1.5 shrink-0">
          {state.isStreaming ? (
            <>
              <div className="w-1.5 h-1.5 rounded-full bg-primary animate-pulse" />
              <span className="text-[8px] font-black text-primary uppercase tracking-widest">stream</span>
            </>
          ) : state.error ? (
            <>
              <AlertCircle size={10} className="text-red-400" />
              <span className="text-[8px] font-mono text-red-400 uppercase">error</span>
            </>
          ) : (
            <>
              <div className="w-1.5 h-1.5 rounded-full bg-zinc-700" />
              <span className="text-[8px] font-mono text-muted-foreground uppercase">idle</span>
            </>
          )}
        </div>
      </div>

      {/* Model name */}
      {state.model && (
        <div className="text-[9px] font-mono text-muted truncate">{state.model}</div>
      )}

      {/* Last prompt preview */}
      {state.lastPrompt && (
        <p className="text-[10px] text-muted-foreground line-clamp-2 leading-relaxed">
          {state.lastPrompt}
        </p>
      )}

      {/* Bottom row: tokens, tools, branch */}
      <div className="flex items-center gap-3 flex-wrap">
        {totalTokens > 0 && (
          <span className="text-[8px] font-mono text-muted-foreground">
            <Zap size={8} className="inline mr-1" />
            {formatTokenCount(totalTokens)} tok
          </span>
        )}
        {activeTools > 0 && (
          <span className="text-[8px] font-mono text-primary flex items-center gap-1">
            <Loader size={8} className="animate-spin" />
            {activeTools} tool{activeTools !== 1 ? 's' : ''}
          </span>
        )}
        {state.branchName && (
          <span className="text-[8px] font-mono text-muted flex items-center gap-1">
            <GitBranch size={8} />
            {state.branchName}
          </span>
        )}
      </div>

      {/* Error message */}
      {state.error && (
        <p className="text-[9px] font-mono text-red-400 truncate">{state.error}</p>
      )}
    </button>
  );
};
