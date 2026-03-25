import React, { useState, useRef, useEffect } from 'react';
import { Terminal as TerminalIcon, ChevronRight, X, Help } from 'lucide-react';
import { cn } from '../lib/utils';

interface CLITerminalProps {
  isOpen: boolean;
  onClose: () => void;
}

interface CommandOutput {
  command: string;
  output: string;
  error?: string;
  timestamp: string;
}

export const CLITerminal: React.FC<CLITerminalProps> = ({ isOpen, onClose }) => {
  const [history, setHistory] = useState<CommandOutput[]>([]);
  const [command, setCommand] = useState('');
  const [isLoading, setIsLoading] = useState(false);
  const outputRef = useRef<HTMLDivElement>(null);

  // Allowed commands
  const allowedCommands = ['ls', 'cat', 'pwd', 'whoami', 'date', 'uname'];

  // Auto-scroll to bottom
  useEffect(() => {
    if (outputRef.current) {
      outputRef.current.scrollTop = outputRef.current.scrollHeight;
    }
  }, [history]);

  // Focus input on open
  const inputRef = useRef<HTMLInputElement>(null);
  useEffect(() => {
    if (isOpen && inputRef.current) {
      inputRef.current.focus();
    }
  }, [isOpen]);

  const executeCommand = async (cmd: string) => {
    const trimmedCmd = cmd.trim();
    if (!trimmedCmd) return;

    // Parse command and args
    const parts = trimmedCmd.split(/\s+/);
    const commandName = parts[0];
    const args = parts.slice(1);

    // Check if command is allowed
    if (!allowedCommands.includes(commandName)) {
      setHistory(prev => [...prev, {
        command: trimmedCmd,
        output: '',
        error: `Command '${commandName}' is not allowed. Allowed commands: ${allowedCommands.join(', ')}`,
        timestamp: new Date().toLocaleTimeString()
      }]);
      return;
    }

    setIsLoading(true);

    try {
      const response = await fetch('/api/cli/execute', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          command: commandName,
          args: args
        })
      });

      const data = await response.json();

      setHistory(prev => [...prev, {
        command: trimmedCmd,
        output: data.success ? data.output : '',
        error: data.error,
        timestamp: new Date().toLocaleTimeString()
      }]);
    } catch (error: any) {
      setHistory(prev => [...prev, {
        command: trimmedCmd,
        output: '',
        error: `Failed to execute: ${error.message}`,
        timestamp: new Date().toLocaleTimeString()
      }]);
    } finally {
      setIsLoading(false);
      setCommand('');
    }
  };

  const handleKeyDown = (e: React.KeyboardEvent<HTMLInputElement>) => {
    if (e.key === 'Enter') {
      executeCommand(command);
    }
  };

  const showHelp = () => {
    setHistory(prev => [...prev, {
      command: 'help',
      output: `Available commands:
  ls [path]     - List directory contents
  cat <file>    - Read file contents
  pwd           - Print working directory
  whoami        - Display current user
  date          - Display current date/time
  uname         - Display system information

Examples:
  ls -la
  ls src/
  cat README.md
  pwd
  whoami`,
      timestamp: new Date().toLocaleTimeString()
    }]);
  };

  if (!isOpen) return null;

  return (
    <div className="fixed inset-0 z-50 flex items-end justify-center pointer-events-none">
      {/* Backdrop */}
      <div 
        className="absolute inset-0 bg-black/60 backdrop-blur-sm pointer-events-auto"
        onClick={onClose}
      />

      {/* Terminal Window */}
      <div className="relative w-full max-w-4xl mx-4 mb-4 glass border border-white/10 rounded-xl overflow-hidden shadow-2xl pointer-events-auto animate-in slide-in-from-bottom-10">
        {/* Header */}
        <div className="h-12 glass-dark border-b border-white/10 flex items-center justify-between px-6 shrink-0">
          <div className="flex items-center gap-3">
            <div className="flex gap-2">
              <div className="w-3 h-3 rounded-full bg-red-500/20 border border-red-500/40" />
              <div className="w-3 h-3 rounded-full bg-amber-500/20 border border-amber-500/40" />
              <div className="w-3 h-3 rounded-full bg-emerald-500/20 border border-emerald-500/40" />
            </div>
            <div className="ml-4 flex items-center gap-2">
              <TerminalIcon size={14} className="text-primary/70" />
              <span className="text-[10px] font-bold tracking-[0.2em] text-zinc-400 uppercase">
                CLI Terminal — Go Backend
              </span>
            </div>
          </div>
          <div className="flex items-center gap-2">
            <button
              onClick={showHelp}
              className="text-zinc-500 hover:text-white transition-colors p-1 hover:bg-white/5 rounded"
              title="Show help"
            >
              <Help size={16} />
            </button>
            <button
              onClick={onClose}
              className="text-zinc-500 hover:text-white transition-colors p-1 hover:bg-white/5 rounded"
              title="Close"
            >
              <X size={16} />
            </button>
          </div>
        </div>

        {/* Output Area */}
        <div 
          ref={outputRef}
          className="h-96 overflow-y-auto p-6 font-mono text-[11px] leading-relaxed text-zinc-300 terminal-scrollbar bg-black/40 select-text"
        >
          {/* Welcome Message */}
          <div className="mb-4 text-zinc-400">
            <div className="text-primary font-bold mb-2">Go Backend CLI Terminal</div>
            <div>Connected to: <span className="text-emerald-400">http://localhost:3847</span></div>
            <div>Type <span className="text-amber-400">help</span> for available commands.</div>
            <div className="mt-2 text-zinc-500">────────────────────────────────────────</div>
          </div>

          {/* Command History */}
          {history.map((item, i) => (
            <div key={i} className="mb-4">
              <div className="flex items-center gap-2 mb-1">
                <ChevronRight size={12} className="text-primary" />
                <span className="text-zinc-500 text-[10px]">{item.timestamp}</span>
                <span className="text-white font-bold">{item.command}</span>
              </div>
              {item.output && (
                <pre className="text-zinc-300 whitespace-pre-wrap ml-4 border-l-2 border-white/5 pl-3">
                  {item.output}
                </pre>
              )}
              {item.error && (
                <div className="text-rose-400 ml-4 border-l-2 border-rose-500/30 pl-3">
                  {item.error}
                </div>
              )}
            </div>
          ))}

          {/* Loading Indicator */}
          {isLoading && (
            <div className="mb-4 ml-4 text-amber-400 animate-pulse">
              Executing...
            </div>
          )}

          {/* Input Line */}
          <div className="flex items-center gap-2 mt-4 pt-4 border-t border-white/5">
            <ChevronRight size={16} className="text-primary" />
            <input
              ref={inputRef}
              type="text"
              value={command}
              onChange={(e) => setCommand(e.target.value)}
              onKeyDown={handleKeyDown}
              placeholder="Enter command..."
              className="flex-1 bg-transparent border-none outline-none text-white font-mono text-[11px]"
              disabled={isLoading}
              autoComplete="off"
              autoCorrect="off"
              autoCapitalize="off"
            />
          </div>
        </div>

        {/* Footer */}
        <div className="h-8 glass-dark border-t border-white/5 flex items-center justify-between px-6 text-[10px] text-zinc-500">
          <div>
            Allowed: <span className="text-zinc-400">{allowedCommands.join(', ')}</span>
          </div>
          <div className="flex items-center gap-4">
            <span>Press <kbd className="px-1.5 py-0.5 bg-white/5 rounded text-zinc-400">Enter</kbd> to execute</span>
            <span className="flex items-center gap-1">
              <div className={`w-2 h-2 rounded-full ${isLoading ? 'bg-amber-400 animate-pulse' : 'bg-emerald-400'}`} />
              {isLoading ? 'Executing' : 'Ready'}
            </span>
          </div>
        </div>
      </div>
    </div>
  );
};
