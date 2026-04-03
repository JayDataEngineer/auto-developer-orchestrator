import React from 'react';
import { MessageSquare, Plus, Clock } from 'lucide-react';
import { cn } from '../lib/utils';

interface ChatSession {
  id: string;
  title: string;
  timestamp: number; // ms
  agentId: string;
}

interface HistorySidebarProps {
  sessions: ChatSession[];
  activeSessionId?: string;
  onSelectSession: (id: string) => void;
  onNewChat: () => void;
}

function groupByDate(sessions: ChatSession[]) {
  const now = Date.now();
  const dayMs = 86400000;
  const today: ChatSession[] = [];
  const yesterday: ChatSession[] = [];
  const older: ChatSession[] = [];

  for (const s of sessions) {
    const age = now - s.timestamp;
    if (age < dayMs) today.push(s);
    else if (age < 2 * dayMs) yesterday.push(s);
    else older.push(s);
  }
  return { today, yesterday, older };
}

function SessionGroup({ label, sessions, activeId, onSelect }: {
  label: string;
  sessions: ChatSession[];
  activeId?: string;
  onSelect: (id: string) => void;
}) {
  if (sessions.length === 0) return null;
  return (
    <div>
      <div className="px-3 py-2 text-[8px] font-black uppercase tracking-[0.2em] text-zinc-600">
        {label}
      </div>
      {sessions.map(s => (
        <button
          key={s.id}
          onClick={() => onSelect(s.id)}
          className={cn(
            'w-full text-left px-3 py-2 flex items-center gap-2 transition-colors',
            activeId === s.id
              ? 'bg-primary/10 text-primary border-l-2 border-primary'
              : 'text-zinc-500 hover:bg-white/5 hover:text-zinc-300 border-l-2 border-transparent'
          )}
        >
          <MessageSquare size={10} className="shrink-0" />
          <span className="text-[10px] font-mono truncate">{s.title}</span>
        </button>
      ))}
    </div>
  );
}

export function HistorySidebar({ sessions, activeSessionId, onSelectSession, onNewChat }: HistorySidebarProps) {
  const { today, yesterday, older } = groupByDate(sessions);

  return (
    <div className="w-52 border-r border-white/5 flex flex-col bg-black shrink-0">
      {/* Header */}
      <div className="p-3 border-b border-white/5 flex items-center justify-between">
        <div className="flex items-center gap-2">
          <Clock size={10} className="text-muted-foreground" />
          <span className="text-[8px] font-black uppercase tracking-[0.2em] text-muted-foreground">
            History
          </span>
        </div>
        <button
          onClick={onNewChat}
          className="p-1 hover:bg-white/5 text-muted hover:text-zinc-300 transition-colors"
          title="New chat"
        >
          <Plus size={12} />
        </button>
      </div>

      {/* Session list */}
      <div className="flex-1 overflow-y-auto custom-scrollbar">
        <SessionGroup label="Today" sessions={today} activeId={activeSessionId} onSelect={onSelectSession} />
        <SessionGroup label="Yesterday" sessions={yesterday} activeId={activeSessionId} onSelect={onSelectSession} />
        <SessionGroup label="Older" sessions={older} activeId={activeSessionId} onSelect={onSelectSession} />

        {sessions.length === 0 && (
          <div className="flex flex-col items-center justify-center p-6 opacity-20">
            <MessageSquare size={20} className="mb-2" />
            <p className="text-[9px] font-mono uppercase tracking-widest text-center">
              No chats yet
            </p>
          </div>
        )}
      </div>
    </div>
  );
}
