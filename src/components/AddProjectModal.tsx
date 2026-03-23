import React, { useState } from 'react';
import { X, FolderPlus, Info } from 'lucide-react';
import { motion, AnimatePresence } from 'motion/react';

interface AddProjectModalProps {
  isOpen: boolean;
  onClose: () => void;
  onAdd: (name: string, path: string) => void;
}

export const AddProjectModal: React.FC<AddProjectModalProps> = ({ isOpen, onClose, onAdd }) => {
  const [name, setName] = useState('');
  const [path, setPath] = useState('');

  const handleSubmit = (e: React.FormEvent) => {
    e.preventDefault();
    if (name && path) {
      onAdd(name, path);
      setName('');
      setPath('');
      onClose();
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
            className="absolute inset-0 bg-black/60 backdrop-blur-sm" 
          />
          <motion.div 
            initial={{ scale: 0.95, opacity: 0, y: 20 }}
            animate={{ scale: 1, opacity: 1, y: 0 }}
            exit={{ scale: 0.95, opacity: 0, y: 20 }}
            className="relative w-full max-w-lg glass-dark border border-white/10 rounded-3xl p-8 lg:p-10 shadow-2xl overflow-hidden"
          >
            <div className="absolute top-0 left-0 w-full h-1 bg-gradient-to-r from-primary/50 to-primary/20" />
            
            <button 
              onClick={onClose}
              className="absolute top-6 right-6 p-2 text-zinc-500 hover:text-white transition-colors"
            >
              <X size={20} />
            </button>

            <div className="flex items-center gap-4 mb-8">
              <div className="p-3 bg-primary/10 rounded-2xl text-primary">
                <FolderPlus size={24} />
              </div>
              <div>
                <h2 className="text-xl font-bold tracking-tight text-white">Add Existing Project</h2>
                <p className="text-sm text-zinc-400">Link a local repository from your machine</p>
              </div>
            </div>

            <form onSubmit={handleSubmit} className="space-y-6">
              <div className="space-y-2">
                <label htmlFor="projectName" className="text-[10px] font-bold uppercase tracking-[0.2em] text-zinc-500 ml-1">Project Name</label>
                <input 
                  id="projectName"
                  type="text"
                  value={name}
                  onChange={(e) => setName(e.target.value)}
                  placeholder="e.g. my-awesome-app"
                  className="w-full bg-white/5 border border-white/10 rounded-2xl px-5 py-4 text-sm text-white placeholder:text-zinc-600 outline-none focus:border-primary/50 focus:bg-primary/5 transition-all"
                  autoFocus
                />
              </div>

              <div className="space-y-2">
                <label htmlFor="projectPath" className="text-[10px] font-bold uppercase tracking-[0.2em] text-zinc-500 ml-1">Absolute File Path</label>
                <input 
                  id="projectPath"
                  type="text"
                  value={path}
                  onChange={(e) => setPath(e.target.value)}
                  placeholder="e.g. /home/user/projects/my-app"
                  className="w-full bg-white/5 border border-white/10 rounded-2xl px-5 py-4 text-sm text-white placeholder:text-zinc-600 outline-none focus:border-primary/50 focus:bg-primary/5 transition-all text-mono"
                />
              </div>

              <div className="p-4 bg-primary/5 border border-primary/10 rounded-2xl flex gap-3 text-zinc-400 text-xs">
                <Info size={16} className="text-primary shrink-0 mt-0.5" />
                <p>The orchestrator will use this path to analyze the codebase and generate technical roadmap tasks.</p>
              </div>

              <button 
                type="submit"
                disabled={!name || !path}
                className="w-full py-4 bg-primary text-white text-[11px] font-bold uppercase tracking-widest rounded-2xl shadow-lg shadow-primary/20 hover:scale-[1.02] active:scale-[0.98] transition-all disabled:opacity-50 disabled:scale-100 disabled:shadow-none glow-primary"
              >
                Register Project
              </button>
            </form>
          </motion.div>
        </div>
      )}
    </AnimatePresence>
  );
};
