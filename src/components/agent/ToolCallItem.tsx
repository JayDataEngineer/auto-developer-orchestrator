import React, { useState } from 'react';
import { ChevronRight, Wrench, Loader, FileCode, Search, Terminal as TerminalIcon } from 'lucide-react';
import { cn } from '../../lib/utils';
import { ToolCall } from '../../lib/pi-events';

const TOOL_ICONS: Record<string, React.ReactNode> = {
  read: <FileCode size={12} />,
  write: <FileCode size={12} />,
  edit: <FileCode size={12} />,
  bash: <TerminalIcon size={12} />,
  grep: <Search size={12} />,
  find: <Search size={12} />,
};

function formatToolArgs(name: string, args: Record<string, unknown>): string {
  if (!args) return '';
  if (name === 'read' || name === 'write' || name === 'edit') {
    return String(args.filePath || args.path || '');
  }
  if (name === 'bash') {
    return String(args.command || '').slice(0, 80);
  }
  if (name === 'grep') {
    return `${args.pattern} in ${args.path || '.'}`;
  }
  return JSON.stringify(args).slice(0, 80);
}

function formatResult(result: unknown): string {
  if (result === undefined || result === null) return '';
  if (typeof result === 'string') return result;
  return JSON.stringify(result, null, 2);
}

export function ToolCallItem({ tc }: { tc: ToolCall }) {
  const [open, setOpen] = useState(false);
  const isRunning = !tc.endTime;
  return (
    <div className="border border-white/5 bg-zinc-950">
      <button
        onClick={() => setOpen(!open)}
        className="w-full flex items-center gap-2 px-3 py-2 text-left"
      >
        {TOOL_ICONS[tc.name] || <Wrench size={11} className="text-muted-foreground" />}
        <span className="text-[9px] font-mono uppercase tracking-widest text-muted-foreground">
          {tc.name}
        </span>
        <span className="text-[9px] font-mono text-zinc-600 truncate">
          {formatToolArgs(tc.name, tc.args)}
        </span>
        <div className="flex-1" />
        {isRunning ? (
          <Loader size={10} className="text-primary animate-spin" />
        ) : (
          <ChevronRight size={10} className={cn("text-muted-foreground transition-transform", open && "rotate-90")} />
        )}
      </button>
      {open && !isRunning && formatResult(tc.result) && (
        <div className="px-3 pb-2 border-t border-white/5">
          <pre className="text-[9px] font-mono text-zinc-400 whitespace-pre-wrap max-h-40 overflow-auto">
            {formatResult(tc.result)}
          </pre>
        </div>
      )}
    </div>
  );
}
