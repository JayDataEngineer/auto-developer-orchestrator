import { useState, useRef, useEffect, useCallback } from 'react';

export interface LogEntry {
  timestamp: string;
  type: 'info' | 'warn' | 'error' | 'success' | 'system' | 'command';
  message: string;
}

export const useTerminal = () => {
  const [logs, setLogs] = useState<string[]>([]);
  const logEndRef = useRef<HTMLDivElement>(null);

  const addLog = useCallback((message: string, type: 'INFO' | 'WARN' | 'ERROR' | 'SUCCESS' | 'SYSTEM' | 'COMMAND' = 'INFO') => {
    const timestamp = new Date().toLocaleTimeString();
    const formattedLog = `[${timestamp}] ${type}: ${message}`;
    setLogs(prev => [...prev, formattedLog]);
  }, []);

  const clearLogs = useCallback(() => {
    setLogs([]);
    addLog('System logs cleared.', 'SYSTEM');
  }, [addLog]);

  useEffect(() => {
    logEndRef.current?.scrollIntoView({ behavior: 'smooth' });
  }, [logs]);

  const processCommand = useCallback((cmd: string, handlers: Record<string, () => void>) => {
    addLog(`$ ${cmd}`, 'COMMAND');
    
    const normalizedCmd = cmd.toLowerCase().trim();
    
    if (normalizedCmd === 'clear') {
      clearLogs();
      return;
    }

    if (handlers[normalizedCmd]) {
      handlers[normalizedCmd]();
    } else if (normalizedCmd === 'help') {
      addLog('Available commands: gen, retry, clear, help, debug', 'SYSTEM');
    } else {
      addLog(`Command not found: ${cmd}`, 'ERROR');
    }
  }, [addLog, clearLogs]);

  return {
    logs,
    logEndRef,
    addLog,
    clearLogs,
    processCommand
  };
};
