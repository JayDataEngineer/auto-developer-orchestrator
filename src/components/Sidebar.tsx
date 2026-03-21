import React from 'react';
import { Terminal, Activity, Github, Settings, X } from 'lucide-react';
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
  onUserClick?: () => void;
}

export const Sidebar: React.FC<SidebarProps> = ({ 
  activeTab = 'terminal', 
  isOpen, 
  onClose, 
  onSettingsClick,
  onTerminalClick,
  onActivityClick,
  onGithubClick,
  onUserClick
}) => {
  const navItems = [
    { id: 'terminal', icon: Terminal, label: 'Terminal', onClick: onTerminalClick },
    { id: 'activity', icon: Activity, label: 'Activity', onClick: onActivityClick },
    { id: 'github', icon: Github, label: 'GitHub', onClick: onGithubClick },
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
        "fixed inset-y-0 left-0 z-[70] w-full lg:w-20 glass-dark border-r border-white/5 flex flex-col items-center py-8 gap-10 transition-transform duration-300 lg:translate-x-0 lg:static shrink-0",
        isOpen ? "translate-x-0" : "-translate-x-full"
      )}>
        <div className="flex items-center justify-between w-full px-8 lg:px-0 lg:justify-center">
          <div className="w-12 h-12 rounded-xl bg-primary/10 border border-primary/30 flex items-center justify-center text-primary glow-primary animate-pulse-slow">
            <Terminal size={24} />
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
                "flex items-center gap-4 lg:justify-center text-zinc-400 hover:text-white transition-all duration-300 relative group w-full lg:w-auto",
                activeTab === item.id && "text-white"
              )}
            >
              <item.icon size={22} className={cn("transition-transform duration-300 group-hover:scale-110", activeTab === item.id && "text-primary")} />
              <span className="lg:hidden text-base font-medium">{item.label}</span>
              <span className="hidden lg:block absolute left-full ml-4 px-3 py-1.5 glass-dark border border-white/10 text-xs text-white rounded-lg opacity-0 group-hover:opacity-100 pointer-events-none whitespace-nowrap z-50 transition-opacity">
                {item.label}
              </span>
              {activeTab === item.id && (
                <div className="absolute -left-8 lg:-left-4 top-1/2 -translate-y-1/2 w-1.5 h-8 lg:h-6 bg-primary rounded-r-full glow-primary" />
              )}
            </button>
          ))}
        </nav>

        <div className="mt-auto w-full px-8 lg:px-0 flex justify-center pb-4">
          <div 
            onClick={onUserClick}
            className="w-12 h-12 lg:w-10 lg:h-10 rounded-full border border-white/10 overflow-hidden cursor-pointer hover:border-primary/50 transition-all duration-300 hover:scale-110 glow-primary"
          >
            <img src="https://api.dicebear.com/7.x/avataaars/svg?seed=Felix" alt="Avatar" referrerPolicy="no-referrer" />
          </div>
        </div>
      </aside>
    </>
  );
};
