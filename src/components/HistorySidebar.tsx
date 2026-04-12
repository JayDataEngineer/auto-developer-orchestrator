import React, { useState, useEffect, useCallback, useRef } from 'react';
import { MessageSquare, Plus, Clock, FolderOpen, ChevronDown, ChevronRight, Trash2, Pencil, Check, X } from 'lucide-react';
import { cn } from '../lib/utils';
import { api, ConversationSummary } from '../lib/api';

interface HistorySidebarProps {
  projects: string[];
  activeProject?: string;
  activeAgentId?: string;
  onSelectSession: (project: string, agentId: string) => void;
  onNewChat: () => void;
}

interface ProjectGroup {
  project: string;
  conversations: ConversationSummary[];
  collapsed: boolean;
}

function formatTimeAgo(dateStr: string): string {
  if (!dateStr) return '';
  const d = new Date(dateStr);
  if (isNaN(d.getTime())) return '';
  const diff = Date.now() - d.getTime();
  if (diff < 60000) return 'just now';
  if (diff < 3600000) return `${Math.floor(diff / 60000)}m ago`;
  if (diff < 86400000) return `${Math.floor(diff / 3600000)}h ago`;
  if (diff < 172800000) return 'yesterday';
  return d.toLocaleDateString();
}

export function HistorySidebar({
  projects,
  activeProject,
  activeAgentId,
  onSelectSession,
  onNewChat,
}: HistorySidebarProps) {
  const [groups, setGroups] = useState<ProjectGroup[]>([]);
  const [loading, setLoading] = useState(true);
  const [contextMenu, setContextMenu] = useState<{ project: string; agentId: string; x: number; y: number } | null>(null);
  const [renaming, setRenaming] = useState<{ project: string; agentId: string } | null>(null);
  const [renameValue, setRenameValue] = useState('');
  const renameInputRef = useRef<HTMLInputElement>(null);

  const fetchHistory = useCallback(async () => {
    try {
      const data = await api.pi.getHistory();
      const summaries: ConversationSummary[] = data.conversations || [];

      const projectMap = new Map<string, ConversationSummary[]>();
      for (const s of summaries) {
        const list = projectMap.get(s.project) || [];
        list.push(s);
        projectMap.set(s.project, list);
      }

      for (const p of projects) {
        if (!projectMap.has(p)) {
          projectMap.set(p, []);
        }
      }

      const built: ProjectGroup[] = [];
      const sortedProjects = [...projectMap.entries()].sort((a, b) => {
        const aLast = a[1][0]?.lastAt || '';
        const bLast = b[1][0]?.lastAt || '';
        return bLast.localeCompare(aLast);
      });

      for (const [project, convos] of sortedProjects) {
        built.push({
          project,
          conversations: convos,
          collapsed: project !== activeProject,
        });
      }

      setGroups(built);
    } catch {
      const built: ProjectGroup[] = projects.map(p => ({
        project: p,
        conversations: [],
        collapsed: p !== activeProject,
      }));
      setGroups(built);
    } finally {
      setLoading(false);
    }
  }, [projects, activeProject]);

  useEffect(() => {
    fetchHistory();
  }, [fetchHistory]);

  useEffect(() => {
    const interval = setInterval(fetchHistory, 15000);
    return () => clearInterval(interval);
  }, [fetchHistory]);

  // Close context menu on click outside
  useEffect(() => {
    if (!contextMenu) return;
    const handler = () => setContextMenu(null);
    window.addEventListener('click', handler);
    return () => window.removeEventListener('click', handler);
  }, [contextMenu]);

  // Focus rename input
  useEffect(() => {
    if (renaming) renameInputRef.current?.focus();
  }, [renaming]);

  const toggleGroup = (project: string) => {
    setGroups(prev =>
      prev.map(g =>
        g.project === project ? { ...g, collapsed: !g.collapsed } : g
      )
    );
  };

  const isActive = (project: string, agentId: string) =>
    project === activeProject && agentId === activeAgentId;

  const totalConversations = groups.reduce(
    (sum, g) => sum + g.conversations.length, 0
  );

  const handleContextMenu = useCallback((e: React.MouseEvent, project: string, agentId: string) => {
    e.preventDefault();
    e.stopPropagation();
    setContextMenu({ project, agentId, x: e.clientX, y: e.clientY });
  }, []);

  const handleDelete = useCallback(async (project: string, agentId: string) => {
    setContextMenu(null);
    try {
      await api.pi.deleteConversation(project, agentId);
      fetchHistory();
    } catch {}
  }, [fetchHistory]);

  const handleRenameStart = useCallback((project: string, agentId: string, currentTitle: string) => {
    setContextMenu(null);
    setRenaming({ project, agentId });
    setRenameValue(currentTitle);
  }, []);

  const handleRenameSubmit = useCallback(async () => {
    if (!renaming || !renameValue.trim()) return;
    try {
      await api.pi.renameConversation(renaming.project, renaming.agentId, renameValue.trim());
      fetchHistory();
    } catch {}
    setRenaming(null);
  }, [renaming, renameValue, fetchHistory]);

  const getConvDisplayTitle = useCallback((conv: ConversationSummary) => {
    if (conv.title) return conv.title;
    if (conv.agentId === 'default') return conv.lastMessage || 'New conversation';
    return conv.agentId;
  }, []);

  return (
    <div className="h-full flex flex-col bg-black">
      {/* Header */}
      <div className="p-3 border-b border-white/5 flex items-center justify-between">
        <div className="flex items-center gap-2">
          <Clock size={10} className="text-muted-foreground" />
          <span className="text-[8px] font-black uppercase tracking-[0.2em] text-muted-foreground">
            History
          </span>
          <span className="text-[8px] font-mono text-zinc-700">{totalConversations}</span>
        </div>
        <button
          onClick={onNewChat}
          className="p-1 hover:bg-white/5 text-muted hover:text-zinc-300 transition-colors"
          title="New chat"
        >
          <Plus size={12} />
        </button>
      </div>

      {/* Project groups */}
      <div className="flex-1 overflow-y-auto custom-scrollbar">
        {loading && groups.length === 0 ? (
          <div className="flex items-center justify-center h-20 text-zinc-700 text-[9px] font-mono">
            Loading...
          </div>
        ) : groups.length === 0 ? (
          <div className="flex flex-col items-center justify-center p-6 opacity-20">
            <MessageSquare size={20} className="mb-2" />
            <p className="text-[9px] font-mono uppercase tracking-widest text-center">
              No projects yet
            </p>
          </div>
        ) : (
          groups.map(group => (
            <div key={group.project}>
              {/* Project header */}
              <button
                onClick={() => toggleGroup(group.project)}
                className={cn(
                  "w-full text-left px-3 py-2 flex items-center gap-2 transition-colors border-b border-white/[0.02]",
                  group.project === activeProject
                    ? "bg-primary/5 text-primary"
                    : "text-zinc-400 hover:bg-white/[0.02] hover:text-zinc-300"
                )}
              >
                {group.collapsed ? (
                  <ChevronRight size={10} className="shrink-0 text-zinc-600" />
                ) : (
                  <ChevronDown size={10} className="shrink-0 text-zinc-600" />
                )}
                <FolderOpen size={10} className="shrink-0" />
                <span className="text-[9px] font-mono uppercase tracking-widest truncate">
                  {group.project}
                </span>
                {group.conversations.length > 0 && (
                  <span className="text-[8px] font-mono text-zinc-600 ml-auto">
                    {group.conversations.length}
                  </span>
                )}
              </button>

              {/* Conversations */}
              {!group.collapsed && (
                <div>
                  {group.conversations.map(conv => {
                    const isRenaming = renaming?.project === conv.project && renaming?.agentId === conv.agentId;

                    return (
                      <div
                        key={`${conv.project}-${conv.agentId}`}
                        onClick={() => {
                          if (!isRenaming) onSelectSession(conv.project, conv.agentId);
                        }}
                        onContextMenu={(e) => handleContextMenu(e, conv.project, conv.agentId)}
                        className={cn(
                          "px-3 py-1.5 pl-8 flex flex-col gap-0.5 transition-colors cursor-pointer",
                          isActive(conv.project, conv.agentId)
                            ? "bg-primary/10 text-primary border-l-2 border-primary"
                            : "text-zinc-500 hover:bg-white/5 hover:text-zinc-300 border-l-2 border-transparent"
                        )}
                      >
                        {isRenaming ? (
                          <div className="flex items-center gap-1" onClick={e => e.stopPropagation()}>
                            <input
                              ref={renameInputRef}
                              value={renameValue}
                              onChange={e => setRenameValue(e.target.value)}
                              onKeyDown={e => {
                                if (e.key === 'Enter') handleRenameSubmit();
                                if (e.key === 'Escape') setRenaming(null);
                              }}
                              className="flex-1 bg-zinc-900 border border-white/10 rounded px-1.5 py-0.5 text-[9px] font-mono text-zinc-200 outline-none focus:border-primary/40"
                            />
                            <button onClick={handleRenameSubmit} className="p-0.5 hover:text-primary">
                              <Check size={9} />
                            </button>
                            <button onClick={() => setRenaming(null)} className="p-0.5 hover:text-red-400">
                              <X size={9} />
                            </button>
                          </div>
                        ) : (
                          <span className="text-[9px] font-mono truncate">
                            {getConvDisplayTitle(conv)}
                          </span>
                        )}
                        <div className="flex items-center gap-2">
                          <span className="text-[8px] font-mono text-zinc-600">
                            {formatTimeAgo(conv.lastAt)}
                          </span>
                          <span className="text-[8px] font-mono text-zinc-700">
                            {conv.messageCount} msgs
                          </span>
                        </div>
                      </div>
                    );
                  })}
                  {group.conversations.length === 0 && (
                    <div className="px-8 py-2 text-[8px] font-mono text-zinc-700">
                      No conversations yet
                    </div>
                  )}
                </div>
              )}
            </div>
          ))
        )}
      </div>

      {/* Context menu */}
      {contextMenu && (
        <div
          className="fixed z-50 bg-zinc-900 border border-white/10 shadow-xl rounded py-1 min-w-[120px]"
          style={{ left: contextMenu.x, top: contextMenu.y }}
          onClick={e => e.stopPropagation()}
        >
          <button
            onClick={() => {
              const conv = groups
                .flatMap(g => g.conversations)
                .find(c => c.project === contextMenu.project && c.agentId === contextMenu.agentId);
              handleRenameStart(contextMenu.project, contextMenu.agentId, conv?.title || '');
            }}
            className="w-full text-left px-3 py-1.5 text-[9px] font-mono text-zinc-300 hover:bg-white/5 flex items-center gap-2"
          >
            <Pencil size={9} /> Rename
          </button>
          <button
            onClick={() => handleDelete(contextMenu.project, contextMenu.agentId)}
            className="w-full text-left px-3 py-1.5 text-[9px] font-mono text-red-400 hover:bg-red-400/10 flex items-center gap-2"
          >
            <Trash2 size={9} /> Delete
          </button>
        </div>
      )}
    </div>
  );
}
