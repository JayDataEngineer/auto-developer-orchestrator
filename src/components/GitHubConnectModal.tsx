import React, { useState } from 'react';
import { X, Github, ShieldCheck, ExternalLink } from 'lucide-react';
import { motion, AnimatePresence } from 'motion/react';

interface GitHubConnectModalProps {
  isOpen: boolean;
  onClose: () => void;
  onConnect: (token: string) => void;
}

export const GitHubConnectModal: React.FC<GitHubConnectModalProps> = ({ isOpen, onClose, onConnect }) => {
  const [token, setToken] = useState('');
  const [isConnecting, setIsConnecting] = useState(false);

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    if (token) {
      setIsConnecting(true);
      try {
        await onConnect(token);
        setToken('');
        onClose();
      } finally {
        setIsConnecting(false);
      }
    }
  };

  return (
    <AnimatePresence>
      {isOpen && (
        <div className="fixed inset-0 z-[100] flex items-center justify-center p-4">
          <motion.div 
            initial={{ opacity: 0 }}
            animate={{ opacity: 1 }}
            exit={{ opacity: 0 }}
            onClick={onClose}
            className="absolute inset-0 bg-black/80 backdrop-blur-md" 
          />
          <motion.div 
            initial={{ scale: 0.95, opacity: 0, y: 20 }}
            animate={{ scale: 1, opacity: 1, y: 0 }}
            exit={{ scale: 0.95, opacity: 0, y: 20 }}
            className="relative w-full max-w-lg bg-black border border-border p-10 shadow-[0_0_50px_rgba(255,0,255,0.1)] overflow-hidden"
          >
            <div className="absolute top-0 left-0 w-full h-1 bg-primary" />
            
            <button 
              onClick={onClose}
              className="absolute top-6 right-6 p-2 text-zinc-600 hover:text-primary transition-colors"
            >
              <X size={20} />
            </button>

            <div className="flex items-center gap-4 mb-8">
              <div className="p-3 bg-primary/10 border border-primary/20 text-primary">
                <Github size={24} />
              </div>
              <div>
                <h2 className="text-xl font-black tracking-[0.2em] text-white uppercase italic">Connect GitHub</h2>
                <p className="text-[10px] text-zinc-700 font-bold uppercase tracking-widest mt-1">Authorize Orchestrator for Real-World Ops</p>
              </div>
            </div>

            <form onSubmit={handleSubmit} className="space-y-6">
              <div className="space-y-2">
                <div className="flex justify-between items-end mb-1">
                  <label className="text-[10px] font-bold uppercase tracking-[0.2em] text-zinc-500 ml-1">Personal Access Token (classic)</label>
                  <a 
                    href="https://github.com/settings/tokens" 
                    target="_blank" 
                    rel="noopener noreferrer"
                    className="text-[10px] text-primary hover:underline flex items-center gap-1 font-bold uppercase tracking-widest"
                  >
                    Generate <ExternalLink size={10} />
                  </a>
                </div>
                <input 
                  type="password"
                  value={token}
                  onChange={(e) => setToken(e.target.value)}
                  placeholder="ghp_xxxxxxxxxxxxxxxxxxxx"
                  className="w-full bg-black border border-border px-5 py-4 text-sm text-white placeholder:text-zinc-900 outline-none focus:border-primary transition-all font-mono"
                  autoFocus
                />
              </div>

              <div className="p-5 bg-zinc-950 border border-border flex gap-4 text-zinc-500 text-[11px] leading-relaxed">
                <ShieldCheck size={18} className="text-primary shrink-0" />
                <div>
                  <p className="font-bold text-zinc-400 uppercase tracking-wider mb-1">Security Protocol</p>
                  <p>Tokens are stored locally in your <code className="text-primary">.env</code> file. The orchestrator requires <code className="text-white">'repo'</code> scopes to clone, merge, and dispatch autonomously.</p>
                </div>
              </div>

              <button 
                type="submit"
                disabled={!token || isConnecting}
                className="w-full py-5 bg-primary text-black text-[12px] font-black uppercase tracking-[0.4em] shadow-lg hover:bg-primary/90 transition-all disabled:opacity-30 disabled:grayscale-0 glow-primary"
              >
                {isConnecting ? 'VERIFYING_AUTH...' : 'INITIALIZE_CONNECTION'}
              </button>
            </form>

            <div className="mt-8 pt-8 border-t border-border/50 text-center">
              <p className="text-[10px] text-zinc-800 font-bold uppercase tracking-[0.3em]">
                SYS_AUTH_READY // WAITING_FOR_INPUT
              </p>
            </div>
          </motion.div>
        </div>
      )}
    </AnimatePresence>
  );
};
