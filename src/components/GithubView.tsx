import React from 'react';
import { Github, GitPullRequest, GitBranch, Check, Clock } from 'lucide-react';

export const GithubView: React.FC = () => {
  const prs = [
    { id: 124, title: 'feat: implement deep agent checklist generation', branch: 'feature/deep-agent', status: 'open', time: '2 hours ago' },
    { id: 123, title: 'fix: resolve SSE streaming issues', branch: 'fix/sse-stream', status: 'merged', time: '5 hours ago' },
    { id: 122, title: 'chore: update dependencies', branch: 'chore/deps', status: 'merged', time: '1 day ago' },
    { id: 121, title: 'refactor: extract sidebar components', branch: 'refactor/sidebar', status: 'merged', time: '2 days ago' },
  ];

  return (
    <div className="flex-1 p-6 lg:p-10 overflow-y-auto bg-black text-slate-200">
      <div className="max-w-4xl mx-auto">
        <h2 className="text-2xl font-semibold mb-8 flex items-center gap-3">
          <Github className="text-primary" /> GitHub Integration
        </h2>
        
        <div className="grid grid-cols-1 md:grid-cols-3 gap-6 mb-10">
          <div className="p-6 rounded-xl border border-white/10 bg-zinc-900/30 flex flex-col gap-2">
            <span className="text-slate-400 text-sm font-medium uppercase tracking-wider">Active Branches</span>
            <span className="text-4xl font-bold text-white">4</span>
          </div>
          <div className="p-6 rounded-xl border border-white/10 bg-zinc-900/30 flex flex-col gap-2">
            <span className="text-slate-400 text-sm font-medium uppercase tracking-wider">Open PRs</span>
            <span className="text-4xl font-bold text-white">1</span>
          </div>
          <div className="p-6 rounded-xl border border-white/10 bg-zinc-900/30 flex flex-col gap-2">
            <span className="text-slate-400 text-sm font-medium uppercase tracking-wider">Deployments</span>
            <span className="text-4xl font-bold text-emerald-400">Passing</span>
          </div>
        </div>

        <h3 className="text-lg font-medium mb-6 border-b border-white/10 pb-4 text-slate-300">Recent Pull Requests</h3>
        <div className="space-y-4">
          {prs.map(pr => (
            <div key={pr.id} className="flex flex-col sm:flex-row sm:items-center justify-between p-5 rounded-xl border border-white/10 bg-zinc-900/30 hover:bg-zinc-900/60 transition-colors gap-4">
              <div className="flex items-start gap-4">
                <div className="mt-1">
                  <GitPullRequest className={pr.status === 'open' ? 'text-emerald-400' : 'text-purple-400'} size={20} />
                </div>
                <div>
                  <div className="font-medium text-slate-200 text-lg">{pr.title}</div>
                  <div className="text-sm text-slate-500 mt-2 flex flex-wrap items-center gap-3">
                    <span className="font-mono text-slate-400">#{pr.id}</span>
                    <span className="hidden sm:inline">•</span>
                    <span className="flex items-center gap-1.5 bg-white/5 px-2 py-0.5 rounded-md"><GitBranch size={14} /> {pr.branch}</span>
                    <span className="hidden sm:inline">•</span>
                    <span className="flex items-center gap-1.5"><Clock size={14} /> {pr.time}</span>
                  </div>
                </div>
              </div>
              <div className="flex-shrink-0 sm:self-center self-start ml-10 sm:ml-0">
                {pr.status === 'merged' ? (
                  <span className="px-3 py-1.5 rounded-md bg-purple-500/10 border border-purple-500/20 text-purple-400 text-sm font-medium flex items-center gap-1.5">
                    <Check size={14} /> Merged
                  </span>
                ) : (
                  <span className="px-3 py-1.5 rounded-md bg-emerald-500/10 border border-emerald-500/20 text-emerald-400 text-sm font-medium">
                    Open
                  </span>
                )}
              </div>
            </div>
          ))}
        </div>
      </div>
    </div>
  );
};
