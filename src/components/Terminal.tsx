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
    <div className="flex-1 flex flex-col glass border border-white/5 rounded-2xl overflow-hidden shadow-2xl min-h-[300px] lg:min-h-0 relative glow-primary">
      <div className="h-12 glass-dark border-b border-white/5 flex items-center justify-between px-6 shrink-0 z-10">
        <div className="flex items-center gap-3">
          <div className="flex gap-2">
            <div className="w-3 h-3 rounded-full bg-red-500/20 border border-red-500/40" />
            <div className="w-3 h-3 rounded-full bg-amber-500/20 border border-amber-500/40" />
            <div className="w-3 h-3 rounded-full bg-emerald-500/20 border border-emerald-500/40" />
          </div>
          <div className="ml-4 flex items-center gap-2">
            <Terminal size={14} className="text-primary/70" />
            <span className="text-[10px] font-bold tracking-[0.2em] text-zinc-500 uppercase">System Core — bash</span>
          </div>
        </div>
        <div className="flex items-center gap-4">
          {hasFailure && onRetry && (
            <button 
              onClick={onRetry}
              className="px-3 py-1 bg-rose-500/10 border border-rose-500/30 text-rose-400 text-[10px] font-bold rounded-lg hover:bg-rose-500/20 transition-all flex items-center gap-2 glow-primary animate-pulse"
            >
              <Zap size={12} />
              RETRY_SEQUENCE
            </button>
          )}
          <div className="h-6 w-[1px] bg-white/5 mx-2" />
          <button className="text-zinc-500 hover:text-white transition-colors p-1 hover:bg-white/5 rounded">
            <ChevronRight size={16} className="rotate-90" />
          </button>
        </div>
      </div>
      <div className="flex-1 overflow-y-auto p-6 font-mono text-[11px] leading-relaxed text-zinc-400 terminal-scrollbar mask-gradient select-text">
        {logs.map((log, i) => (
          <div key={i} className="mb-2 group hover:bg-white/[0.03] px-2 py-1 rounded-lg transition-all border border-transparent hover:border-white/5">
            <span className="text-zinc-600 mr-3 select-none tabular-nums opacity-60">[{log.split(']')[0].split('[')[1]}]</span>
            <span className={cn(
              "font-medium",
              log.includes('WARN') ? 'text-amber-400/90' : 
              log.includes('ERROR') ? 'text-red-400/90' : 
              log.includes('PASSED') ? 'text-emerald-400/90' :
              log.includes('FAILED') ? 'text-rose-400/90' :
              log.includes('AGENT_OBSERVATION') ? 'text-sky-400/80 italic' :
              log.includes('DEEP AGENT') ? 'text-primary font-bold' :
              log.includes('SYSTEM') ? 'text-zinc-400 font-bold' :
              log.includes('$') ? 'text-white font-bold' :
              'text-zinc-300'
            )}>
              {log.split(']')[1]}
            </span>
          </div>
        ))}
        <div ref={logEndRef} />
      </div>
      <form onSubmit={handleCommandSubmit} className="p-3 glass-dark border-t border-white/5 flex items-center gap-3 shrink-0 relative">
        <div className="absolute inset-0 bg-primary/5 opacity-40 pointer-events-none" />
        <ChevronRight size={16} className="text-primary glow-primary" />
        <input 
          type="text"
          value={command}
          onChange={(e) => setCommand(e.target.value)}
          placeholder="Execute system command..."
          className="flex-1 bg-transparent border-none outline-none text-[12px] font-mono text-white/90 placeholder:text-zinc-700/80 z-10"
        />
        <div className="flex items-center gap-2 px-2 py-1 rounded bg-white/5 border border-white/5 text-[9px] font-bold text-zinc-500 uppercase tracking-widest z-10">
          Enter to exec
        </div>
      </form>
    </div>
  );
};
