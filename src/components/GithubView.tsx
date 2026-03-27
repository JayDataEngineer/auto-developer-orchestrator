import React, { useState, useEffect } from 'react';
import { Github, GitPullRequest, GitBranch, Check, Clock, Star, AlertCircle, GitFork, Eye } from 'lucide-react';
import { cn } from '../lib/utils';

interface GithubViewProps {
  repoOwner?: string;
  repoName?: string;
}

export const GithubView: React.FC<GithubViewProps> = ({ 
  repoOwner, 
  repoName
}) => {
  const [prs, setPrs] = useState<any[]>([]);
  const [stats, setStats] = useState<any>(null);
  const [branches, setBranches] = useState<any[]>([]);
  const [isLoading, setIsLoading] = useState(true);

  useEffect(() => {
    if (repoName) {
      fetchData();
    }
  }, [repoOwner, repoName]);

  const fetchData = async () => {
    setIsLoading(true);
    try {
      const query = `owner=${repoOwner}&repo=${repoName}`;
      const [prRes, statsRes, branchRes] = await Promise.all([
        fetch(`/api/github/prs?${query}`),
        fetch(`/api/github/stats?${query}`),
        fetch(`/api/github/branches?${query}`)
      ]);

      const prData = await prRes.json();
      const statsData = await statsRes.json();
      const branchData = await branchRes.json();

      setPrs(prData.prs || []);
      setStats(statsData.stats);
      setBranches(branchData.branches || []);
    } catch (e) {
      console.error("Failed to fetch GitHub data", e);
    } finally {
      setIsLoading(false);
    }
  };

  if (!repoOwner) {
    return (
      <div className="flex-1 flex flex-col items-center justify-center bg-black p-10 text-center">
        <div className="w-20 h-20 rounded-full bg-primary/5 flex items-center justify-center mb-8 border border-primary/20 glow-primary">
          <Github size={40} className="text-primary" />
        </div>
        <h3 className="text-xl font-black italic uppercase tracking-[0.3em] text-white mb-4">Connection Required</h3>
        <p className="text-zinc-500 text-xs uppercase tracking-widest max-w-md leading-relaxed mb-8">
          The GitHub real-time stream requires an active PAT connection to verify repository ownership and fetch live requests.
        </p>
        <div className="text-[10px] font-mono text-zinc-700 uppercase tracking-[0.5em] font-bold">
          STATUS: UNAUTHORIZED // SESSION_IDLE
        </div>
      </div>
    );
  }

  if (!repoName) {
    return (
      <div className="flex-1 flex items-center justify-center bg-black text-zinc-500 uppercase tracking-[0.2em] font-bold text-xs">
        Select a project to view GitHub integration
      </div>
    );
  }

  return (
    <div className="flex-1 p-6 lg:p-10 hide-scrollbar overflow-y-auto bg-black text-slate-200">
      <div className="max-w-5xl mx-auto">
        <div className="flex flex-col md:flex-row md:items-end justify-between gap-6 mb-12">
          <div>
            <div className="flex items-center gap-3 mb-2">
              <Github className="text-primary" size={20} />
              <span className="text-[10px] font-black uppercase tracking-[0.4em] text-zinc-500">Repository Live Stream</span>
            </div>
            <h2 className="text-4xl font-black italic uppercase tracking-tight text-white">
              {repoOwner}/<span className="text-primary glow-primary">{repoName}</span>
            </h2>
          </div>
          
          <div className="flex gap-4">
            <button 
              onClick={fetchData}
              className="px-4 py-2 border border-border text-[10px] font-bold uppercase tracking-widest hover:bg-white/5 transition-all"
            >
              Refresh_Feed
            </button>
          </div>
        </div>
        
        {/* Real Stats */}
        <div className="grid grid-cols-2 md:grid-cols-4 gap-4 mb-12">
          {[
            { label: 'Stars', value: stats?.stars ?? '-', icon: Star, color: 'text-yellow-400' },
            { label: 'Open Issues', value: stats?.issues ?? '-', icon: AlertCircle, color: 'text-red-400' },
            { label: 'Forks', value: stats?.forks ?? '-', icon: GitFork, color: 'text-blue-400' },
            { label: 'Branches', value: branches.length || '-', icon: GitBranch, color: 'text-primary' },
          ].map((stat, i) => (
            <div key={i} className="p-6 border border-border bg-zinc-950 flex flex-col gap-2 relative overflow-hidden group">
              <div className="absolute top-0 right-0 p-2 opacity-10 group-hover:opacity-20 transition-opacity">
                <stat.icon size={40} />
              </div>
              <span className="text-[10px] text-zinc-600 font-bold uppercase tracking-widest">{stat.label}</span>
              <span className={cn("text-3xl font-black", stat.color)}>{stat.value}</span>
            </div>
          ))}
        </div>

        <div className="grid grid-cols-1 lg:grid-cols-3 gap-10">
          <div className="lg:col-span-2 space-y-6">
            <h3 className="text-[11px] font-black italic uppercase tracking-[0.3em] text-zinc-500 flex items-center gap-2">
              <GitPullRequest size={14} className="text-primary" /> Active Pull Requests
            </h3>
            
            <div className="space-y-3">
              {isLoading ? (
                Array(3).fill(0).map((_, i) => (
                  <div key={i} className="h-24 w-full bg-zinc-900/50 animate-pulse border border-border" />
                ))
              ) : prs.length === 0 ? (
                <div className="p-10 border border-border border-dashed text-center text-zinc-600 text-[10px] uppercase font-bold tracking-widest">
                  No active pull requests found
                </div>
              ) : (
                prs.map(pr => (
                  <a 
                    key={pr.id} 
                    href={pr.html_url}
                    target="_blank"
                    rel="noopener noreferrer"
                    className="flex flex-col sm:flex-row sm:items-center justify-between p-5 border border-border bg-zinc-950 hover:border-primary/30 transition-all gap-4 group"
                  >
                    <div className="flex items-start gap-4">
                      <div className="mt-1">
                        <GitPullRequest className={pr.state === 'open' ? 'text-emerald-400' : 'text-purple-400'} size={18} />
                      </div>
                      <div>
                        <div className="font-bold text-white text-md group-hover:text-primary transition-colors leading-tight mb-1">{pr.title}</div>
                        <div className="text-[10px] text-zinc-500 font-bold uppercase tracking-widest flex flex-wrap items-center gap-3">
                          <span className="text-zinc-400">#{pr.number}</span>
                          <span className="w-1 h-1 bg-zinc-800 rounded-full" />
                          <span className="flex items-center gap-1.5"><GitBranch size={12} /> {pr.head.ref}</span>
                          <span className="w-1 h-1 bg-zinc-800 rounded-full" />
                          <span className="flex items-center gap-1.5"><Clock size={12} /> {new Date(pr.created_at).toLocaleDateString()}</span>
                        </div>
                      </div>
                    </div>
                    <div className="flex-shrink-0">
                      <span className={cn(
                        "px-3 py-1 text-[9px] font-black uppercase tracking-tighter border",
                        pr.state === 'open' ? "border-emerald-500/30 text-emerald-400 bg-emerald-500/5" : "border-zinc-800 text-zinc-500"
                      )}>
                        {pr.state}
                      </span>
                    </div>
                  </a>
                ))
              )}
            </div>
          </div>

          <div className="space-y-6">
            <h3 className="text-[11px] font-black italic uppercase tracking-[0.3em] text-zinc-500 flex items-center gap-2">
              <GitBranch size={14} className="text-primary" /> Repository Branches
            </h3>
            <div className="border border-border bg-zinc-950 divide-y divide-border">
              {isLoading ? (
                Array(5).fill(0).map((_, i) => (
                  <div key={i} className="h-10 w-full bg-zinc-900/50 animate-pulse" />
                ))
              ) : branches.length === 0 ? (
                <div className="p-4 text-center text-zinc-600 text-[10px] uppercase font-bold tracking-widest">
                  No branches found
                </div>
              ) : (
                branches.map(branch => (
                  <div key={branch.name} className="p-4 flex items-center justify-between hover:bg-white/5 transition-colors">
                    <span className="text-[11px] font-bold text-zinc-300 font-mono tracking-wider">{branch.name}</span>
                    {branch.name === 'main' || branch.name === 'master' ? (
                      <span className="text-[8px] bg-primary/10 text-primary border border-primary/20 px-1.5 py-0.5 font-black uppercase tracking-tighter shadow-[0_0_5px_rgba(255,0,255,0.2)]">Protected</span>
                    ) : null}
                  </div>
                ))
              )}
            </div>
          </div>
        </div>
      </div>
    </div>
  );
};
