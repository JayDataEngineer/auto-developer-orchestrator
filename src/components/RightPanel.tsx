import React, { useState, useEffect, useCallback } from 'react';
import {
  FileText, CheckSquare, StickyNote, Loader, Monitor,
  ChevronLeft, ChevronRight
} from 'lucide-react';
import { cn } from '../lib/utils';
import { Artifact } from '../lib/api';
import { useComputerUse } from '../hooks/useComputerUse';
import { ArtifactView } from './ArtifactView';
import { BrowserTools } from './BrowserTools';
import { SectionHeader } from './ui/SectionHeader';

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
      <SectionHeader
        label="Artifacts"
        action={
          cu.enabled ? (
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
          ) : undefined
        }
      />

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

