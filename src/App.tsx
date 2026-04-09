import React from 'react';
import { AppShell } from './components/AppShell';
import { ErrorBoundary } from './components/ui/ErrorBoundary';

export default function App() {
  return (
    <ErrorBoundary>
      <AppShell />
    </ErrorBoundary>
  );
}
