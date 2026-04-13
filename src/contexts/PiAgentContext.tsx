import React, { createContext, useContext, useState, useCallback, useRef, useEffect, useMemo } from 'react';
import { usePiAgent } from '../hooks/usePiAgent';
import { PiAgentState } from '../hooks/agentReducer';
import { SubAgentInfo } from '../lib/api';
import { ToolCall, PiModel, AssistantMessage } from '../lib/pi-events';

interface PiAgentContextValue {
  state: PiAgentState;
  sendPrompt: (message: string, project: string, opts?: any) => Promise<void>;
  abort: (project: string, agentId?: string) => Promise<void>;
  compact: (project: string, agentId?: string) => Promise<void>;
  switchModel: (project: string, provider: string, modelId: string, agentId?: string) => Promise<void>;
  getModels: (project: string, agentId?: string) => Promise<PiModel[]>;
  reset: () => void;
  hydrateState: (project: string, agentId?: string) => Promise<void>;
  loadHistory: (project: string, agentId?: string) => Promise<void>;
  respondToApproval: (project: string, agentId: string, requestId: string, action: 'approve' | 'deny' | 'answer', message?: string) => Promise<void>;
}

const PiAgentContext = createContext<PiAgentContextValue | undefined>(undefined);

export const PiAgentProvider: React.FC<{ children: React.ReactNode }> = ({ children }) => {
  // For now, we share a single agent state (default). 
  // In a more complex app, we might have a map of states per agentId.
  const pi = usePiAgent('default');

  return (
    <PiAgentContext.Provider value={pi}>
      {children}
    </PiAgentContext.Provider>
  );
};

export const usePiAgentContext = () => {
  const context = useContext(PiAgentContext);
  if (!context) {
    throw new Error('usePiAgentContext must be used within a PiAgentProvider');
  }
  return context;
};
