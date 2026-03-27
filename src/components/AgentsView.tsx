import React, { useState, useRef, useEffect } from 'react';
import { Send, Bot, MessageSquare, Loader, Sparkles, Trash2, ChevronRight } from 'lucide-react';
import { cn } from '../lib/utils';
import { ConversationSidebar } from './ConversationSidebar';

interface Message {
  role: 'user' | 'assistant';
  content: string;
  timestamp: string;
}

interface Conversation {
  id: string;
  projectId: string;
  title: string;
  lastActive: string;
  messages: Message[];
}

interface AgentsViewProps {
  className?: string;
  selectedProject?: string;
  projects?: string[];
}

type AIProvider = 'openai' | 'claude' | 'gemini';

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
  }
};

export const AgentsView: React.FC<AgentsViewProps> = ({ className, selectedProject, projects = [] }) => {
  const [selectedProvider, setSelectedProvider] = useState<AIProvider>('gemini');
  const [conversations, setConversations] = useState<Conversation[]>([]);
  const [activeConversationId, setActiveConversationId] = useState<string | null>(null);
  const [input, setInput] = useState('');
  const [isLoading, setIsLoading] = useState(false);
  const messagesEndRef = useRef<HTMLDivElement>(null);

  const activeConversation = conversations.find(c => c.id === activeConversationId);
  const messages = activeConversation?.messages || [];

  // Fetch conversations on mount
  useEffect(() => {
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
    fetchConversations();
  }, []);

  // Auto-scroll to bottom
  useEffect(() => {
    messagesEndRef.current?.scrollIntoView({ behavior: 'smooth' });
  }, [messages]);

  const handleNewConversation = async (projectId: string) => {
    const newId = `conv-${Date.now()}`;
    const newConv: Conversation = {
      id: newId,
      projectId,
      title: "New Architectural Sync",
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

    // Optimistic update
    setConversations(prev => prev.map(c => 
      c.id === activeConversationId 
        ? { ...c, messages: [...c.messages, userMessage], lastActive: new Date().toISOString() } 
        : c
    ));
    
    setInput('');
    setIsLoading(true);

    try {
      const response = await fetch('/api/ai/chat', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          provider: selectedProvider,
          message: input,
          context: activeConversation?.projectId || selectedProject || 'codebase-analysis',
          conversationId: activeConversationId
        })
      });

      if (!response.ok) throw new Error('AI Engine Offline');

      const data = await response.json();
      
      const assistantMessage: Message = {
        role: 'assistant',
        content: data.response || 'No valid synthesis received.',
        timestamp: new Date().toISOString()
      };

      setConversations(prev => prev.map(c => 
        c.id === activeConversationId 
          ? { ...c, messages: [...c.messages, assistantMessage], lastActive: new Date().toISOString() } 
          : c
      ));
    } catch (error: any) {
      const errorMessage: Message = {
        role: 'assistant',
        content: `CORE_ERROR: ${error.message}`,
        timestamp: new Date().toISOString()
      };
      setConversations(prev => prev.map(c => 
        c.id === activeConversationId ? { ...c, messages: [...c.messages, errorMessage] } : c
      ));
    } finally {
      setIsLoading(false);
    }
  };

  const handleKeyDown = (e: React.KeyboardEvent) => {
    if (e.key === 'Enter' && !e.shiftKey) {
      e.preventDefault();
      handleSend();
    }
  };

  return (
    <div className={cn("flex h-full bg-black overflow-hidden", className)}>
      {/* Nested Sidebar */}
      <ConversationSidebar 
        projects={projects}
        conversations={conversations}
        activeConversationId={activeConversationId || undefined}
        onSelectConversation={setActiveConversationId}
        onNewConversation={handleNewConversation}
      />

      {/* Main Chat Area */}
      <div className="flex-1 flex flex-col min-w-0">
        {/* Navigation Header */}
        <div className="h-12 border-b border-white/5 flex items-center px-6 shrink-0 bg-black/50 backdrop-blur-md">
          <div className="flex items-center space-x-2 text-[10px] font-mono tracking-widest text-zinc-500 uppercase font-bold">
            <span className="hover:text-primary transition-colors cursor-pointer">{activeConversation?.projectId || 'GLOBAL_COBEBASE'}</span>
            <ChevronRight size={10} className="text-zinc-700" />
            <span className="text-white truncate max-w-[300px]">{activeConversation?.title || 'SELECTING_CONTEXT...'}</span>
          </div>
          <div className="flex-1" />
          <div className="flex items-center space-x-4">
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

        {/* Messages */}
        <div className="flex-1 overflow-y-auto p-8 space-y-6 custom-scrollbar">
          {!activeConversationId ? (
            <div className="h-full flex flex-col items-center justify-center text-center p-12">
              <div className="w-16 h-16 rounded-full bg-primary/5 border border-primary/20 flex items-center justify-center mb-6 glow-primary">
                <Sparkles size={32} className="text-primary" />
              </div>
              <h3 className="text-xl font-black italic uppercase tracking-[0.3em] text-white mb-2">Sync Initialized</h3>
              <p className="text-zinc-500 text-xs uppercase tracking-widest leading-relaxed max-w-sm">
                Select a project workspace or create a new conversation to begin the deep-scan synthesis.
              </p>
            </div>
          ) : messages.length === 0 ? (
            <div className="h-full flex flex-col items-center justify-center text-center">
              <Bot size={40} className="text-zinc-800 mb-4 animate-pulse" />
              <div className="text-[10px] font-mono text-zinc-600 uppercase tracking-[0.4em] font-bold">
                Awaiting Input // Context Ready
              </div>
            </div>
          ) : (
            <>
              {messages.map((message, i) => (
                <div
                  key={i}
                  className={cn(
                    'flex items-start space-x-4',
                    message.role === 'user' ? 'justify-end' : 'justify-start'
                  )}
                >
                  {message.role === 'assistant' && (
                    <div className="w-8 h-8 rounded shrink-0 bg-primary/10 border border-primary/20 flex items-center justify-center">
                      <Bot size={16} className="text-primary" />
                    </div>
                  )}
                  
                  <div className={cn(
                    'max-w-[85%] rounded p-4 font-mono text-[11px] leading-relaxed',
                    message.role === 'user'
                      ? 'bg-zinc-900 text-zinc-300 border border-white/5 order-1'
                      : 'text-zinc-400 border-l-2 border-primary/40 bg-white/5'
                  )}>
                    <div className="whitespace-pre-wrap">{message.content}</div>
                    <div className="mt-2 text-[8px] opacity-30 text-right uppercase tracking-widest">
                      {new Date(message.timestamp).toLocaleTimeString()}
                    </div>
                  </div>

                  {message.role === 'user' && (
                    <div className="w-8 h-8 rounded shrink-0 bg-zinc-800 border border-white/10 flex items-center justify-center order-2 ml-4">
                      <span className="text-[10px] font-bold text-zinc-500">YOU</span>
                    </div>
                  )}
                </div>
              ))}
              
              {isLoading && (
                <div className="flex items-start space-x-4 animate-pulse">
                  <div className="w-8 h-8 rounded shrink-0 bg-primary/10 border border-primary/20 flex items-center justify-center">
                    <Loader size={16} className="text-primary animate-spin" />
                  </div>
                  <div className="p-4 border-l-2 border-primary/20 bg-white/5 text-[10px] font-mono text-zinc-600 uppercase tracking-widest">
                    Synthesizing response...
                  </div>
                </div>
              )}
              
              <div ref={messagesEndRef} />
            </>
          )}
        </div>

        {/* Input Area */}
        <div className="p-6 border-t border-white/5">
          <div className="relative">
            <textarea
              value={input}
              onChange={(e) => setInput(e.target.value)}
              onKeyDown={handleKeyDown}
              placeholder={activeConversationId ? "Sync thoughts..." : "Select context first..."}
              disabled={!activeConversationId || isLoading}
              className="w-full bg-zinc-900 border border-white/5 rounded p-4 pr-16 text-[11px] text-white placeholder-zinc-700 outline-none focus:border-primary/40 transition-all resize-none font-mono"
              rows={3}
            />
            <button
              onClick={handleSend}
              disabled={!input.trim() || isLoading || !activeConversationId}
              className="absolute right-4 bottom-4 p-2 bg-primary text-black rounded hover:bg-primary/80 disabled:opacity-20 disabled:cursor-not-allowed transition-all"
            >
              <Send size={16} />
            </button>
          </div>
          <div className="mt-4 flex items-center justify-between text-[8px] font-mono text-zinc-700 uppercase tracking-[0.3em] font-bold">
            <span>Security: encrypted</span>
            <span>Agent: {PROVIDERS[selectedProvider].name} // online</span>
          </div>
        </div>
      </div>
    </div>
  );
};
