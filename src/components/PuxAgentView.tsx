import React, { useState, useRef, useEffect, useCallback } from 'react';
import {
  Loader, Zap, ArrowLeft, Wrench, Check, ExternalLink
} from 'lucide-react';
import { cn } from '../lib/utils';
import { usePuxAgentContext } from '../contexts/PuxAgentContext';
import { api } from '../lib/api';
import { PuxModel, AssistantMessage, ToolCall } from '../lib/pux-events';
import { ToolCallItem } from './agent/ToolCallItem';
import { SubAgentCard } from './agent/SubAgentCard';
import { MarkdownBlock } from './agent/MarkdownBlock';
import { ReasoningBlock } from './agent/ReasoningBlock';
import { FleetBar } from './agent/FleetBar';
import { ApprovalBanner } from './agent/ApprovalBanner';
import { InputBar } from './InputBar';
import { Button } from './ui/button';

interface PuxAgentViewProps {
  selectedProject?: string;
  selectedAgentId?: string;
  projects?: string[];
  onBack?: () => void;
  isZenMode?: boolean;
  onZenToggle?: () => void;
  hideInputBar?: boolean;
  onStreamingStateChange?: (state: { isStreaming: boolean; runningTool: ToolCall | undefined; thinking: string }) => void;
}

// ─── Main component ────────────────────────────────────────────────────────

export const PuxAgentView: React.FC<PuxAgentViewProps> = ({ selectedProject, selectedAgentId = 'default', projects = [], onBack, isZenMode = false, onZenToggle, hideInputBar = false, onStreamingStateChange }) => {
  const { state, sendPrompt, abort, compact, switchModel, reset, hydrateState, getModels, loadHistory, respondToApproval } = usePuxAgentContext();
  const [models, setModels] = useState<PuxModel[]>([]);
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
              <span className="text-primary">Pux</span>
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
                {/* Pux mascot — friendly sprite */}
                <div className="relative w-20 h-20">
                  {/* Glow */}
                  <div className="absolute -inset-2 rounded-full bg-primary/10 animate-pulse-slow" />
                  <svg viewBox="0 0 80 80" fill="none" className="relative w-full h-full">
                    {/* Left wing */}
                    <ellipse cx="18" cy="38" rx="12" ry="7" fill="currentColor" opacity="0.15" />
                    {/* Right wing */}
                    <ellipse cx="62" cy="38" rx="12" ry="7" fill="currentColor" opacity="0.15" />
                    {/* Body */}
                    <ellipse cx="40" cy="42" rx="16" ry="18" fill="currentColor" opacity="0.2" />
                    <ellipse cx="40" cy="42" rx="14" ry="16" fill="currentColor" opacity="0.15" />
                    {/* Eyes */}
                    <circle cx="34" cy="38" r="3" fill="currentColor" opacity="0.6" />
                    <circle cx="46" cy="38" r="3" fill="currentColor" opacity="0.6" />
                    {/* Eye highlights */}
                    <circle cx="35" cy="37" r="1.2" fill="white" opacity="0.8" />
                    <circle cx="47" cy="37" r="1.2" fill="white" opacity="0.8" />
                    {/* Smile */}
                    <path d="M36 45 Q40 49 44 45" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" opacity="0.4" fill="none" />
                    {/* Antenna spark */}
                    <line x1="40" y1="24" x2="40" y2="16" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" opacity="0.3" />
                    <circle cx="40" cy="14" r="2.5" fill="currentColor" opacity="0.5" />
                  </svg>
                </div>
                <div className="space-y-3 max-w-md">
                  <h3 className="text-xl font-semibold text-foreground">Hi, I'm Pux</h3>
                  <p className="text-sm text-muted-foreground leading-relaxed">
                    What would you like me to build, research, or automate for you today?
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
                      <span className="text-xs text-muted-foreground">Thinking...</span>
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
          <div className="w-full border-t border-border">
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
                <div className="text-sm font-bold text-primary">
                  Pull Request #{state.prNumber} Created
                </div>
                <div className="text-xs text-muted-foreground truncate mt-0.5">
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
        {!hideInputBar && <InputBar
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
        }
      </div>
    </div>
  );
};
