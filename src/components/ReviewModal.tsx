import React from 'react';
import { motion, AnimatePresence } from 'motion/react';
import { Github, ExternalLink, CheckCircle2, Circle, Check } from 'lucide-react';

interface ReviewModalProps {
  isOpen: boolean;
  onClose: () => void;
  onMerge?: () => void;
}

export const ReviewModal: React.FC<ReviewModalProps> = ({ isOpen, onClose, onMerge }) => {
  return (
    <AnimatePresence>
      {isOpen && (
        <div className="fixed inset-0 z-50 flex items-center justify-center p-6">
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
            className="relative w-full max-w-2xl bg-zinc-900 border border-white/10 rounded-3xl overflow-hidden shadow-2xl max-h-[90vh] flex flex-col"
          >
            <div className="p-4 lg:p-6 border-b border-white/10 flex items-center justify-between bg-zinc-900/50 shrink-0">
              <div className="flex items-center gap-3">
                <div className="w-8 h-8 lg:w-10 lg:h-10 rounded-xl bg-primary/20 flex items-center justify-center text-primary">
                  <Github size={18} />
                </div>
                <div>
                  <h2 className="text-sm lg:text-lg font-bold tracking-tight">Review Changes: PR #102</h2>
                  <p className="text-[10px] lg:text-xs text-zinc-500">Jules has finished. Tests are passing.</p>
                </div>
              </div>
              <button 
                onClick={onClose}
                className="text-zinc-500 hover:text-white transition-colors p-2 hover:bg-white/5 rounded-full"
              >
                <Circle size={18} className="rotate-45" />
              </button>
            </div>
            
            <div className="p-4 lg:p-6 space-y-4 lg:space-y-6 overflow-y-auto flex-1 terminal-scrollbar">
              <div className="bg-black rounded-xl p-4 font-mono text-[10px] lg:text-xs leading-relaxed border border-white/5 shadow-inner overflow-x-auto">
                <div className="text-zinc-500 mb-2">@@ -45,8 +45,12 @@</div>
                <div className="text-red-400/80">- if (!token) return res.status(401).send('Unauthorized');</div>
                <div className="text-emerald-400">+ if (!token) &#123;</div>
                <div className="text-emerald-400">+   logger.warn('Missing auth token from request headers');</div>
                <div className="text-emerald-400">+   return res.status(401).json(&#123; error: 'Unauthorized', code: 'AUTH_001' &#125;);</div>
                <div className="text-emerald-400">+ &#125;</div>
              </div>

              <div className="flex items-center gap-4 p-4 bg-emerald-500/5 border border-emerald-500/20 rounded-2xl">
                <div className="w-8 h-8 rounded-full bg-emerald-500/20 flex items-center justify-center text-emerald-500 shrink-0">
                  <Check size={16} />
                </div>
                <div className="text-xs">
                  <span className="font-bold text-emerald-500">All checks passed.</span>
                  <p className="text-zinc-500">Unit tests, linting, and security scan successful.</p>
                </div>
              </div>
            </div>

            <div className="p-4 lg:p-6 bg-zinc-950 flex flex-col lg:flex-row gap-3 shrink-0">
              <a 
                href="https://jules.google.com" 
                target="_blank" 
                rel="noopener noreferrer"
                className="flex-1 bg-zinc-800 hover:bg-zinc-700 text-white font-bold py-3 rounded-xl text-[10px] lg:text-xs uppercase tracking-widest transition-all flex items-center justify-center gap-2"
              >
                <ExternalLink size={14} />
                View in Jules
              </a>
              <button 
                onClick={() => {
                  onMerge?.();
                  onClose();
                }}
                className="flex-1 bg-primary text-black font-bold py-3 rounded-xl text-[10px] lg:text-xs uppercase tracking-widest hover:bg-primary/90 transition-all flex items-center justify-center gap-2 shadow-lg shadow-primary/20"
              >
                <CheckCircle2 size={14} />
                Approve & Merge
              </button>
            </div>
          </motion.div>
        </div>
      )}
    </AnimatePresence>
  );
};
