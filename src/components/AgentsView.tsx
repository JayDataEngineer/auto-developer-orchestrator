import React, { useState, useRef, useEffect } from 'react';
import { Send, Bot, MessageSquare, Loader, Sparkles } from 'lucide-react';
import { cn } from '../lib/utils';

interface AgentsViewProps {
  className?: string;
}

interface Message {
  role: 'user' | 'assistant';
  content: string;
  timestamp: string;
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

export const AgentsView: React.FC<AgentsViewProps> = ({ className }) => {
  const [selectedProvider, setSelectedProvider] = useState<AIProvider>('openai');
  const [messages, setMessages] = useState<Message[]>([]);
  const [input, setInput] = useState('');
  const [isLoading, setIsLoading] = useState(false);
  const messagesEndRef = useRef<HTMLDivElement>(null);

  // Auto-scroll to bottom
  useEffect(() => {
    messagesEndRef.current?.scrollIntoView({ behavior: 'smooth' });
  }, [messages]);

  const handleSend = async () => {
    if (!input.trim() || isLoading) return;

    const userMessage: Message = {
      role: 'user',
      content: input,
      timestamp: new Date().toLocaleTimeString()
    };

    setMessages(prev => [...prev, userMessage]);
    setInput('');
    setIsLoading(true);

    try {
      // TODO: Call actual AI provider API
      // For now, simulate response
      await new Promise(resolve => setTimeout(resolve, 1500));

      const assistantMessage: Message = {
        role: 'assistant',
        content: `[${PROVIDERS[selectedProvider].name}] I understand you want to: "${input}". This is a simulated response. In production, this would call the ${PROVIDERS[selectedProvider].model} API.`,
        timestamp: new Date().toLocaleTimeString()
      };

      setMessages(prev => [...prev, assistantMessage]);
    } catch (error) {
      const errorMessage: Message = {
        role: 'assistant',
        content: 'Error: Failed to get response from AI provider',
        timestamp: new Date().toLocaleTimeString()
      };
      setMessages(prev => [...prev, errorMessage]);
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

  const clearChat = () => {
    setMessages([]);
  };

  return (
    <div className={cn("flex flex-col h-full", className)}>
      {/* Provider Selector */}
      <div className="p-4 border-b border-white/5 shrink-0">
        <div className="flex items-center gap-2 mb-3">
          <Bot size={16} className="text-primary" />
          <h2 className="text-xs font-bold uppercase tracking-widest text-white">AI Agents</h2>
        </div>
        <div className="flex gap-2">
          {(Object.keys(PROVIDERS) as AIProvider[]).map(provider => (
            <button
              key={provider}
              onClick={() => setSelectedProvider(provider)}
              className={cn(
                'flex-1 p-2 rounded-lg border transition-all text-left',
                selectedProvider === provider
                  ? `bg-gradient-to-r ${PROVIDERS[provider].gradient} ${PROVIDERS[provider].color} border-white/20`
                  : 'glass-dark border-white/5 hover:border-white/10'
              )}
            >
              <div className="flex items-center gap-2">
                <span className="text-lg">{PROVIDERS[provider].icon}</span>
                <div>
                  <div className="text-[9px] font-bold text-white">{PROVIDERS[provider].name}</div>
                  <div className="text-[8px] text-zinc-400">{PROVIDERS[provider].model}</div>
                </div>
              </div>
            </button>
          ))}
        </div>
      </div>

      {/* Messages */}
      <div className="flex-1 overflow-y-auto p-4 space-y-3">
        {messages.length === 0 ? (
          <div className="h-full flex items-center justify-center">
            <div className="text-center">
              <div className="w-16 h-16 mx-auto mb-4 rounded-full glass-dark flex items-center justify-center">
                <MessageSquare size={32} className="text-zinc-600" />
              </div>
              <h3 className="text-xs font-bold text-zinc-400 mb-2">No messages yet</h3>
              <p className="text-[10px] text-zinc-500">Ask your AI assistant about your codebase</p>
              <div className="mt-4 flex flex-wrap gap-2 justify-center">
                {['Analyze codebase', 'Find bugs', 'Suggest improvements'].map(action => (
                  <button
                    key={action}
                    onClick={() => setInput(action)}
                    className="px-3 py-1.5 text-[9px] text-zinc-300 glass-dark border border-white/5 rounded-lg hover:border-white/10 transition-all"
                  >
                    {action}
                  </button>
                ))}
              </div>
            </div>
          </div>
        ) : (
          <>
            {messages.map((message, i) => (
              <div
                key={i}
                className={cn(
                  'flex gap-2',
                  message.role === 'user' ? 'flex-row-reverse' : ''
                )}
              >
                <div className={cn(
                  'w-7 h-7 rounded-full flex items-center justify-center shrink-0 text-xs',
                  message.role === 'user'
                    ? 'bg-primary text-white font-bold'
                    : `bg-gradient-to-br ${PROVIDERS[selectedProvider].gradient}`
                )}>
                  {message.role === 'user' ? 'U' : PROVIDERS[selectedProvider].icon}
                </div>
                
                <div className={cn(
                  'max-w-[80%] rounded-xl p-3 glass-dark border',
                  message.role === 'user'
                    ? 'border-primary/30 bg-primary/10'
                    : `border-white/10`
                )}>
                  <div className="flex items-center gap-2 mb-1">
                    <span className="text-[8px] font-bold uppercase tracking-widest text-zinc-400">
                      {message.role === 'user' ? 'You' : PROVIDERS[selectedProvider].name}
                    </span>
                    <span className="text-[7px] text-zinc-600">{message.timestamp}</span>
                  </div>
                  <p className="text-[10px] text-zinc-200 whitespace-pre-wrap leading-relaxed">
                    {message.content}
                  </p>
                </div>
              </div>
            ))}
            
            {isLoading && (
              <div className="flex gap-2">
                <div className={`w-7 h-7 rounded-full flex items-center justify-center shrink-0 bg-gradient-to-br ${PROVIDERS[selectedProvider].gradient}`}>
                  <span className="text-xs">{PROVIDERS[selectedProvider].icon}</span>
                </div>
                <div className="max-w-[80%] rounded-xl p-3 glass-dark border border-white/10">
                  <div className="flex items-center gap-2">
                    <Loader size={10} className="animate-spin text-primary" />
                    <span className="text-[8px] font-bold uppercase tracking-widest text-zinc-400">
                      Thinking...
                    </span>
                  </div>
                </div>
              </div>
            )}
            
            <div ref={messagesEndRef} />
          </>
        )}
      </div>

      {/* Input Area */}
      <div className="p-4 border-t border-white/5 shrink-0">
        <div className="flex gap-2 mb-2">
          <button
            onClick={clearChat}
            className="px-2 py-1 text-[8px] text-zinc-500 hover:text-white glass-dark border border-white/5 rounded transition-all"
          >
            Clear Chat
          </button>
        </div>
        <div className="flex gap-2">
          <textarea
            value={input}
            onChange={(e) => setInput(e.target.value)}
            onKeyDown={handleKeyDown}
            placeholder={`Ask ${PROVIDERS[selectedProvider].name}...`}
            className="flex-1 bg-transparent border border-white/5 rounded-lg px-3 py-2 text-[10px] text-white placeholder-zinc-500 outline-none focus:border-white/10 resize-none"
            rows={2}
            disabled={isLoading}
          />
          <button
            onClick={handleSend}
            disabled={!input.trim() || isLoading}
            className="px-4 py-2 bg-primary text-white rounded-lg text-[9px] font-bold uppercase tracking-widest hover:bg-primary/80 disabled:opacity-50 disabled:cursor-not-allowed transition-all flex items-center gap-1"
          >
            <Send size={12} />
          </button>
        </div>
      </div>
    </div>
  );
};
