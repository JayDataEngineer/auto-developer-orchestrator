import React, { useState, useRef, useEffect } from 'react';
import { 
  Send, Bot, MessageSquare, Loader, Sparkles, Trash2, 
  ChevronRight, CheckCircle2, AlertTriangle, Play, ExternalLink,
  Terminal as TerminalIcon, GitPullRequest, ListTodo, Link as LinkIcon
} from 'lucide-react';
import { motion, AnimatePresence } from 'motion/react';
import { cn } from '../lib/utils';
import { ConversationSidebar } from './ConversationSidebar';

interface Message {
  role: 'user' | 'assistant' | 'system' | 'jules_activity';
  content: string;
  timestamp: string;
  activityData?: any;
}

interface Conversation {
  id: string;
  projectId: string;
  title: string;
  lastActive: string;
  messages: Message[];
  julesSessionId?: string;
  julesState?: string;
  julesSource?: string;
}

interface AgentsViewProps {
  className?: string;
  selectedProject?: string;
  projects?: string[];
}

type AIProvider = 'openai' | 'claude' | 'gemini' | 'jules';

interface ProviderConfig {
  name: string;
  icon: string;
  color: string;
  gradient: string;
  model: string;
}

const PROVIDERS: Record<AIProvider, ProviderConfig> = {
  openai: {
    name: 'OpenAI',
    icon: '🟢',
    color: 'border-green-500/30',
    gradient: 'from-green-500/20 to-green-600/20',
    model: 'gpt-4o'
  },
  claude: {
    name: 'Claude',
    icon: '🟠',
    color: 'border-orange-500/30',
    gradient: 'from-orange-500/20 to-orange-600/20',
    model: 'claude-3-5-sonnet'
  },
  gemini: {
    name: 'Gemini',
    icon: '🔵',
    color: 'border-blue-500/30',
    gradient: 'from-blue-500/20 to-blue-600/20',
    model: 'gemini-2.0'
  },
  jules: {
    name: 'Jules AI',
    icon: '⚡',
    color: 'border-primary/30',
    gradient: 'from-primary/20 to-magenta-500/20',
    model: 'v1-autonomous'
  }
};

export const AgentsView: React.FC<AgentsViewProps> = ({ className, selectedProject, projects = [] }) => {
  const [selectedProvider, setSelectedProvider] = useState<AIProvider>('jules');
  const [conversations, setConversations] = useState<Conversation[]>([]);
  const [activeConversationId, setActiveConversationId] = useState<string | null>(null);
  const [input, setInput] = useState('');
  const [isLoading, setIsLoading] = useState(false);
  const [julesSources, setJulesSources] = useState<any[]>([]);
  const [isSearchingSources, setIsSearchingSources] = useState(false);
  
  const messagesEndRef = useRef<HTMLDivElement>(null);
  const pollInterval = useRef<NodeJS.Timeout | null>(null);

  const activeConversation = conversations.find(c => c.id === activeConversationId);
  const messages = activeConversation?.messages || [];

  // Fetch conversations and sources
  useEffect(() => {
    fetchConversations();
    fetchJulesSources();
  }, []);

  const fetchConversations = async () => {
    try {
      const res = await fetch('/api/ai/conversations');
      const data = await res.json();
      setConversations(data.conversations || []);
      if (data.conversations?.length > 0) {
        setActiveConversationId(data.conversations[0].id);
      }
    } catch (e) {
      console.error("Failed to fetch conversations");
    }
  };

  const fetchJulesSources = async () => {
    setIsSearchingSources(true);
    try {
      const res = await fetch('/api/jules/sources');
      const data = await res.json();
      setJulesSources(data.sources || []);
    } catch (e) {
      console.error("Failed to fetch Jules sources");
    } finally {
      setIsSearchingSources(false);
    }
  };

  // Auto-scroll to bottom
  useEffect(() => {
    messagesEndRef.current?.scrollIntoView({ behavior: 'smooth' });
  }, [messages]);

  // Polling for Jules activities
  useEffect(() => {
    if (activeConversation?.julesSessionId && activeConversation.julesState !== 'COMPLETED') {
      startPolling();
    } else {
      stopPolling();
    }
    return () => stopPolling();
  }, [activeConversationId, activeConversation?.julesSessionId, activeConversation?.julesState]);

  const startPolling = () => {
    stopPolling();
    pollInterval.current = setInterval(syncJulesData, 10000);
  };

  const stopPolling = () => {
    if (pollInterval.current) clearInterval(pollInterval.current);
  };

  const syncJulesData = async () => {
    if (!activeConversation?.julesSessionId) return;
    try {
      const [sessionRes, activitiesRes] = await Promise.all([
        fetch(`/api/jules/sessions/${activeConversation.julesSessionId}`),
        fetch(`/api/jules/sessions/${activeConversation.julesSessionId}/activities`)
      ]);
      
      const sessionData = await sessionRes.json();
      const activitiesData = await activitiesRes.json();

      // Jules returns activity list as { activities: [...] } or null if empty
      const newActivities = activitiesData.activities || [];
      const newMessages: Message[] = newActivities.map((act: any) => ({
        role: 'jules_activity',
        content: act.description || act.type,
        timestamp: act.createTime,
        activityData: act
      }));

      setConversations(prev => prev.map(c => 
        c.id === activeConversationId 
          ? { ...c, julesState: sessionData.state, messages: mergeMessages(c.messages, newMessages) } 
          : c
      ));
    } catch (e) {
      console.error("Sync failed", e);
    }
  };

  const mergeMessages = (existing: Message[], incoming: Message[]) => {
    const combined = [...existing];
    incoming.forEach(inc => {
      const exists = combined.find(ex => ex.timestamp === inc.timestamp && ex.role === 'jules_activity');
      if (!exists) combined.push(inc);
    });
    return combined.sort((a, b) => new Date(a.timestamp).getTime() - new Date(b.timestamp).getTime());
  };

  const handleNewConversation = async (projectId: string) => {
    const newId = `conv-${Date.now()}`;
    const newConv: Conversation = {
      id: newId,
      projectId,
      title: "New Agent Sync",
      lastActive: new Date().toISOString(),
      messages: []
    };

    try {
      await fetch('/api/ai/conversations', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(newConv)
      });
      setConversations(prev => [newConv, ...prev]);
      setActiveConversationId(newId);
    } catch (e) {
      console.error("Failed to create conversation");
    }
  };

  const handleSend = async () => {
    if (!input.trim() || isLoading || !activeConversationId) return;

    const userMessage: Message = {
      role: 'user',
      content: input,
      timestamp: new Date().toISOString()
    };

    setConversations(prev => prev.map(c => 
      c.id === activeConversationId 
        ? { ...c, messages: [...c.messages, userMessage], lastActive: new Date().toISOString() } 
        : c
    ));
    
    const currentInput = input;
    setInput('');
    setIsLoading(true);

    try {
      if (selectedProvider === 'jules') {
        if (!activeConversation?.julesSessionId) {
          // Initialize Jules Session
          // Try to auto-detect source if not already mapped
          let source = activeConversation?.julesSource;
          if (!source) {
             const matched = julesSources.find(s => s.githubRepo?.repo === activeConversation?.projectId);
             if (matched) source = matched.name;
          }

          if (!source) {
             throw new Error("Target source not found. Please link this conversation to a Jules GitHub project.");
          }

          const res = await fetch('/api/dispatch', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({
              taskId: `manual-${Date.now()}`,
              project: activeConversation?.projectId,
              repoName: activeConversation?.projectId,
              prompt: currentInput
            })
          });
          const data = await res.json();
          if (data.success) {
             setConversations(prev => prev.map(c => 
               c.id === activeConversationId ? { ...c, julesSessionId: data.julesSessionId, julesState: 'QUEUED', julesSource: source } : c
             ));
          } else {
            throw new Error(data.error || "Launch sequence failed.");
          }
        } else {
          // Send message to existing Jules session
          await fetch(`/api/jules/sessions/${activeConversation.julesSessionId}/message`, {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ prompt: currentInput })
          });
        }
      } else {
        // Generic LLM handling
        const response = await fetch('/api/ai/chat', {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({
            provider: selectedProvider,
            message: currentInput,
            context: activeConversation?.projectId || selectedProject,
            conversationId: activeConversationId
          })
        });
        const data = await response.json();
        const assistantMessage: Message = {
          role: 'assistant',
          content: data.response || 'No synthesis received.',
          timestamp: new Date().toISOString()
        };
        setConversations(prev => prev.map(c => 
          c.id === activeConversationId 
            ? { ...c, messages: [...c.messages, assistantMessage] } 
            : c
        ));
      }
    } catch (error: any) {
      const errMessage: Message = {
        role: 'system',
        content: `ERROR: ${error.message}`,
        timestamp: new Date().toISOString()
      };
      setConversations(prev => prev.map(c => 
        c.id === activeConversationId ? { ...c, messages: [...c.messages, errMessage] } : c
      ));
    } finally {
      setIsLoading(false);
    }
  };

  const handleApprovePlan = async () => {
    if (!activeConversation?.julesSessionId) return;
    try {
      await fetch(`/api/jules/sessions/${activeConversation.julesSessionId}/approve`, { method: 'POST' });
      syncJulesData();
    } catch (e) {
      console.error("Approval failed", e);
    }
  };

  const handleLinkSource = (sourceName: string) => {
    setConversations(prev => prev.map(c => 
      c.id === activeConversationId ? { ...c, julesSource: sourceName } : c
    ));
  };

  const renderActivity = (act: any) => {
    if (act.planGenerated) {
      return (
        <div className="bg-zinc-900/50 border border-primary/20 p-6 rounded space-y-4">
          <div className="flex items-center justify-between">
            <h4 className="text-[10px] font-black uppercase tracking-[0.4em] text-primary flex items-center gap-2">
              <ListTodo size={14} /> Strategic Plan Generated
            </h4>
            <button 
              onClick={handleApprovePlan}
              disabled={activeConversation?.julesState !== 'AWAITING_PLAN_APPROVAL'}
              className="px-4 py-1.5 bg-primary text-black text-[9px] font-black uppercase tracking-[0.2em] rounded disabled:opacity-20 flex items-center gap-2"
            >
              <Play size={10} fill="currentColor" /> Authorized Execution
            </button>
          </div>
          <div className="space-y-3">
             {act.planGenerated.plan?.steps.map((step: any) => (
               <div key={step.id} className="flex gap-4">
                 <span className="text-primary font-mono text-[10px] w-4 shrink-0">{step.index + 1}.</span>
                 <div className="space-y-1">
                    <div className="text-[11px] font-bold text-white uppercase tracking-tight">{step.title}</div>
                    <div className="text-[10px] text-zinc-500">{step.description}</div>
                 </div>
               </div>
             ))}
          </div>
        </div>
      );
    }

    if (act.artifacts?.[0]?.changeSet) {
      const patch = act.artifacts[0].changeSet.gitPatch;
      return (
        <div className="bg-zinc-950 border border-white/5 rounded-sm overflow-hidden">
          <div className="px-4 py-2 border-b border-white/5 bg-zinc-900/50 flex items-center justify-between">
            <span className="text-[9px] font-black uppercase tracking-widest text-zinc-500 flex items-center gap-2">
              <GitPullRequest size={12} className="text-emerald-500" /> Proposed Changeset
            </span>
          </div>
          <pre className="p-4 text-[10px] font-mono text-zinc-400 overflow-x-auto max-h-60 custom-scrollbar whitespace-pre">
            {patch.unidiffPatch}
          </pre>
        </div>
      );
    }

    return (
      <div className="flex items-center gap-4 text-[10px] text-zinc-500 font-mono italic">
        <span className="text-zinc-800 tracking-tighter">[{new Date(act.createTime).toLocaleTimeString()}]</span>
        <span className="uppercase tracking-[0.2em]">{act.description}</span>
      </div>
    );
  };

  return (
    <div className={cn("flex h-full bg-black overflow-hidden", className)}>
      <ConversationSidebar 
        projects={projects}
        conversations={conversations}
        activeConversationId={activeConversationId || undefined}
        onSelectConversation={setActiveConversationId}
        onNewConversation={handleNewConversation}
      />

      <div className="flex-1 flex flex-col min-w-0">
        {/* Header */}
        <div className="h-12 border-b border-white/5 flex items-center px-6 shrink-0 bg-black/50 backdrop-blur-md">
          <div className="flex items-center space-x-2 text-[10px] font-mono tracking-widest text-zinc-500 uppercase font-bold">
            <span className="hover:text-primary transition-colors cursor-pointer">{activeConversation?.projectId || 'GLOBAL'}</span>
            <ChevronRight size={10} />
            <span className="text-white truncate">{activeConversation?.title || 'SYNCING...'}</span>
          </div>
          <div className="flex-1" />
          <div className="flex items-center space-x-6">
            {activeConversation?.julesState && (
              <div className="flex items-center gap-3">
                 <div className={cn(
                   "w-1.5 h-1.5 rounded-full animate-pulse",
                   activeConversation.julesState === 'COMPLETED' ? "bg-emerald-500 glow-emerald" : "bg-primary glow-primary"
                 )} />
                 <span className="text-[9px] font-black text-white uppercase tracking-widest">{activeConversation.julesState}</span>
              </div>
            )}
             <div className="h-4 w-px bg-white/10" />
             {(Object.keys(PROVIDERS) as AIProvider[]).map(p => (
               <button
                 key={p}
                 onClick={() => setSelectedProvider(p)}
                 className={cn(
                   "text-[9px] font-black uppercase tracking-[0.2em] transition-all",
                   selectedProvider === p ? "text-primary" : "text-zinc-700 hover:text-zinc-500"
                 )}
               >
                 {p}
               </button>
             ))}
          </div>
        </div>

        {/* Content */}
        <div className="flex-1 overflow-y-auto p-10 space-y-8 custom-scrollbar">
          {selectedProvider === 'jules' && !activeConversation?.julesSessionId && !activeConversation?.julesSource && (
            <div className="flex flex-col items-center justify-center h-full p-12 text-center bg-zinc-900/20 border border-white/5 rounded">
              <LinkIcon size={32} className="text-zinc-800 mb-6" />
              <h3 className="text-sm font-black uppercase tracking-[0.3em] text-white mb-2">Source Connection Required</h3>
              <p className="text-zinc-500 text-[10px] uppercase tracking-widest max-w-sm mb-8">
                JULES sessions must be anchored to an authenticated repository source. Select one below to begin.
              </p>
              
              <div className="grid grid-cols-1 md:grid-cols-2 gap-4 w-full max-w-2xl">
                {julesSources.map(source => (
                  <button
                    key={source.name}
                    onClick={() => handleLinkSource(source.name)}
                    className="p-4 border border-white/5 bg-zinc-900/50 hover:border-primary/50 text-left transition-all group"
                  >
                    <div className="text-[11px] font-bold text-white group-hover:text-primary transition-colors">{source.githubRepo?.repo}</div>
                    <div className="text-[8px] font-mono text-zinc-600 uppercase mt-1 tracking-tighter">{source.name}</div>
                  </button>
                ))}
              </div>
            </div>
          )}

          {messages.map((message, i) => (
            <div key={i} className={cn('flex items-start gap-4', message.role === 'user' ? 'justify-end' : 'justify-start')}>
              {message.role === 'jules_activity' ? (
                 <div className="w-full">
                    {renderActivity(message.activityData)}
                 </div>
              ) : (
                <>
                  {message.role === 'assistant' && (
                    <div className="w-8 h-8 rounded shrink-0 bg-primary/10 border border-primary/20 flex items-center justify-center">
                      <Bot size={16} className="text-primary" />
                    </div>
                  )}
                  {message.role === 'system' && (
                    <div className="w-8 h-8 rounded shrink-0 bg-red-500/10 border border-red-500/20 flex items-center justify-center">
                      <AlertTriangle size={16} className="text-red-500" />
                    </div>
                  )}
                  <div className={cn(
                    'max-w-[85%] rounded p-5 font-mono text-[11px] leading-relaxed',
                    message.role === 'user' ? 'bg-zinc-900 text-zinc-300 border border-white/5' : 
                    message.role === 'system' ? 'text-red-400 bg-red-500/5 border border-red-500/10' :
                    'text-zinc-400 border-l-2 border-primary/40 bg-white/5'
                  )}>
                    <div className="whitespace-pre-wrap">{message.content}</div>
                  </div>
                </>
              )}
            </div>
          ))}
          <div ref={messagesEndRef} />
        </div>

        {/* Input */}
        <div className="p-8 border-t border-white/5">
          <div className="relative">
            <textarea
              value={input}
              onChange={(e) => setInput(e.target.value)}
              onKeyDown={(e) => e.key === 'Enter' && !e.shiftKey && (e.preventDefault(), handleSend())}
              placeholder={activeConversationId ? "Instruct Agent..." : "Initialize Context..."}
              disabled={isLoading || !activeConversationId}
              className="w-full bg-zinc-900 border border-white/5 rounded p-5 pr-16 text-[11px] text-white placeholder-zinc-700 outline-none focus:border-primary/40 transition-all font-mono"
              rows={2}
            />
            <button
              onClick={handleSend}
              disabled={!input.trim() || isLoading}
              className="absolute right-5 bottom-5 p-2.5 bg-primary text-black rounded hover:bg-primary/80 disabled:opacity-20 transition-all"
            >
              <Send size={18} />
            </button>
          </div>
        </div>
      </div>
    </div>
  );
};
