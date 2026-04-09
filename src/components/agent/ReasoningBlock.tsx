import React, { useState } from 'react';
import { Brain, ChevronDown, ChevronRight } from 'lucide-react';

export function ReasoningBlock({ content, defaultOpen = false }: { content: string; defaultOpen?: boolean }) {
  const [open, setOpen] = useState(defaultOpen);
  return (
    <div className="border border-zinc-800 bg-zinc-950">
      <button
        onClick={() => setOpen(!open)}
        className="w-full flex items-center gap-3 p-3 text-left"
      >
        <Brain size={12} className="text-muted-foreground" />
        <span className="text-[9px] font-black uppercase tracking-widest text-muted-foreground">
          Reasoning
        </span>
        <span className="text-[9px] font-mono text-zinc-700">
          {content.length} chars
        </span>
        <div className="flex-1" />
        {open ? <ChevronDown size={10} className="text-muted-foreground" /> : <ChevronRight size={10} className="text-muted-foreground" />}
      </button>
      {open && (
        <div className="px-3 pb-3 border-t border-zinc-800">
          <pre className="text-[10px] font-mono text-muted whitespace-pre-wrap max-h-64 overflow-auto">
            {content}
          </pre>
        </div>
      )}
    </div>
  );
}
