import React, { useState } from 'react';
import { CheckCircle2, Circle, Sparkles } from 'lucide-react';
import { cn } from '../lib/utils';

interface Task {
  id: string;
  text: string;
  completed: boolean;
  status: 'completed' | 'in-progress' | 'pending';
}

interface ChecklistProps {
  tasks: Task[];
  onGenerateAI?: (prompt?: string) => void;
  isGenerating?: boolean;
}

export const Checklist: React.FC<ChecklistProps> = ({ tasks, onGenerateAI, isGenerating }) => {
  const [guidancePrompt, setGuidancePrompt] = useState('');

  return (
    <section className="w-full lg:w-1/2 border-b lg:border-b-0 lg:border-r border-white/10 flex flex-col bg-zinc-950/50 h-[40vh] lg:h-auto shrink-0">
      <div className="p-4 border-b border-white/10 flex flex-col gap-3 bg-zinc-950/30">
        <div className="flex justify-between items-center">
          <div className="flex items-center gap-4">
            <h2 className="text-[10px] font-bold uppercase tracking-[0.2em] text-zinc-500">Active Directives</h2>
            {onGenerateAI && (
              <button 
                onClick={() => onGenerateAI(guidancePrompt)}
                disabled={isGenerating}
                className={cn(
                  "flex items-center gap-1.5 px-2 py-0.5 bg-primary/10 border border-primary/20 rounded text-[10px] font-bold text-primary hover:bg-primary/20 transition-all",
                  isGenerating && "opacity-50 cursor-not-allowed animate-pulse"
                )}
              >
                <Sparkles size={10} />
                {isGenerating ? 'GENERATING...' : 'GENERATE_WITH_AI'}
              </button>
            )}
          </div>
          <span className="text-[10px] font-mono text-primary">
            {tasks.filter(t => t.completed).length}/{tasks.length} Complete
          </span>
        </div>
        {onGenerateAI && (
          <input
            type="text"
            placeholder="Optional guidance (e.g., 'Focus on adding Stripe integration')"
            value={guidancePrompt}
            onChange={(e) => setGuidancePrompt(e.target.value)}
            disabled={isGenerating}
            className="w-full bg-black/50 border border-white/5 rounded-lg px-3 py-1.5 text-xs text-zinc-300 placeholder:text-zinc-600 focus:outline-none focus:border-primary/30 transition-colors"
          />
        )}
      </div>
      <div className="flex-1 overflow-y-auto p-6 space-y-2 terminal-scrollbar">
        {tasks.length === 0 ? (
          <div className="h-full flex flex-col items-center justify-center text-center p-8 space-y-6">
            <div className="w-16 h-16 rounded-3xl bg-primary/10 flex items-center justify-center text-primary/40">
              <Sparkles size={32} />
            </div>
            <div className="space-y-2 max-w-xs">
              <h3 className="text-lg font-bold text-white tracking-tight">No Directives Found</h3>
              <p className="text-xs text-zinc-500 leading-relaxed">
                This project hasn't been initialized with a <span className="text-primary italic">TODO_FOR_JULES.md</span> file yet.
              </p>
            </div>
            {onGenerateAI && (
              <button 
                onClick={() => onGenerateAI(guidancePrompt)}
                disabled={isGenerating}
                className={cn(
                  "w-full max-w-xs bg-primary text-black font-bold py-4 rounded-xl text-xs uppercase tracking-widest hover:bg-primary/90 transition-all flex items-center justify-center gap-2 shadow-lg shadow-primary/20",
                  isGenerating && "opacity-50 cursor-not-allowed"
                )}
              >
                {isGenerating ? (
                  <>
                    <div className="w-4 h-4 border-2 border-black border-t-transparent rounded-full animate-spin" />
                    Analyzing Codebase...
                  </>
                ) : (
                  <>
                    <Sparkles size={16} />
                    Initialize with AI
                  </>
                )}
              </button>
            )}
          </div>
        ) : (
          tasks.map((task) => (
            <div 
              key={task.id}
              className={cn(
                "group flex items-start gap-4 p-4 rounded-xl border transition-all duration-300",
                task.status === 'completed' ? "bg-zinc-900/30 border-transparent opacity-50" : 
                task.status === 'in-progress' ? "bg-primary/5 border-primary/20 shadow-lg shadow-primary/5" :
                "bg-transparent border-white/5 hover:border-white/10"
              )}
            >
              <div className="mt-1 shrink-0">
                {task.status === 'completed' ? (
                  <CheckCircle2 className="text-emerald-500" size={18} />
                ) : task.status === 'in-progress' ? (
                  <div className="w-[18px] h-[18px] rounded-full border-2 border-primary border-t-transparent animate-spin" />
                ) : (
                  <Circle className="text-zinc-700" size={18} />
                )}
              </div>
              <div className="flex-1">
                <h3 className={cn(
                  "text-sm font-medium",
                  task.status === 'completed' ? "line-through text-zinc-500" : "text-zinc-200"
                )}>
                  {task.text}
                </h3>
                {task.status === 'in-progress' && (
                  <div className="mt-2 flex items-center gap-2">
                    <span className="text-[10px] font-mono text-primary uppercase tracking-widest">In Progress</span>
                  </div>
                )}
              </div>
            </div>
          ))
        )}
      </div>
    </section>
  );
};
