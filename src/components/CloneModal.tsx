import React, { useState } from 'react';
import { motion, AnimatePresence } from 'motion/react';
import { X, GitBranch, Download, Loader2, CheckCircle2 } from 'lucide-react';

interface CloneModalProps {
  isOpen: boolean;
  onClose: () => void;
  onClone: (url: string) => Promise<void>;
}

export const CloneModal: React.FC<CloneModalProps> = ({ isOpen, onClose, onClone }) => {
  const [url, setUrl] = useState('');
  const [isCloning, setIsCloning] = useState(false);
  const [isSuccess, setIsSuccess] = useState(false);

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!url) return;
    
    setIsCloning(true);
    try {
      await onClone(url);
      setIsSuccess(true);
      setTimeout(() => {
        setIsSuccess(false);
        setUrl('');
        onClose();
      }, 2000);
    } catch (error) {
      console.error("Clone failed", error);
    } finally {
      setIsCloning(false);
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
            className="absolute inset-0 bg-black/80 backdrop-blur-sm"
          />
          
          <motion.div 
            initial={{ opacity: 0, scale: 0.95, y: 20 }}
            animate={{ opacity: 1, scale: 1, y: 0 }}
            exit={{ opacity: 0, scale: 0.95, y: 20 }}
            className="relative w-full max-w-md bg-zinc-900 border border-white/10 rounded-3xl overflow-hidden shadow-2xl p-6"
          >
            <div className="flex items-center justify-between mb-6">
              <div className="flex items-center gap-3">
                <div className="w-10 h-10 rounded-xl bg-primary/20 flex items-center justify-center text-primary">
                  <GitBranch size={20} />
                </div>
                <div>
                  <h2 className="text-lg font-bold tracking-tight">Clone Repository</h2>
                  <p className="text-xs text-zinc-500">Add a new project from GitHub</p>
                </div>
              </div>
              <button 
                onClick={onClose}
                className="text-zinc-500 hover:text-white transition-colors p-2 hover:bg-white/5 rounded-full"
              >
                <X size={18} />
              </button>
            </div>

            {isSuccess ? (
              <div className="py-8 flex flex-col items-center justify-center text-center space-y-4">
                <div className="w-16 h-16 rounded-full bg-emerald-500/20 flex items-center justify-center text-emerald-500">
                  <CheckCircle2 size={32} />
                </div>
                <div>
                  <h3 className="text-lg font-bold text-white">Repository Cloned!</h3>
                  <p className="text-sm text-zinc-400">JULES is now analyzing the codebase.</p>
                </div>
              </div>
            ) : (
              <form onSubmit={handleSubmit} className="space-y-4">
                <div className="space-y-2">
                  <label htmlFor="repoUrl" className="text-[10px] font-bold uppercase tracking-widest text-zinc-500">GitHub Repository URL</label>
                  <input 
                    id="repoUrl"
                    type="text"
                    value={url}
                    onChange={(e) => setUrl(e.target.value)}
                    placeholder="https://github.com/username/repo.git"
                    className="w-full bg-black border border-white/10 rounded-xl px-4 py-3 text-sm text-zinc-300 focus:border-primary/50 outline-none transition-all"
                    disabled={isCloning}
                  />
                </div>
                
                <div className="p-3 bg-white/5 rounded-xl border border-white/5">
                  <p className="text-[10px] text-zinc-400 leading-relaxed">
                    JULES will clone this repository into your configured <span className="text-primary">Projects Root Directory</span> and initialize a <span className="text-primary italic">TODO_FOR_JULES.md</span> file.
                  </p>
                </div>

                <button 
                  type="submit"
                  disabled={isCloning || !url}
                  className="w-full bg-primary text-black font-bold py-4 rounded-xl text-xs uppercase tracking-widest hover:bg-primary/90 transition-all flex items-center justify-center gap-2 shadow-lg shadow-primary/20 disabled:opacity-50 disabled:cursor-not-allowed"
                >
                  {isCloning ? (
                    <>
                      <Loader2 size={16} className="animate-spin" />
                      Cloning...
                    </>
                  ) : (
                    <>
                      <Download size={16} />
                      Clone Repository
                    </>
                  )}
                </button>
              </form>
            )}
          </motion.div>
        </div>
      )}
    </AnimatePresence>
  );
};
