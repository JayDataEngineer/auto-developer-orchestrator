import React, { useState, useEffect } from 'react';
import { Globe, FileText, CheckSquare, StickyNote, Loader } from 'lucide-react';
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

type Tab = 'browser' | 'plan' | 'todo' | 'notes';

const TABS: { id: Tab; label: string; icon: React.ReactNode }[] = [
  { id: 'browser', label: 'Browser', icon: <Globe size={11} /> },
  { id: 'plan', label: 'Plan', icon: <FileText size={11} /> },
  { id: 'todo', label: 'Todo', icon: <CheckSquare size={11} /> },
  { id: 'notes', label: 'Notes', icon: <StickyNote size={11} /> },
];

export function RightPanel({ agentId, sandboxId, artifacts, artifactsLoading }: RightPanelProps) {
  const [activeTab, setActiveTab] = useState<Tab>('browser');
  const cu = useComputerUse();
  const [urlInput, setUrlInput] = useState('https://');
  const [typeText, setTypeText] = useState('');
  const [selectedElement, setSelectedElement] = useState<number | null>(null);

  // Auto-select browser tab when computer use is enabled
  useEffect(() => {
    if (cu.enabled) setActiveTab('browser');
  }, [cu.enabled]);

  const hasPlan = artifacts.some(a => a.type === 'plan');
  const hasTodo = artifacts.some(a => a.type === 'todo');
  const hasNotes = artifacts.some(a => a.type === 'notes');

  const latestPlan = artifacts.filter(a => a.type === 'plan').pop();
  const latestTodo = artifacts.filter(a => a.type === 'todo').pop();
  const latestNotes = artifacts.filter(a => a.type === 'notes').pop();

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
      {/* Tab bar */}
      <div className="flex border-b border-white/5">
        {TABS.map(tab => {
          const hasContent = tab.id === 'browser'
            ? cu.enabled
            : tab.id === 'plan' ? hasPlan
            : tab.id === 'todo' ? hasTodo
            : hasNotes;

          return (
            <button
              key={tab.id}
              onClick={() => setActiveTab(tab.id)}
              className={cn(
                'flex-1 flex items-center justify-center gap-1.5 py-2.5 text-[9px] font-mono uppercase tracking-widest transition-colors relative',
                activeTab === tab.id
                  ? 'text-primary bg-primary/5'
                  : 'text-muted hover:text-muted-foreground hover:bg-white/5'
              )}
            >
              {tab.icon}
              {tab.label}
              {hasContent && (
                <div className="absolute top-1.5 right-[calc(50%-16px)] w-1 h-1 rounded-full bg-primary" />
              )}
            </button>
          );
        })}
      </div>

      {/* Tab content */}
      <div className="flex-1 overflow-y-auto custom-scrollbar">
        {activeTab === 'browser' && (
          <BrowserTab
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
        )}
        {activeTab === 'plan' && (
          <ArtifactTab
            artifact={latestPlan}
            loading={artifactsLoading}
            emptyMessage="No plan yet. The agent will create one when planning implementation."
          />
        )}
        {activeTab === 'todo' && (
          <ArtifactTab
            artifact={latestTodo}
            loading={artifactsLoading}
            emptyMessage="No todo list yet. The agent will create one when breaking down tasks."
          />
        )}
        {activeTab === 'notes' && (
          <ArtifactTab
            artifact={latestNotes}
            loading={artifactsLoading}
            emptyMessage="No notes yet. The agent will create notes for research findings."
          />
        )}
      </div>
    </div>
  );
}

// Browser tab content
function BrowserTab({ cu, sandboxId, urlInput, setUrlInput, typeText, setTypeText, selectedElement, setSelectedElement, onNavigate, onElementClick, onType, onEnable }: {
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
      <div className="flex flex-col items-center justify-center h-full p-6 text-center space-y-4">
        <Globe size={32} className="text-muted-foreground opacity-20" />
        <div className="space-y-2">
          <p className="text-[10px] font-mono text-muted-foreground">
            Computer Use is not enabled
          </p>
          <button
            onClick={onEnable}
            disabled={!sandboxId || cu.loading}
            className="px-4 py-2 bg-primary text-black text-[9px] font-black uppercase tracking-widest hover:bg-primary/80 disabled:opacity-30 transition-colors"
          >
            {cu.loading ? 'Starting...' : 'Enable Computer Use'}
          </button>
        </div>
      </div>
    );
  }

  return (
    <div className="flex flex-col h-full">
      {/* Toolbar */}
      <div className="flex items-center gap-1 px-2 py-1.5 border-b border-white/5 bg-black/50">
        <button
          onClick={cu.takeScreenshot}
          disabled={cu.loading}
          className="px-2 py-1 text-[9px] font-mono uppercase tracking-widest bg-white/5 text-muted hover:text-zinc-300 hover:bg-white/10 disabled:opacity-30 transition-colors"
        >
          {cu.loading ? <Loader size={10} className="animate-spin" /> : 'Screenshot'}
        </button>
        <button
          onClick={cu.getSnapshot}
          disabled={cu.loading}
          className="px-2 py-1 text-[9px] font-mono uppercase tracking-widest bg-white/5 text-muted hover:text-zinc-300 hover:bg-white/10 disabled:opacity-30 transition-colors"
        >
          Elements
        </button>
        <div className="flex-1" />
        <button
          onClick={cu.disableComputerUse}
          className="px-2 py-1 text-[9px] font-mono uppercase tracking-widest text-red-400/50 hover:text-red-400 hover:bg-red-400/5 transition-colors"
        >
          Disable
        </button>
      </div>

      {/* URL bar */}
      <form onSubmit={onNavigate} className="flex items-center gap-1 px-2 py-1 border-b border-white/5">
        <input
          type="text"
          value={urlInput}
          onChange={e => setUrlInput(e.target.value)}
          placeholder="URL..."
          className="flex-1 bg-zinc-900 border border-white/5 px-2 py-1 text-[10px] font-mono text-white placeholder-zinc-600 focus:outline-none focus:border-primary/40"
        />
        <button type="submit" className="px-2 py-1 bg-primary/20 text-primary text-[9px] font-mono uppercase tracking-widest hover:bg-primary/30 transition-colors">
          Go
        </button>
      </form>

      {/* Screenshot */}
      <div className="flex-1 min-h-0 overflow-auto bg-zinc-950 flex items-center justify-center">
        {cu.screenshot ? (
          <img
            src={`data:image/png;base64,${cu.screenshot}`}
            alt="Browser"
            className="max-w-full max-h-full object-contain"
          />
        ) : (
          <div className="text-center text-zinc-600">
            <Globe size={24} className="mx-auto mb-2 opacity-20" />
            <p className="text-[10px] font-mono">Click Screenshot to capture</p>
          </div>
        )}
      </div>

      {/* Vision description */}
      {cu.description && (
        <div className="px-2 py-1.5 border-t border-white/5 bg-zinc-950 max-h-20 overflow-y-auto">
          <p className="text-[9px] font-mono text-muted-foreground leading-relaxed">{cu.description}</p>
        </div>
      )}

      {/* Element list */}
      {cu.elements.length > 0 && (
        <div className="border-t border-white/5 max-h-40 overflow-y-auto">
          <div className="px-2 py-1 text-[8px] font-black uppercase tracking-[0.2em] text-muted-foreground border-b border-white/5">
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
              <span className="text-[8px] bg-red-600/80 text-white px-0.5 font-mono min-w-[14px] text-center">
                {el.id}
              </span>
              <span className="text-[9px] font-mono text-zinc-500">{el.tag}</span>
              {el.text && (
                <span className="text-[9px] font-mono text-zinc-400 truncate">{el.text}</span>
              )}
            </button>
          ))}
        </div>
      )}

      {/* Type input */}
      {selectedElement !== null && (
        <div className="flex items-center gap-1 px-2 py-1 border-t border-white/5 bg-black/50">
          <span className="text-[9px] font-mono text-muted-foreground">
            [{selectedElement}]
          </span>
          <input
            type="text"
            value={typeText}
            onChange={e => setTypeText(e.target.value)}
            onKeyDown={e => e.key === 'Enter' && onType()}
            placeholder="Type text..."
            className="flex-1 bg-zinc-900 border border-white/5 px-2 py-0.5 text-[10px] font-mono text-white focus:outline-none focus:border-primary/40"
            autoFocus
          />
          <button onClick={onType} className="px-2 py-0.5 bg-primary/20 text-primary text-[9px]">Send</button>
        </div>
      )}

      {/* Error */}
      {cu.error && (
        <div className="px-2 py-1 border-t border-red-900/30 text-[9px] font-mono text-red-400">
          {cu.error}
        </div>
      )}
    </div>
  );
}

// Artifact tab content
function ArtifactTab({ artifact, loading, emptyMessage }: {
  artifact?: Artifact;
  loading: boolean;
  emptyMessage: string;
}) {
  if (loading && !artifact) {
    return (
      <div className="flex items-center justify-center h-full">
        <Loader size={14} className="animate-spin text-muted-foreground" />
      </div>
    );
  }

  if (!artifact) {
    return (
      <div className="flex flex-col items-center justify-center h-full p-6 text-center">
        <p className="text-[10px] font-mono text-muted-foreground opacity-50">{emptyMessage}</p>
      </div>
    );
  }

  return (
    <div className="p-4">
      <div className="mb-3">
        <h3 className="text-[10px] font-black uppercase tracking-widest text-white">{artifact.title}</h3>
      </div>
      <ArtifactView artifact={artifact} />
    </div>
  );
}
