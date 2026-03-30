import React, { useState, useRef, useEffect } from 'react';
import {
  Send, Square, Sparkles, ChevronDown, ChevronRight, Trash2,
  FileCode, Terminal as TerminalIcon, Search, Wrench, Brain,
  Loader, Zap, RotateCcw
} from 'lucide-react';
import { cn } from '../lib/utils';
import { usePiAgent } from '../hooks/usePiAgent';
import { ToolCall } from '../lib/pi-events';

interface PiAgentViewProps {
  selectedProject?: string;
  projects?: string[];
}

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
  if (typeof result === 'string') return result.slice(0, 200);
  return JSON.stringify(result).slice(0, 200);
}

function ToolCallItem({ call }: { call: ToolCall }) {
  const [expanded, setExpanded] = useState(false);
  const isActive = !call.endTime;
  const duration = call.endTime ? call.endTime - call.startTime : Date.now() - call.startTime;

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
          isActive ? "text-primary" : "text-zinc-500"
        )}>
          {TOOL_ICONS[call.name] || <Wrench size={12} />}
        </div>
        <div className="flex-1 min-w-0">
          <div className="flex items-center gap-2">
            <span className="text-[10px] font-black uppercase tracking-widest text-white">
              {call.name}
            </span>
            <span className="text-[9px] font-mono text-zinc-600 truncate">
              {formatToolArgs(call.name, call.args)}
            </span>
          </div>
        </div>
        <div className="flex items-center gap-2 shrink-0">
          {isActive ? (
            <Loader size={10} className="text-primary animate-spin" />
          ) : (
            <span className="text-[9px] font-mono text-zinc-600">{duration}ms</span>
          )}
          {expanded ? <ChevronDown size={10} className="text-zinc-600" /> : <ChevronRight size={10} className="text-zinc-600" />}
        </div>
      </button>
      {expanded && (
        <div className="px-3 pb-3 border-t border-border">
          {call.result !== undefined && (
            <pre className="text-[10px] font-mono text-zinc-400 mt-2 whitespace-pre-wrap max-h-40 overflow-auto">
              {formatResult(call.result)}
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

export const PiAgentView: React.FC<PiAgentViewProps> = ({ selectedProject, projects = [] }) => {
  const { state, sendPrompt, abort, compact, switchModel, reset, hydrateState } = usePiAgent();
  const [input, setInput] = useState('');
  const [showThinking, setShowThinking] = useState(false);
  const [showTools, setShowTools] = useState(true);
  const messagesEndRef = useRef<HTMLDivElement>(null);
  const textareaRef = useRef<HTMLTextAreaElement>(null);

  // Hydrate state from backend when project changes
  useEffect(() => {
    if (selectedProject) {
      hydrateState(selectedProject);
    }
  }, [selectedProject, hydrateState]);

  useEffect(() => {
    messagesEndRef.current?.scrollIntoView({ behavior: 'smooth' });
  }, [state.text, state.thinking]);

  const handleSend = () => {
    if (!input.trim() || !selectedProject || state.isStreaming) return;
    sendPrompt(input.trim(), selectedProject);
    setInput('');
  };

  const handleKeyDown = (e: React.KeyboardEvent) => {
    if (e.key === 'Enter' && !e.shiftKey) {
      e.preventDefault();
      handleSend();
    }
  };

  const handleAbort = () => {
    if (selectedProject) abort(selectedProject);
  };

  const handleCompact = () => {
    if (selectedProject) compact(selectedProject);
  };

  const activeTools = state.toolCalls.filter(tc => !tc.endTime);
  const completedTools = state.toolCalls.filter(tc => tc.endTime);

  return (
    <div className="flex h-full bg-black overflow-hidden">
      {/* Main content */}
      <div className="flex-1 flex flex-col min-w-0">
        {/* Header */}
        <div className="h-12 border-b border-white/5 flex items-center px-6 shrink-0 bg-black/50 backdrop-blur-md">
          <div className="flex items-center gap-2 text-[10px] font-mono tracking-widest text-zinc-500 uppercase font-bold">
            <Zap size={12} className="text-primary" />
            <span className="text-primary">PI</span>
            <span className="text-zinc-700">CODING AGENT</span>
          </div>
          <div className="flex-1" />
          <div className="flex items-center gap-4">
            {state.model && (
              <span className="text-[9px] font-mono text-zinc-600">{state.model}</span>
            )}
            {state.isStreaming && (
              <div className="flex items-center gap-2">
                <div className="w-1.5 h-1.5 rounded-full bg-primary animate-pulse" />
                <span className="text-[9px] font-black text-primary uppercase tracking-widest">Streaming</span>
              </div>
            )}
            {state.error && (
              <span className="text-[9px] font-mono text-red-400">{state.error}</span>
            )}
          </div>
        </div>

        {/* Response area */}
        <div className="flex-1 overflow-y-auto p-6 space-y-6 custom-scrollbar">
          {!state.text && !state.thinking && !state.isStreaming && state.toolCalls.length === 0 && (
            <div className="h-full flex flex-col items-center justify-center text-center p-8 space-y-6">
              <div className="w-16 h-16 border border-primary flex items-center justify-center text-primary">
                <Sparkles size={32} className="animate-pulse-slow" />
              </div>
              <div className="space-y-3 max-w-md">
                <h3 className="text-lg font-bold text-white tracking-widest uppercase">Pi Agent Ready</h3>
                <p className="text-xs text-zinc-600 leading-relaxed font-mono">
                  Describe a coding task and Pi will implement it with real file editing, bash execution, and intelligent analysis.
                </p>
              </div>
              <div className="text-[9px] text-zinc-700 font-mono uppercase tracking-widest">
                {selectedProject ? `Project: ${selectedProject}` : 'Select a project to begin'}
              </div>
            </div>
          )}

          {/* Streaming text */}
          {state.text && (
            <div className="max-w-none">
              <div className="text-[12px] leading-relaxed text-zinc-300 whitespace-pre-wrap font-mono">
                {state.text}
                {state.isStreaming && (
                  <span className="inline-block w-2 h-4 bg-primary/60 animate-pulse ml-0.5" />
                )}
              </div>
            </div>
          )}

          {/* Thinking panel */}
          {state.thinking && (
            <div className="border border-zinc-800 bg-zinc-950">
              <button
                onClick={() => setShowThinking(!showThinking)}
                className="w-full flex items-center gap-3 p-3 text-left"
              >
                <Brain size={12} className="text-zinc-600" />
                <span className="text-[9px] font-black uppercase tracking-widest text-zinc-600">
                  Agent Thinking
                </span>
                <div className="flex-1" />
                {showThinking ? <ChevronDown size={10} className="text-zinc-600" /> : <ChevronRight size={10} className="text-zinc-600" />}
              </button>
              {showThinking && (
                <div className="px-3 pb-3 border-t border-zinc-800">
                  <pre className="text-[10px] font-mono text-zinc-500 whitespace-pre-wrap">
                    {state.thinking}
                  </pre>
                </div>
              )}
            </div>
          )}

          <div ref={messagesEndRef} />
        </div>

        {/* Token usage footer */}
        {(state.tokenUsage.input > 0 || state.tokenUsage.output > 0) && (
          <div className="px-6 py-2 border-t border-white/5 flex items-center gap-6 text-[9px] font-mono text-zinc-600">
            <span>Tokens: {state.tokenUsage.input}in / {state.tokenUsage.output}out / {state.tokenUsage.cache}cache</span>
          </div>
        )}

        {/* Input area */}
        <div className="p-4 border-t border-white/5">
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
                  <button
                    onClick={handleAbort}
                    className="p-2 bg-red-500/20 text-red-400 rounded hover:bg-red-500/30 transition-all"
                  >
                    <Square size={16} />
                  </button>
                ) : (
                  <button
                    onClick={handleSend}
                    disabled={!input.trim() || !selectedProject}
                    className="p-2 bg-primary text-black rounded hover:bg-primary/80 disabled:opacity-20 transition-all"
                  >
                    <Send size={16} />
                  </button>
                )}
              </div>
            </div>
          </div>
          <div className="flex items-center gap-3 mt-2">
            <button
              onClick={reset}
              className="text-[9px] font-mono text-zinc-700 hover:text-zinc-400 flex items-center gap-1 uppercase tracking-widest"
            >
              <RotateCcw size={10} /> New Task
            </button>
            <button
              onClick={handleCompact}
              disabled={state.isStreaming}
              className="text-[9px] font-mono text-zinc-700 hover:text-zinc-400 flex items-center gap-1 uppercase tracking-widest disabled:opacity-30"
            >
              <Trash2 size={10} /> Compact
            </button>
          </div>
        </div>
      </div>

      {/* Tool execution sidebar */}
      {state.toolCalls.length > 0 && (
        <div className="w-80 border-l border-white/5 flex flex-col bg-black shrink-0">
          <button
            onClick={() => setShowTools(!showTools)}
            className="p-4 border-b border-white/5 flex items-center gap-3 text-left"
          >
            <Wrench size={12} className="text-zinc-600" />
            <span className="text-[9px] font-black uppercase tracking-widest text-zinc-600">
              Tool Calls ({state.toolCalls.length})
            </span>
            {activeTools.length > 0 && (
              <div className="flex items-center gap-1.5">
                <div className="w-1.5 h-1.5 rounded-full bg-primary animate-pulse" />
                <span className="text-[9px] font-mono text-primary">{activeTools.length} active</span>
              </div>
            )}
            <div className="flex-1" />
            {showTools ? <ChevronDown size={10} className="text-zinc-600" /> : <ChevronRight size={10} className="text-zinc-600" />}
          </button>
          {showTools && (
            <div className="flex-1 overflow-y-auto custom-scrollbar">
              {activeTools.map(tc => (
                <ToolCallItem key={tc.id} call={tc} />
              ))}
              {completedTools.map(tc => (
                <ToolCallItem key={tc.id} call={tc} />
              ))}
            </div>
          )}
        </div>
      )}
    </div>
  );
};
