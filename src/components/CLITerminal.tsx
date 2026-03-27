import React, { useState, useRef, useEffect } from 'react';
import { Terminal as TerminalIcon, ChevronRight, X, HelpCircle, Maximize2, Minimize2 } from 'lucide-react';
import { motion, AnimatePresence } from 'motion/react';
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
  const [isMaximized, setIsMaximized] = useState(false);
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

  return (
    <AnimatePresence>
      {isOpen && (
        <motion.div 
          initial={{ y: "100%" }}
          animate={{ y: 0 }}
          exit={{ y: "100%" }}
          transition={{ type: "spring", damping: 20, stiffness: 100 }}
          className={cn(
            "fixed bottom-0 left-0 right-0 z-40 bg-black border-t border-white/10 shadow-[0_-10px_40px_rgba(0,0,0,0.8)] flex flex-col pointer-events-auto",
            isMaximized ? "h-[80vh]" : "h-96"
          )}
        >
          {/* Header/Grab-handle */}
          <div className="h-10 bg-zinc-900/50 border-b border-white/5 flex items-center justify-between px-6 shrink-0 cursor-default select-none">
            <div className="flex items-center gap-4">
              <div className="flex items-center gap-2">
                <TerminalIcon size={14} className="text-primary" />
                <span className="text-[10px] font-black uppercase tracking-[0.3em] text-white">
                  CLI Terminal // DOCKED
                </span>
              </div>
              <div className="text-[9px] font-mono text-zinc-600 uppercase tracking-widest hidden sm:block">
                SYS_ID: 0x3A2B // PORT: 3848
              </div>
            </div>
            
            <div className="flex items-center gap-4">
              <button
                onClick={showHelp}
                className="text-zinc-500 hover:text-white transition-colors"
                title="Help"
              >
                <HelpCircle size={14} />
              </button>
              <button
                onClick={() => setIsMaximized(!isMaximized)}
                className="text-zinc-500 hover:text-white transition-colors"
                title={isMaximized ? "Minimize" : "Maximize"}
              >
                {isMaximized ? <Minimize2 size={14} /> : <Maximize2 size={14} />}
              </button>
              <button
                onClick={onClose}
                className="text-zinc-500 hover:text-rose-500 transition-colors"
                title="Close"
              >
                <X size={16} />
              </button>
            </div>
          </div>

          {/* Output Area */}
          <div 
            ref={outputRef}
            className="flex-1 overflow-y-auto p-6 font-mono text-[11px] leading-relaxed text-zinc-400 custom-scrollbar select-text bg-[#020202]"
          >
            {/* Welcome */}
            <div className="mb-6 opacity-40">
              <div className="text-primary font-black uppercase tracking-[0.2em] mb-1">Architecture Interface v1.0.4</div>
              <div>Connection: local_loopback // root_authorized</div>
              <div>────────────────────────────────────────</div>
            </div>

            {/* History */}
            {history.map((item, i) => (
              <div key={i} className="mb-4 group">
                <div className="flex items-center gap-3 mb-1">
                  <ChevronRight size={12} className="text-primary opacity-0 group-hover:opacity-100 transition-opacity" />
                  <span className="text-white font-bold">{item.command}</span>
                  <span className="text-[8px] text-zinc-700 uppercase">{item.timestamp}</span>
                </div>
                {item.output && (
                  <pre className="text-zinc-400 whitespace-pre-wrap ml-6 border-l border-white/5 pl-4 py-1">
                    {item.output}
                  </pre>
                )}
                {item.error && (
                  <div className="text-rose-900/80 ml-6 border-l border-rose-950 pl-4 py-1 italic">
                    {item.error}
                  </div>
                )}
              </div>
            ))}

            {/* Input Line */}
            <div className="flex items-center gap-3 mt-6 pt-4 border-t border-white/5 sticky bottom-0 bg-[#020202] py-2">
              <ChevronRight size={16} className="text-primary animate-pulse" />
              <input
                ref={inputRef}
                type="text"
                value={command}
                onChange={(e) => setCommand(e.target.value)}
                onKeyDown={handleKeyDown}
                placeholder="Enter command..."
                className="flex-1 bg-transparent border-none outline-none text-white font-mono text-[11px] placeholder-zinc-800"
                disabled={isLoading}
                autoFocus
              />
            </div>
          </div>

          {/* Industrial Status Bar */}
          <div className="h-6 bg-zinc-950 border-t border-white/5 flex items-center justify-between px-6 text-[8px] font-mono text-zinc-600 uppercase tracking-widest">
            <div className="flex items-center gap-4">
              <div className="flex items-center gap-1.5">
                <div className={cn("w-1.5 h-1.5 rounded-full", isLoading ? "bg-amber-500 animate-pulse glow-amber" : "bg-emerald-500 glow-emerald")} />
                <span>{isLoading ? 'Processing...' : 'System Ready'}</span>
              </div>
              <span>Buffer: optimal</span>
            </div>
            <div className="flex gap-4">
              <span>Encoding: u-utf8</span>
              <span>PID: 1042</span>
            </div>
          </div>
        </motion.div>
      )}
    </AnimatePresence>
  );
};
