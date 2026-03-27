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
    <section className="w-full lg:w-1/2 border-b lg:border-b-0 lg:border-r border-border flex flex-col bg-black h-[45vh] lg:h-auto shrink-0 overflow-hidden">
      <div className="p-6 border-b border-border flex flex-col gap-6 bg-secondary">
        <div className="flex justify-between items-end">
          <div className="flex flex-col gap-1">
            <h2 className="text-[10px] font-bold uppercase tracking-[0.3em] text-primary">Architectural Directives</h2>
            <div className="flex items-center gap-2">
              <span className="text-xl font-bold tracking-[0.1em] text-white uppercase italic">Project Backlog</span>
              <div className={cn(
                "px-2 py-0.5 text-[9px] font-bold border font-mono",
                tasks.length > 0 ? "bg-primary/10 border-primary/30 text-primary" : "bg-black border-border text-zinc-600"
              )}>
                {tasks.length.toString().padStart(2, '0')} {tasks.length === 1 ? 'TASK' : 'TASKS'}
              </div>
            </div>
          </div>
          <div className="flex flex-col items-end gap-1 font-mono">
            <span className="text-[9px] text-zinc-500 uppercase tracking-widest font-bold">Progress</span>
            <span className="text-sm font-bold text-primary tabular-nums">
              {Math.round((tasks.filter(t => t.completed).length / (tasks.length || 1)) * 100)}%
            </span>
          </div>
        </div>
        
        <div className="flex items-center gap-2">
          <div className="flex-1 relative group">
            <input
              type="text"
              placeholder="Inject technical guidance..."
              value={guidancePrompt}
              onChange={(e) => setGuidancePrompt(e.target.value)}
              disabled={isGenerating}
              className="w-full bg-black border border-border px-4 py-2.5 text-xs text-zinc-300 placeholder:text-zinc-800 focus:outline-none focus:border-primary transition-all font-mono"
            />
          </div>
          
          {onGenerateAI && (
            <button 
              onClick={() => onGenerateAI(guidancePrompt)}
              disabled={isGenerating || isDispatching}
              className={cn(
                "flex items-center gap-2 px-4 py-2.5 bg-black border border-primary text-[10px] font-bold text-primary hover:bg-primary/10 transition-all shrink-0 uppercase tracking-widest",
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
                "flex items-center gap-2 px-4 py-2.5 bg-primary border border-primary text-[10px] font-bold text-white hover:bg-primary/90 transition-all shrink-0 uppercase tracking-widest",
                (isGenerating || isDispatching) && "opacity-50 cursor-not-allowed"
              )}
            >
              <CheckCircle2 size={14} className={cn(isDispatching && "animate-pulse")} />
              {isDispatching ? 'DISPATCHING...' : 'DISPATCH ALL'}
            </button>
          )}
        </div>
      </div>

      <div className="flex-1 overflow-y-auto p-6 space-y-2 terminal-scrollbar bg-black">
        {tasks.length === 0 ? (
          <div className="h-full flex flex-col items-center justify-center text-center p-8 space-y-8 animate-in fade-in zoom-in duration-700">
            <div className="relative">
              <div className="w-16 h-16 border border-primary flex items-center justify-center text-primary glow-primary">
                <Sparkles size={32} className="animate-pulse-slow" />
              </div>
            </div>
            <div className="space-y-3 max-w-sm">
              <h3 className="text-lg font-bold text-white tracking-widest uppercase">System Standby</h3>
              <p className="text-xs text-zinc-600 leading-relaxed font-mono font-bold">
                NEURAL_ENGINE_READY // INITIALIZE_PROJECT_AUDIT
              </p>
            </div>
            {onGenerateAI && (
              <button 
                onClick={() => onGenerateAI(guidancePrompt)}
                disabled={isGenerating}
                className={cn(
                  "w-full max-w-[240px] bg-black border border-primary text-primary font-bold py-4 text-[10px] uppercase tracking-[0.3em] hover:bg-primary/10 transition-all flex items-center justify-center gap-3 glow-primary",
                  isGenerating && "opacity-50 cursor-not-allowed"
                )}
              >
                {isGenerating ? (
                  <>
                    <div className="w-4 h-4 border-2 border-primary border-t-transparent rounded-full animate-spin" />
                    PROCESSING...
                  </>
                ) : (
                  <>
                    <Sparkles size={16} />
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
              style={{ animationDelay: `${idx * 40}ms` }}
              className={cn(
                "group flex items-start gap-4 p-5 border transition-all duration-300 animate-in slide-in-from-right-4 fade-in",
                task.status === 'completed' ? "bg-black border-border opacity-40" : 
                task.status === 'in-progress' ? "bg-black border-primary shadow-[0_0_20px_rgba(255,0,255,0.1)]" :
                "bg-black border-border hover:border-primary/50 hover:bg-primary/5"
              )}
            >
              <div className="mt-1 shrink-0">
                {task.status === 'completed' ? (
                  <div className="w-5 h-5 bg-success/10 flex items-center justify-center border border-success/40">
                    <CheckCircle2 className="text-success" size={14} />
                  </div>
                ) : task.status === 'in-progress' ? (
                  <div className="w-5 h-5 border-2 border-primary border-t-transparent animate-spin" />
                ) : (
                  <div className="w-5 h-5 border border-primary/20 bg-black transition-colors group-hover:border-primary/50" />
                )}
              </div>
              <div className="flex-1">
                <h3 className={cn(
                  "text-[13px] font-bold leading-relaxed tracking-tight uppercase",
                  task.status === 'completed' ? "line-through text-zinc-600" : "text-white"
                )}>
                  {task.text}
                </h3>
                {task.status === 'in-progress' && (
                  <div className="mt-2 flex items-center gap-2">
                    <div className="px-2 py-0.5 bg-primary/10 border border-primary/30 font-mono">
                      <span className="text-[9px] font-bold text-primary uppercase tracking-widest italic">Executing Agent Phase</span>
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
