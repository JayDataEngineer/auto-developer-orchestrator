import React from 'react';
import { Globe, Loader } from 'lucide-react';
import { cn } from '../lib/utils';
import { useComputerUse } from '../hooks/useComputerUse';

export function BrowserTools({ cu, sandboxId, urlInput, setUrlInput, typeText, setTypeText, selectedElement, setSelectedElement, onNavigate, onElementClick, onType, onEnable }: {
  cu: ReturnType<typeof useComputerUse>;
  sandboxId: string | null;
  urlInput: string;
  setUrlInput: (v: string) => void;
  typeText: string;
  setTypeText: (v: string) => void;
  selectedElement: number | null;
  setSelectedElement: (v: number | null) => void;
  onNavigate: (e: React.FormEvent) => void;
  onElementClick: (elId: number, tag: string) => void;
  onType: () => void;
  onEnable: () => void;
}) {
  if (!cu.enabled) {
    return (
      <div className="border-b border-white/5 p-4">
        <div className="flex items-center gap-2 mb-3">
          <Globe size={11} className="text-muted-foreground" />
          <span className="text-[9px] font-black uppercase tracking-widest text-muted-foreground">
            Computer Use
          </span>
        </div>
        <button
          onClick={onEnable}
          disabled={!sandboxId || cu.loading}
          className="px-4 py-2 bg-primary text-black text-[9px] font-black uppercase tracking-widest hover:bg-primary/80 disabled:opacity-30 transition-colors"
        >
          {cu.loading ? 'Starting...' : 'Enable Computer Use'}
        </button>
      </div>
    );
  }

  return (
    <div className="border-b border-white/5">
      {/* Section header */}
      <div className="flex items-center gap-2 px-3 py-1.5 bg-zinc-950/30">
        <Globe size={11} className="text-muted-foreground" />
        <span className="text-[9px] font-black uppercase tracking-widest text-muted-foreground">
          Browser
        </span>
      </div>

      {/* Toolbar */}
      <div className="flex items-center gap-1 px-2 py-1.5 border-b border-white/5 bg-black/30">
        <button
          onClick={cu.takeScreenshot}
          disabled={cu.loading}
          className="px-2 py-1 text-[8px] font-mono uppercase tracking-widest bg-white/5 text-muted hover:text-zinc-300 hover:bg-white/10 disabled:opacity-30 transition-colors"
        >
          {cu.loading ? <Loader size={9} className="animate-spin" /> : 'Screenshot'}
        </button>
        <button
          onClick={cu.getSnapshot}
          disabled={cu.loading}
          className="px-2 py-1 text-[8px] font-mono uppercase tracking-widest bg-white/5 text-muted hover:text-zinc-300 hover:bg-white/10 disabled:opacity-30 transition-colors"
        >
          Elements
        </button>
        <div className="flex-1" />
        <button
          onClick={cu.disableComputerUse}
          className="px-2 py-1 text-[8px] font-mono uppercase tracking-widest text-red-400/50 hover:text-red-400 hover:bg-red-400/5 transition-colors"
        >
          Off
        </button>
      </div>

      {/* URL bar */}
      <form onSubmit={onNavigate} className="flex items-center gap-1 px-2 py-1 border-b border-white/5">
        <input
          type="text"
          value={urlInput}
          onChange={e => setUrlInput(e.target.value)}
          placeholder="URL..."
          className="flex-1 bg-zinc-900 border border-white/5 px-2 py-0.5 text-[9px] font-mono text-white placeholder-zinc-600 focus:outline-none focus:border-primary/40"
        />
        <button type="submit" className="px-2 py-0.5 bg-primary/20 text-primary text-[8px] font-mono uppercase tracking-widest hover:bg-primary/30 transition-colors">
          Go
        </button>
      </form>

      {/* Screenshot */}
      <div className="min-h-[100px] max-h-[280px] overflow-auto bg-zinc-950 flex items-center justify-center">
        {cu.screenshot ? (
          <img
            src={`data:image/png;base64,${cu.screenshot}`}
            alt="Browser"
            className="max-w-full max-h-full object-contain"
          />
        ) : (
          <div className="text-center text-zinc-700 py-6">
            <Globe size={18} className="mx-auto mb-1 opacity-20" />
            <p className="text-[9px] font-mono">Screenshot to capture</p>
          </div>
        )}
      </div>

      {/* Vision description */}
      {cu.description && (
        <div className="px-2 py-1 border-t border-white/5 bg-zinc-950/50 max-h-16 overflow-y-auto">
          <p className="text-[8px] font-mono text-muted-foreground leading-relaxed">{cu.description}</p>
        </div>
      )}

      {/* Element list */}
      {cu.elements.length > 0 && (
        <div className="border-t border-white/5 max-h-36 overflow-y-auto">
          <div className="px-2 py-0.5 text-[8px] font-black uppercase tracking-[0.2em] text-muted-foreground border-b border-white/5">
            Elements ({cu.elements.length})
          </div>
          {cu.elements.map(el => (
            <button
              key={el.id}
              onClick={() => onElementClick(el.id, el.tag)}
              className={cn(
                'w-full text-left px-2 py-0.5 flex items-center gap-1.5 hover:bg-white/5 transition-colors border-b border-white/[0.02]',
                selectedElement === el.id && 'bg-primary/10'
              )}
            >
              <span className="text-[7px] bg-red-600/80 text-white px-0.5 font-mono min-w-[12px] text-center">
                {el.id}
              </span>
              <span className="text-[8px] font-mono text-zinc-500">{el.tag}</span>
              {el.text && (
                <span className="text-[8px] font-mono text-zinc-400 truncate">{el.text}</span>
              )}
            </button>
          ))}
        </div>
      )}

      {/* Type input */}
      {selectedElement !== null && (
        <div className="flex items-center gap-1 px-2 py-1 border-t border-white/5 bg-black/50">
          <span className="text-[8px] font-mono text-muted-foreground">
            [{selectedElement}]
          </span>
          <input
            type="text"
            value={typeText}
            onChange={e => setTypeText(e.target.value)}
            onKeyDown={e => e.key === 'Enter' && onType()}
            placeholder="Type text..."
            className="flex-1 bg-zinc-900 border border-white/5 px-2 py-0.5 text-[9px] font-mono text-white focus:outline-none focus:border-primary/40"
            autoFocus
          />
          <button onClick={onType} className="px-2 py-0.5 bg-primary/20 text-primary text-[8px]">Send</button>
        </div>
      )}

      {/* Error */}
      {cu.error && (
        <div className="px-2 py-1 border-t border-red-900/30 text-[8px] font-mono text-red-400">
          {cu.error}
        </div>
      )}
    </div>
  );
}
