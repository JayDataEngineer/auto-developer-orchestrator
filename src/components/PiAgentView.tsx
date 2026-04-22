import React, { useState, useRef, useEffect, useCallback, memo } from 'react';
import {
  Send, Square, Sparkles, ChevronDown, Trash2,
  Loader, Zap, RotateCcw, ArrowLeft, GitBranch,
  ExternalLink, Check, GitPullRequest, Wrench
} from 'lucide-react';
import { cn } from '../lib/utils';
import { usePiAgentContext } from '../contexts/PiAgentContext';
import { SubAgentInfo, api } from '../lib/api';
import { PiModel, AssistantMessage, ToolCall } from '../lib/pi-events';
import { ToolCallItem } from './agent/ToolCallItem';
import { SubAgentCard } from './agent/SubAgentCard';
import { MarkdownBlock } from './agent/MarkdownBlock';
import { ReasoningBlock } from './agent/ReasoningBlock';
import { FleetBar } from './agent/FleetBar';
import { ApprovalBanner } from './agent/ApprovalBanner';
import { Button } from './ui/button';
import { Textarea } from './ui/textarea';
import { Tooltip, TooltipContent, TooltipTrigger } from './ui/tooltip';
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from './ui/dropdown-menu';

interface PiAgentViewProps {
  selectedProject?: string;
  selectedAgentId?: string;
  projects?: string[];
  onBack?: () => void;
  isZenMode?: boolean;
  onZenToggle?: () => void;
  onStreamingStateChange?: (state: { isStreaming: boolean; runningTool: ToolCall | undefined; thinking: string }) => void;
}

// ─── Input bar (isolated so typing doesn't re-render the message list) ──────

interface InputBarProps {
  isStreaming: boolean;
  disabled: boolean;
  model: string | null;
  models: PiModel[];
  selectedProject?: string;
  selectedAgentId: string;
  isZenMode: boolean;
  autoBranch: boolean;
  autoMerge: boolean;
  toolModel: string | null;
  onSend: (text: string) => void;
  onAbort: () => void;
  onReset: () => void;
  onSwitchModel: (provider: string, modelId: string) => void;
  onCompact: () => void;
  onAutoBranchChange: (v: boolean) => void;
  onAutoMergeChange: (v: boolean) => void;
  onSetToolModel: (provider: string, modelId: string) => void;
}

const InputBar = memo(function InputBar({
  isStreaming, disabled, model, models, selectedProject, selectedAgentId,
  isZenMode, autoBranch, autoMerge, toolModel,
  onSend, onAbort, onReset, onSwitchModel, onCompact,
  onAutoBranchChange, onAutoMergeChange, onSetToolModel,
}: InputBarProps) {
  const [input, setInput] = useState('');

  const handleSend = useCallback(() => {
    if (!input.trim() || isStreaming) return;
    onSend(input.trim());
    setInput('');
  }, [input, isStreaming, onSend]);

  const handleKeyDown = useCallback((e: React.KeyboardEvent) => {
    if (e.key === 'Enter' && !e.shiftKey) {
      e.preventDefault();
      handleSend();
    }
  }, [handleSend]);

  return (
    <div className="w-full border-t border-white/5 bg-black/50 backdrop-blur-md">
      <div className={cn(
        "mx-auto p-4 transition-all duration-500",
        isZenMode ? "max-w-7xl" : "max-w-3xl"
      )}>
        <div className="flex gap-2">
          <div className="flex-1 relative">
            <Textarea
              value={input}
              onChange={(e) => setInput(e.target.value)}
              onKeyDown={handleKeyDown}
              placeholder={selectedProject ? "Tell Pi what you want to build or automate..." : "Select a project to get started..."}
              disabled={disabled}
              data-prompt-input
              className="w-full border-border rounded-lg p-4 pr-14 text-base text-foreground placeholder-muted-foreground/60 resize-none bg-card"
              rows={4}
            />
            <div className="absolute right-3 bottom-3 flex items-center gap-2">
              {isStreaming ? (
                <Button variant="destructive" size="xs" onClick={onAbort}>
                  <Square size={16} />
                </Button>
              ) : (
                <Button variant="default" size="xs" onClick={handleSend} disabled={!input.trim() || disabled}>
                  <Send size={16} />
                </Button>
              )}
            </div>
          </div>
        </div>
        <div className="flex items-center gap-4 mt-2">
          <div className="flex items-center gap-3">
            <DropdownMenu>
              <DropdownMenuTrigger asChild>
                <Button variant="ghost" size="xs" className="text-muted hover:text-muted-foreground">
                  Main: {model || 'default'} <ChevronDown size={10} />
                </Button>
              </DropdownMenuTrigger>
              <DropdownMenuContent align="start" className="w-64 max-h-[300px] overflow-y-auto">
                <DropdownMenuLabel>Main Model (conversation)</DropdownMenuLabel>
                <DropdownMenuSeparator />
                {models.length === 0 && (
                  <div className="px-2 py-2 text-xs font-mono text-muted uppercase tracking-widest">Loading models...</div>
                )}
                {models.map(m => (
                  <DropdownMenuItem
                    key={m.id}
                    onClick={() => onSwitchModel(m.provider || 'llamacpp', m.id)}
                    className={model === m.id ? 'text-primary' : ''}
                  >
                    {m.name || m.id}
                  </DropdownMenuItem>
                ))}
              </DropdownMenuContent>
            </DropdownMenu>
            <DropdownMenu>
              <DropdownMenuTrigger asChild>
                <Button variant="ghost" size="xs" className="text-muted hover:text-muted-foreground">
                  Tool: {toolModel || 'default'} <ChevronDown size={10} />
                </Button>
              </DropdownMenuTrigger>
              <DropdownMenuContent align="start" className="w-64 max-h-[300px] overflow-y-auto">
                <DropdownMenuLabel>Tool Model (sub-agents/vision)</DropdownMenuLabel>
                <DropdownMenuSeparator />
                {models.length === 0 && (
                  <div className="px-2 py-2 text-xs font-mono text-muted uppercase tracking-widest">Loading models...</div>
                )}
                {models.map(m => (
                  <DropdownMenuItem
                    key={m.id}
                    onClick={() => onSetToolModel(m.provider || 'llamacpp', m.id)}
                    className={toolModel === m.id ? 'text-primary' : ''}
                  >
                    {m.name || m.id}
                  </DropdownMenuItem>
                ))}
              </DropdownMenuContent>
            </DropdownMenu>
          </div>

          <Tooltip>
            <TooltipTrigger asChild>
              <Button variant="ghost" size="xs" onClick={onReset}>
                <RotateCcw size={10} /> New Task
              </Button>
            </TooltipTrigger>
            <TooltipContent>New Task <span className="kbd ml-1">Ctrl+Shift+N</span></TooltipContent>
          </Tooltip>

          <Tooltip>
            <TooltipTrigger asChild>
              <Button
                variant="ghost"
                size="xs"
                onClick={() => { onAutoBranchChange(!autoBranch); if (!autoBranch) onAutoMergeChange(false); }}
                className={autoBranch ? 'text-primary' : ''}
              >
                <GitBranch size={10} /> Auto-Branch
              </Button>
            </TooltipTrigger>
            <TooltipContent>Auto-Branch <span className="kbd ml-1">Ctrl+Shift+B</span></TooltipContent>
          </Tooltip>

          {autoBranch && (
            <Tooltip>
              <TooltipTrigger asChild>
                <Button
                  variant="ghost"
                  size="xs"
                  onClick={() => onAutoMergeChange(!autoMerge)}
                  className={autoMerge ? 'text-primary' : ''}
                >
                  <GitPullRequest size={10} /> Auto-Merge
                </Button>
              </TooltipTrigger>
              <TooltipContent>Auto-Merge <span className="kbd ml-1">Ctrl+Shift+M</span></TooltipContent>
            </Tooltip>
          )}

          <Tooltip>
            <TooltipTrigger asChild>
              <Button variant="ghost" size="xs" onClick={onCompact} disabled={isStreaming}>
                <Trash2 size={10} /> Compact
              </Button>
            </TooltipTrigger>
            <TooltipContent>Compact <span className="kbd ml-1">Ctrl+Shift+C</span></TooltipContent>
          </Tooltip>
        </div>
      </div>
    </div>
  );
});

// ─── Main component ────────────────────────────────────────────────────────

export const PiAgentView: React.FC<PiAgentViewProps> = ({ selectedProject, selectedAgentId = 'default', projects = [], onBack, isZenMode = false, onZenToggle, onStreamingStateChange }) => {
  const { state, sendPrompt, abort, compact, switchModel, reset, hydrateState, getModels, loadHistory, respondToApproval } = usePiAgentContext();
  const [models, setModels] = useState<PiModel[]>([]);
  const [toolModel, setToolModel] = useState<string | null>(null);
  const messagesEndRef = useRef<HTMLDivElement>(null);

  const [autoBranch, setAutoBranch] = useState(false);
  const [autoMerge, setAutoMerge] = useState(false);

  // Load model config on mount
  useEffect(() => {
    api.config.getModels().then((cfg) => {
      if (cfg.toolModel?.modelId) setToolModel(cfg.toolModel.modelId);
    }).catch(() => {});
  }, []);

  useEffect(() => {
    if (selectedProject) {
      hydrateState(selectedProject, selectedAgentId);
      loadHistory(selectedProject, selectedAgentId);
    }
  }, [selectedProject, selectedAgentId, hydrateState, loadHistory]);

  useEffect(() => {
    if (!selectedProject) return;
    getModels(selectedProject, selectedAgentId).then(setModels);
  }, [selectedProject, selectedAgentId, getModels]);

  // Auto-scroll: use RAF to debounce during streaming
  const scrollRafRef = useRef<number | null>(null);
  useEffect(() => {
    if (scrollRafRef.current !== null) cancelAnimationFrame(scrollRafRef.current);
    scrollRafRef.current = requestAnimationFrame(() => {
      try {
        messagesEndRef.current?.scrollIntoView({ behavior: 'smooth' });
      } catch {
        // scrollIntoView can throw in some environments (jsdom, iframes)
      }
    });
    return () => {
      if (scrollRafRef.current !== null) cancelAnimationFrame(scrollRafRef.current);
    };
  }, [state.text, state.thinking, state.toolCalls.length]);

  const handleSend = useCallback((text: string) => {
    if (!selectedProject || state.isStreaming) return;
    sendPrompt(text, selectedProject, { agentId: selectedAgentId, model: state.model || 'or-free', thinkingLevel: 'medium', autoBranch, autoMerge });
  }, [selectedProject, state.isStreaming, state.model, sendPrompt, selectedAgentId, autoBranch, autoMerge]);

  const handleAbort = useCallback(() => {
    if (selectedProject) abort(selectedProject, selectedAgentId);
  }, [selectedProject, selectedAgentId, abort]);

  const handleSwitchModel = useCallback((provider: string, modelId: string) => {
    if (selectedProject) switchModel(selectedProject, provider, modelId, selectedAgentId);
  }, [selectedProject, selectedAgentId, switchModel]);

  const handleSetToolModel = useCallback(async (provider: string, modelId: string) => {
    try {
      await api.config.setModels({ toolModel: { provider, modelId } });
      setToolModel(modelId);
    } catch {}
  }, []);

  const handleCompact = useCallback(() => {
    if (selectedProject) compact(selectedProject, selectedAgentId);
  }, [selectedProject, selectedAgentId, compact]);

  const hasContent = state.messages.length > 0 || state.isStreaming;
  const runningTool = state.toolCalls.find(tc => !tc.endTime);

  // Notify parent of streaming state changes
  useEffect(() => {
    onStreamingStateChange?.({ isStreaming: state.isStreaming, runningTool, thinking: state.thinking });
  }, [state.isStreaming, runningTool?.id, state.thinking, onStreamingStateChange]);

  return (
    <div className="flex h-full w-full bg-background overflow-hidden">
      {/* Main content */}
      <div className="flex-1 flex flex-col min-w-0">
        {/* Header */}
        <div className="w-full h-12 border-b border-border flex items-center px-6 shrink-0 bg-background/50 backdrop-blur-md">
          <div className="flex items-center gap-3">
            {onBack && !isZenMode && (
              <Button variant="ghost" size="icon-xs" onClick={onBack}>
                <ArrowLeft size={14} />
              </Button>
            )}
            <div className="flex items-center gap-2 text-sm font-semibold">
              <Zap size={12} className="text-primary" />
              <span className="text-primary">Pi</span>
              <span className="text-muted-foreground">Assistant</span>
            </div>
          </div>
          <div className="flex-1" />
          <div className="flex items-center gap-3">
            {state.isStreaming && (
              <span className="text-xs font-semibold text-primary animate-pulse">
                Streaming
              </span>
            )}
            {runningTool && (
              <span className="flex items-center gap-1.5 text-xs text-muted-foreground">
                <Wrench size={9} className="text-primary" />
                Running: {runningTool.name}
              </span>
            )}
            {state.error && (
              <span className="text-xs font-mono text-red-400">{state.error}</span>
            )}
          </div>
        </div>

        {/* Fleet context bar */}
        <FleetBar
          project={selectedProject}
          branch={state.branchName}
          model={state.model}
          streaming={state.isStreaming}
        />

        {/* Activity Feed */}
        <div className="flex-1 overflow-y-auto custom-scrollbar w-full">
          <div className={cn(
            "mx-auto px-6 py-6 space-y-4 transition-all duration-500",
            isZenMode ? "max-w-7xl" : "max-w-3xl"
          )}>

            {/* Empty state */}
            {!hasContent && (
              <div className="h-full flex flex-col items-center justify-center text-center py-20 space-y-6">
                <div className="w-16 h-16 border border-primary flex items-center justify-center text-primary">
                  <Sparkles size={32} className="animate-pulse-slow" />
                </div>
                <div className="space-y-3 max-w-md">
                  <h3 className="text-lg font-bold text-foreground">Ready to help</h3>
                  <p className="text-sm text-muted-foreground leading-relaxed">
                    Tell Pi what you want to build or automate, and watch it work.
                  </p>
                </div>
              </div>
            )}

            {/* Sub-Agent Status Cards */}
            {state.subAgents.length > 0 && (
              <div className="space-y-1">
                <div className="text-xs text-muted-foreground px-1">
                  Working on it ({state.subAgents.length})
                </div>
                {state.subAgents.map(sa => (
                  <SubAgentCard key={sa.subAgentId} agent={sa} />
                ))}
              </div>
            )}

            {/* Conversation Thread */}
            {state.messages.map((msg) => {
              if (msg.role === 'user') {
                return (
                  <div key={msg.id} className="flex justify-end">
                    <div className="max-w-[80%] bg-muted border border-border px-4 py-3 rounded-lg">
                      <p className="text-xs text-foreground leading-relaxed whitespace-pre-wrap">{msg.content}</p>
                    </div>
                  </div>
                );
              }

              // Assistant message
              const aMsg = msg as AssistantMessage;
              return (
                <div key={msg.id} className="space-y-3">
                  {aMsg.thinking && (
                    <ReasoningBlock content={aMsg.thinking} defaultOpen={false} />
                  )}
                  {aMsg.text && (
                    <MarkdownBlock content={aMsg.text} streaming={!!aMsg.streaming} />
                  )}
                  {aMsg.toolCalls.length > 0 && (
                    <div className="space-y-1">
                      {aMsg.toolCalls.map(tc => (
                        <ToolCallItem key={tc.id} tc={tc} />
                      ))}
                    </div>
                  )}
                  {aMsg.streaming && !aMsg.text && !aMsg.thinking && aMsg.toolCalls.length === 0 && (
                    <div className="flex items-center gap-2 py-2">
                      <Loader size={12} className="text-primary animate-spin" />
                      <span className="text-xs font-mono text-muted-foreground uppercase tracking-widest">Thinking...</span>
                    </div>
                  )}
                </div>
              );
            })}

            <div ref={messagesEndRef} />
          </div>
        </div>

        {/* Token usage */}
        {(state.tokenUsage.input > 0 || state.tokenUsage.output > 0) && (
          <div className="w-full border-t border-white/5">
            <div className={cn(
              "mx-auto py-2 px-6 flex items-center gap-6 text-xs font-mono text-muted-foreground transition-all duration-500",
              isZenMode ? "max-w-7xl" : "max-w-3xl"
            )}>
              <span>Tokens: {state.tokenUsage.input}in / {state.tokenUsage.output}out / {state.tokenUsage.cache}cache</span>
            </div>
          </div>
        )}

        {/* PR Created Banner */}
        {state.prUrl && !state.isStreaming && (
          <div className="w-full border-t border-primary/20 bg-primary/5">
            <div className={cn(
              "mx-auto py-3 px-6 flex items-center gap-3 transition-all duration-500",
              isZenMode ? "max-w-7xl" : "max-w-3xl"
            )}>
              <div className="shrink-0 w-6 h-6 flex items-center justify-center bg-primary/20 text-primary rounded-full">
                <Check size={12} />
              </div>
              <div className="flex-1 min-w-0">
                <div className="text-sm font-black uppercase tracking-widest text-primary">
                  Pull Request #{state.prNumber} Created
                </div>
                <div className="text-xs font-mono text-muted-foreground truncate mt-0.5">
                  {state.prUrl}
                </div>
              </div>
              <Button
                variant="default"
                size="xs"
                asChild
              >
                <a
                  href={state.prUrl}
                  target="_blank"
                  rel="noopener noreferrer"
                >
                  <ExternalLink size={10} />
                  Open PR
                </a>
              </Button>
            </div>
          </div>
        )}

        {/* Approval/Question Banner */}
        {state.pendingApproval && selectedProject && (
          <ApprovalBanner
            approval={state.pendingApproval}
            onRespond={(requestId, action, message) =>
              respondToApproval(selectedProject, selectedAgentId, requestId, action, message)
            }
          />
        )}

        {/* Input area — isolated via React.memo so typing doesn't re-render messages */}
        <InputBar
          isStreaming={state.isStreaming}
          disabled={state.isStreaming || !selectedProject}
          model={state.model}
          models={models}
          selectedProject={selectedProject}
          selectedAgentId={selectedAgentId}
          isZenMode={isZenMode}
          autoBranch={autoBranch}
          autoMerge={autoMerge}
          onSend={handleSend}
          onAbort={handleAbort}
          onReset={reset}
          onSwitchModel={handleSwitchModel}
          onCompact={handleCompact}
          onAutoBranchChange={setAutoBranch}
          onAutoMergeChange={setAutoMerge}
          toolModel={toolModel}
          onSetToolModel={handleSetToolModel}
        />
      </div>
    </div>
  );
};
