import React, { useState, useRef, useEffect, memo } from 'react';
import { ChevronDown, ChevronUp, Terminal } from 'lucide-react';
import { cn } from '../lib/utils';
import { ToolCall } from '../lib/pi-events';

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
  const [collapsed, setCollapsed] = useState(false);
  const scrollRef = useRef<HTMLDivElement>(null);

  const filtered = toolCalls.filter(tc => isDesktopTool(tc.name)).slice(-100);

  // Auto-scroll on new entries
  useEffect(() => {
    if (!collapsed && scrollRef.current) {
      scrollRef.current.scrollTop = scrollRef.current.scrollHeight;
    }
  }, [filtered.length, collapsed]);

  return (
    <div className="border-t border-white/5 bg-zinc-950 shrink-0 flex flex-col" style={{ maxHeight: '35%' }}>
      {/* Header */}
      <button
        onClick={() => setCollapsed(!collapsed)}
        className="flex items-center gap-2 px-3 py-1.5 bg-zinc-900/50 border-b border-white/5 w-full text-left hover:bg-zinc-900 transition-colors"
      >
        <Terminal size={10} className="text-zinc-500" />
        <span className="text-xs font-mono text-zinc-500 uppercase tracking-widest">
          Console ({filtered.length})
        </span>
        <div className="flex-1" />
        {collapsed ? <ChevronUp size={10} className="text-zinc-600" /> : <ChevronDown size={10} className="text-zinc-600" />}
      </button>

      {/* Log entries */}
      {!collapsed && (
        <div ref={scrollRef} className="overflow-y-auto custom-scrollbar text-xs font-mono leading-relaxed" style={{ minHeight: 60, maxHeight: 200 }}>
          {filtered.length === 0 ? (
            <div className="px-3 py-2 text-zinc-700">No desktop tool calls yet</div>
          ) : (
            filtered.map(tc => (
              <div key={tc.id} className="px-3 py-0.5 border-b border-white/[0.02] hover:bg-white/[0.02]">
                <span className="text-zinc-600">{formatTime(tc.startTime)}</span>
                {' '}
                <span className={cn('font-bold', toolColor[tc.name] || 'text-zinc-400')}>{tc.name}</span>
                {argsSummary(tc.name, tc.args) && (
                  <span className="text-zinc-500"> {argsSummary(tc.name, tc.args)}</span>
                )}
                {tc.error && (
                  <span className="text-red-400"> ERR: {tc.error.slice(0, 60)}</span>
                )}
                {!tc.endTime && (
                  <span className="text-primary animate-pulse"> ...</span>
                )}
                {tc.endTime && tc.result && (
                  <span className="text-zinc-600">{'->'} {resultSummary(tc.result)}</span>
                )}
              </div>
            ))
          )}
        </div>
      )}
    </div>
  );
});

export { ConsolePanel as DesktopConsolePanel };
