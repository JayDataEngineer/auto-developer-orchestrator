import React from 'react';
import { Terminal, Activity, Github, Settings, X, Bot, Shield } from 'lucide-react';
import { cn } from '../lib/utils';
import { motion, AnimatePresence } from 'motion/react';

interface SidebarProps {
  activeTab?: string;
  isOpen?: boolean;
  onClose?: () => void;
  onSettingsClick?: () => void;
  onTerminalClick?: () => void;
  onActivityClick?: () => void;
  onGithubClick?: () => void;
  onConnectGitHubClick?: () => void;
  onAgentsClick?: () => void;
  onManifestoClick?: () => void;
  onUserClick?: () => void;
  isGitHubConnected?: boolean;
  githubUser?: any;
}

export const Sidebar: React.FC<SidebarProps> = ({
  activeTab = 'terminal',
  isOpen,
  onClose,
  onSettingsClick,
  onTerminalClick,
  onActivityClick,
  onGithubClick,
  onAgentsClick,
  onManifestoClick,
  onUserClick,
  onConnectGitHubClick,
  isGitHubConnected,
  githubUser
}) => {
  const navItems = [
    { id: 'terminal', icon: Terminal, label: 'Terminal', onClick: onTerminalClick },
    { id: 'activity', icon: Activity, label: 'Activity', onClick: onActivityClick },
    { id: 'github', icon: Github, label: 'GitHub', onClick: onGithubClick },
    { id: 'agents', icon: Bot, label: 'Agents', onClick: onAgentsClick },
    { id: 'manifesto', icon: Shield, label: 'Manifesto', onClick: onManifestoClick },
    { id: 'settings', icon: Settings, label: 'Settings', onClick: onSettingsClick },
  ];

  return (
    <>
      {/* Mobile Overlay */}
      <AnimatePresence>
        {isOpen && (
          <motion.div
            initial={{ opacity: 0 }}
            animate={{ opacity: 1 }}
            exit={{ opacity: 0 }}
            onClick={onClose}
            className="fixed inset-0 bg-black/80 backdrop-blur-md z-[60] lg:hidden"
          />
        )}
      </AnimatePresence>

      <aside className={cn(
        "fixed inset-y-0 left-0 z-[70] w-full lg:w-20 bg-black border-r border-border flex flex-col items-center py-8 gap-10 transition-transform duration-300 lg:translate-x-0 lg:static shrink-0",
        isOpen ? "translate-x-0" : "-translate-x-full"
      )}>
        <div className="flex items-center justify-between w-full px-8 lg:px-0 lg:justify-center">
          <div className="w-12 h-12 border-2 border-primary flex items-center justify-center text-primary glow-primary">
            <Bot size={26} strokeWidth={2.5} />
          </div>
          <button 
            onClick={onClose}
            className="lg:hidden text-zinc-400 hover:text-white p-2"
          >
            <X size={28} />
          </button>
        </div>
        
        <nav className="flex flex-col gap-8 w-full px-8 lg:px-0 lg:items-center">
          {navItems.map((item) => (
            <button 
              key={item.id}
              title={item.label}
              onClick={() => {
                if (item.onClick) {
                  item.onClick();
                  if (onClose) onClose();
                }
              }}
              className={cn(
                "flex items-center gap-4 lg:justify-center text-zinc-600 hover:text-white transition-all duration-200 relative group w-full lg:w-auto",
                activeTab === item.id && "text-white"
              )}
            >
              <item.icon size={22} strokeWidth={activeTab === item.id ? 2.5 : 2} className={cn("transition-all duration-200", activeTab === item.id && "text-primary")} />
              <span className="lg:hidden text-[10px] font-bold uppercase tracking-[0.2em]">{item.label}</span>
              <span className="hidden lg:block absolute left-full ml-4 px-3 py-1.5 bg-secondary border border-border text-[10px] font-bold uppercase tracking-tighter text-white opacity-0 group-hover:opacity-100 pointer-events-none whitespace-nowrap z-50 transition-opacity">
                {item.label}
              </span>
              {activeTab === item.id && (
                <div className="absolute -left-8 lg:-left-4 top-1/2 -translate-y-1/2 w-1.5 h-10 lg:h-8 bg-primary glow-primary shadow-[0_0_15px_rgba(255,0,255,0.6)]" />
              )}
            </button>
          ))}
        </nav>

        <div className="mt-auto w-full px-8 lg:px-0 flex flex-col items-center gap-6 pb-6">
          {!isGitHubConnected ? (
            <button 
              onClick={onConnectGitHubClick}
              className="w-12 h-12 lg:w-10 lg:h-10 border border-zinc-800 flex items-center justify-center text-zinc-600 hover:text-primary hover:border-primary transition-all duration-200 group"
              title="Connect GitHub"
            >
              <Github size={20} />
            </button>
          ) : (
            <button 
              onClick={onUserClick}
              className="w-12 h-12 lg:w-10 lg:h-10 border border-primary/50 overflow-hidden cursor-pointer hover:border-primary transition-all duration-200 glow-primary"
              title={`Logged in as ${githubUser?.login}`}
            >
              <img src={githubUser?.avatar_url} alt="Avatar" referrerPolicy="no-referrer" className="w-full h-full object-cover" />
            </button>
          )}
          
          <button 
            onClick={onSettingsClick}
            className="text-zinc-600 hover:text-white transition-colors"
            title="System Settings"
          >
            <Settings size={20} />
          </button>
        </div>
      </aside>
    </>
  );
};
