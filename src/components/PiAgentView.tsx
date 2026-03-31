import React, { useState, useRef, useEffect, useCallback } from 'react';
import {
  Send, Square, Sparkles, ChevronDown, ChevronRight, Trash2,
  FileCode, Terminal as TerminalIcon, Search, Wrench, Brain,
  Loader, Zap, RotateCcw, ArrowLeft, ChevronUp, GitBranch, Box,
  ExternalLink, Check
} from 'lucide-react';
import ReactMarkdown from 'react-markdown';
import remarkGfm from 'remark-gfm';
import { Prism as SyntaxHighlighter } from 'react-syntax-highlighter';
import { oneDark } from 'react-syntax-highlighter/dist/esm/styles/prism';
import { cn } from '../lib/utils';
import { usePiAgent } from '../hooks/usePiAgent';
import { ToolCall, PiModel } from '../lib/pi-events';

interface PiAgentViewProps {
  selectedProject?: string;
  selectedAgentId?: string;
  projects?: string[];
  onBack?: () => void;
}

// ─── Tool Helpers ───────────────────────────────────────────────

const TOOL_ICONS: Record<string, React.ReactNode> = {
  read: <FileCode size={12} />,
  write: <FileCode size={12} />,
  edit: <FileCode size={12} />,
  bash: <TerminalIcon size={12} />,
  grep: <Search size={12} />,
  find: <Search size={12} />,
};

function formatToolArgs(name: string, args: Record<string, unknown>): string {
  if (name === 'read' || name === 'write' || name === 'edit') {
    return String(args.filePath || args.path || '');
  }
  if (name === 'bash') {
    return String(args.command || '').slice(0, 80);
  }
  if (name === 'grep') {
    return `${args.pattern} in ${args.path || '.'}`;
  }
  return JSON.stringify(args).slice(0, 80);
}

function formatResult(result: unknown): string {
  if (typeof result === 'string') return result;
  return JSON.stringify(result, null, 2);
}

// ─── Block Components ───────────────────────────────────────────

/** Markdown block with syntax-highlighted code fences */
function MarkdownBlock({ content, streaming }: { content: string; streaming: boolean }) {
  return (
    <div className="prose prose-invert prose-sm max-w-none
      prose-headings:text-white prose-headings:font-bold prose-headings:tracking-widest prose-headings:uppercase
      prose-p:text-zinc-300 prose-p:text-[12px] prose-p:leading-relaxed prose-p:font-mono
      prose-code:text-primary prose-code:bg-primary/10 prose-code:px-1 prose-code:rounded
      prose-pre:bg-zinc-950 prose-pre:border prose-pre:border-white/5 prose-pre:rounded-none
      prose-a:text-primary prose-a:no-underline hover:prose-a:underline
      prose-strong:text-white
      prose-ul:text-zinc-300 prose-ol:text-zinc-300
      prose-li:text-[12px] prose-li:font-mono
    ">
      <ReactMarkdown
        remarkPlugins={[remarkGfm]}
        components={{
          code({ className, children, ...props }) {
            const match = /language-(\w+)/.exec(className || '');
            const code = String(children).replace(/\n$/, '');
            return match ? (
              <SyntaxHighlighter
                style={oneDark}
                language={match[1]}
                PreTag="div"
                customStyle={{
                  margin: 0,
                  padding: '12px',
                  fontSize: '11px',
                  background: '#09090b',
                  border: '1px solid rgba(255,255,255,0.05)',
                }}
              >
                {code}
              </SyntaxHighlighter>
            ) : (
              <code className={className} {...props}>{children}</code>
            );
          },
        }}
      >
        {content}
      </ReactMarkdown>
      {streaming && (
        <span className="inline-block w-2 h-4 bg-primary/60 animate-pulse ml-0.5 align-text-bottom" />
      )}
    </div>
  );
}

/** Collapsible reasoning/thinking block */
function ReasoningBlock({ content, defaultOpen = false }: { content: string; defaultOpen?: boolean }) {
  const [open, setOpen] = useState(defaultOpen);
  return (
    <div className="border border-zinc-800 bg-zinc-950">
      <button
        onClick={() => setOpen(!open)}
        className="w-full flex items-center gap-3 p-3 text-left"
      >
        <Brain size={12} className="text-muted-foreground" />
        <span className="text-[9px] font-black uppercase tracking-widest text-muted-foreground">
          Reasoning
        </span>
        <span className="text-[9px] font-mono text-zinc-700">
          {content.length} chars
        </span>
        <div className="flex-1" />
        {open ? <ChevronDown size={10} className="text-muted-foreground" /> : <ChevronRight size={10} className="text-muted-foreground" />}
      </button>
      {open && (
        <div className="px-3 pb-3 border-t border-zinc-800">
          <pre className="text-[10px] font-mono text-muted whitespace-pre-wrap max-h-64 overflow-auto">
            {content}
          </pre>
        </div>
      )}
    </div>
  );
}

/** Tool accordion with auto-collapse for long output */
const COLLAPSE_THRESHOLD = 10;

function ToolCallItem({ call }: { call: ToolCall }) {
  const [expanded, setExpanded] = useState(false);
  const isActive = !call.endTime;
  const duration = call.endTime ? call.endTime - call.startTime : Date.now() - call.startTime;

  const resultText = call.result !== undefined ? formatResult(call.result) : '';
  const resultLines = resultText.split('\n');
  const isLong = resultLines.length > COLLAPSE_THRESHOLD;

  return (
    <div className={cn(
      "border transition-all",
      isActive ? "border-primary/30 bg-primary/5" : "border-border bg-black"
    )}>
      <button
        onClick={() => setExpanded(!expanded)}
        className="w-full flex items-center gap-3 p-3 text-left"
      >
        <div className={cn(
          "shrink-0 w-6 h-6 flex items-center justify-center",
          isActive ? "text-primary" : "text-muted"
        )}>
          {TOOL_ICONS[call.name] || <Wrench size={12} />}
        </div>
        <div className="flex-1 min-w-0">
          <div className="flex items-center gap-2">
            <span className="text-[10px] font-black uppercase tracking-widest text-white">
              {call.name}
            </span>
            <span className="text-[9px] font-mono text-muted-foreground truncate">
              {formatToolArgs(call.name, call.args)}
            </span>
          </div>
        </div>
        <div className="flex items-center gap-2 shrink-0">
          {isActive ? (
            <Loader size={10} className="text-primary animate-spin" />
          ) : (
            <span className="text-[9px] font-mono text-muted-foreground">{duration}ms</span>
          )}
          {expanded ? <ChevronDown size={10} className="text-muted-foreground" /> : <ChevronRight size={10} className="text-muted-foreground" />}
        </div>
      </button>
      {expanded && (
        <div className="px-3 pb-3 border-t border-border">
          {resultText && (
            <pre className="text-[10px] font-mono text-muted-foreground mt-2 whitespace-pre-wrap max-h-64 overflow-auto">
              {isLong && !expanded
                ? resultLines.slice(0, COLLAPSE_THRESHOLD).join('\n') + `\n... +${resultLines.length - COLLAPSE_THRESHOLD} more lines`
                : resultText
              }
            </pre>
          )}
          {call.error && (
            <p className="text-[10px] font-mono text-red-400 mt-2">{call.error}</p>
          )}
        </div>
      )}
    </div>
  );
}

/** Fleet context bar: Project | Branch | Sandbox | Model */
function FleetBar({ project, branch, model, streaming }: {
  project?: string;
  branch?: string | null;
  model?: string | null;
  streaming?: boolean;
}) {
  return (
    <div className="w-full border-b border-white/5 flex items-center gap-4 px-6 py-1.5 bg-black/30 text-[9px] font-mono uppercase tracking-widest shrink-0">
      {project && (
        <span className="flex items-center gap-1.5 text-muted-foreground">
          <Box size={9} />
          {project}
        </span>
      )}
      {branch && (
        <span className="flex items-center gap-1.5 text-muted-foreground">
          <GitBranch size={9} />
          {branch}
        </span>
      )}
      {model && (
        <span className="flex items-center gap-1.5 text-muted-foreground">
          <Zap size={9} />
          {model}
        </span>
      )}
      {streaming && (
        <span className="flex items-center gap-1.5 text-primary">
          <div className="w-1.5 h-1.5 rounded-full bg-primary animate-pulse" />
          Live
        </span>
      )}
    </div>
  );
}

// ─── Main Component ─────────────────────────────────────────────

export const PiAgentView: React.FC<PiAgentViewProps> = ({ selectedProject, selectedAgentId = 'default', projects = [], onBack }) => {
  const { state, sendPrompt, abort, compact, switchModel, reset, hydrateState, getModels } = usePiAgent(selectedAgentId);
  const [input, setInput] = useState('');
  const [models, setModels] = useState<PiModel[]>([]);
  const [modelDropdownOpen, setModelDropdownOpen] = useState(false);
  const messagesEndRef = useRef<HTMLDivElement>(null);
  const textareaRef = useRef<HTMLTextAreaElement>(null);
  const modelDropdownRef = useRef<HTMLDivElement>(null);

  // Hydrate state from backend when project changes
  useEffect(() => {
    if (selectedProject) {
      hydrateState(selectedProject, selectedAgentId);
    }
  }, [selectedProject, selectedAgentId, hydrateState]);

  // Fetch available models on mount
  useEffect(() => {
    if (!selectedProject) return;
    getModels(selectedProject, selectedAgentId).then(setModels);
  }, [selectedProject, selectedAgentId, getModels]);

  // Close model dropdown on outside click
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

  // Auto-scroll on new content
  useEffect(() => {
    messagesEndRef.current?.scrollIntoView({ behavior: 'smooth' });
  }, [state.text, state.thinking, state.toolCalls.length]);

  const handleSend = useCallback(() => {
    if (!input.trim() || !selectedProject || state.isStreaming) return;
    sendPrompt(input.trim(), selectedProject, { agentId: selectedAgentId, model: state.model || undefined });
    setInput('');
  }, [input, selectedProject, state.isStreaming, sendPrompt, selectedAgentId]);

  const handleKeyDown = useCallback((e: React.KeyboardEvent) => {
    if (e.key === 'Enter' && !e.shiftKey) {
      e.preventDefault();
      handleSend();
    }
  }, [handleSend]);

  const activeTools = state.toolCalls.filter(tc => !tc.endTime);
  const completedTools = state.toolCalls.filter(tc => tc.endTime);
  const hasContent = state.text || state.thinking || state.isStreaming || state.toolCalls.length > 0;

  return (
    <div className="flex h-full bg-black overflow-hidden">
      {/* Main content */}
      <div className="flex-1 flex flex-col min-w-0 items-center">
        {/* Header */}
        <div className="w-full h-12 border-b border-white/5 flex items-center px-6 shrink-0 bg-black/50 backdrop-blur-md">
          <div className="flex items-center gap-2 text-[10px] font-mono tracking-widest text-muted uppercase font-bold">
            {onBack && (
              <button onClick={onBack} className="flex items-center gap-1 text-muted hover:text-zinc-300 transition-colors">
                <ArrowLeft size={12} />
              </button>
            )}
            <Zap size={12} className="text-primary" />
            <span className="text-primary">PI</span>
            <span className="text-muted">CODING AGENT</span>
          </div>
          <div className="flex-1" />
          <div className="flex items-center gap-4">
            {state.isStreaming && (
              <span className="text-[9px] font-black text-primary uppercase tracking-widest animate-pulse">
                Streaming
              </span>
            )}
            {state.error && (
              <span className="text-[9px] font-mono text-red-400">{state.error}</span>
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
          <div className="max-w-3xl mx-auto px-6 py-6 space-y-4">

            {/* Empty state */}
            {!hasContent && (
              <div className="h-full flex flex-col items-center justify-center text-center p-8 space-y-6">
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

            {/* Reasoning block */}
            {state.thinking && (
              <ReasoningBlock content={state.thinking} defaultOpen={false} />
            )}

            {/* Markdown response block */}
            {state.text && (
              <MarkdownBlock content={state.text} streaming={state.isStreaming} />
            )}

            {/* Inline tool calls in feed */}
            {completedTools.length > 0 && (
              <div className="space-y-2">
                <span className="text-[9px] font-black uppercase tracking-widest text-muted-foreground">
                  Tool Executions ({completedTools.length})
                </span>
                {completedTools.map(tc => (
                  <ToolCallItem key={tc.id} call={tc} />
                ))}
              </div>
            )}

            <div ref={messagesEndRef} />
          </div>
        </div>

        {/* Token usage */}
        {(state.tokenUsage.input > 0 || state.tokenUsage.output > 0) && (
          <div className="w-full border-t border-white/5">
            <div className="max-w-3xl mx-auto py-2 px-6 flex items-center gap-6 text-[9px] font-mono text-muted-foreground">
              <span>Tokens: {state.tokenUsage.input}in / {state.tokenUsage.output}out / {state.tokenUsage.cache}cache</span>
            </div>
          </div>
        )}

        {/* PR Created Banner */}
        {state.prUrl && !state.isStreaming && (
          <div className="w-full border-t border-primary/20 bg-primary/5">
            <div className="max-w-3xl mx-auto py-3 px-6 flex items-center gap-3">
              <div className="shrink-0 w-6 h-6 flex items-center justify-center bg-primary/20 text-primary rounded-full">
                <Check size={12} />
              </div>
              <div className="flex-1 min-w-0">
                <div className="text-[10px] font-black uppercase tracking-widest text-primary">
                  Pull Request #{state.prNumber} Created
                </div>
                <div className="text-[9px] font-mono text-muted-foreground truncate mt-0.5">
                  {state.prUrl}
                </div>
              </div>
              <a
                href={state.prUrl}
                target="_blank"
                rel="noopener noreferrer"
                className="shrink-0 flex items-center gap-1.5 px-3 py-1.5 bg-primary text-black text-[9px] font-black uppercase tracking-widest hover:bg-primary/80 transition-colors"
              >
                <ExternalLink size={10} />
                Open PR
              </a>
            </div>
          </div>
        )}

        {/* Input area */}
        <div className="w-full border-t border-white/5">
          <div className="max-w-3xl mx-auto p-4">
            <div className="flex gap-2">
              <div className="flex-1 relative">
                <textarea
                  ref={textareaRef}
                  value={input}
                  onChange={(e) => setInput(e.target.value)}
                  onKeyDown={handleKeyDown}
                  placeholder={selectedProject ? "Describe a coding task..." : "Select a project first..."}
                  disabled={state.isStreaming || !selectedProject}
                  className="w-full bg-zinc-900 border border-white/5 rounded p-4 pr-14 text-[12px] text-white placeholder-zinc-700 outline-none focus:border-primary/40 transition-all font-mono resize-none"
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
            <div className="flex items-center gap-3 mt-2">
              {/* Model selector */}
              <div className="relative" ref={modelDropdownRef}>
                <button
                  onClick={() => setModelDropdownOpen(!modelDropdownOpen)}
                  className="text-[9px] font-mono text-muted hover:text-muted-foreground flex items-center gap-1 uppercase tracking-widest"
                >
                  Model: {state.model || 'default'}
                  {modelDropdownOpen ? <ChevronUp size={10} /> : <ChevronDown size={10} />}
                </button>
                {modelDropdownOpen && models.length > 0 && (
                  <div className="absolute bottom-full left-0 mb-1 w-56 max-h-60 overflow-y-auto border border-white/10 bg-zinc-950 shadow-xl z-50">
                    {models.map((m) => (
                      <button
                        key={m.id}
                        onClick={() => {
                          if (selectedProject) switchModel(selectedProject, 'litellm', m.id, selectedAgentId);
                          setModelDropdownOpen(false);
                        }}
                        className={cn(
                          "w-full text-left px-3 py-2 text-[9px] font-mono uppercase tracking-widest transition-colors",
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
              <button onClick={reset} className="text-[9px] font-mono text-muted hover:text-muted-foreground flex items-center gap-1 uppercase tracking-widest">
                <RotateCcw size={10} /> New Task
              </button>
              <button onClick={() => selectedProject && compact(selectedProject, selectedAgentId)} disabled={state.isStreaming} className="text-[9px] font-mono text-muted hover:text-muted-foreground flex items-center gap-1 uppercase tracking-widest disabled:opacity-30">
                <Trash2 size={10} /> Compact
              </button>
            </div>
          </div>
        </div>
      </div>

      {/* Tool execution sidebar */}
      <div className="w-80 border-l border-white/5 flex flex-col bg-black shrink-0">
        <div className="p-4 border-b border-white/5 flex items-center gap-3">
          <Wrench size={12} className="text-muted-foreground" />
          <span className="text-[9px] font-black uppercase tracking-widest text-muted-foreground">
            Tool Calls ({state.toolCalls.length})
          </span>
          {activeTools.length > 0 && (
            <div className="flex items-center gap-1.5">
              <div className="w-1.5 h-1.5 rounded-full bg-primary animate-pulse" />
              <span className="text-[9px] font-mono text-primary">{activeTools.length} active</span>
            </div>
          )}
        </div>
        {state.toolCalls.length > 0 ? (
          <div className="flex-1 overflow-y-auto custom-scrollbar">
            {activeTools.map(tc => <ToolCallItem key={tc.id} call={tc} />)}
            {completedTools.map(tc => <ToolCallItem key={tc.id} call={tc} />)}
          </div>
        ) : (
          <div className="flex-1 flex flex-col items-center justify-center p-6">
            <Wrench size={20} className="text-zinc-800 mb-3" />
            <p className="text-[9px] font-mono text-zinc-700 uppercase tracking-widest text-center">
              No tool calls yet
            </p>
          </div>
        )}
      </div>
    </div>
  );
};
