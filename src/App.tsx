import React from 'react';
import { AppShell } from './components/AppShell';
import { ErrorBoundary } from './components/ui/ErrorBoundary';
import { PiAgentProvider } from './contexts/PiAgentContext';

export default function App() {
  return (
    <ErrorBoundary>
      <PiAgentProvider>
        <AppShell />
      </PiAgentProvider>
    </ErrorBoundary>
  );
}

// Apply theme immediately to prevent flash of wrong theme
(function initTheme() {
  const stored = localStorage.getItem('pi-theme') as 'light' | 'dark' | 'system' | null;
  const theme = stored || 'system';
  const dark = theme === 'dark' || (theme === 'system' && window.matchMedia('(prefers-color-scheme: dark)').matches);
  if (dark) {
    document.documentElement.classList.add('dark');
  }
})();
