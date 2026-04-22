import React, { useState, useEffect, useCallback, useRef } from 'react';
import { MessageSquare, Plus, Clock, FolderOpen, ChevronDown, ChevronRight, Trash2, Pencil, Check, X } from 'lucide-react';
import { cn } from '../lib/utils';
import { api, ConversationSummary } from '../lib/api';
import { useToastContext } from './ui/Toast';
import { usePolling } from '../hooks/usePolling';
import { Button } from './ui/button';
import { Tooltip, TooltipContent, TooltipProvider, TooltipTrigger } from './ui/tooltip';
import { ScrollArea } from './ui/scroll-area';
import {
  ContextMenu,
  ContextMenuContent,
  ContextMenuItem,
  ContextMenuSeparator,
  ContextMenuTrigger,
} from './ui/context-menu';
import { Input } from './ui/input';

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
  const [renaming, setRenaming] = useState<{ project: string; agentId: string } | null>(null);
  const [renameValue, setRenameValue] = useState('');
  const renameInputRef = useRef<HTMLInputElement>(null);
  const { addToast } = useToastContext();

  const fetchHistory = useCallback(async () => {
    try {
      const data = await api.pux.getHistory();
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

  usePolling(fetchHistory, 15000, true);

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

  const handleDelete = useCallback(async (project: string, agentId: string) => {
    try {
      await api.pux.deleteConversation(project, agentId);
      addToast('success', 'Conversation deleted');
      fetchHistory();
    } catch {
      addToast('error', 'Failed to delete conversation');
    }
  }, [fetchHistory, addToast]);

  const handleRenameStart = useCallback((project: string, agentId: string, currentTitle: string) => {
    setRenaming({ project, agentId });
    setRenameValue(currentTitle);
  }, []);

  const handleRenameSubmit = useCallback(async () => {
    if (!renaming || !renameValue.trim()) return;
    try {
      await api.pux.renameConversation(renaming.project, renaming.agentId, renameValue.trim());
      addToast('success', 'Conversation renamed');
      fetchHistory();
    } catch {
      addToast('error', 'Failed to rename conversation');
    }
    setRenaming(null);
  }, [renaming, renameValue, fetchHistory, addToast]);

  const getConvDisplayTitle = useCallback((conv: ConversationSummary) => {
    if (conv.title) return conv.title;
    if (conv.agentId === 'default') return conv.lastMessage || 'New conversation';
    return conv.agentId;
  }, []);

  return (
    <TooltipProvider delayDuration={300}>
      <div className="h-full flex flex-col bg-background">
        {/* Header */}
        <div className="p-3 border-b border-border flex items-center justify-between">
          <div className="flex items-center gap-2">
            <Clock size={10} className="text-muted-foreground" />
            <span className="text-xs font-semibold text-muted-foreground">
              History
            </span>
            <span className="text-xs text-muted-foreground/50">{totalConversations}</span>
          </div>
          <Tooltip>
            <TooltipTrigger asChild>
              <Button variant="ghost" size="icon-xs" onClick={onNewChat}>
                <Plus size={12} />
              </Button>
            </TooltipTrigger>
            <TooltipContent>
              New chat <span className="kbd">Ctrl+Shift+N</span>
            </TooltipContent>
          </Tooltip>
        </div>

        {/* Project groups */}
        <ScrollArea className="flex-1">
          {loading && groups.length === 0 ? (
            <div className="flex items-center justify-center h-20 text-muted-foreground/50 text-xs font-mono">
              Loading...
            </div>
          ) : groups.length === 0 ? (
            <div className="flex flex-col items-center justify-center p-6 opacity-20">
              <MessageSquare size={20} className="mb-2" />
              <p className="text-xs text-muted-foreground text-center">
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
                    "w-full text-left px-3 py-2 flex items-center gap-2 transition-colors border-b border-border/50",
                    group.project === activeProject
                      ? "bg-primary/5 text-primary"
                      : "text-foreground hover:bg-muted hover:text-foreground"
                  )}
                >
                  {group.collapsed ? (
                    <ChevronRight size={10} className="shrink-0 text-muted-foreground" />
                  ) : (
                    <ChevronDown size={10} className="shrink-0 text-muted-foreground" />
                  )}
                  <FolderOpen size={10} className="shrink-0" />
                  <span className="text-xs font-medium truncate">
                    {group.project}
                  </span>
                  {group.conversations.length > 0 && (
                    <span className="text-xs font-mono text-muted-foreground ml-auto">
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
                        <ContextMenu key={`${conv.project}-${conv.agentId}`}>
                          <ContextMenuTrigger asChild>
                            <div
                              onClick={() => {
                                if (!isRenaming) onSelectSession(conv.project, conv.agentId);
                              }}
                              className={cn(
                                "px-3 py-1.5 pl-8 flex flex-col gap-0.5 transition-all duration-150 cursor-pointer",
                                isActive(conv.project, conv.agentId)
                                  ? "bg-primary/10 text-primary border-l-2 border-primary"
                                  : "text-muted-foreground hover:bg-muted hover:text-foreground border-l-2 border-transparent"
                              )}
                            >
                              {isRenaming ? (
                                <div className="flex items-center gap-1" onClick={e => e.stopPropagation()}>
                                  <Input
                                    ref={renameInputRef}
                                    value={renameValue}
                                    onChange={e => setRenameValue(e.target.value)}
                                    onKeyDown={e => {
                                      if (e.key === 'Enter') handleRenameSubmit();
                                      if (e.key === 'Escape') setRenaming(null);
                                    }}
                                    className="flex-1 px-1.5 py-0.5 text-xs"
                                  />
                                  <Button variant="ghost" size="icon-xs" onClick={handleRenameSubmit}>
                                    <Check size={9} />
                                  </Button>
                                  <Button variant="ghost" size="icon-xs" onClick={() => setRenaming(null)}>
                                    <X size={9} />
                                  </Button>
                                </div>
                              ) : (
                                <span className="text-xs font-mono truncate">
                                  {getConvDisplayTitle(conv)}
                                </span>
                              )}
                              <div className="flex items-center gap-2">
                                <span className="text-xs font-mono text-muted-foreground">
                                  {formatTimeAgo(conv.lastAt)}
                                </span>
                                <span className="text-xs font-mono text-muted-foreground/50">
                                  {conv.messageCount} msgs
                                </span>
                              </div>
                            </div>
                          </ContextMenuTrigger>
                          <ContextMenuContent>
                            <ContextMenuItem
                              onClick={() => {
                                const c = conv;
                                handleRenameStart(c.project, c.agentId, getConvDisplayTitle(c));
                              }}
                            >
                              <Pencil size={9} /> Rename
                            </ContextMenuItem>
                            <ContextMenuSeparator />
                            <ContextMenuItem
                              className="text-red-400 focus:text-red-400 focus:bg-red-400/10"
                              onClick={() => handleDelete(conv.project, conv.agentId)}
                            >
                              <Trash2 size={9} /> Delete
                            </ContextMenuItem>
                          </ContextMenuContent>
                        </ContextMenu>
                      );
                    })}
                    {group.conversations.length === 0 && (
                      <div className="px-8 py-2 text-xs font-mono text-muted-foreground/50">
                        No conversations yet
                      </div>
                    )}
                  </div>
                )}
              </div>
            ))
          )}
        </ScrollArea>
      </div>
    </TooltipProvider>
  );
}
