import React from 'react';

interface ErrorBoundaryProps {
  children: React.ReactNode;
  fallback?: React.ReactNode;
}

interface ErrorBoundaryState {
  hasError: boolean;
  error: Error | null;
}

export class ErrorBoundary extends React.Component<ErrorBoundaryProps, ErrorBoundaryState> {
  constructor(props: ErrorBoundaryProps) {
    super(props);
    this.state = { hasError: false, error: null };
  }

  static getDerivedStateFromError(error: Error): ErrorBoundaryState {
    return { hasError: true, error };
  }

  render() {
    if (this.state.hasError) {
      if (this.props.fallback) return this.props.fallback;

      return (
        <div className="flex items-center justify-center h-screen bg-black text-white p-8">
          <div className="max-w-md text-center space-y-4">
            <div className="text-red-400 text-sm font-mono uppercase tracking-widest">
              Something went wrong
            </div>
            <pre className="text-[11px] font-mono text-zinc-400 bg-zinc-900 border border-white/5 p-4 overflow-auto max-h-48 text-left">
              {this.state.error?.message || 'Unknown error'}
            </pre>
            <button
              onClick={() => this.setState({ hasError: false, error: null })}
              className="px-4 py-2 bg-primary text-black text-[9px] font-black uppercase tracking-widest hover:bg-primary/80 transition-colors"
            >
              Try Again
            </button>
          </div>
        </div>
      );
    }

    return this.props.children;
  }
}
