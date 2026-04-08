import React, { useState, useEffect, useCallback } from 'react';
import {
  Globe, FileText, CheckSquare, StickyNote, Loader, Monitor,
  ChevronLeft, ChevronRight, ExternalLink
} from 'lucide-react';
import { cn } from '../lib/utils';
import { Artifact } from '../lib/api';
import { useComputerUse } from '../hooks/useComputerUse';
import { ArtifactView } from './ArtifactView';

interface RightPanelProps {
  agentId: string | null;
  sandboxId: string | null;
  artifacts: Artifact[];
  artifactsLoading: boolean;
}

// Artifact section with navigation
interface ArtifactSectionProps {
  title: string;
  icon: React.ReactNode;
  items: Artifact[];
  currentIndex: number;
  onNavigate: (dir: -1 | 1) => void;
  loading: boolean;
  emptyMessage: string;
}

function ArtifactSection({ title, icon, items, currentIndex, onNavigate, loading, emptyMessage }: ArtifactSectionProps) {
  const current = items[currentIndex];

  return (
    <div className="border-b border-white/5">
      {/* Section header with navigation */}
      <div className="flex items-center gap-2 px-3 py-1.5 bg-zinc-950/30">
        <span className="text-muted-foreground">{icon}</span>
        <span className="text-[9px] font-black uppercase tracking-widest text-muted-foreground">
          {title}
        </span>
        {items.length > 1 && (
          <>
            <div className="flex-1" />
            <button
              onClick={() => onNavigate(-1)}
              disabled={currentIndex === 0}
              className="p-0.5 text-zinc-500 hover:text-zinc-300 disabled:opacity-20 disabled:hover:text-zinc-500 transition-colors"
            >
              <ChevronLeft size={12} />
            </button>
            <span className="text-[9px] font-mono text-muted-foreground min-w-[24px] text-center">
              {currentIndex + 1}/{items.length}
            </span>
            <button
              onClick={() => onNavigate(1)}
              disabled={currentIndex === items.length - 1}
              className="p-0.5 text-zinc-500 hover:text-zinc-300 disabled:opacity-20 disabled:hover:text-zinc-500 transition-colors"
            >
              <ChevronRight size={12} />
            </button>
          </>
        )}
      </div>

      {/* Content */}
      <div className="max-h-72 overflow-y-auto">
        {loading && !current ? (
          <div className="flex items-center justify-center py-8">
            <Loader size={14} className="animate-spin text-muted-foreground" />
          </div>
        ) : current ? (
          <div className="p-3">
            <h4 className="text-[10px] font-medium text-zinc-300 mb-2">{current.title}</h4>
            <ArtifactView artifact={current} />
          </div>
        ) : (
          <div className="p-6 text-center">
            <p className="text-[10px] font-mono text-muted-foreground opacity-50">{emptyMessage}</p>
          </div>
        )}
      </div>
    </div>
  );
}

export function RightPanel({ agentId, sandboxId: passedSandboxId, artifacts, artifactsLoading }: RightPanelProps) {
  const cu = useComputerUse();
  const [urlInput, setUrlInput] = useState('https://');
  const [typeText, setTypeText] = useState('');
  const [selectedElement, setSelectedElement] = useState<number | null>(null);

  // Artifact navigation indices
  const [artifactIdx, setArtifactIdx] = useState({ plan: 0, todo: 0, notes: 0 });

  const sandboxId = passedSandboxId
    || (agentId ? `sandbox-${agentId.split(':')[0]}` : null);

  // Group artifacts by type (in order of creation)
  const plans = artifacts.filter(a => a.type === 'plan');
  const todos = artifacts.filter(a => a.type === 'todo');
  const notes = artifacts.filter(a => a.type === 'notes');

  // Clamp indices when artifacts change
  useEffect(() => {
    setArtifactIdx(prev => ({
      plan: Math.min(prev.plan, Math.max(0, plans.length - 1)),
      todo: Math.min(prev.todo, Math.max(0, todos.length - 1)),
      notes: Math.min(prev.notes, Math.max(0, notes.length - 1)),
    }));
  }, [plans.length, todos.length, notes.length]);

  const navigateArtifact = useCallback((type: 'plan' | 'todo' | 'notes', dir: -1 | 1) => {
    setArtifactIdx(prev => {
      const items = type === 'plan' ? plans : type === 'todo' ? todos : notes;
      return {
        ...prev,
        [type]: Math.max(0, Math.min(items.length - 1, prev[type] + dir)),
      };
    });
  }, [plans.length, todos.length, notes.length]);

  const handleEnableCU = () => {
    if (sandboxId) cu.enableComputerUse(sandboxId);
  };

  const handleNavigate = (e: React.FormEvent) => {
    e.preventDefault();
    if (urlInput && urlInput !== 'https://') {
      cu.navigate(urlInput);
    }
  };

  const handleElementClick = (elId: number, tag: string) => {
    const isInput = ['input', 'textarea', 'select'].includes(tag);
    if (isInput) {
      setSelectedElement(elId);
    } else {
      cu.clickElement(elId);
      setSelectedElement(null);
    }
  };

  const handleType = () => {
    if (selectedElement !== null && typeText) {
      cu.typeText(selectedElement, typeText, true);
      setTypeText('');
      setSelectedElement(null);
    }
  };

  return (
    <div className="w-96 border-l border-white/5 flex flex-col bg-black shrink-0">
      {/* Header */}
      <div className="px-3 py-2 border-b border-white/5 flex items-center gap-2">
        <span className="text-[9px] font-black uppercase tracking-[0.2em] text-muted-foreground">
          Artifacts
        </span>
        <div className="flex-1" />
        {cu.enabled && (
          <button
            onClick={() => {
              if (sandboxId) {
                window.open(`/api/sandbox/vnc/${sandboxId}/vnc.html`, '_blank', 'width=1280,height=720');
              }
            }}
            disabled={!sandboxId}
            className="flex items-center gap-1 text-[8px] font-mono text-muted hover:text-zinc-300 transition-colors"
          >
            <Monitor size={9} /> Desktop
          </button>
        )}
      </div>

      {/* Scrollable content */}
      <div className="flex-1 overflow-y-auto custom-scrollbar">
        {/* Plans */}
        <ArtifactSection
          title="Plans"
          icon={<FileText size={11} />}
          items={plans}
          currentIndex={artifactIdx.plan}
          onNavigate={(dir) => navigateArtifact('plan', dir)}
          loading={artifactsLoading && plans.length === 0}
          emptyMessage="No plan yet. The agent will create one when planning implementation."
        />

        {/* Todos */}
        <ArtifactSection
          title="Todos"
          icon={<CheckSquare size={11} />}
          items={todos}
          currentIndex={artifactIdx.todo}
          onNavigate={(dir) => navigateArtifact('todo', dir)}
          loading={artifactsLoading && todos.length === 0}
          emptyMessage="No todo list yet. The agent will create one when breaking down tasks."
        />

        {/* Notes */}
        <ArtifactSection
          title="Notes"
          icon={<StickyNote size={11} />}
          items={notes}
          currentIndex={artifactIdx.notes}
          onNavigate={(dir) => navigateArtifact('notes', dir)}
          loading={artifactsLoading && notes.length === 0}
          emptyMessage="No notes yet. The agent will create notes for research findings."
        />

        {/* Browser / Computer Use tools */}
        <BrowserTools
          cu={cu}
          sandboxId={sandboxId}
          urlInput={urlInput}
          setUrlInput={setUrlInput}
          typeText={typeText}
          setTypeText={setTypeText}
          selectedElement={selectedElement}
          setSelectedElement={setSelectedElement}
          onNavigate={handleNavigate}
          onElementClick={handleElementClick}
          onType={handleType}
          onEnable={handleEnableCU}
        />
      </div>
    </div>
  );
}

// Browser tools section (always visible at bottom)
function BrowserTools({ cu, sandboxId, urlInput, setUrlInput, typeText, setTypeText, selectedElement, setSelectedElement, onNavigate, onElementClick, onType, onEnable }: {
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

