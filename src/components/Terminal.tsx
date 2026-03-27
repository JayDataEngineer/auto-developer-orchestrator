import React from 'react';
import { ChevronRight, Zap, Square, Terminal as TerminalIcon } from 'lucide-react';
import { cn } from '../lib/utils';

interface TerminalProps {
  logs: string[];
  logEndRef: React.RefObject<HTMLDivElement | null>;
  onRetry?: () => void;
  onCommand?: (cmd: string) => void;
}

export const Terminal: React.FC<TerminalProps> = ({ logs = [], logEndRef, onRetry, onCommand }) => {
  const hasFailure = logs.some(log => log.includes('FAILED') || log.includes('ERROR'));

  const [command, setCommand] = React.useState('');

  const handleCommandSubmit = (e: React.FormEvent) => {
    e.preventDefault();
    if (!command.trim()) return;
    
    onCommand?.(command.trim());
    setCommand('');
  };

  return (
    <div className="flex-1 flex flex-col bg-black border border-border overflow-hidden min-h-[300px] lg:min-h-0 relative group">
      <div className="h-10 bg-secondary border-b border-border flex items-center justify-between px-6 shrink-0 z-10">
        <div className="flex items-center gap-3">
          <div className="flex gap-1.5">
            <div className="w-2.5 h-2.5 border border-border bg-black" />
            <div className="w-2.5 h-2.5 border border-border bg-black" />
            <div className="w-2.5 h-2.5 border border-border bg-black" />
          </div>
          <div className="ml-4 flex items-center gap-2">
            <TerminalIcon size={12} className="text-primary" />
            <span className="text-[10px] font-bold tracking-[0.25em] text-zinc-500 uppercase font-mono">SYS_CORE_BASH</span>
          </div>
        </div>
        <div className="flex items-center gap-4">
          {hasFailure && onRetry && (
            <button 
              onClick={onRetry}
              className="px-3 py-1 bg-red-500/10 border border-red-500/30 text-red-500 text-[10px] font-bold hover:bg-red-500/20 transition-all flex items-center gap-2 glow-primary"
            >
              <Zap size={12} />
              RETRY_SEQUENCE
            </button>
          )}
          <div className="h-4 w-[1px] bg-border mx-2" />
          <button className="text-zinc-500 hover:text-white transition-colors p-1">
            <ChevronRight size={14} className="rotate-90" />
          </button>
        </div>
      </div>
      <div className="flex-1 overflow-y-auto p-6 font-mono text-[11px] leading-relaxed text-zinc-400 terminal-scrollbar select-text bg-black">
        {logs.map((log, i) => (
          <div key={i} className="mb-1 group hover:bg-primary/5 px-2 py-0.5 transition-all border-l-2 border-transparent hover:border-primary">
            <span className="text-zinc-600 mr-3 select-none tabular-nums opacity-60">[{log.split(']')[0].split('[')[1]}]</span>
            <span className={cn(
              "font-mono tracking-tight",
              log.includes('WARN') ? 'text-warning font-bold' :
              log.includes('ERROR') ? 'text-error font-bold' :
              log.includes('PASSED') ? 'text-success font-bold' :
              log.includes('FAILED') ? 'text-error font-bold' :
              log.includes('SYSTEM') ? 'text-primary font-bold' :
              log.includes('$') ? 'text-white font-bold' :
              'text-zinc-300'
            )}>
              {log.split(']')[1]}
            </span>
          </div>
        ))}
        <div ref={logEndRef} />
      </div>
      <form onSubmit={handleCommandSubmit} className="p-3 bg-black border-t border-border flex items-center gap-3 shrink-0">
        <div className="flex items-center gap-2 px-3 py-1 bg-secondary border border-border text-[9px] font-bold text-primary uppercase tracking-widest font-mono">
          $
        </div>
        <div className="relative flex-1 flex items-center">
          <input 
            type="text"
            value={command}
            onChange={(e) => setCommand(e.target.value)}
            placeholder="TYPE_SYSTEM_CMD"
            className="flex-1 bg-transparent border-none outline-none text-[12px] font-mono text-white placeholder:text-zinc-900 z-10"
          />
          {command.length === 0 && (
            <div className="absolute left-0 w-2.5 h-4 bg-primary animate-pulse ml-0.5" />
          )}
        </div>
        <div className="hidden md:flex items-center gap-2 px-3 py-1 bg-secondary border border-border text-[8px] font-bold text-zinc-700 uppercase tracking-[0.3em]">
          SYS_READY_TTY1
        </div>
      </form>
    </div>
  );
};
