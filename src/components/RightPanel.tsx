import React, { useState, useEffect } from 'react';
import {
  FileText, CheckSquare, StickyNote, Loader, Monitor,
  Settings, Wrench, Brain, Github, Globe, Key, Check, Eye, EyeOff
} from 'lucide-react';
import { cn } from '../lib/utils';
import { Artifact, api } from '../lib/api';
import { ToolCall } from '../lib/pi-events';
import { ArtifactView } from './ArtifactView';
import { BrowserTools } from './BrowserTools';
import { useComputerUse } from '../hooks/useComputerUse';
import { Button } from './ui/button';
import { Tooltip, TooltipContent, TooltipTrigger } from './ui/tooltip';
import { ScrollArea, ScrollBar } from './ui/scroll-area';
import { Separator } from './ui/separator';

interface StreamingState {
  isStreaming: boolean;
  runningTool: ToolCall | undefined;
  thinking: string;
}

type CUState = ReturnType<typeof useComputerUse>;

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
    <div className="h-full flex flex-col bg-background">
      {/* Top bar: artifact tabs + action buttons */}
      <div className="h-10 border-b border-border flex items-center px-1 shrink-0 bg-background/50 gap-0.5">
        {/* Artifact icon tabs — horizontal, dynamic */}
        <ScrollArea className="flex-1 min-w-0">
          <div className="flex items-center gap-0.5">
            {artifacts.map(a => (
              <Button
                key={a.id}
                variant="ghost"
                size="xs"
                onClick={() => { setSelectedArtifactId(a.id); setShowSettings(false); setShowBrowser(false); }}
                title={`${typeLabels[a.type]}: ${a.title}`}
                className={cn(
                  'shrink-0',
                  selectedArtifact?.id === a.id && !showSettings && !showBrowser
                    ? 'text-primary bg-primary/10'
                    : 'text-muted-foreground hover:bg-muted'
                )}
              >
                {typeIcons[a.type]}
                <span className="truncate max-w-[80px]">{a.title}</span>
              </Button>
            ))}
            {artifacts.length === 0 && !artifactsLoading && (
              <span className="text-xs text-muted-foreground px-2">No artifacts yet</span>
            )}
            {artifactsLoading && (
              <Loader size={10} className="text-muted-foreground animate-spin mx-2" />
            )}
          </div>
          <ScrollBar orientation="horizontal" />
        </ScrollArea>

        {/* Browser toggle */}
        <Tooltip>
          <TooltipTrigger asChild>
            <Button
              variant="ghost"
              size="icon-xs"
              onClick={() => { setShowBrowser(!showBrowser); setShowSettings(false); }}
              className={cn(
                showBrowser
                  ? 'text-primary bg-primary/10'
                  : 'text-muted-foreground hover:bg-muted'
              )}
            >
              <Globe size={12} />
            </Button>
          </TooltipTrigger>
          <TooltipContent>Browser / Computer Use</TooltipContent>
        </Tooltip>

        <Separator orientation="vertical" className="h-3" />

        {/* Settings toggle */}
        <Tooltip>
          <TooltipTrigger asChild>
            <Button
              variant="ghost"
              size="icon-xs"
              onClick={() => { setShowSettings(!showSettings); setShowBrowser(false); }}
              className={cn(
                showSettings
                  ? 'text-primary bg-primary/10'
                  : 'text-muted-foreground hover:bg-muted'
              )}
            >
              <Settings size={12} />
            </Button>
          </TooltipTrigger>
          <TooltipContent>Settings</TooltipContent>
        </Tooltip>
      </div>

      {/* Live streaming status */}
      {streamingState?.isStreaming && (
        <div className="border-b border-primary/20 bg-primary/5 px-3 py-2">
          <div className="flex items-center gap-2">
            <div className="w-1.5 h-1.5 rounded-full bg-primary animate-pulse" />
            <span className="text-xs font-black text-primary uppercase tracking-widest">
              Agent Active
            </span>
          </div>
          {streamingState.runningTool && (
            <div className="flex items-center gap-1.5 mt-1.5">
              <Wrench size={9} className="text-primary/70" />
              <span className="text-xs font-mono text-primary/80">
                Running: {streamingState.runningTool.name}
              </span>
              <span className="text-xs font-mono text-muted-foreground truncate">
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
              <Brain size={9} className="text-muted-foreground" />
              <span className="text-xs font-mono text-muted-foreground truncate">
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
            <div className="text-xs font-black uppercase tracking-widest text-muted-foreground mb-3">
              Settings
            </div>

            {/* Computer Use */}
            <div className="border border-border p-3 space-y-2">
              <div className="flex items-center gap-2">
                <Monitor size={12} className="text-muted-foreground" />
                <span className="text-sm font-bold uppercase tracking-widest text-foreground">
                  Computer Use
                </span>
              </div>
              <p className="text-xs font-mono text-muted-foreground leading-relaxed">
                Enable browser automation for the sandbox. Allows the agent to take screenshots, click elements, and navigate pages.
              </p>
              {cu.enabled ? (
                <div className="flex items-center gap-2">
                  <span className="text-xs font-mono text-primary">Enabled</span>
                  <Button
                    variant="destructive"
                    size="xs"
                    onClick={() => {
                      cu.disableComputerUse();
                    }}
                  >
                    Disable
                  </Button>
                  {sandboxId && (
                    <Button
                      variant="outline"
                      size="xs"
                      onClick={() => {
                        window.open(`/api/sandbox/vnc/${sandboxId}/vnc.html`, '_blank', 'width=1280,height=720');
                      }}
                    >
                      Open Desktop
                    </Button>
                  )}
                </div>
              ) : (
                <Button
                  variant="default"
                  size="xs"
                  onClick={() => { if (sandboxId) cu.enableComputerUse(sandboxId); }}
                  disabled={!sandboxId || cu.loading}
                >
                  {cu.loading ? 'Starting...' : 'Enable Computer Use'}
                </Button>
              )}
              {cu.error && (
                <p className="text-xs font-mono text-red-400">{cu.error}</p>
              )}
            </div>

            {/* GitHub connection link */}
            <div className="border border-border p-3 space-y-2">
              <div className="flex items-center gap-2">
                <Github size={12} className="text-muted-foreground" />
                <span className="text-sm font-bold uppercase tracking-widest text-foreground">
                  GitHub
                </span>
              </div>
              <p className="text-xs font-mono text-muted-foreground leading-relaxed">
                Connect your GitHub account to enable PR creation, branch management, and repository cloning.
              </p>
              <Button
                variant="outline"
                size="xs"
                onClick={() => {
                  // Trigger the GitHub modal in AppShell via custom event
                  window.dispatchEvent(new CustomEvent('open-github-settings'));
                }}
              >
                Configure GitHub
              </Button>
            </div>

            {/* AI Providers */}
            <div className="border border-border p-3 space-y-2">
              <div className="flex items-center gap-2">
                <Brain size={12} className="text-muted-foreground" />
                <span className="text-sm font-bold text-foreground">
                  AI Providers
                </span>
              </div>
              <p className="text-xs font-mono text-muted-foreground leading-relaxed">
                Add API keys to use cloud models alongside your local model. Keys are stored locally on this machine.
              </p>
              <ProviderSettings />
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
                <span className="text-sm font-bold text-zinc-300 uppercase tracking-widest">
                  {typeLabels[selectedArtifact.type]}
                </span>
                <span className="text-xs font-mono text-muted-foreground">
                  {selectedArtifact.title}
                </span>
              </div>
              <ArtifactView artifact={selectedArtifact} />
            </div>
          ) : (
            <div className="flex flex-col items-center justify-center h-40 text-muted-foreground/50">
              <FileText size={20} className="mb-2 opacity-20" />
              <p className="text-xs font-mono uppercase tracking-widest">
                {artifactsLoading ? 'Loading...' : 'No artifacts yet'}
              </p>
              <p className="text-xs font-mono text-muted-foreground/30 mt-1">
                Artifacts appear here as the agent works
              </p>
            </div>
          )
        )}
      </div>
    </div>
  );
}

// ── Provider Settings sub-component ─────────────────────────────────────

interface ProviderInfo {
  id: string;
  name: string;
  baseUrl: string;
  hasKey: boolean;
  models: Array<{ id: string; name: string }>;
}

function ProviderSettings() {
  const [providers, setProviders] = useState<ProviderInfo[]>([]);
  const [expandedId, setExpandedId] = useState<string | null>(null);
  const [keyInput, setKeyInput] = useState('');
  const [showKey, setShowKey] = useState(false);
  const [saving, setSaving] = useState(false);
  const [savedId, setSavedId] = useState<string | null>(null);

  useEffect(() => {
    api.config.getProviders().then(data => {
      setProviders(data.providers || []);
    }).catch(() => {});
  }, []);

  const handleSave = async (providerId: string) => {
    if (!keyInput.trim()) return;
    setSaving(true);
    try {
      await api.config.setProviderKey(providerId, keyInput.trim());
      setProviders(prev => prev.map(p =>
        p.id === providerId ? { ...p, hasKey: true } : p
      ));
      setKeyInput('');
      setShowKey(false);
      setSavedId(providerId);
      setTimeout(() => setSavedId(null), 2000);
    } catch {
      // ignore
    } finally {
      setSaving(false);
    }
  };

  return (
    <div className="space-y-2">
      {providers.map(provider => (
        <div key={provider.id} className="border border-border rounded p-2 space-y-1.5">
          <button
            className="w-full flex items-center gap-2 text-left"
            onClick={() => setExpandedId(expandedId === provider.id ? null : provider.id)}
          >
            <Key size={10} className={provider.hasKey ? 'text-green-500' : 'text-muted-foreground'} />
            <span className="text-xs font-semibold text-foreground flex-1">{provider.name}</span>
            {provider.hasKey && (
              <span className="text-[10px] font-mono text-green-500">connected</span>
            )}
          </button>

          {expandedId === provider.id && (
            <div className="space-y-1.5 pt-1">
              <div className="text-[10px] font-mono text-muted-foreground">
                Models: {provider.models.map(m => m.name).join(', ')}
              </div>
              <div className="flex gap-1">
                <div className="flex-1 relative">
                  <input
                    type={showKey ? 'text' : 'password'}
                    value={keyInput}
                    onChange={e => setKeyInput(e.target.value)}
                    placeholder={provider.hasKey ? 'Update API key...' : 'Enter API key...'}
                    className="w-full bg-muted border border-border rounded px-2 py-1 text-xs font-mono text-foreground placeholder:text-muted-foreground/50 pr-7"
                  />
                  <button
                    type="button"
                    onClick={() => setShowKey(!showKey)}
                    className="absolute right-1.5 top-1/2 -translate-y-1/2 text-muted-foreground hover:text-foreground"
                  >
                    {showKey ? <EyeOff size={10} /> : <Eye size={10} />}
                  </button>
                </div>
                <Button
                  variant="default"
                  size="xs"
                  onClick={() => handleSave(provider.id)}
                  disabled={!keyInput.trim() || saving}
                >
                  {savedId === provider.id ? <Check size={10} /> : saving ? <Loader size={10} className="animate-spin" /> : 'Save'}
                </Button>
              </div>
            </div>
          )}
        </div>
      ))}
      {providers.length === 0 && (
        <p className="text-[10px] font-mono text-muted-foreground text-center">Loading providers...</p>
      )}
    </div>
  );
}
