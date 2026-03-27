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
            className="relative w-full max-w-lg bg-black border border-border p-10 shadow-2xl overflow-hidden"
          >
            <div className="absolute top-0 left-0 w-full h-1 bg-primary" />
            
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
                <h2 className="text-xl font-bold tracking-[0.1em] text-white uppercase italic">Add Existing Project</h2>
                <p className="text-[10px] text-zinc-600 font-bold uppercase tracking-widest mt-1">Link a local repository from your machine</p>
              </div>
            </div>

            <form onSubmit={handleSubmit} className="space-y-6">
              <div className="space-y-2">
                <label className="text-[10px] font-bold uppercase tracking-[0.2em] text-zinc-500 ml-1">Project Name</label>
                <input 
                  type="text"
                  value={name}
                  onChange={(e) => setName(e.target.value)}
                  placeholder="e.g. my-awesome-app"
                  className="w-full bg-black border border-border px-5 py-4 text-sm text-white placeholder:text-zinc-900 outline-none focus:border-primary transition-all font-mono"
                  autoFocus
                />
              </div>

              <div className="space-y-2">
                <label className="text-[10px] font-bold uppercase tracking-[0.2em] text-zinc-500 ml-1">Absolute File Path</label>
                <input 
                  type="text"
                  value={path}
                  onChange={(e) => setPath(e.target.value)}
                  placeholder="e.g. /home/user/projects/my-app"
                  className="w-full bg-black border border-border px-5 py-4 text-sm text-white placeholder:text-zinc-900 outline-none focus:border-primary transition-all font-mono"
                />
              </div>

              <div className="p-4 bg-primary/5 border border-primary/10 rounded-2xl flex gap-3 text-zinc-400 text-xs">
                <Info size={16} className="text-primary shrink-0 mt-0.5" />
                <p>The orchestrator will use this path to analyze the codebase and generate technical roadmap tasks.</p>
              </div>

              <button 
                type="submit"
                disabled={!name || !path}
                className="w-full py-5 bg-primary text-black text-[12px] font-black uppercase tracking-[0.4em] shadow-lg hover:bg-primary/90 transition-all disabled:opacity-30 disabled:grayscale-0 glow-primary"
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
