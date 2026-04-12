import React, { useState, useEffect } from 'react';
import {
  FileText, CheckSquare, StickyNote, Loader, Monitor,
  Settings, Wrench, Brain, Github, Globe
} from 'lucide-react';
import { cn } from '../lib/utils';
import { Artifact } from '../lib/api';
import { ToolCall } from '../lib/pi-events';
import { ArtifactView } from './ArtifactView';
import { BrowserTools } from './BrowserTools';

interface StreamingState {
  isStreaming: boolean;
  runningTool: ToolCall | undefined;
  thinking: string;
}

// Match the shape returned by useComputerUse
interface CUState {
  enabled: boolean;
  loading: boolean;
  error: string | null;
  sandboxId: string | null;
  screenshot: string | null;
  description: string | null;
  elements: any[];
  url: string;
  title: string;
  cdpPort: number | null;
  enableComputerUse: (sandboxId: string) => Promise<void>;
  disableComputerUse: () => Promise<void>;
  takeScreenshot: () => Promise<void>;
  getSnapshot: () => Promise<void>;
  act: (action: any) => Promise<void>;
  navigate: (url: string) => void;
  clickElement: (element: number) => void;
  typeText: (element: number, text: string, submit?: boolean) => void;
  scroll: (direction: string, amount?: number) => void;
}

interface RightPanelProps {
  agentId: string | null;
  sandboxId: string | null;
  artifacts: Artifact[];
  artifactsLoading: boolean;
  streamingState?: StreamingState;
  cu: CUState;
  showSettings?: boolean;
  onShowSettingsChange?: (show: boolean) => void;
}

// Icon map for artifact types
const typeIcons: Record<Artifact['type'], React.ReactNode> = {
  plan: <FileText size={11} />,
  todo: <CheckSquare size={11} />,
  notes: <StickyNote size={11} />,
};

const typeLabels: Record<Artifact['type'], string> = {
  plan: 'Plan',
  todo: 'Todo',
  notes: 'Notes',
};

export function RightPanel({ agentId, sandboxId, artifacts, artifactsLoading, streamingState, cu, showSettings: showSettingsProp, onShowSettingsChange }: RightPanelProps) {
  const [urlInput, setUrlInput] = useState('https://');
  const [typeText, setTypeText] = useState('');
  const [selectedElement, setSelectedElement] = useState<number | null>(null);

  // Selected artifact for detail view (null = nothing selected)
  const [selectedArtifactId, setSelectedArtifactId] = useState<string | null>(null);
  const [showBrowser, setShowBrowser] = useState(false);

  // Use prop-controlled settings state (from AppShell) or internal fallback
  const showSettings = showSettingsProp ?? false;
  const setShowSettings = onShowSettingsChange ?? (() => {});

  const selectedArtifact = artifacts.find(a => a.id === selectedArtifactId) || null;

  // Auto-select first artifact when list changes
  useEffect(() => {
    if (artifacts.length > 0 && !selectedArtifactId) {
      setSelectedArtifactId(artifacts[0].id);
    }
  }, [artifacts, selectedArtifactId]);

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
    <div className="h-full flex flex-col bg-black">
      {/* Top bar: artifact tabs + action buttons */}
      <div className="h-10 border-b border-white/5 flex items-center px-1 shrink-0 bg-black/50 gap-0.5">
        {/* Artifact icon tabs — horizontal, dynamic */}
        <div className="flex items-center gap-0.5 overflow-x-auto custom-scrollbar">
          {artifacts.map(a => (
            <button
              key={a.id}
              onClick={() => { setSelectedArtifactId(a.id); setShowSettings(false); setShowBrowser(false); }}
              title={`${typeLabels[a.type]}: ${a.title}`}
              className={cn(
                'flex items-center gap-1 px-2 py-1 text-[9px] font-mono uppercase tracking-widest transition-colors rounded shrink-0',
                selectedArtifact?.id === a.id && !showSettings && !showBrowser
                  ? 'text-primary bg-primary/10'
                  : 'text-muted hover:text-muted-foreground hover:bg-white/5'
              )}
            >
              {typeIcons[a.type]}
              <span className="truncate max-w-[80px]">{a.title}</span>
            </button>
          ))}
          {artifacts.length === 0 && !artifactsLoading && (
            <span className="text-[9px] font-mono text-zinc-700 px-2">No artifacts yet</span>
          )}
          {artifactsLoading && (
            <Loader size={10} className="text-muted-foreground animate-spin mx-2" />
          )}
        </div>

        <div className="flex-1" />

        {/* Browser toggle */}
        <button
          onClick={() => { setShowBrowser(!showBrowser); setShowSettings(false); }}
          className={cn(
            'p-1.5 transition-colors rounded',
            showBrowser
              ? 'text-primary bg-primary/10'
              : 'text-muted hover:text-muted-foreground hover:bg-white/5'
          )}
          title="Browser / Computer Use"
        >
          <Globe size={12} />
        </button>

        {/* Settings toggle */}
        <button
          onClick={() => { setShowSettings(!showSettings); setShowBrowser(false); }}
          className={cn(
            'p-1.5 transition-colors rounded',
            showSettings
              ? 'text-primary bg-primary/10'
              : 'text-muted hover:text-muted-foreground hover:bg-white/5'
          )}
          title="Settings"
        >
          <Settings size={12} />
        </button>
      </div>

      {/* Live streaming status */}
      {streamingState?.isStreaming && (
        <div className="border-b border-primary/20 bg-primary/5 px-3 py-2">
          <div className="flex items-center gap-2">
            <div className="w-1.5 h-1.5 rounded-full bg-primary animate-pulse" />
            <span className="text-[9px] font-black text-primary uppercase tracking-widest">
              Agent Active
            </span>
          </div>
          {streamingState.runningTool && (
            <div className="flex items-center gap-1.5 mt-1.5">
              <Wrench size={9} className="text-primary/70" />
              <span className="text-[9px] font-mono text-primary/80">
                Running: {streamingState.runningTool.name}
              </span>
              <span className="text-[8px] font-mono text-zinc-600 truncate">
                {streamingState.runningTool.name === 'bash' && typeof streamingState.runningTool.args?.command === 'string'
                  ? String(streamingState.runningTool.args.command).slice(0, 40)
                  : streamingState.runningTool.name === 'read' || streamingState.runningTool.name === 'write' || streamingState.runningTool.name === 'edit'
                    ? String(streamingState.runningTool.args?.filePath || streamingState.runningTool.args?.path || '')
                    : ''}
              </span>
            </div>
          )}
          {streamingState.thinking && (
            <div className="flex items-center gap-1.5 mt-1.5">
              <Brain size={9} className="text-zinc-500" />
              <span className="text-[9px] font-mono text-zinc-500 truncate">
                Thinking... ({streamingState.thinking.length} chars)
              </span>
            </div>
          )}
        </div>
      )}

      {/* Content area */}
      <div className="flex-1 overflow-y-auto custom-scrollbar">
        {/* Settings panel */}
        {showSettings && (
          <div className="p-4 space-y-4">
            <div className="text-[9px] font-black uppercase tracking-widest text-muted-foreground mb-3">
              Settings
            </div>

            {/* Computer Use */}
            <div className="border border-white/5 p-3 space-y-2">
              <div className="flex items-center gap-2">
                <Monitor size={12} className="text-muted-foreground" />
                <span className="text-[10px] font-bold uppercase tracking-widest text-zinc-300">
                  Computer Use
                </span>
              </div>
              <p className="text-[9px] font-mono text-muted-foreground leading-relaxed">
                Enable browser automation for the sandbox. Allows the agent to take screenshots, click elements, and navigate pages.
              </p>
              {cu.enabled ? (
                <div className="flex items-center gap-2">
                  <span className="text-[9px] font-mono text-primary">Enabled</span>
                  <button
                    onClick={() => {
                      cu.disableComputerUse();
                    }}
                    className="px-3 py-1 text-[8px] font-mono uppercase tracking-widest text-red-400/70 hover:text-red-400 border border-red-400/20 hover:border-red-400/40 transition-colors"
                  >
                    Disable
                  </button>
                  {sandboxId && (
                    <button
                      onClick={() => {
                        window.open(`/api/sandbox/vnc/${sandboxId}/vnc.html`, '_blank', 'width=1280,height=720');
                      }}
                      className="px-3 py-1 text-[8px] font-mono uppercase tracking-widest text-primary border border-primary/20 hover:border-primary/40 transition-colors"
                    >
                      Open Desktop
                    </button>
                  )}
                </div>
              ) : (
                <button
                  onClick={handleEnableCU}
                  disabled={!sandboxId || cu.loading}
                  className="px-3 py-1.5 bg-primary text-black text-[9px] font-black uppercase tracking-widest hover:bg-primary/80 disabled:opacity-30 transition-colors"
                >
                  {cu.loading ? 'Starting...' : 'Enable Computer Use'}
                </button>
              )}
              {cu.error && (
                <p className="text-[8px] font-mono text-red-400">{cu.error}</p>
              )}
            </div>

            {/* GitHub connection link */}
            <div className="border border-white/5 p-3 space-y-2">
              <div className="flex items-center gap-2">
                <Github size={12} className="text-muted-foreground" />
                <span className="text-[10px] font-bold uppercase tracking-widest text-zinc-300">
                  GitHub
                </span>
              </div>
              <p className="text-[9px] font-mono text-muted-foreground leading-relaxed">
                Connect your GitHub account to enable PR creation, branch management, and repository cloning.
              </p>
              <button
                onClick={() => {
                  // Trigger the GitHub modal in AppShell via custom event
                  window.dispatchEvent(new CustomEvent('open-github-settings'));
                }}
                className="px-3 py-1.5 text-[9px] font-black uppercase tracking-widest text-primary border border-primary/20 hover:border-primary/40 hover:bg-primary/5 transition-colors"
              >
                Configure GitHub
              </button>
            </div>
          </div>
        )}

        {/* Browser panel */}
        {showBrowser && !showSettings && (
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
            onEnable={() => { if (sandboxId) cu.enableComputerUse(sandboxId); }}
          />
        )}

        {/* Artifact detail view */}
        {!showSettings && !showBrowser && (
          selectedArtifact ? (
            <div className="p-3">
              <div className="flex items-center gap-2 mb-3">
                <span className="text-muted-foreground">{typeIcons[selectedArtifact.type]}</span>
                <span className="text-[10px] font-bold text-zinc-300 uppercase tracking-widest">
                  {typeLabels[selectedArtifact.type]}
                </span>
                <span className="text-[9px] font-mono text-zinc-600">
                  {selectedArtifact.title}
                </span>
              </div>
              <ArtifactView artifact={selectedArtifact} />
            </div>
          ) : (
            <div className="flex flex-col items-center justify-center h-40 text-zinc-700">
              <FileText size={20} className="mb-2 opacity-20" />
              <p className="text-[9px] font-mono uppercase tracking-widest">
                {artifactsLoading ? 'Loading...' : 'No artifacts yet'}
              </p>
              <p className="text-[8px] font-mono text-zinc-800 mt-1">
                Artifacts appear here as the agent works
              </p>
            </div>
          )
        )}
      </div>
    </div>
  );
}
