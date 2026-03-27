import React from 'react';
import { Plus, History, ChevronDown, MessageSquare, Clock } from 'lucide-react';

interface Conversation {
  id: string;
  projectId: string;
  title: string;
  lastActive: string;
}

interface ConversationSidebarProps {
  projects: string[];
  conversations: Conversation[];
  activeConversationId?: string;
  onSelectConversation: (id: string) => void;
  onNewConversation: (projectId: string) => void;
}

export const ConversationSidebar: React.FC<ConversationSidebarProps> = ({
  projects,
  conversations,
  activeConversationId,
  onSelectConversation,
  onNewConversation
}) => {
  return (
    <div className="w-[280px] h-full bg-black border-r border-white/5 flex flex-col shrink-0">
      <div className="p-4 flex items-center justify-between">
        <button 
          onClick={() => onNewConversation(projects[0] || 'default')}
          className="flex-1 mr-2 flex items-center justify-center space-x-2 bg-primary/10 hover:bg-primary/20 text-primary border border-primary/20 rounded py-2 px-3 text-[10px] font-black uppercase tracking-widest transition-all duration-300 glow-primary-sm"
        >
          <Plus size={14} strokeWidth={3} />
          <span>New Chat</span>
        </button>
        <button className="p-2 text-zinc-500 hover:text-white transition-colors">
          <History size={16} />
        </button>
      </div>

      <div className="flex-1 overflow-y-auto px-4 py-2 custom-scrollbar">
        <div className="text-[10px] font-mono text-zinc-700 uppercase tracking-[0.4em] font-bold mb-6">
          Workspaces
        </div>

        {projects.map(project => {
          const projectConvs = conversations.filter(c => c.projectId === project);
          
          return (
            <div key={project} className="mb-6">
              <div className="flex items-center justify-between text-zinc-400 group cursor-pointer mb-2">
                <span className="text-[10px] uppercase tracking-widest font-bold group-hover:text-primary transition-colors">
                  {project}
                </span>
                <ChevronDown size={12} className="text-zinc-600" />
              </div>
              
              <div className="space-y-1 ml-1 border-l border-white/5 pl-3">
                {projectConvs.map(conv => (
                  <button
                    key={conv.id}
                    onClick={() => onSelectConversation(conv.id)}
                    className={`w-full text-left p-2 rounded text-[11px] transition-all group ${
                      activeConversationId === conv.id 
                        ? 'bg-primary/10 text-primary border border-primary/20' 
                        : 'text-zinc-500 hover:text-white hover:bg-white/5'
                    }`}
                  >
                    <div className="truncate mb-1">{conv.title}</div>
                    <div className="flex items-center space-x-2 text-[9px] opacity-50 font-mono">
                      <Clock size={10} />
                      <span className="uppercase">{new Date(conv.lastActive).toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' })}</span>
                    </div>
                  </button>
                ))}
                
                {projectConvs.length === 0 && (
                  <button 
                    onClick={() => onNewConversation(project)}
                    className="w-full text-left p-2 text-[10px] text-zinc-700 italic hover:text-zinc-500"
                  >
                    Start context-aware sync...
                  </button>
                )}
              </div>
            </div>
          );
        })}
      </div>

      <div className="p-4 border-t border-white/5">
        <div className="flex items-center space-x-3 text-zinc-500 text-[10px] uppercase tracking-widest font-bold">
          <div className="w-2 h-2 rounded-full bg-primary glow-primary" />
          <span>Jules Core Live</span>
        </div>
      </div>
    </div>
  );
};
