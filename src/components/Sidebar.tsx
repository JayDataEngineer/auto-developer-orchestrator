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
        "fixed inset-y-0 left-0 z-[70] w-full lg:w-16 bg-zinc-950 border-r border-white/10 flex flex-col items-center py-6 gap-8 transition-transform duration-300 lg:translate-x-0 lg:static shrink-0",
        isOpen ? "translate-x-0" : "-translate-x-full"
      )}>
        <div className="flex items-center justify-between w-full px-6 lg:px-0 lg:justify-center">
          <div className="w-10 h-10 rounded-lg bg-primary/20 border border-primary/50 flex items-center justify-center text-primary shadow-lg shadow-primary/10">
            <Terminal size={20} />
          </div>
          <button 
            onClick={onClose}
            className="lg:hidden text-zinc-500 hover:text-white p-2"
          >
            <X size={24} />
          </button>
        </div>
        
        <nav className="flex flex-col gap-6 w-full px-6 lg:px-0 lg:items-center">
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
                "flex items-center gap-4 lg:justify-center text-zinc-500 hover:text-white transition-colors relative group w-full lg:w-auto",
                activeTab === item.id && "text-white"
              )}
            >
              <item.icon size={20} />
              <span className="lg:hidden text-sm font-medium">{item.label}</span>
              <span className="hidden lg:block absolute left-full ml-4 px-2 py-1 bg-zinc-900 border border-white/10 text-[10px] text-white rounded opacity-0 group-hover:opacity-100 pointer-events-none whitespace-nowrap z-50 transition-opacity">
                {item.label}
              </span>
              {activeTab === item.id && (
                <div className="absolute -left-6 lg:-left-3 top-1/2 -translate-y-1/2 w-1 h-6 lg:h-4 bg-primary rounded-r-full" />
              )}
            </button>
          ))}
        </nav>

        <div className="mt-auto w-full px-6 lg:px-0 flex justify-center">
          <div 
            onClick={onUserClick}
            className="w-10 h-10 lg:w-8 lg:h-8 rounded-full bg-zinc-800 border border-white/10 overflow-hidden cursor-pointer hover:border-primary/50 transition-colors"
          >
            <img src="https://api.dicebear.com/7.x/avataaars/svg?seed=Felix" alt="Avatar" referrerPolicy="no-referrer" />
          </div>
        </div>
      </aside>
    </>
  );
};
