import React from 'react';
import { motion, AnimatePresence } from 'motion/react';
import { X, User, Mail, Shield, LogOut, ExternalLink } from 'lucide-react';

interface UserModalProps {
  isOpen: boolean;
  onClose: () => void;
  userEmail: string;
}

export const UserModal: React.FC<UserModalProps> = ({ isOpen, onClose, userEmail }) => {
  return (
    <AnimatePresence>
      {isOpen && (
        <div className="fixed inset-0 z-[100] flex items-center justify-center p-4">
          <motion.div 
            initial={{ opacity: 0 }}
            animate={{ opacity: 1 }}
            exit={{ opacity: 0 }}
            onClick={onClose}
            className="absolute inset-0 bg-black/80 backdrop-blur-sm"
          />
          
          <motion.div 
            initial={{ opacity: 0, scale: 0.95, y: 20 }}
            animate={{ opacity: 1, scale: 1, y: 0 }}
            exit={{ opacity: 0, scale: 0.95, y: 20 }}
            className="relative w-full max-w-sm bg-zinc-900 border border-white/10 rounded-3xl overflow-hidden shadow-2xl"
          >
            <div className="p-6 bg-gradient-to-br from-primary/20 to-transparent border-b border-white/5">
              <div className="flex items-center justify-between mb-6">
                <div className="w-16 h-16 rounded-2xl bg-zinc-800 border border-white/10 overflow-hidden">
                  <img src="https://api.dicebear.com/7.x/avataaars/svg?seed=Felix" alt="Avatar" referrerPolicy="no-referrer" />
                </div>
                <button 
                  onClick={onClose}
                  className="text-zinc-500 hover:text-white transition-colors p-2 hover:bg-white/5 rounded-full"
                >
                  <X size={18} />
                </button>
              </div>
              <h2 className="text-xl font-bold text-white tracking-tight">Felix Agent</h2>
              <p className="text-xs text-zinc-400 mt-1 flex items-center gap-1.5">
                <Mail size={12} /> {userEmail}
              </p>
            </div>

            <div className="p-4 space-y-1">
              <button className="w-full flex items-center justify-between p-3 rounded-xl hover:bg-white/5 transition-colors group">
                <div className="flex items-center gap-3">
                  <User size={18} className="text-zinc-500 group-hover:text-primary transition-colors" />
                  <span className="text-sm text-zinc-300">Profile Settings</span>
                </div>
                <ExternalLink size={14} className="text-zinc-600" />
              </button>
              
              <button className="w-full flex items-center justify-between p-3 rounded-xl hover:bg-white/5 transition-colors group">
                <div className="flex items-center gap-3">
                  <Shield size={18} className="text-zinc-500 group-hover:text-primary transition-colors" />
                  <span className="text-sm text-zinc-300">Security & Keys</span>
                </div>
                <ExternalLink size={14} className="text-zinc-600" />
              </button>

              <div className="h-[1px] bg-white/5 my-2" />

              <button className="w-full flex items-center gap-3 p-3 rounded-xl hover:bg-red-500/10 transition-colors group text-red-400">
                <LogOut size={18} />
                <span className="text-sm font-medium">Sign Out</span>
              </button>
            </div>

            <div className="p-4 bg-zinc-950/50 border-t border-white/5 text-center">
              <p className="text-[10px] text-zinc-600 uppercase tracking-widest font-bold">JULES v1.4.2-stable</p>
            </div>
          </motion.div>
        </div>
      )}
    </AnimatePresence>
  );
};
