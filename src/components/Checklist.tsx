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
  onDispatchAll?: () => void;
  isGenerating?: boolean;
  isDispatching?: boolean;
}

export const Checklist: React.FC<ChecklistProps> = ({ 
  tasks, 
  onGenerateAI, 
  onDispatchAll,
  isGenerating, 
  isDispatching 
}) => {
  const [guidancePrompt, setGuidancePrompt] = useState('');

  return (
    <section className="w-full lg:w-1/2 border-b lg:border-b-0 lg:border-r border-white/5 flex flex-col bg-transparent h-[45vh] lg:h-auto shrink-0 overflow-hidden">
      <div className="p-6 border-b border-white/5 flex flex-col gap-4 bg-white/[0.02]">
        <div className="flex justify-between items-end">
          <div className="flex flex-col gap-1">
            <h2 className="text-[10px] font-bold uppercase tracking-[0.3em] text-primary/70">Architectural Directives</h2>
            <div className="flex items-center gap-2">
              <span className="text-xl font-bold tracking-tight text-white/90">Project Backlog</span>
              <div className={cn(
                "px-2 py-0.5 rounded-md text-[10px] font-bold border",
                tasks.length > 0 ? "bg-emerald-500/10 border-emerald-500/20 text-emerald-400" : "bg-zinc-500/10 border-zinc-500/20 text-zinc-500"
              )}>
                {tasks.length} {tasks.length === 1 ? 'TASK' : 'TASKS'}
              </div>
            </div>
          </div>
          <div className="flex flex-col items-end gap-1">
            <span className="text-[10px] font-mono text-zinc-500 uppercase tracking-widest">Progress</span>
            <span className="text-sm font-bold text-primary tabular-nums">
              {Math.round((tasks.filter(t => t.completed).length / (tasks.length || 1)) * 100)}%
            </span>
          </div>
        </div>
        
        <div className="flex items-center gap-3">
          <div className="flex-1 relative group">
            <input
              type="text"
              placeholder="Inject technical guidance (e.g. 'Optimize for scale')"
              value={guidancePrompt}
              onChange={(e) => setGuidancePrompt(e.target.value)}
              disabled={isGenerating}
              className="w-full bg-black/40 border border-white/5 rounded-xl px-4 py-2.5 text-xs text-zinc-300 placeholder:text-zinc-600 focus:outline-none focus:border-primary/40 focus:ring-1 focus:ring-primary/20 transition-all"
            />
            <div className="absolute inset-0 rounded-xl bg-primary/5 opacity-0 group-hover:opacity-100 pointer-events-none transition-opacity" />
          </div>
          
          {onGenerateAI && (
            <button 
              onClick={() => onGenerateAI(guidancePrompt)}
              disabled={isGenerating || isDispatching}
              className={cn(
                "flex items-center gap-2 px-4 py-2.5 glass border border-primary/30 rounded-xl text-[10px] font-bold text-primary hover:bg-primary/10 transition-all hover:scale-105 active:scale-95 glow-primary shrink-0",
                (isGenerating || isDispatching) && "opacity-50 cursor-not-allowed"
              )}
            >
              <Sparkles size={14} className={cn(isGenerating && "animate-spin")} />
              {isGenerating ? 'ANALYZING...' : 'RE-GENERATE'}
            </button>
          )}

          {tasks.length > 0 && onDispatchAll && (
            <button 
              onClick={onDispatchAll}
              disabled={isGenerating || isDispatching}
              className={cn(
                "flex items-center gap-2 px-4 py-2.5 bg-primary/20 border border-primary/40 rounded-xl text-[10px] font-bold text-primary hover:bg-primary/30 transition-all hover:scale-105 active:scale-95 glow-primary shrink-0",
                (isGenerating || isDispatching) && "opacity-50 cursor-not-allowed"
              )}
            >
              <CheckCircle2 size={14} className={cn(isDispatching && "animate-pulse")} />
              {isDispatching ? 'DISPATCHING...' : 'DISPATCH ALL'}
            </button>
          )}
        </div>
      </div>

      <div className="flex-1 overflow-y-auto p-6 space-y-3 terminal-scrollbar scroll-smooth">
        {tasks.length === 0 ? (
          <div className="h-full flex flex-col items-center justify-center text-center p-8 space-y-8 animate-in fade-in zoom-in duration-700">
            <div className="relative">
              <div className="absolute inset-0 bg-primary/20 blur-3xl rounded-full scale-150 animate-pulse" />
              <div className="relative w-24 h-24 rounded-[2.5rem] glass border border-primary/30 flex items-center justify-center text-primary glow-primary">
                <Sparkles size={48} className="animate-pulse-slow" />
              </div>
            </div>
            <div className="space-y-3 max-w-sm">
              <h3 className="text-2xl font-bold text-white tracking-tight">System Ready.</h3>
              <p className="text-sm text-zinc-400 leading-relaxed font-medium">
                The neural engine is standby. Initialize a <span className="text-primary font-bold">deep analysis</span> to generate your architectural roadmap.
              </p>
            </div>
            {onGenerateAI && (
              <button 
                onClick={() => onGenerateAI(guidancePrompt)}
                disabled={isGenerating}
                className={cn(
                  "w-full max-w-[240px] glass-dark border border-primary/40 text-primary font-bold py-5 rounded-2xl text-[11px] uppercase tracking-[0.2em] hover:bg-primary/5 transition-all flex items-center justify-center gap-3 glow-primary hover:scale-105 active:scale-95",
                  isGenerating && "opacity-50 cursor-not-allowed"
                )}
              >
                {isGenerating ? (
                  <>
                    <div className="w-5 h-5 border-2 border-primary border-t-transparent rounded-full animate-spin" />
                    Neural Processing...
                  </>
                ) : (
                  <>
                    <Sparkles size={18} />
                    Begin Codebase Audit
                  </>
                )}
              </button>
            )}
          </div>
        ) : (
          tasks.map((task, idx) => (
            <div 
              key={task.id}
              style={{ animationDelay: `${idx * 50}ms` }}
              className={cn(
                "group flex items-start gap-4 p-4 rounded-2xl border transition-all duration-500 animate-in slide-in-from-right-4 fade-in",
                task.status === 'completed' ? "bg-zinc-900/10 border-white/5 opacity-40 grayscale-[50%]" : 
                task.status === 'in-progress' ? "glass border-primary/30 shadow-2xl shadow-primary/10 glow-primary scale-[1.02]" :
                "glass-dark border-white/5 hover:border-white/10 hover:bg-white/[0.02] hover:-translate-y-0.5"
              )}
            >
              <div className="mt-1 shrink-0">
                {task.status === 'completed' ? (
                  <div className="w-5 h-5 rounded-full bg-emerald-500/20 flex items-center justify-center border border-emerald-500/30">
                    <CheckCircle2 className="text-emerald-500" size={14} />
                  </div>
                ) : task.status === 'in-progress' ? (
                  <div className="w-5 h-5 rounded-full border-2 border-primary border-t-transparent animate-spin" />
                ) : (
                  <div className="w-5 h-5 rounded-full border-2 border-zinc-700 transition-colors group-hover:border-zinc-500" />
                )}
              </div>
              <div className="flex-1">
                <h3 className={cn(
                  "text-[13px] font-semibold leading-relaxed tracking-tight",
                  task.status === 'completed' ? "line-through text-zinc-500" : "text-white/90"
                )}>
                  {task.text}
                </h3>
                {task.status === 'in-progress' && (
                  <div className="mt-2 flex items-center gap-2">
                    <div className="px-2 py-0.5 rounded bg-primary/10 border border-primary/20">
                      <span className="text-[9px] font-bold text-primary uppercase tracking-widest">Executing Agent Phase</span>
                    </div>
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
