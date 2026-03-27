import React, { useState } from 'react';
import { Plus, History, ChevronDown, MessageSquare, Clock, Search, FolderSync } from 'lucide-react';
import { motion, AnimatePresence } from 'motion/react';
import { cn } from '../lib/utils';

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
  const [showProjectSelector, setShowProjectSelector] = useState(false);

  return (
    <div className="w-[280px] h-full bg-black border-r border-white/5 flex flex-col shrink-0 relative">
      <div className="p-4 space-y-4">
        <div className="flex items-center justify-between">
          <button 
            onClick={() => setShowProjectSelector(!showProjectSelector)}
            className="flex-1 mr-2 flex items-center justify-center space-x-2 bg-primary/10 hover:bg-primary/20 text-primary border border-primary/20 rounded py-2.5 px-3 text-[10px] font-black uppercase tracking-widest transition-all duration-300 glow-primary-sm"
          >
            <Plus size={14} strokeWidth={3} />
            <span>New Chat</span>
          </button>
          <button className="p-2 text-zinc-500 hover:text-white transition-colors border border-white/5 rounded bg-zinc-900/40">
            <History size={16} />
          </button>
        </div>

        {/* Project Selector Dropdown */}
        <AnimatePresence>
          {showProjectSelector && (
            <motion.div
              initial={{ opacity: 0, y: -10 }}
              animate={{ opacity: 1, y: 0 }}
              exit={{ opacity: 0, y: -10 }}
              className="absolute left-4 right-4 top-[70px] z-50 bg-zinc-950 border border-primary/30 shadow-2xl rounded p-2 space-y-1"
            >
              <div className="px-3 py-2 text-[9px] font-mono text-zinc-600 uppercase tracking-widest border-b border-white/5 mb-1">
                Select Context // Sync Engine
              </div>
              {projects.map(project => (
                <button
                  key={project}
                  onClick={() => {
                    onNewConversation(project);
                    setShowProjectSelector(false);
                  }}
                  className="w-full text-left p-3 hover:bg-white/5 rounded text-[10px] text-zinc-400 hover:text-primary flex items-center justify-between group transition-all"
                >
                  <span className="uppercase tracking-widest font-bold">{project}</span>
                  <Plus size={12} className="opacity-0 group-hover:opacity-100" />
                </button>
              ))}
            </motion.div>
          )}
        </AnimatePresence>

        <div className="relative">
          <Search size={14} className="absolute left-3 top-1/2 -translate-y-1/2 text-zinc-700" />
          <input 
            type="text" 
            placeholder="Search syncs..."
            className="w-full bg-zinc-900/50 border border-white/5 rounded py-2 pl-9 pr-4 text-[10px] text-white placeholder-zinc-800 focus:outline-none focus:border-white/10 transition-all font-mono"
          />
        </div>
      </div>

      <div className="flex-1 overflow-y-auto px-4 py-2 custom-scrollbar">
        <div className="text-[10px] font-mono text-zinc-800 uppercase tracking-[0.4em] font-black mb-6 mt-2">
          Workspaces
        </div>

        {projects.map(project => {
          const projectConvs = conversations.filter(c => c.projectId === project);
          
          return (
            <div key={project} className="mb-6">
              <div className="flex items-center justify-between text-zinc-500 group cursor-pointer mb-2">
                <div className="flex items-center gap-2">
                  <span className="text-[10px] uppercase tracking-widest font-black group-hover:text-primary transition-colors">
                    {project}
                  </span>
                  <span className="text-[9px] text-zinc-800 font-mono">[{projectConvs.length}]</span>
                </div>
                <div className="flex items-center gap-2">
                   <button 
                     onClick={(e) => { e.stopPropagation(); onNewConversation(project); }}
                     className="p-1 hover:text-primary transition-colors hover:bg-white/5 rounded"
                     title="Add new chat for this project"
                   >
                     <Plus size={12} />
                   </button>
                   <ChevronDown size={12} className="text-zinc-700" />
                </div>
              </div>
              
              <div className="space-y-1 ml-1 border-l border-white/5 pl-4 py-1">
                {projectConvs.map(conv => (
                  <button
                    key={conv.id}
                    onClick={() => onSelectConversation(conv.id)}
                    className={cn(
                      "w-full text-left p-3 rounded text-[11px] transition-all group relative",
                      activeConversationId === conv.id 
                        ? 'bg-primary/5 text-primary border border-primary/20' 
                        : 'text-zinc-500 hover:text-white hover:bg-white/5 border border-transparent'
                    )}
                  >
                    <div className="truncate font-bold tracking-tight mb-1">{conv.title}</div>
                    <div className="flex items-center space-x-2 text-[9px] opacity-40 font-mono">
                      <Clock size={10} />
                      <span className="uppercase">{new Date(conv.lastActive).toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' })}</span>
                    </div>
                    {activeConversationId === conv.id && (
                      <motion.div 
                        layoutId="active-indicator"
                        className="absolute left-[-17px] top-1/2 -translate-y-1/2 w-1.5 h-6 bg-primary glow-primary" 
                      />
                    )}
                  </button>
                ))}
                
                {projectConvs.length === 0 && (
                  <button 
                    onClick={() => onNewConversation(project)}
                    className="w-full text-left p-3 text-[10px] text-zinc-800 italic hover:text-zinc-600 transition-colors flex items-center gap-2 group"
                  >
                    <FolderSync size={10} className="group-hover:text-primary" />
                    Initialize deep context...
                  </button>
                )}
              </div>
            </div>
          );
        })}
      </div>

      <div className="p-5 border-t border-white/5 bg-zinc-950/50">
        <div className="flex items-center gap-4">
          <div className="flex items-center justify-center w-8 h-8 rounded shrink-0 bg-primary/5 border border-primary/20">
             <div className="w-2 h-2 rounded-full bg-primary glow-primary animate-pulse" />
          </div>
          <div className="flex flex-col">
            <span className="text-[10px] font-black text-white uppercase tracking-widest">Jules_Service</span>
            <span className="text-[8px] font-mono text-zinc-600 uppercase tracking-tighter">Heartbeat: Nominal</span>
          </div>
        </div>
      </div>
    </div>
  );
};
