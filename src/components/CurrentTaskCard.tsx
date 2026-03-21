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
      className="bg-zinc-900 border border-white/10 rounded-2xl p-6 shadow-2xl relative overflow-hidden shrink-0"
    >
      <div className="absolute top-0 right-0 p-4">
        <div className="text-[10px] font-mono text-zinc-500 bg-black/40 px-2 py-1 rounded border border-white/5 uppercase">
          {task.id.replace('task-', 'TSK-')}
        </div>
      </div>
      <div className="flex items-center gap-2 mb-4">
        <div className={cn(
          "w-2 h-2 rounded-full",
          task.status === 'in-progress' ? "bg-primary animate-pulse" : "bg-zinc-600"
        )} />
        <span className={cn(
          "text-[10px] font-bold uppercase tracking-widest",
          task.status === 'in-progress' ? "text-primary" : "text-zinc-500"
        )}>
          {task.status === 'in-progress' ? "Current Task" : "Next Task"}
        </span>
      </div>
      <h2 className="text-lg lg:text-xl font-bold tracking-tight mb-2">{task.text}</h2>
      <p className="text-xs lg:text-sm text-zinc-500 mb-6 leading-relaxed">
        {task.status === 'in-progress' 
          ? "JULES is currently working on this task. Review changes when ready." 
          : "This task is next in the queue. Dispatch to JULES to begin work."}
      </p>
      
      <div className="grid grid-cols-3 gap-2 lg:gap-4 pt-6 border-t border-white/5">
        <div>
          <span className="block text-[8px] lg:text-[10px] uppercase text-zinc-500 mb-1">Agent</span>
          <span className="text-[10px] lg:text-xs font-mono text-zinc-300">CodeAct-v2.1</span>
        </div>
        <div>
          <span className="block text-[8px] lg:text-[10px] uppercase text-zinc-500 mb-1">Context</span>
          <span className="text-[10px] lg:text-xs font-mono text-zinc-300 truncate block">stats.ts</span>
        </div>
        <div>
          <span className="block text-[8px] lg:text-[10px] uppercase text-zinc-500 mb-1">Elapsed</span>
          <span className="text-[10px] lg:text-xs font-mono text-primary">00:04:23</span>
        </div>
      </div>

      {/* Manual Mode Action */}
      {!isAutoMode && (
        <div className="mt-8 pt-6 border-t border-white/5 flex flex-col sm:flex-row gap-3">
          {task.status === 'in-progress' ? (
            <>
              <button 
                onClick={onReview}
                className="flex-1 bg-primary text-black font-bold py-3 rounded-xl text-[10px] lg:text-xs uppercase tracking-widest hover:bg-primary/90 transition-all flex items-center justify-center gap-2 shadow-lg shadow-primary/20"
              >
                <ExternalLink size={14} />
                Review & Merge
              </button>
              <button className="px-6 py-3 sm:py-0 border border-white/10 rounded-xl text-[10px] lg:text-xs font-bold uppercase tracking-widest hover:bg-white/5 transition-all text-zinc-400">
                Reject
              </button>
            </>
          ) : (
            <button 
              onClick={onDispatch}
              className="flex-1 bg-zinc-800 hover:bg-zinc-700 text-white font-bold py-3 rounded-xl text-[10px] lg:text-xs uppercase tracking-widest transition-all flex items-center justify-center gap-2 border border-white/5"
            >
              <Zap size={14} className="text-primary" />
              Dispatch to JULES
            </button>
          )}
        </div>
      )}
    </motion.div>
  );
};
