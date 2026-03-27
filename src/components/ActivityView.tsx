import React from 'react';
import { motion, AnimatePresence } from 'motion/react';
import { 
  Activity as ActivityIcon, GitCommit, CheckCircle, XCircle, 
  Terminal as TerminalIcon, Cpu, Zap, Radio, AlertTriangle
} from 'lucide-react';
import { cn } from '../lib/utils';

interface ActivityViewProps {
  logs: string[];
}

export const ActivityView: React.FC<ActivityViewProps> = ({ logs }) => {
  // Safe parsing of logs into activity items
  const activities = React.useMemo(() => {
    try {
      return (logs || []).slice().reverse().map((log, index) => {
        try {
          const timeMatch = log.match(/\[(.*?)\]/);
          const time = timeMatch ? timeMatch[1] : 'T_UNKNOWN';
          const message = log.replace(/\[.*?\]\s*/, '');
          
          let icon = TerminalIcon;
          let color = 'text-zinc-500';
          let bg = 'bg-zinc-900/50';
          let border = 'border-white/5';
          
          if (message.includes('ERROR')) {
            icon = XCircle;
            color = 'text-red-500';
            bg = 'bg-red-500/5';
            border = 'border-red-500/10';
          } else if (message.includes('SUCCESS') || message.includes('PASSED') || message.includes('complete') || message.includes('merged')) {
            icon = CheckCircle;
            color = 'text-emerald-500';
            bg = 'bg-emerald-500/5';
            border = 'border-emerald-500/10';
          } else if (message.includes('AGENT') || message.includes('JULES') || message.includes('DISPATCH')) {
            icon = Zap;
            color = 'text-primary';
            bg = 'bg-primary/5';
            border = 'border-primary/10';
          } else if (message.includes('RUNNING') || message.includes('EXECUTING')) {
            icon = Cpu;
            color = 'text-blue-500';
          }

          return { id: `log-${index}-${time}`, message, time, icon, color, bg, border };
        } catch (e) {
          return { 
            id: `err-${index}`, 
            message: log || 'MALFORMED_TELEMETRY_RECORD', 
            time: 'N/A', 
            icon: AlertTriangle, 
            color: 'text-zinc-700', 
            bg: 'bg-zinc-900/20', 
            border: 'border-white/5' 
          };
        }
      });
    } catch (e) {
      console.error("Activity mapping failed", e);
      return [];
    }
  }, [logs]);

  return (
    <div className="flex-1 bg-black overflow-y-auto custom-scrollbar p-12">
      <div className="max-w-5xl mx-auto space-y-12">
        {/* Header Telemetry */}
        <header className="flex flex-col md:flex-row md:items-end justify-between gap-8 border-b border-white/5 pb-12">
          <div className="space-y-4">
            <div className="flex items-center gap-3 text-primary font-mono text-[10px] font-black uppercase tracking-[0.4em]">
              <div className="w-1.5 h-1.5 rounded-full bg-primary animate-pulse" />
              Live Telemetry Stream
            </div>
            <h1 className="text-4xl font-black italic uppercase tracking-tighter text-white">
              System Activity
            </h1>
          </div>
          
          <div className="grid grid-cols-2 lg:grid-cols-3 gap-6">
            {[
              { label: "Stability", value: "99.9%", icon: <Zap size={14} /> },
              { label: "Sync Latency", value: "14ms", icon: <Radio size={14} /> },
              { label: "Active Procs", value: activities.length + 8, icon: <Cpu size={14} /> }
            ].map(stat => (
              <div key={stat.label} className="p-4 border border-white/5 bg-zinc-900/20 rounded">
                <div className="flex items-center gap-2 text-[8px] font-mono text-zinc-600 uppercase tracking-widest mb-1">
                  {stat.icon} {stat.label}
                </div>
                <div className="text-lg font-black text-white glow-white">{stat.value}</div>
              </div>
            ))}
          </div>
        </header>

        {/* Activity Stream */}
        <div className="space-y-4 relative">
          <div className="absolute left-4 top-0 bottom-0 w-px bg-gradient-to-b from-primary/20 via-white/5 to-transparent" />
          
          <div className="space-y-4">
            <AnimatePresence initial={false} mode="popLayout">
              {activities.map((act, i) => (
                <motion.div 
                  key={act.id}
                  layout
                  initial={{ opacity: 0, x: -10 }}
                  animate={{ opacity: 1, x: 0 }}
                  exit={{ opacity: 0, scale: 0.95 }}
                  transition={{ duration: 0.2 }}
                  className={cn(
                    "relative pl-12 pr-6 py-6 border rounded-sm transition-all duration-300 group",
                    act.bg, act.border,
                    "hover:border-white/20"
                  )}
                >
                  {/* Connector Point */}
                  <div className={cn(
                    "absolute left-[13px] top-1/2 -translate-y-1/2 w-1.5 h-1.5 rounded-full border border-black",
                    act.color.replace('text-', 'bg-')
                  )} />
                  
                  <div className="flex flex-col md:flex-row md:items-center justify-between gap-4">
                    <div className="flex items-start gap-4">
                      <div className={cn("p-2 rounded bg-black/40 border border-white/5", act.color)}>
                        <act.icon size={16} />
                      </div>
                      <div>
                        <div className={cn("text-[11px] font-bold uppercase tracking-tight", act.color)}>
                          {act.message.includes(':') ? act.message.split(':')[0] : 'PROCESS_MONITOR'}
                        </div>
                        <div className="text-xs text-zinc-300 font-mono mt-0.5 max-w-2xl leading-relaxed">
                          {act.message.includes(':') ? act.message.split(':').slice(1).join(':') : act.message}
                        </div>
                      </div>
                    </div>
                    
                    <div className="text-[9px] font-mono text-zinc-700 uppercase tracking-widest bg-black/40 px-2 py-1 rounded border border-white/5 shrink-0 self-start md:self-center">
                      {act.time}
                    </div>
                  </div>
                </motion.div>
              ))}
            </AnimatePresence>
          </div>

          {activities.length === 0 && (
            <div className="flex flex-col items-center justify-center py-24 text-center">
              <ActivityIcon className="text-zinc-900 mb-6 animate-pulse" size={64} />
              <div className="text-[10px] font-mono text-zinc-700 uppercase tracking-[0.4em]">
                Awaiting System Heartbeat...
              </div>
            </div>
          )}
        </div>
      </div>
    </div>
  );
};
