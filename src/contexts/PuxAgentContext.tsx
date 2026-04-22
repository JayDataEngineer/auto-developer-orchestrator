import React, { createContext, useContext, useState, useCallback, useRef, useEffect, useMemo } from 'react';
import { usePuxAgent } from '../hooks/usePuxAgent';
import { PuxAgentState } from '../hooks/agentReducer';
import { SubAgentInfo } from '../lib/api';
import { ToolCall, PuxModel, AssistantMessage } from '../lib/pux-events';

interface PuxAgentContextValue {
  state: PuxAgentState;
  sendPrompt: (message: string, project: string, opts?: any) => Promise<void>;
  abort: (project: string, agentId?: string) => Promise<void>;
  compact: (project: string, agentId?: string) => Promise<void>;
  switchModel: (project: string, provider: string, modelId: string, agentId?: string) => Promise<void>;
  getModels: (project: string, agentId?: string) => Promise<PuxModel[]>;
  reset: () => void;
  hydrateState: (project: string, agentId?: string) => Promise<void>;
  loadHistory: (project: string, agentId?: string) => Promise<void>;
  respondToApproval: (project: string, agentId: string, requestId: string, action: 'approve' | 'deny' | 'answer', message?: string) => Promise<void>;
}

const PuxAgentContext = createContext<PuxAgentContextValue | undefined>(undefined);

export const PuxAgentProvider: React.FC<{ children: React.ReactNode }> = ({ children }) => {
  // For now, we share a single agent state (default).
  // In a more complex app, we might have a map of states per agentId.
  const pux = usePuxAgent('default');

  return (
    <PuxAgentContext.Provider value={pux}>
      {children}
    </PuxAgentContext.Provider>
  );
};

export const usePuxAgentContext = () => {
  const context = useContext(PuxAgentContext);
  if (!context) {
    throw new Error('usePuxAgentContext must be used within a PuxAgentProvider');
  }
  return context;
};
