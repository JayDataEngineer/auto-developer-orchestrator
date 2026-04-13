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
