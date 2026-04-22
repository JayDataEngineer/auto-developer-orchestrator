import React, { useState } from 'react';
import { ChevronRight, Wrench, Loader, FileCode, Search, Terminal as TerminalIcon } from 'lucide-react';
import { cn } from '../../lib/utils';
import { ToolCall } from '../../lib/pux-events';
import { Button } from '../ui/button';
import { ScrollArea } from '../ui/scroll-area';

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
  const isRunning = !tc.endTime;
  const [open, setOpen] = useState(isRunning);
  return (
    <div className={cn(
      "border bg-background",
      isRunning ? "border-primary/30 bg-primary/5" : "border-border"
    )}>
      <Button
        variant="ghost"
        size="xs"
        onClick={() => setOpen(!open)}
        className="w-full flex items-center gap-2 px-3 py-2 text-left justify-start"
      >
        {TOOL_ICONS[tc.name] || <Wrench size={11} className="text-muted-foreground" />}
        <span className={cn(
          "text-xs font-semibold",
          isRunning ? "text-primary" : "text-muted-foreground"
        )}>
          {tc.name}
        </span>
        <span className="text-xs font-mono text-muted-foreground/60 truncate">
          {formatToolArgs(tc.name, tc.args)}
        </span>
        <div className="flex-1" />
        {isRunning ? (
          <Loader size={10} className="text-primary animate-spin" />
        ) : (
          <ChevronRight size={10} className={cn("text-muted-foreground transition-transform", open && "rotate-90")} />
        )}
      </Button>
      {open && formatResult(tc.result) && (
        <div className="px-3 pb-2 border-t border-border">
          <ScrollArea className="max-h-40">
            <pre className="text-xs font-mono text-muted-foreground whitespace-pre-wrap">
              {formatResult(tc.result)}
            </pre>
          </ScrollArea>
        </div>
      )}
      {open && tc.error && (
        <div className="px-3 pb-2 border-t border-border">
          <ScrollArea className="max-h-40">
            <pre className="text-xs font-mono text-red-400 whitespace-pre-wrap">
              {tc.error}
            </pre>
          </ScrollArea>
        </div>
      )}
    </div>
  );
}
