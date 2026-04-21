import React from 'react';
import { Globe, Loader } from 'lucide-react';
import { cn } from '../lib/utils';
import { useComputerUse } from '../hooks/useComputerUse';
import { Button } from './ui/button';
import { Input } from './ui/input';
import { ScrollArea } from './ui/scroll-area';

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
          <span className="text-xs font-black uppercase tracking-widest text-muted-foreground">
            Computer Use
          </span>
        </div>
        <Button
          variant="default"
          size="xs"
          onClick={onEnable}
          disabled={!sandboxId || cu.loading}
        >
          {cu.loading ? 'Starting...' : 'Enable Computer Use'}
        </Button>
      </div>
    );
  }

  return (
    <div className="border-b border-white/5">
      {/* Section header */}
      <div className="flex items-center gap-2 px-3 py-1.5 bg-zinc-950/30">
        <Globe size={11} className="text-muted-foreground" />
        <span className="text-xs font-black uppercase tracking-widest text-muted-foreground">
          Browser
        </span>
      </div>

      {/* Toolbar */}
      <div className="flex items-center gap-1 px-2 py-1.5 border-b border-white/5 bg-black/30">
        <Button
          variant="ghost"
          size="xs"
          onClick={cu.takeScreenshot}
          disabled={cu.loading}
        >
          {cu.loading ? <Loader size={9} className="animate-spin" /> : 'Screenshot'}
        </Button>
        <Button
          variant="ghost"
          size="xs"
          onClick={cu.getSnapshot}
          disabled={cu.loading}
        >
          Elements
        </Button>
        <div className="flex-1" />
        <Button
          variant="ghost"
          size="xs"
          onClick={cu.disableComputerUse}
          className="text-red-400/50 hover:text-red-400 hover:bg-red-400/5"
        >
          Off
        </Button>
      </div>

      {/* URL bar */}
      <form onSubmit={onNavigate} className="flex items-center gap-1 px-2 py-1 border-b border-white/5">
        <Input
          type="text"
          value={urlInput}
          onChange={e => setUrlInput(e.target.value)}
          placeholder="URL..."
          className="flex-1"
        />
        <Button variant="default" size="xs" type="submit">
          Go
        </Button>
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
            <p className="text-xs font-mono">Screenshot to capture</p>
          </div>
        )}
      </div>

      {/* Vision description */}
      {cu.description && (
        <div className="px-2 py-1 border-t border-white/5 bg-zinc-950/50 max-h-16 overflow-y-auto">
          <p className="text-xs font-mono text-muted-foreground leading-relaxed">{cu.description}</p>
        </div>
      )}

      {/* Element list */}
      {cu.elements.length > 0 && (
        <div className="border-t border-white/5">
          <div className="px-2 py-0.5 text-xs font-black uppercase tracking-[0.2em] text-muted-foreground border-b border-white/5">
            Elements ({cu.elements.length})
          </div>
          <ScrollArea className="max-h-36">
            {cu.elements.map(el => (
              <Button
                key={el.id}
                variant="ghost"
                size="xs"
                className={cn(
                  'w-full justify-start border-b border-white/[0.02]',
                  selectedElement === el.id && 'bg-primary/10'
                )}
                onClick={() => onElementClick(el.id, el.tag)}
              >
                <span className="text-[10px] bg-red-600/80 text-white px-0.5 font-mono min-w-[12px] text-center">
                  {el.id}
                </span>
                <span className="text-xs font-mono text-zinc-500">{el.tag}</span>
                {el.text && (
                  <span className="text-xs font-mono text-zinc-400 truncate">{el.text}</span>
                )}
              </Button>
            ))}
          </ScrollArea>
        </div>
      )}

      {/* Type input */}
      {selectedElement !== null && (
        <div className="flex items-center gap-1 px-2 py-1 border-t border-white/5 bg-black/50">
          <span className="text-xs font-mono text-muted-foreground">
            [{selectedElement}]
          </span>
          <Input
            type="text"
            value={typeText}
            onChange={e => setTypeText(e.target.value)}
            onKeyDown={e => e.key === 'Enter' && onType()}
            placeholder="Type text..."
            className="flex-1"
            autoFocus
          />
          <Button variant="default" size="xs" onClick={onType}>Send</Button>
        </div>
      )}

      {/* Error */}
      {cu.error && (
        <div className="px-2 py-1 border-t border-red-900/30 text-xs font-mono text-red-400">
          {cu.error}
        </div>
      )}
    </div>
  );
}
