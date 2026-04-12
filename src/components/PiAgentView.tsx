import React, { useState, useRef, useEffect, useCallback } from 'react';
import {
  Send, Square, Sparkles, ChevronDown, ChevronRight, Trash2,
  Loader, Zap, RotateCcw, ArrowLeft, ChevronUp, GitBranch,
  ExternalLink, Check, GitPullRequest, Wrench
} from 'lucide-react';
import { cn } from '../lib/utils';
import { usePiAgent } from '../hooks/usePiAgent';
import { SubAgentInfo } from '../lib/api';
import { PiModel, AssistantMessage, ToolCall } from '../lib/pi-events';
import { ToolCallItem } from './agent/ToolCallItem';
import { SubAgentCard } from './agent/SubAgentCard';
import { MarkdownBlock } from './agent/MarkdownBlock';
import { ReasoningBlock } from './agent/ReasoningBlock';
import { FleetBar } from './agent/FleetBar';
import { ApprovalBanner } from './agent/ApprovalBanner';

interface PiAgentViewProps {
  selectedProject?: string;
  selectedAgentId?: string;
  projects?: string[];
  onBack?: () => void;
  isZenMode?: boolean;
  onZenToggle?: () => void;
  onStreamingStateChange?: (state: { isStreaming: boolean; runningTool: ToolCall | undefined; thinking: string }) => void;
}

export const PiAgentView: React.FC<PiAgentViewProps> = ({ selectedProject, selectedAgentId = 'default', projects = [], onBack, isZenMode = false, onZenToggle, onStreamingStateChange }) => {
  const { state, sendPrompt, abort, compact, switchModel, reset, hydrateState, getModels, loadHistory, respondToApproval } = usePiAgent(selectedAgentId);
  const [input, setInput] = useState('');
  const [models, setModels] = useState<PiModel[]>([]);
  const [modelDropdownOpen, setModelDropdownOpen] = useState(false);
  const messagesEndRef = useRef<HTMLDivElement>(null);
  const textareaRef = useRef<HTMLTextAreaElement>(null);
  const modelDropdownRef = useRef<HTMLDivElement>(null);

  const [autoBranch, setAutoBranch] = useState(false);
  const [autoMerge, setAutoMerge] = useState(false);
  const sandboxId = selectedProject ? `sandbox-${selectedProject}` : '';

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

  useEffect(() => {
    if (!modelDropdownOpen) return;
    const handleClick = (e: MouseEvent) => {
      if (modelDropdownRef.current && !modelDropdownRef.current.contains(e.target as Node)) {
        setModelDropdownOpen(false);
      }
    };
    document.addEventListener('mousedown', handleClick);
    return () => document.removeEventListener('mousedown', handleClick);
  }, [modelDropdownOpen]);

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

  const handleSend = useCallback(() => {
    if (!input.trim() || !selectedProject || state.isStreaming) return;
    sendPrompt(input.trim(), selectedProject, { agentId: selectedAgentId, model: state.model || 'or-free', thinkingLevel: 'medium', autoBranch, autoMerge });
    setInput('');
  }, [input, selectedProject, state.isStreaming, sendPrompt, selectedAgentId, autoBranch, autoMerge]);

  const handleKeyDown = useCallback((e: React.KeyboardEvent) => {
    if (e.key === 'Enter' && !e.shiftKey) {
      e.preventDefault();
      handleSend();
    }
  }, [handleSend]);

  const hasContent = state.messages.length > 0 || state.isStreaming;
  const runningTool = state.toolCalls.find(tc => !tc.endTime);

  // Notify parent of streaming state changes
  useEffect(() => {
    onStreamingStateChange?.({ isStreaming: state.isStreaming, runningTool, thinking: state.thinking });
  }, [state.isStreaming, runningTool?.id, state.thinking, onStreamingStateChange]);

  return (
    <div className="flex h-full w-full bg-black overflow-hidden">
      {/* Main content */}
      <div className="flex-1 flex flex-col min-w-0">
        {/* Header */}
        <div className="w-full h-12 border-b border-white/5 flex items-center px-6 shrink-0 bg-black/50 backdrop-blur-md">
          <div className="flex items-center gap-3">
            {onBack && !isZenMode && (
              <button onClick={onBack} className="flex items-center gap-1.5 text-muted hover:text-zinc-300 transition-colors">
                <ArrowLeft size={14} />
              </button>
            )}
            <div className="flex items-center gap-2 text-sm font-mono tracking-widest text-muted uppercase font-bold">
              <Zap size={12} className="text-primary" />
              <span className="text-primary">PI</span>
              <span className="text-muted">CODING AGENT</span>
            </div>
          </div>
          <div className="flex-1" />
          <div className="flex items-center gap-3">
            {state.isStreaming && (
              <span className="text-xs font-black text-primary uppercase tracking-widest animate-pulse">
                Streaming
              </span>
            )}
            {runningTool && (
              <span className="flex items-center gap-1.5 text-xs font-mono text-primary/80 uppercase tracking-widest">
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
                  <h3 className="text-lg font-bold text-white tracking-widest uppercase">Pi Agent Ready</h3>
                  <p className="text-xs text-muted-foreground leading-relaxed font-mono">
                    Describe a coding task and Pi will implement it with real file editing, bash execution, and intelligent analysis.
                  </p>
                </div>
              </div>
            )}

            {/* Sub-Agent Status Cards */}
            {state.subAgents.length > 0 && (
              <div className="space-y-1">
                <div className="text-xs font-mono text-muted-foreground uppercase tracking-widest px-1">
                  Sub-Agents ({state.subAgents.length})
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
                    <div className="max-w-[80%] bg-white/5 border border-white/10 px-4 py-3 rounded-none">
                      <p className="text-xs text-white font-mono leading-relaxed whitespace-pre-wrap">{msg.content}</p>
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
              <a
                href={state.prUrl}
                target="_blank"
                rel="noopener noreferrer"
                className="shrink-0 flex items-center gap-1.5 px-3 py-1.5 bg-primary text-black text-xs font-black uppercase tracking-widest hover:bg-primary/80 transition-colors"
              >
                <ExternalLink size={10} />
                Open PR
              </a>
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

        {/* Input area */}
        <div className="w-full border-t border-white/5 bg-black/50 backdrop-blur-md">
          <div className={cn(
            "mx-auto p-4 transition-all duration-500",
            isZenMode ? "max-w-7xl" : "max-w-3xl"
          )}>
            <div className="flex gap-2">
              <div className="flex-1 relative">
                <textarea
                  ref={textareaRef}
                  value={input}
                  onChange={(e) => setInput(e.target.value)}
                  onKeyDown={handleKeyDown}
                  placeholder={selectedProject ? "Describe a coding task..." : "Select a project first..."}
                  disabled={state.isStreaming || !selectedProject}
                  data-prompt-input
                  className="w-full bg-zinc-900 border border-white/5 rounded p-4 pr-14 text-sm text-white placeholder-zinc-700 outline-none focus:border-primary/40 transition-all font-mono resize-none"
                  rows={3}
                />
                <div className="absolute right-3 bottom-3 flex items-center gap-2">
                  {state.isStreaming ? (
                    <button onClick={() => selectedProject && abort(selectedProject, selectedAgentId)} className="p-2 bg-red-500/20 text-red-400 rounded hover:bg-red-500/30 transition-all">
                      <Square size={16} />
                    </button>
                  ) : (
                    <button onClick={handleSend} disabled={!input.trim() || !selectedProject} className="p-2 bg-primary text-black rounded hover:bg-primary/80 disabled:opacity-20 transition-all">
                      <Send size={16} />
                    </button>
                  )}
                </div>
              </div>
            </div>
            <div className="flex items-center gap-4 mt-2">
              <div className="relative" ref={modelDropdownRef}>
                <button
                  onClick={() => setModelDropdownOpen(!modelDropdownOpen)}
                  className="text-xs font-mono text-muted hover:text-muted-foreground flex items-center gap-1 uppercase tracking-widest"
                >
                  Model: {state.model || 'default'}
                  {modelDropdownOpen ? <ChevronUp size={10} /> : <ChevronDown size={10} />}
                </button>
                {modelDropdownOpen && (
                  <div className="absolute bottom-full left-0 mb-1 w-64 max-h-[300px] overflow-y-auto border border-white/10 bg-zinc-950 shadow-2xl z-[100] custom-scrollbar scrollbar-gutter-stable">
                    {models.length === 0 && (
                      <div className="px-3 py-2 text-xs font-mono text-muted uppercase tracking-widest">Loading models...</div>
                    )}
                    {models.map((m) => (
                      <button
                        key={m.id}
                        onClick={() => {
                          if (selectedProject) switchModel(selectedProject, m.provider || 'litellm', m.id, selectedAgentId);
                          setModelDropdownOpen(false);
                        }}
                        className={cn(
                          "w-full text-left px-3 py-2 text-xs font-mono uppercase tracking-widest transition-colors",
                          state.model === m.id
                            ? "bg-primary/10 text-primary"
                            : "text-muted hover:bg-white/5 hover:text-muted-foreground"
                        )}
                      >
                        {m.name || m.id}
                      </button>
                    ))}
                  </div>
                )}
              </div>
              <button onClick={reset} className="text-xs font-mono text-muted hover:text-muted-foreground flex items-center gap-1 uppercase tracking-widest">
                <RotateCcw size={10} /> New Task
              </button>
              <button
                onClick={() => { setAutoBranch(!autoBranch); if (!autoBranch) setAutoMerge(false); }}
                className={cn(
                  "text-xs font-mono flex items-center gap-1 uppercase tracking-widest transition-colors",
                  autoBranch ? "text-primary" : "text-muted hover:text-muted-foreground"
                )}
              >
                <GitBranch size={10} /> Auto-Branch
              </button>
              {autoBranch && (
                <button
                  onClick={() => setAutoMerge(!autoMerge)}
                  className={cn(
                    "text-xs font-mono flex items-center gap-1 uppercase tracking-widest transition-colors",
                    autoMerge ? "text-primary" : "text-muted hover:text-muted-foreground"
                  )}
                >
                  <GitPullRequest size={10} /> Auto-Merge
                </button>
              )}
              <button onClick={() => selectedProject && compact(selectedProject, selectedAgentId)} disabled={state.isStreaming} className="text-xs font-mono text-muted hover:text-muted-foreground flex items-center gap-1 uppercase tracking-widest disabled:opacity-30">
                <Trash2 size={10} /> Compact
              </button>
            </div>
          </div>
        </div>
      </div>
    </div>
  );
};
