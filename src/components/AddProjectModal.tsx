import React, { useState, useEffect } from 'react';
import { 
  X, FolderPlus, Info, Github, Search, Check, 
  Lock, Globe, Loader2, RefreshCw, ChevronRight 
} from 'lucide-react';
import { motion, AnimatePresence } from 'motion/react';
import { cn } from '../lib/utils';
import { api, Repo } from '../lib/api';


interface AddProjectModalProps {
  isOpen: boolean;
  onClose: () => void;
  onAdd: (name: string, path?: string, repoUrl?: string) => void;
}

export const AddProjectModal: React.FC<AddProjectModalProps> = ({ isOpen, onClose, onAdd }) => {
  const [tab, setTab] = useState<'local' | 'github'>('local');
  const [name, setName] = useState('');
  const [path, setPath] = useState('');
  const [repoUrl, setRepoUrl] = useState('');
  const [repos, setRepos] = useState<Repo[]>([]);
  const [searchQuery, setSearchQuery] = useState('');
  const [isLoadingRepos, setIsLoadingRepos] = useState(false);
  const [isGitHubConnected, setIsGitHubConnected] = useState(true);
  const [selectedRepo, setSelectedRepo] = useState<string | null>(null);

  useEffect(() => {
    if (isOpen && tab === 'github') {
      fetchRepos();
    }
  }, [isOpen, tab]);

  const fetchRepos = async () => {
    setIsLoadingRepos(true);
    try {
      const data = await api.github.getRepos();
      if (data.connected) {
        setRepos(data.repos || []);
        setIsGitHubConnected(true);
      } else {
        setIsGitHubConnected(false);
      }
    } catch (e) {
      console.error("Failed to fetch repos", e);
    } finally {
      setIsLoadingRepos(false);
    }
  };

  const filteredRepos = repos.filter(r => 
    r.name.toLowerCase().includes(searchQuery.toLowerCase()) || 
    r.full_name.toLowerCase().includes(searchQuery.toLowerCase())
  );

  const handleSelectRepo = (repo: Repo) => {
    setSelectedRepo(repo.full_name);
    setName(repo.name);
    setRepoUrl(repo.html_url);
  };

  const handleSubmit = (e: React.FormEvent) => {
    e.preventDefault();
    if (tab === 'local' && name && path) {
      onAdd(name, path);
    } else if (tab === 'github' && name && repoUrl) {
      onAdd(name, undefined, repoUrl);
    }
    resetForm();
    onClose();
  };

  const resetForm = () => {
    setName('');
    setPath('');
    setRepoUrl('');
    setSelectedRepo(null);
    setSearchQuery('');
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
            initial={{ scale: 0.9, opacity: 0, y: 40 }}
            animate={{ scale: 1, opacity: 1, y: 0 }}
            exit={{ scale: 0.9, opacity: 0, y: 40 }}
            className="relative w-full max-w-2xl bg-black border border-white/10 shadow-[0_0_50px_rgba(0,0,0,1)] overflow-hidden flex flex-col max-h-[90vh]"
          >
            {/* Header / Accent */}
            <div className="absolute top-0 left-0 w-full h-1 bg-gradient-to-r from-primary via-white/20 to-primary" />
            
            <button 
              onClick={onClose}
              className="absolute top-8 right-8 p-2 text-zinc-600 hover:text-white transition-colors z-10"
            >
              <X size={20} />
            </button>

            <div className="p-10 pb-6 shrink-0">
              <div className="flex items-center gap-6 mb-10">
                <div className="w-16 h-16 bg-primary/5 border border-primary/20 rounded-2xl flex items-center justify-center text-primary glow-primary shrink-0">
                  <FolderPlus size={32} />
                </div>
                <div>
                  <h2 className="text-2xl font-black italic uppercase tracking-[0.2em] text-white">Project Discovery</h2>
                  <p className="text-sm text-zinc-600 font-bold uppercase tracking-[0.4em] mt-2">Initialize or link systemic codebases</p>
                </div>
              </div>

              {/* Tab Switcher */}
              <div className="flex gap-2 p-1 bg-zinc-900/50 border border-white/5 rounded-sm max-w-[320px]">
                <button 
                  onClick={() => setTab('local')}
                  className={cn(
                    "flex-1 py-2 text-sm font-black uppercase tracking-widest transition-all",
                    tab === 'local' ? "bg-white text-black" : "text-zinc-500 hover:text-zinc-300"
                  )}
                >
                  Local Disk
                </button>
                <button 
                  onClick={() => setTab('github')}
                  className={cn(
                    "flex-1 py-2 text-sm font-black uppercase tracking-widest transition-all flex items-center justify-center gap-2",
                    tab === 'github' ? "bg-white text-black" : "text-zinc-500 hover:text-zinc-300"
                  )}
                >
                  <Github size={12} /> GitHub Cloud
                </button>
              </div>
            </div>

            <div className="flex-1 overflow-y-auto px-10 pb-10 custom-scrollbar">
              <form onSubmit={handleSubmit} className="space-y-8">
                {tab === 'local' ? (
                  <motion.div 
                    initial={{ opacity: 0, x: -10 }} 
                    animate={{ opacity: 1, x: 0 }}
                    className="space-y-6"
                  >
                    <div className="space-y-3">
                      <label className="text-xs font-black uppercase tracking-[0.3em] text-zinc-500 ml-1">Identity Profile</label>
                      <input 
                        type="text"
                        value={name}
                        onChange={(e) => setName(e.target.value)}
                        placeholder="Project Name (UID)"
                        className="w-full bg-black border border-white/5 px-6 py-5 text-sm text-white placeholder:text-zinc-800 outline-none focus:border-primary transition-all font-mono"
                        autoFocus
                      />
                    </div>

                    <div className="space-y-3">
                      <label className="text-xs font-black uppercase tracking-[0.3em] text-zinc-500 ml-1">Absolute Logic Path</label>
                      <input 
                        type="text"
                        value={path}
                        onChange={(e) => setPath(e.target.value)}
                        placeholder="/home/dev/matrix-v3"
                        className="w-full bg-black border border-white/5 px-6 py-5 text-sm text-white placeholder:text-zinc-800 outline-none focus:border-primary transition-all font-mono"
                      />
                    </div>
                  </motion.div>
                ) : (
                  <motion.div 
                    initial={{ opacity: 0, x: 10 }} 
                    animate={{ opacity: 1, x: 0 }}
                    className="space-y-6"
                  >
                    {!isGitHubConnected ? (
                      <div className="p-12 border border-dashed border-white/5 flex flex-col items-center justify-center text-center gap-6">
                        <Github size={48} className="text-zinc-800" />
                        <div className="space-y-2">
                           <h3 className="text-sm font-bold text-white uppercase tracking-widest">Authentication Required</h3>
                           <p className="text-sm text-zinc-600 uppercase tracking-widest max-w-xs mx-auto leading-relaxed">Please connect your GitHub account in the Settings panel to browse your cloud repositories.</p>
                        </div>
                      </div>
                    ) : (
                      <div className="space-y-6">
                        <div className="relative">
                          <Search className="absolute left-5 top-1/2 -translate-y-1/2 text-zinc-600" size={16} />
                          <input 
                            type="text"
                            value={searchQuery}
                            onChange={(e) => setSearchQuery(e.target.value)}
                            placeholder="Filter Cloud Repositories..."
                            className="w-full bg-zinc-900/30 border border-white/5 pl-14 pr-6 py-5 text-sm text-white placeholder:text-zinc-700 outline-none focus:border-primary transition-all font-mono"
                          />
                          {isLoadingRepos && (
                            <Loader2 className="absolute right-5 top-1/2 -translate-y-1/2 text-primary animate-spin" size={16} />
                          )}
                        </div>

                        <div className="max-h-[300px] overflow-y-auto border border-white/5 bg-zinc-950/50 rounded-sm divide-y divide-white/5 custom-scrollbar">
                          {filteredRepos.map(repo => (
                            <div 
                              key={repo.full_name}
                              onClick={() => handleSelectRepo(repo)}
                              className={cn(
                                "group p-4 flex items-center justify-between cursor-pointer transition-all",
                                selectedRepo === repo.full_name ? "bg-primary/10 border-l-2 border-primary" : "hover:bg-white/5"
                              )}
                            >
                              <div className="flex items-center gap-4">
                                <div className={cn("shrink-0", selectedRepo === repo.full_name ? "text-primary" : "text-zinc-700 group-hover:text-zinc-400")}>
                                  {repo.private ? <Lock size={14} /> : <Globe size={14} />}
                                </div>
                                <div>
                                  <div className={cn("text-xs font-bold transition-colors", selectedRepo === repo.full_name ? "text-white" : "text-zinc-400 group-hover:text-zinc-200")}>
                                    {repo.name}
                                  </div>
                                  <div className="text-xs text-zinc-600 font-mono mt-0.5 line-clamp-1 italic">{repo.description || "No description provided."}</div>
                                </div>
                              </div>
                              <div className="flex items-center gap-4">
                                <span className="text-xs font-mono text-zinc-800 uppercase tracking-tighter">Updated {new Date(repo.updated_at).toLocaleDateString()}</span>
                                <div className={cn(
                                  "w-6 h-6 rounded-full border border-white/10 flex items-center justify-center transition-all",
                                  selectedRepo === repo.full_name ? "bg-primary border-primary text-black" : "group-hover:border-primary/50 text-transparent"
                                )}>
                                  <Check size={12} strokeWidth={3} />
                                </div>
                              </div>
                            </div>
                          ))}
                          
                          {!isLoadingRepos && filteredRepos.length === 0 && (
                            <div className="p-10 text-center text-sm font-mono text-zinc-700 uppercase tracking-[0.4em]">No matching repositories found.</div>
                          )}
                        </div>
                      </div>
                    )}
                  </motion.div>
                )}

                <div className="p-5 bg-primary/5 border border-primary/10 rounded-sm flex gap-4">
                  <Info size={18} className="text-primary shrink-0 mt-0.5" />
                  <div className="space-y-1">
                    <div className="text-sm font-black uppercase tracking-[0.2em] text-white">System Protocol</div>
                    <p className="text-xs text-zinc-500 font-mono tracking-tight leading-normal">
                      {tab === 'local' 
                        ? "Registering a local path allows instantaneous analysis. Ensure the path is absolute." 
                        : "Choosing a GitHub repo will initiate a background clone into the system library. This may take several moments."}
                    </p>
                  </div>
                </div>

                <div className="flex flex-col gap-4">
                   {tab === 'github' && selectedRepo && (
                     <div className="text-center text-sm font-mono text-zinc-600 uppercase tracking-widest animate-pulse">
                        Selected: <span className="text-primary">{selectedRepo}</span>
                     </div>
                   )}
                   <button 
                    type="submit"
                    disabled={!name || (tab === 'local' ? !path : !selectedRepo)}
                    className="w-full py-6 bg-primary text-black text-sm font-black uppercase tracking-[0.5em] shadow-lg hover:bg-primary/90 transition-all disabled:opacity-20 disabled:grayscale glow-primary relative overflow-hidden group"
                  >
                    <span className="relative z-10">{tab === 'local' ? 'Register Local Link' : 'Initialize Cloud Sync'}</span>
                    <div className="absolute inset-0 bg-white/20 translate-x-[-100%] group-hover:translate-x-[100%] transition-transform duration-700 skew-x-12" />
                  </button>
                </div>
              </form>
            </div>
          </motion.div>
        </div>
      )}
    </AnimatePresence>
  );
};
