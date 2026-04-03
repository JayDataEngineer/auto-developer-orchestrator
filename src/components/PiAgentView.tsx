import React, { useState, useRef, useEffect, useCallback } from 'react';
import {
  Send, Square, Sparkles, ChevronDown, ChevronRight, Trash2,
  FileCode, Terminal as TerminalIcon, Search, Wrench, Brain,
  Loader, Zap, RotateCcw, ArrowLeft, ChevronUp, GitBranch, Box,
  ExternalLink, Check, Maximize2, Minimize2, File, GitPullRequest
} from 'lucide-react';
import ReactMarkdown from 'react-markdown';
import remarkGfm from 'remark-gfm';
import { Prism as SyntaxHighlighter } from 'react-syntax-highlighter';
import { oneDark } from 'react-syntax-highlighter/dist/esm/styles/prism';
import { cn } from '../lib/utils';
import { usePiAgent } from '../hooks/usePiAgent';
import { ToolCall, PiModel, ConversationMessage, AssistantMessage } from '../lib/pi-events';

interface PiAgentViewProps {
  selectedProject?: string;
  selectedAgentId?: string;
  projects?: string[];
  onBack?: () => void;
  isZenMode?: boolean;
  onZenToggle?: () => void;
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

// ─── Tool Call Item ─────────────────────────────────────────────
function ToolCallItem({ tc }: { tc: ToolCall }) {
  const [open, setOpen] = useState(false);
  const isRunning = !tc.endTime;
  return (
    <div className="border border-white/5 bg-zinc-950">
      <button
        onClick={() => setOpen(!open)}
        className="w-full flex items-center gap-2 px-3 py-2 text-left"
      >
        {TOOL_ICONS[tc.name] || <Wrench size={11} className="text-muted-foreground" />}
        <span className="text-[9px] font-mono uppercase tracking-widest text-muted-foreground">
          {tc.name}
        </span>
        <span className="text-[9px] font-mono text-zinc-600 truncate">
          {formatToolArgs(tc.name, tc.args)}
        </span>
        <div className="flex-1" />
        {isRunning ? (
          <Loader size={10} className="text-primary animate-spin" />
        ) : (
          <ChevronRight size={10} className={cn("text-muted-foreground transition-transform", open && "rotate-90")} />
        )}
      </button>
      {open && !isRunning && (
        <div className="px-3 pb-2 border-t border-white/5">
          <pre className="text-[9px] font-mono text-zinc-400 whitespace-pre-wrap max-h-40 overflow-auto">
            {formatResult(tc.result)}
          </pre>
        </div>
      )}
    </div>
  );
}

// ─── Block Components ───────────────────────────────────────────

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
          code({ className, children, ...props }: any) {
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

function ArtifactItem({ type, name, subtitle, status, onClick, href }: { 
  type: 'file' | 'pr'; 
  name: string; 
  subtitle?: string; 
  status?: 'active' | 'completed';
  onClick?: () => void;
  href?: string;
}) {
  const Icon = type === 'pr' ? GitPullRequest : File;
  
  const content = (
    <div className={cn(
      "border border-white/5 bg-black hover:bg-white/5 transition-all p-3 flex items-start gap-4 h-full",
      status === 'active' ? "border-primary/30 bg-primary/5" : ""
    )}>
      <div className={cn(
        "shrink-0 w-8 h-8 flex items-center justify-center border border-white/5",
        status === 'active' ? "text-primary border-primary/20" : "text-muted-foreground"
      )}>
        <Icon size={14} />
      </div>
      <div className="flex-1 min-w-0">
        <div className="flex items-center gap-2">
          <span className="text-[10px] font-black uppercase tracking-widest text-white truncate">
            {name}
          </span>
          {status === 'active' && <Loader size={8} className="text-primary animate-spin" />}
        </div>
        {subtitle && (
          <div className="text-[9px] font-mono text-muted-foreground truncate mt-1">
            {subtitle}
          </div>
        )}
      </div>
      {href && <ExternalLink size={10} className="text-muted-foreground shrink-0 mt-1" />}
    </div>
  );

  if (href) {
    return <a href={href} target="_blank" rel="noopener noreferrer" className="block h-full">{content}</a>;
  }

  return <button onClick={onClick} className="block w-full text-left h-full">{content}</button>;
}

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

export const PiAgentView: React.FC<PiAgentViewProps> = ({ selectedProject, selectedAgentId = 'default', projects = [], onBack, isZenMode = false, onZenToggle }) => {
  const { state, sendPrompt, abort, compact, switchModel, reset, hydrateState, getModels, loadHistory } = usePiAgent(selectedAgentId);
  const [input, setInput] = useState('');
  const [models, setModels] = useState<PiModel[]>([]);
  const [modelDropdownOpen, setModelDropdownOpen] = useState(false);
  const messagesEndRef = useRef<HTMLDivElement>(null);
  const textareaRef = useRef<HTMLTextAreaElement>(null);
  const modelDropdownRef = useRef<HTMLDivElement>(null);
  
  // Computer Use Mode state
  const [isBrowserModeActive, setIsBrowserModeActive] = useState(false);
  const [isDesktopModeActive, setIsDesktopModeActive] = useState(false);
  const sandboxId = selectedProject ? `sandbox-${selectedProject}-${selectedAgentId}` : '';

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

  useEffect(() => {
    messagesEndRef.current?.scrollIntoView({ behavior: 'smooth' });
  }, [state.text, state.thinking, state.toolCalls.length]);

  const handleSend = useCallback(() => {
    if (!input.trim() || !selectedProject || state.isStreaming) return;
    sendPrompt(input.trim(), selectedProject, { agentId: selectedAgentId, model: state.model || 'or-free' });
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
  const hasContent = state.messages.length > 0 || state.isStreaming;

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
            <div className="flex items-center gap-2 text-[10px] font-mono tracking-widest text-muted uppercase font-bold">
              <Zap size={12} className="text-primary" />
              <span className="text-primary">PI</span>
              <span className="text-muted">CODING AGENT</span>
            </div>
          </div>
          <div className="flex-1" />
          <div className="flex items-center gap-3">
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
                      <span className="text-[9px] font-mono text-muted-foreground uppercase tracking-widest">Thinking...</span>
                    </div>
                  )}
                </div>
              );
            })}

            {/* Inline artifact summaries in feed (optional, keep it simple) */}
            
            <div ref={messagesEndRef} />
          </div>
        </div>

        {/* Token usage */}
        {(state.tokenUsage.input > 0 || state.tokenUsage.output > 0) && (
          <div className="w-full border-t border-white/5">
            <div className={cn(
              "mx-auto py-2 px-6 flex items-center gap-6 text-[9px] font-mono text-muted-foreground transition-all duration-500",
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
            <div className="flex items-center gap-4 mt-2">
              <div className="relative" ref={modelDropdownRef}>
                <button
                  onClick={() => setModelDropdownOpen(!modelDropdownOpen)}
                  className="text-[9px] font-mono text-muted hover:text-muted-foreground flex items-center gap-1 uppercase tracking-widest"
                >
                  Model: {state.model || 'default'}
                  {modelDropdownOpen ? <ChevronUp size={10} /> : <ChevronDown size={10} />}
                </button>
                {modelDropdownOpen && models.length > 0 && (
                  <div className="absolute bottom-full left-0 mb-1 w-64 max-h-[300px] overflow-y-auto border border-white/10 bg-zinc-950 shadow-2xl z-[100] custom-scrollbar scrollbar-gutter-stable">
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
    </div>
  );
};
