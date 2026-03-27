import React from 'react';
import { motion } from 'motion/react';
import { ExternalLink, Zap } from 'lucide-react';
import { cn } from '../lib/utils';

interface Task {
  id: string;
  text: string;
  completed: boolean;
  status: 'completed' | 'in-progress' | 'pending';
}

interface CurrentTaskCardProps {
  task: Task;
  isAutoMode: boolean;
  onReview: () => void;
  onDispatch: () => void;
}

export const CurrentTaskCard: React.FC<CurrentTaskCardProps> = ({ task, isAutoMode, onReview, onDispatch }) => {
  return (
    <motion.div 
      initial={{ opacity: 0, y: 20 }}
      animate={{ opacity: 1, y: 0 }}
      exit={{ opacity: 0, y: -20 }}
      className="bg-black border border-border rounded-none p-10 shadow-2xl relative overflow-hidden shrink-0 glow-primary"
    >
      <div className="absolute top-0 right-0 p-8">
        <div className="text-[10px] font-bold tracking-[0.3em] text-primary bg-primary/5 px-4 py-2 border border-primary/30 uppercase font-mono">
          {task.id.replace('task-', 'TSK_')}
        </div>
      </div>
      <div className="flex items-center gap-3 mb-6">
        <div className={cn(
          "w-3 h-3 rounded-full",
          task.status === 'in-progress' ? "bg-primary glow-primary animate-pulse" : "bg-zinc-600/50"
        )} />
        <span className={cn(
          "text-[10px] font-bold uppercase tracking-[0.3em]",
          task.status === 'in-progress' ? "text-primary" : "text-zinc-500"
        )}>
          {task.status === 'in-progress' ? "Operational Phase Active" : "Queued Directive"}
        </span>
      </div>
      <h2 className="text-2xl lg:text-3xl font-bold tracking-tight mb-4 text-white/90">{task.text}</h2>
      <p className="text-sm lg:text-base text-zinc-400 mb-8 leading-relaxed font-medium max-w-xl">
        {task.status === 'in-progress' 
          ? "The autonomous agent is currently executing this directive. Monitor the terminal for live telemetry and audit logs." 
          : "This architectural directive is staged for execution. Dispatch the agent to begin the implementation phase."}
      </p>
      
      <div className="grid grid-cols-3 gap-4 lg:gap-12 pt-10 border-t border-border">
        <div className="flex flex-col gap-2">
          <span className="block text-[9px] uppercase font-bold tracking-[0.2em] text-zinc-600">Neural Agent</span>
          <span className="text-xs lg:text-sm font-mono text-white font-bold uppercase tracking-tight">Jules-A1-Pro</span>
        </div>
        <div className="flex flex-col gap-2">
          <span className="block text-[9px] uppercase font-bold tracking-[0.2em] text-zinc-600">Active Buffer</span>
          <span className="text-xs lg:text-sm font-mono text-white truncate block font-bold">server.ts</span>
        </div>
        <div className="flex flex-col gap-2">
          <span className="block text-[9px] uppercase font-bold tracking-[0.2em] text-zinc-600">Processing Time</span>
          <span className="text-sm lg:text-base font-mono text-primary font-bold shadow-[0_0_10px_rgba(255,0,255,0.2)]">00:04:23</span>
        </div>
      </div>

      {/* Manual Mode Action */}
      {!isAutoMode && (
        <div className="mt-10 pt-8 border-t border-white/5 flex flex-col sm:flex-row gap-4">
          {task.status === 'in-progress' ? (
            <>
              <button 
                onClick={onReview}
                className="flex-[2] glass border border-primary/40 text-primary font-bold py-5 rounded-2xl text-[11px] uppercase tracking-[0.2em] hover:bg-primary/5 transition-all flex items-center justify-center gap-3 glow-primary hover:scale-105 active:scale-95"
              >
                <ExternalLink size={18} />
                Deep Review & Pivot
              </button>
              <button className="flex-1 glass-dark border border-white/10 rounded-2xl text-[11px] font-bold uppercase tracking-[0.2em] hover:bg-white/5 transition-all text-zinc-400 hover:text-white hover:border-white/20">
                Cancel
              </button>
            </>
          ) : (
            <button 
              onClick={onDispatch}
              className="flex-1 bg-primary text-black font-black py-6 text-[12px] uppercase tracking-[0.4em] hover:bg-primary/90 transition-all flex items-center justify-center gap-4 glow-primary shadow-[0_0_25px_rgba(255,0,255,0.4)] active:scale-95"
            >
              <Zap size={20} fill="currentColor" />
              Initialize Execution
            </button>
          )}
        </div>
      )}
    </motion.div>
  );
};
