import React, { useState, useEffect, useCallback } from 'react';
import {
  FileText, CheckSquare, StickyNote, Loader, Monitor,
  Settings, Wrench, Brain, Github, Globe, Key, Check, Eye, EyeOff,
  Sun, Moon, MonitorSmartphone, Sliders, RotateCcw
} from 'lucide-react';
import { cn } from '../lib/utils';
import { Artifact, api, AgentConfig } from '../lib/api';
import { ToolCall } from '../lib/pux-events';
import { ArtifactView } from './ArtifactView';
import { BrowserTools } from './BrowserTools';
import { useComputerUse } from '../hooks/useComputerUse';
import { useTheme, Theme } from '../hooks/useTheme';
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
            <span className="text-xs font-semibold text-primary">
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
            <div className="text-xs font-semibold text-muted-foreground mb-3">
              Settings
            </div>

            {/* Computer Use */}
            <div className="border border-border p-3 space-y-2">
              <div className="flex items-center gap-2">
                <Monitor size={12} className="text-muted-foreground" />
                <span className="text-sm font-semibold text-foreground">
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
                <span className="text-sm font-semibold text-foreground">
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
                <span className="text-sm font-semibold text-foreground">
                  AI Providers
                </span>
              </div>
              <p className="text-xs font-mono text-muted-foreground leading-relaxed">
                Add API keys to use cloud models alongside your local model. Keys are stored locally on this machine.
              </p>
              <ProviderSettings />
            </div>

            {/* Theme */}
            <ThemeSection />

            {/* Agent Settings */}
            <AgentSettingsSection />
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
                <span className="text-sm font-semibold text-foreground">
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
              <p className="text-xs text-muted-foreground">
                {artifactsLoading ? 'Loading...' : 'No artifacts yet'}
              </p>
              <p className="text-xs text-muted-foreground/50 mt-1">
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

// ── Theme Section ────────────────────────────────────────────────────

function ThemeSection() {
  const { theme, resolved, setTheme } = useTheme();

  const themes: { value: Theme; label: string; icon: React.ReactNode }[] = [
    { value: 'light', label: 'Light', icon: <Sun size={11} /> },
    { value: 'dark', label: 'Dark', icon: <Moon size={11} /> },
    { value: 'system', label: 'System', icon: <MonitorSmartphone size={11} /> },
  ];

  return (
    <div className="border border-border p-3 space-y-2">
      <div className="flex items-center gap-2">
        {resolved === 'dark' ? <Moon size={12} className="text-muted-foreground" /> : <Sun size={12} className="text-muted-foreground" />}
        <span className="text-sm font-semibold text-foreground">Theme</span>
      </div>
      <div className="flex gap-1">
        {themes.map(t => (
          <Button
            key={t.value}
            variant={theme === t.value ? 'default' : 'outline'}
            size="xs"
            onClick={() => setTheme(t.value)}
            className="flex-1 gap-1"
          >
            {t.icon}
            <span className="text-[10px]">{t.label}</span>
          </Button>
        ))}
      </div>
    </div>
  );
}

// ── Agent Settings Section ───────────────────────────────────────────

const defaultConfig: AgentConfig = {
  defaultContextSize: 32768,
  subAgentContextSize: 8192,
  maxTokens: 4096,
  temperature: 1.0,
  topP: 0.95,
  topK: 64,
  thinkingBudgetTokens: 2048,
  defaultMaxToolRounds: 20,
  browserMaxToolRounds: 30,
  toolExecTimeoutSec: 120,
  toolResultMaxChars: 6000,
  microCompactThreshold: 0.70,
  fullCompactThreshold: 0.87,
  maxConcurrentAgents: 3,
};

function AgentSettingsSection() {
  const [config, setConfig] = useState<AgentConfig>(defaultConfig);
  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState(false);
  const [saved, setSaved] = useState(false);

  useEffect(() => {
    api.config.getAgent().then(data => {
      setConfig(data);
      setLoading(false);
    }).catch(() => setLoading(false));
  }, []);

  const save = useCallback(async (updates: Partial<AgentConfig>) => {
    const next = { ...config, ...updates };
    setConfig(next);
    setSaving(true);
    try {
      await api.config.setAgent(updates);
      setSaved(true);
      setTimeout(() => setSaved(false), 1500);
    } catch {
      // revert on failure
      setConfig(config);
    } finally {
      setSaving(false);
    }
  }, [config]);

  const resetToDefaults = useCallback(() => {
    save(defaultConfig);
  }, [save]);

  if (loading) {
    return (
      <div className="border border-border p-3 space-y-2">
        <div className="flex items-center gap-2">
          <Sliders size={12} className="text-muted-foreground" />
          <span className="text-sm font-semibold text-foreground">Agent Settings</span>
        </div>
        <p className="text-xs text-muted-foreground">Loading...</p>
      </div>
    );
  }

  return (
    <div className="border border-border p-3 space-y-3">
      <div className="flex items-center justify-between">
        <div className="flex items-center gap-2">
          <Sliders size={12} className="text-muted-foreground" />
          <span className="text-sm font-semibold text-foreground">Agent Settings</span>
        </div>
        <div className="flex items-center gap-1">
          {saved && <span className="text-[10px] font-mono text-green-500">saved</span>}
          {saving && <Loader size={10} className="animate-spin text-muted-foreground" />}
          <Tooltip>
            <TooltipTrigger asChild>
              <Button variant="ghost" size="icon-xs" onClick={resetToDefaults}>
                <RotateCcw size={10} />
              </Button>
            </TooltipTrigger>
            <TooltipContent>Reset to defaults</TooltipContent>
          </Tooltip>
        </div>
      </div>

      {/* Generation */}
      <div className="space-y-2">
        <div className="text-[10px] font-semibold text-muted-foreground uppercase tracking-wider">Generation</div>

        <SliderRow
          label="Temperature"
          value={config.temperature}
          min={0} max={2} step={0.1}
          onChange={v => save({ temperature: v })}
        />
        <SliderRow
          label="Top P"
          value={config.topP}
          min={0} max={1} step={0.05}
          onChange={v => save({ topP: v })}
        />
        <SliderRow
          label="Top K"
          value={config.topK}
          min={1} max={128} step={1}
          onChange={v => save({ topK: v })}
        />
        <SliderRow
          label="Max Tokens"
          value={config.maxTokens}
          min={256} max={8192} step={256}
          onChange={v => save({ maxTokens: v })}
        />
      </div>

      <Separator />

      {/* Agent Loop */}
      <div className="space-y-2">
        <div className="text-[10px] font-semibold text-muted-foreground uppercase tracking-wider">Agent Loop</div>

        <SliderRow
          label="Thinking Budget"
          value={config.thinkingBudgetTokens}
          min={0} max={8192} step={256}
          onChange={v => save({ thinkingBudgetTokens: v })}
          hint={config.thinkingBudgetTokens === 0 ? 'unlimited' : `${config.thinkingBudgetTokens} tokens`}
        />
        <SliderRow
          label="Max Tool Rounds"
          value={config.defaultMaxToolRounds}
          min={5} max={50} step={1}
          onChange={v => save({ defaultMaxToolRounds: v })}
        />
        <SliderRow
          label="Browser Rounds"
          value={config.browserMaxToolRounds}
          min={5} max={60} step={1}
          onChange={v => save({ browserMaxToolRounds: v })}
        />
        <SliderRow
          label="Tool Timeout"
          value={config.toolExecTimeoutSec}
          min={10} max={300} step={10}
          onChange={v => save({ toolExecTimeoutSec: v })}
          hint={`${config.toolExecTimeoutSec}s`}
        />
      </div>

      <Separator />

      {/* Context */}
      <div className="space-y-2">
        <div className="text-[10px] font-semibold text-muted-foreground uppercase tracking-wider">Context</div>

        <SliderRow
          label="Compact Trigger"
          value={config.microCompactThreshold}
          min={0.5} max={0.95} step={0.05}
          onChange={v => save({ microCompactThreshold: v })}
          hint={`${Math.round(config.microCompactThreshold * 100)}%`}
        />
        <SliderRow
          label="Full Compact"
          value={config.fullCompactThreshold}
          min={0.6} max={0.99} step={0.05}
          onChange={v => save({ fullCompactThreshold: v })}
          hint={`${Math.round(config.fullCompactThreshold * 100)}%`}
        />
      </div>
    </div>
  );
}

// SliderRow is a labeled range slider that shows the current value.
function SliderRow({ label, value, min, max, step, onChange, hint }: {
  label: string;
  value: number;
  min: number;
  max: number;
  step: number;
  onChange: (v: number) => void;
  hint?: string;
}) {
  return (
    <div className="flex items-center gap-2">
      <span className="text-[10px] font-mono text-muted-foreground w-24 shrink-0">{label}</span>
      <input
        type="range"
        min={min} max={max} step={step}
        value={value}
        onChange={e => onChange(Number(e.target.value))}
        className="flex-1 h-1 accent-primary cursor-pointer"
      />
      <span className="text-[10px] font-mono text-foreground w-12 text-right shrink-0">
        {hint ?? value}
      </span>
    </div>
  );
}
