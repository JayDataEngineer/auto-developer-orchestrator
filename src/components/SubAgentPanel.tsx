import React, { useState, useCallback } from 'react';
import { cn } from '../lib/utils';
import { SubAgentInfo } from '../lib/api';
import { useSubAgents } from '../hooks/useSubAgents';
import { StatusBadge } from './ui/StatusBadge';
import { EmptyState } from './ui/EmptyState';
import { useToastContext } from './ui/Toast';
import {
  Cpu, Play, Square, Eye, RefreshCw, X, Loader, ChevronDown, ChevronUp
} from 'lucide-react';

function formatDuration(ms: number): string {
  if (ms < 1000) return `${ms}ms`;
  if (ms < 60000) return `${(ms / 1000).toFixed(1)}s`;
  return `${(ms / 60000).toFixed(1)}m`;
}

function formatTokens(n: number): string {
  if (n < 1000) return String(Math.round(n));
  return `${(n / 1000).toFixed(1)}k`;
}

function truncateId(id: string): string {
  return id.length > 12 ? `${id.slice(0, 8)}...${id.slice(-4)}` : id;
}

// ─── Sub-Agent Row ────────────────────────────────────────────

interface SubAgentRowProps {
  agent: SubAgentInfo;
  isWatching: boolean;
  onAbort: () => void;
  onWatch: () => void;
}

function SubAgentRow({ agent, isWatching, onAbort, onWatch }: SubAgentRowProps) {
  return (
    <div className="flex items-center gap-3 px-3 py-2 border-b border-white/5 hover:bg-white/[0.02] transition-colors">
      <StatusBadge status={agent.status} size="sm" />
      <span className="text-sm font-mono text-zinc-400 w-24 truncate" title={agent.subAgentId}>
        {truncateId(agent.subAgentId)}
      </span>
      <span className="text-xs font-mono text-zinc-500 uppercase w-20">{agent.type}</span>
      <span className="text-xs font-mono text-zinc-600 w-16">{agent.durationMs ? formatDuration(agent.durationMs) : '—'}</span>
      <span className="text-xs font-mono text-zinc-600 w-16">{agent.toolCalls} calls</span>
      <div className="flex-1" />
      <div className="flex items-center gap-1">
        <button
          onClick={onWatch}
          className={cn(
            'p-1 transition-colors',
            isWatching ? 'text-primary' : 'text-zinc-500 hover:text-zinc-300'
          )}
          title="View result"
        >
          <Eye size={12} />
        </button>
        {(agent.status === 'running' || agent.status === 'pending') && (
          <button onClick={onAbort} className="p-1 text-zinc-500 hover:text-red-400 transition-colors" title="Abort">
            <Square size={10} />
          </button>
        )}
      </div>
    </div>
  );
}

// ─── Sub-Agent Panel ──────────────────────────────────────────

interface SubAgentPanelProps {
  parentAgentId: string | null;
  className?: string;
}

export function SubAgentPanel({ parentAgentId, className }: SubAgentPanelProps) {
  const { subAgents, loading, error, abort, watchResult, watchedOutput, watchedId, clearWatch, fetchList } = useSubAgents(parentAgentId);
  const { addToast } = useToastContext();
  const [expanded, setExpanded] = useState(true);

  const handleAbort = useCallback(async (id: string) => {
    try {
      await abort(id);
      addToast('info', 'Sub-agent aborted');
    } catch (err) {
      addToast('error', `Failed to abort: ${err}`);
    }
  }, [abort, addToast]);

  const handleWatch = useCallback((id: string) => {
    if (watchedId === id) {
      clearWatch();
    } else {
      watchResult(id);
    }
  }, [watchedId, watchResult, clearWatch]);

  return (
    <div className={cn('flex flex-col h-full', className)}>
      {/* Header */}
      <div className="flex items-center gap-2 px-3 py-2 border-b border-white/5 shrink-0">
        <Cpu size={12} className="text-primary" />
        <span className="text-xs font-mono uppercase tracking-widest text-zinc-500 flex-1">
          Sub-Agents
        </span>
        <span className="text-xs font-mono text-zinc-700">{subAgents.length}</span>
        <button onClick={fetchList} className="p-1 text-zinc-500 hover:text-zinc-300 transition-colors">
          <RefreshCw size={10} />
        </button>
        <button onClick={() => setExpanded(!expanded)} className="p-1 text-zinc-500 hover:text-zinc-300 transition-colors">
          {expanded ? <ChevronUp size={10} /> : <ChevronDown size={10} />}
        </button>
      </div>

      {/* Agent list */}
      {expanded && (
        <>
          {/* Column headers */}
          <div className="flex items-center gap-3 px-3 py-1 border-b border-white/5 text-xs font-mono uppercase text-zinc-700 tracking-widest">
            <span className="w-4" />
            <span className="w-24">ID</span>
            <span className="w-20">Type</span>
            <span className="w-16">Duration</span>
            <span className="w-16">Tools</span>
          </div>

          <div className="flex-1 overflow-y-auto custom-scrollbar">
            {loading && subAgents.length === 0 ? (
              <div className="flex items-center justify-center h-16">
                <Loader size={14} className="animate-spin text-zinc-600" />
              </div>
            ) : subAgents.length === 0 ? (
              <EmptyState
                icon={<Cpu size={24} />}
                title="No sub-agents"
                description="Sub-agents will appear here when spawned"
                className="h-32"
              />
            ) : (
              subAgents.map(sa => (
                <SubAgentRow
                  key={sa.subAgentId}
                  agent={sa}
                  isWatching={watchedId === sa.subAgentId}
                  onAbort={() => handleAbort(sa.subAgentId)}
                  onWatch={() => handleWatch(sa.subAgentId)}
                />
              ))
            )}
          </div>

          {/* Metrics bar */}
          {subAgents.length > 0 && (
            <div className="flex items-center gap-4 px-3 py-1.5 border-t border-white/5 text-xs font-mono text-zinc-600">
              <span>Total: {subAgents.length}</span>
              <span>In: {formatTokens(subAgents.reduce((s, a) => s + a.inputTokens, 0))}</span>
              <span>Out: {formatTokens(subAgents.reduce((s, a) => s + a.outputTokens, 0))}</span>
              <span>Calls: {subAgents.reduce((s, a) => s + a.toolCalls, 0)}</span>
            </div>
          )}

          {/* Watched result viewer */}
          {watchedId && watchedOutput !== null && (
            <div className="border-t border-white/5 flex flex-col max-h-[40%]">
              <div className="flex items-center gap-2 px-3 py-1.5 border-b border-white/5 shrink-0">
                <Eye size={10} className="text-primary" />
                <span className="text-xs font-mono text-zinc-400 truncate">{truncateId(watchedId)}</span>
                <div className="flex-1" />
                <button onClick={clearWatch} className="p-1 text-zinc-500 hover:text-zinc-300">
                  <X size={10} />
                </button>
              </div>
              <pre className="flex-1 p-3 text-xs font-mono text-zinc-300 overflow-auto custom-scrollbar whitespace-pre-wrap bg-zinc-950/50">
                {watchedOutput || 'Waiting for output...'}
              </pre>
            </div>
          )}
        </>
      )}
    </div>
  );
}
