import React from 'react';
import { motion, AnimatePresence } from 'motion/react';
import { X, User, Mail, Shield, LogOut, ExternalLink } from 'lucide-react';

interface UserModalProps {
  isOpen: boolean;
  onClose: () => void;
  userEmail: string;
  userName?: string;
  avatarUrl?: string;
}

export const UserModal: React.FC<UserModalProps> = ({ isOpen, onClose, userEmail, userName, avatarUrl }) => {
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
            className="relative w-full max-w-sm bg-black border border-border overflow-hidden shadow-[0_0_40px_rgba(255,0,255,0.1)]"
          >
            <div className="p-6 border-b border-border">
              <div className="flex items-center justify-between mb-6">
                <div className="w-16 h-16 bg-black border border-primary overflow-hidden shadow-[0_0_10px_rgba(255,0,255,0.3)]">
                  <img src={avatarUrl || "https://api.dicebear.com/7.x/avataaars/svg?seed=Felix"} alt="Avatar" referrerPolicy="no-referrer" />
                </div>
                <button 
                  onClick={onClose}
                  className="text-zinc-500 hover:text-white transition-colors p-2 hover:bg-white/5 rounded-full"
                >
                  <X size={18} />
                </button>
              </div>
              <h2 className="text-xl font-black text-white tracking-[0.2em] uppercase italic">{userName || "Felix Agent"}</h2>
              <p className="text-[10px] text-primary mt-1 font-bold uppercase tracking-widest flex items-center gap-1.5 opacity-80">
                <Mail size={12} /> {userEmail || "anonymous@orchestrator"}
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

            <div className="p-4 bg-black border-t border-border text-center">
              <p className="text-[10px] text-zinc-800 uppercase tracking-[0.4em] font-black italic">JULES_CORE_v2.1</p>
            </div>
          </motion.div>
        </div>
      )}
    </AnimatePresence>
  );
};
