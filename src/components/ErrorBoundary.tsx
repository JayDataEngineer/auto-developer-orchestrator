import React, { Component, ErrorInfo, ReactNode } from 'react';
import { AlertTriangle } from 'lucide-react';

interface Props {
  children?: ReactNode;
}

interface State {
  hasError: boolean;
  error?: Error;
}

export class ErrorBoundary extends Component<Props, State> {
  public state: State = {
    hasError: false
  };

  public static getDerivedStateFromError(error: Error): State {
    return { hasError: true, error };
  }

  public componentDidCatch(error: Error, errorInfo: ErrorInfo) {
    console.error('Uncaught error:', error, errorInfo);
  }

  public render() {
    if (this.state.hasError) {
      return (
        <div className="flex h-screen items-center justify-center bg-black text-white p-6 font-mono border-4 border-error/20">
          <div className="max-w-md w-full bg-secondary border border-border p-6 rounded-none shadow-2xl relative overflow-hidden">
            <div className="bg-error/10 border-b border-error/20 -mx-6 -mt-6 p-4 mb-6 flex items-center gap-3">
              <AlertTriangle className="text-error" size={24} />
              <h2 className="text-sm font-bold tracking-widest uppercase text-error">CRITICAL_SYSTEM_FAILURE</h2>
            </div>
            
            <p className="text-xs text-zinc-400 leading-relaxed mb-6">
              The Auto-Developer Orchestrator has encountered an unexpected exception in the UI thread. State may be corrupted. 
            </p>
            
            <div className="bg-black/50 p-4 border border-border/50 overflow-x-auto mb-6">
              <pre className="text-sm text-error font-mono">
                {this.state.error?.message || 'Unknown Exception'}
                {this.state.error?.stack && `\n\n${this.state.error.stack.slice(0, 200)}...`}
              </pre>
            </div>
            
            <button 
              onClick={() => window.location.reload()}
              className="w-full py-2 bg-primary border border-primary/50 text-black text-sm font-bold hover:bg-primary/80 transition-all uppercase tracking-widest"
            >
              REBOOT_INTERFACE
            </button>
          </div>
        </div>
      );
    }

    return this.props.children;
  }
}
