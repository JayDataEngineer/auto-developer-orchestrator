import React from 'react';
import { ChevronRight, Zap } from 'lucide-react';
import { cn } from '../lib/utils';

interface TerminalProps {
  logs: string[];
  logEndRef: React.RefObject<HTMLDivElement | null>;
  onRetry?: () => void;
  onCommand?: (cmd: string) => void;
}

export const Terminal: React.FC<TerminalProps> = ({ logs, logEndRef, onRetry, onCommand }) => {
  const hasFailure = logs.some(log => log.includes('FAILED') || log.includes('ERROR'));

  const [command, setCommand] = React.useState('');

  const handleCommandSubmit = (e: React.FormEvent) => {
    e.preventDefault();
    if (!command.trim()) return;
    
    onCommand?.(command.trim());
    setCommand('');
  };

  return (
    <div className="flex-1 flex flex-col bg-zinc-950 border border-white/10 rounded-2xl overflow-hidden shadow-2xl min-h-[300px] lg:min-h-0 relative">
      <div className="h-10 bg-zinc-900 border-b border-white/10 flex items-center justify-between px-4 shrink-0">
        <div className="flex items-center gap-2">
          <div className="flex gap-1.5">
            <div className="w-2.5 h-2.5 rounded-full bg-red-500/20 border border-red-500/50" />
            <div className="w-2.5 h-2.5 rounded-full bg-amber-500/20 border border-amber-500/50" />
            <div className="w-2.5 h-2.5 rounded-full bg-emerald-500/20 border border-emerald-500/50" />
          </div>
          <span className="text-[10px] font-mono text-zinc-500 ml-2">bash — orchestrator</span>
        </div>
        <div className="flex items-center gap-3">
          {hasFailure && onRetry && (
            <button 
              onClick={onRetry}
              className="px-2 py-0.5 bg-rose-500/10 border border-rose-500/30 text-rose-400 text-[10px] font-mono rounded hover:bg-rose-500/20 transition-colors flex items-center gap-1"
            >
              <Zap size={10} />
              RETRY_TESTS
            </button>
          )}
          <button className="text-zinc-500 hover:text-white transition-colors">
            <ChevronRight size={14} className="rotate-90" />
          </button>
        </div>
      </div>
      <div className="flex-1 overflow-y-auto p-4 font-mono text-[11px] leading-relaxed text-zinc-400 terminal-scrollbar">
        {logs.map((log, i) => (
          <div key={i} className="mb-1 group hover:bg-white/5 px-1 rounded transition-colors">
            <span className="text-zinc-600 mr-2 select-none">{log.split(']')[0]}]</span>
            <span className={cn(
              log.includes('WARN') ? 'text-amber-400' : 
              log.includes('ERROR') ? 'text-red-400' : 
              log.includes('PASSED') ? 'text-emerald-400' :
              log.includes('FAILED') ? 'text-rose-400' :
              log.includes('AGENT_OBSERVATION') ? 'text-sky-400 italic' :
              log.includes('DEEP AGENT') ? 'text-indigo-400 font-bold' :
              log.includes('$') ? 'text-primary font-bold' :
              'text-zinc-300'
            )}>
              {log.split(']')[1]}
            </span>
          </div>
        ))}
        <div ref={logEndRef} />
      </div>
      <form onSubmit={handleCommandSubmit} className="p-2 bg-black/50 border-t border-white/5 flex items-center gap-2 shrink-0">
        <ChevronRight size={14} className="text-primary" />
        <input 
          type="text"
          value={command}
          onChange={(e) => setCommand(e.target.value)}
          placeholder="Type a command (e.g. generate-checklist, retry, clear)..."
          className="flex-1 bg-transparent border-none outline-none text-[11px] font-mono text-zinc-300 placeholder:text-zinc-700"
        />
      </form>
    </div>
  );
};
