import React, { useState, useRef, useEffect, memo } from 'react';
import { ChevronDown, ChevronUp, Terminal } from 'lucide-react';
import { cn } from '../lib/utils';
import { ToolCall } from '../lib/pux-events';
import { Button } from './ui/button';
import { ScrollArea } from './ui/scroll-area';

interface DesktopConsolePanelProps {
  toolCalls: ToolCall[];
}

const DESKTOP_TOOL_PREFIXES = ['computer_use_', 'desktop_', 'x11_'];

function isDesktopTool(name: string): boolean {
  return DESKTOP_TOOL_PREFIXES.some(p => name.startsWith(p));
}

const toolColor: Record<string, string> = {
  computer_use_screenshot: 'text-blue-400',
  computer_use_navigate: 'text-cyan-400',
  computer_use_click: 'text-yellow-400',
  computer_use_type: 'text-green-400',
  computer_use_scroll: 'text-purple-400',
  computer_use_snapshot: 'text-blue-300',
  computer_use_enable: 'text-emerald-400',
  computer_use_disable: 'text-red-400',
  computer_use_exec: 'text-orange-400',
  desktop_screenshot: 'text-blue-400',
  desktop_click: 'text-yellow-400',
  desktop_type: 'text-green-400',
  desktop_key: 'text-pink-400',
  desktop_resolution: 'text-cyan-400',
  desktop_active_window: 'text-purple-400',
};

function formatTime(ts: number): string {
  return new Date(ts).toLocaleTimeString('en-US', { hour12: false, hour: '2-digit', minute: '2-digit', second: '2-digit' });
}

function argsSummary(name: string, args: Record<string, unknown>): string {
  switch (name) {
    case 'computer_use_navigate': return String(args.url || '');
    case 'computer_use_click': return `element #${args.element}`;
    case 'computer_use_type': {
      const t = String(args.text || '');
      return `element #${args.element} "${t.length > 30 ? t.slice(0, 30) + '...' : t}"`;
    }
    case 'computer_use_scroll': return `${args.direction} ${args.amount || 300}px`;
    case 'desktop_click': return `(${args.x}, ${args.y})`;
    case 'desktop_type': return String(args.text || '').slice(0, 40);
    case 'desktop_key': return String(args.key || '');
    case 'computer_use_exec': return String(args.command || '').slice(0, 50);
    default: return '';
  }
}

function resultSummary(result: unknown): string {
  if (!result) return '';
  if (typeof result === 'string') return result.slice(0, 80);
  try {
    const obj = result as Record<string, unknown>;
    const content = obj.content as Array<{ type: string; text: string }> | undefined;
    if (content?.[0]?.text) return content[0].text.slice(0, 80);
    return JSON.stringify(result).slice(0, 80);
  } catch {
    return '';
  }
}

const ConsolePanel = memo(function ConsolePanel({ toolCalls }: DesktopConsolePanelProps) {
  const [collapsed, setCollapsed] = useState(true);
  const scrollRef = useRef<HTMLDivElement>(null);

  const filtered = toolCalls.filter(tc => isDesktopTool(tc.name)).slice(-100);

  // Auto-scroll on new entries
  useEffect(() => {
    if (!collapsed && scrollRef.current) {
      scrollRef.current.scrollTop = scrollRef.current.scrollHeight;
    }
  }, [filtered.length, collapsed]);

  return (
    <div className="border-t border-border bg-background shrink-0 flex flex-col" style={{ maxHeight: '35%' }}>
      {/* Header */}
      <Button
        variant="ghost"
        size="xs"
        onClick={() => setCollapsed(!collapsed)}
        className="flex items-center gap-2 px-3 py-1.5 bg-muted/50 border-b border-border w-full justify-start hover:bg-muted transition-colors"
      >
        <Terminal size={10} className="text-muted-foreground" />
        <span className="text-xs font-mono text-muted-foreground">
          Activity ({filtered.length})
        </span>
        <div className="flex-1" />
        {collapsed ? <ChevronUp size={10} className="text-muted-foreground" /> : <ChevronDown size={10} className="text-muted-foreground" />}
      </Button>

      {/* Log entries */}
      {!collapsed && (
        <ScrollArea className="text-xs font-mono leading-relaxed" style={{ minHeight: 60, maxHeight: 200 }}>
          <div ref={scrollRef}>
            {filtered.length === 0 ? (
              <div className="px-3 py-2 text-muted-foreground/50">Pux will show activity here as it works</div>
            ) : (
              filtered.map(tc => (
                <div key={tc.id} className="px-3 py-0.5 border-b border-border/50 hover:bg-muted/50">
                  <span className="text-muted-foreground">{formatTime(tc.startTime)}</span>
                  {' '}
                  <span className={cn('font-bold', toolColor[tc.name] || 'text-foreground')}>{tc.name}</span>
                  {argsSummary(tc.name, tc.args) && (
                    <span className="text-muted-foreground"> {argsSummary(tc.name, tc.args)}</span>
                  )}
                  {tc.error && (
                    <span className="text-red-400"> ERR: {tc.error.slice(0, 60)}</span>
                  )}
                  {!tc.endTime && (
                    <span className="text-primary animate-pulse"> ...</span>
                  )}
                  {tc.endTime && tc.result && (
                    <span className="text-muted-foreground">{'->'} {resultSummary(tc.result)}</span>
                  )}
                </div>
              ))
            )}
          </div>
        </ScrollArea>
      )}
    </div>
  );
});

export { ConsolePanel as DesktopConsolePanel };
