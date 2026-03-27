import React, { useState, useEffect, useRef } from 'react';
import { 
  Github, GitPullRequest, GitBranch, Check, Clock, Star, 
  AlertCircle, GitFork, Eye, Activity, Terminal as TerminalIcon,
  ChevronRight, ArrowRight, User, Plus
} from 'lucide-react';
import { motion, AnimatePresence } from 'motion/react';
import { cn } from '../lib/utils';

interface GithubViewProps {
  repoOwner?: string;
  repoName?: string;
}

interface GithubEvent {
  id: string;
  type: string;
  actor: { login: string; avatar_url: string };
  repo: { name: string };
  payload: any;
  created_at: string;
}

export const GithubView: React.FC<GithubViewProps> = ({ 
  repoOwner, 
  repoName
}) => {
  const [prs, setPrs] = useState<any[]>([]);
  const [stats, setStats] = useState<any>(null);
  const [branches, setBranches] = useState<any[]>([]);
  const [events, setEvents] = useState<GithubEvent[]>([]);
  const [isLoading, setIsLoading] = useState(true);
  const [isLive, setIsLive] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const pollInterval = useRef<NodeJS.Timeout | null>(null);

  useEffect(() => {
    if (repoName) {
      fetchData();
      if (isLive) {
        startPolling();
      }
    }
    return () => stopPolling();
  }, [repoOwner, repoName, isLive]);

  const startPolling = () => {
    stopPolling();
    pollInterval.current = setInterval(() => {
      fetchActivity();
    }, 30000); // 30s polling
  };

  const stopPolling = () => {
    if (pollInterval.current) {
      clearInterval(pollInterval.current);
    }
  };

  const fetchData = async () => {
    setIsLoading(true);
    setError(null);
    try {
      const query = `owner=${repoOwner}&repo=${repoName}`;
      const [prRes, statsRes, branchRes, activityRes] = await Promise.all([
        fetch(`/api/github/prs?${query}`),
        fetch(`/api/github/stats?${query}`),
        fetch(`/api/github/branches?${query}`),
        fetch(`/api/github/activity?${query}`)
      ]);

      const prData = prRes.ok ? await prRes.json() : { prs: [] };
      const statsData = statsRes.ok ? await statsRes.json() : { stats: null };
      const branchData = branchRes.ok ? await branchRes.json() : { branches: [] };
      const activityData = activityRes.ok ? await activityRes.json() : { events: [] };

      setPrs(prData.prs || []);
      setStats(statsData.stats);
      setBranches(branchData.branches || []);
      setEvents(activityData.events || []);

      if (activityData.error || statsData.error) {
         setError(activityData.error || statsData.error);
      }
    } catch (e: any) {
      console.error("Failed to fetch GitHub data", e);
      setError("Network error: Failed to connect to GitHub Proxy");
    } finally {
      setIsLoading(false);
    }
  };

  const fetchActivity = async () => {
    try {
      const query = `owner=${repoOwner}&repo=${repoName}`;
      const res = await fetch(`/api/github/activity?${query}`);
      if (!res.ok) throw new Error("Activity poll failed");
      const data = await res.json();
      if (data.events) {
        setEvents(data.events);
      }
    } catch (e) {
      console.error("Activity poll failed", e);
    }
  };

  const renderEventIcon = (type: string) => {
    switch (type) {
      case 'PushEvent': return <ArrowRight size={14} className="text-emerald-500" />;
      case 'PullRequestEvent': return <GitPullRequest size={14} className="text-purple-500" />;
      case 'IssuesEvent': return <AlertCircle size={14} className="text-amber-500" />;
      case 'WatchEvent': return <Star size={14} className="text-yellow-500" />;
      case 'CreateEvent': return <Plus size={14} className="text-blue-500" />;
      default: return <Activity size={14} className="text-zinc-500" />;
    }
  };

  const renderEventDescription = (event: GithubEvent) => {
    const { type, payload, actor } = event;
    switch (type) {
      case 'PushEvent': 
        return (
          <span>
            <span className="text-white font-bold">{actor.login}</span> pushed to{' '}
            <span className="text-primary font-mono">{payload.ref?.replace('refs/heads/', '')}</span>
          </span>
        );
      case 'PullRequestEvent':
        return (
          <span>
            <span className="text-white font-bold">{actor.login}</span> {payload.action} PR{' '}
            <span className="text-primary">#{payload.pull_request?.number}</span>
          </span>
        );
      case 'IssuesEvent':
        return (
          <span>
            <span className="text-white font-bold">{actor.login}</span> {payload.action} issue{' '}
            <span className="text-primary">#{payload.issue?.number}</span>
          </span>
        );
      case 'WatchEvent':
        return (
          <span>
            <span className="text-white font-bold">{actor.login}</span> starred the repository
          </span>
        );
      default:
        return <span>System event: <span className="text-white">{type}</span> by {actor.login}</span>;
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

  return (
    <div className="flex-1 flex flex-col h-full bg-black overflow-hidden font-sans">
      {/* Dynamic Activity Header */}
      <div className="h-16 border-b border-white/5 flex items-center justify-between px-8 bg-zinc-950/20 backdrop-blur-md shrink-0">
        <div className="flex items-center gap-4">
          <TerminalIcon size={16} className="text-primary" />
          <div className="flex flex-col">
            <span className="text-[9px] font-black uppercase tracking-[0.4em] text-zinc-600">Unified Repository Stream</span>
            <span className="text-xs font-mono font-bold text-white tracking-widest">
              {repoOwner}/<span className="text-primary">{repoName}</span>
            </span>
          </div>
        </div>
        <div className="flex items-center gap-6">
          {error && (
             <div className="flex items-center gap-2 text-red-500 text-[10px] font-mono uppercase font-bold animate-pulse">
               <AlertCircle size={12} /> {error}
             </div>
          )}
          <div className="flex items-center gap-2">
            <div className={cn("w-1.5 h-1.5 rounded-full", isLive ? "bg-emerald-500 animate-pulse glow-emerald" : "bg-zinc-700")} />
            <span className="text-[9px] font-bold text-zinc-500 uppercase tracking-widest">{isLive ? 'Live_Sync_Active' : 'Sync_Paused'}</span>
          </div>
          <button 
            onClick={() => setIsLive(!isLive)}
            className="text-[9px] font-black uppercase tracking-widest text-zinc-400 hover:text-white transition-colors"
          >
            {isLive ? '[Pause]' : '[Resume]'}
          </button>
        </div>
      </div>

      <div className="flex-1 flex overflow-hidden">
        {/* Main Feed Section */}
        <div className="flex-1 overflow-y-auto p-10 custom-scrollbar">
          <div className="max-w-4xl mx-auto space-y-12">
            
            {/* Stats Dashboard */}
            <div className="grid grid-cols-4 gap-4">
               {[
                { label: 'Stars', value: stats?.stars ?? '-', color: 'text-yellow-500' },
                { label: 'Issues', value: stats?.issues ?? '-', color: 'text-rose-500' },
                { label: 'Forks', value: stats?.forks ?? '-', color: 'text-blue-500' },
                { label: 'Activity', value: events.length || '0', color: 'text-emerald-500' },
              ].map((s, i) => (
                <div key={i} className="bg-zinc-900/30 border border-white/5 p-4 rounded-sm flex flex-col gap-1">
                  <span className="text-[8px] font-black uppercase tracking-widest text-zinc-600">{s.label}</span>
                  <span className={cn("text-2xl font-black italic", s.color)}>{s.value}</span>
                </div>
              ))}
            </div>

            {/* Live Event Stream */}
            <div className="space-y-6">
              <h3 className="text-[10px] font-black italic uppercase tracking-[0.4em] text-zinc-500 flex items-center gap-3">
                <Activity size={14} className="text-primary" /> Logistical Heartbeat
              </h3>
              
              <div className="space-y-2 text-xs">
                <AnimatePresence mode="popLayout">
                  {events.map((event, i) => (
                    <motion.div
                      key={event.id}
                      initial={{ opacity: 0, x: -10 }}
                      animate={{ opacity: 1, x: 0 }}
                      transition={{ delay: i * 0.05 }}
                      className="group flex items-center gap-6 p-4 bg-zinc-950 border border-white/5 hover:border-primary/20 transition-all rounded-sm"
                    >
                      <div className="w-8 shrink-0 flex flex-col items-center gap-1">
                        {renderEventIcon(event.type)}
                      </div>
                      
                      <div className="flex-1 flex items-center gap-4">
                        <div className="w-6 h-6 rounded bg-zinc-900 overflow-hidden border border-white/10">
                          <img 
                            src={event.actor.avatar_url} 
                            alt="" 
                            className="w-full h-full object-cover grayscale group-hover:grayscale-0 transition-all" 
                            onError={(e) => (e.currentTarget.src = 'https://github.com/identicons/jason.png')}
                          />
                        </div>
                        <div className="text-[11px] font-mono text-zinc-400 group-hover:text-zinc-200 transition-colors">
                          {renderEventDescription(event)}
                        </div>
                      </div>

                      <div className="shrink-0 text-[9px] font-mono text-zinc-700 group-hover:text-zinc-500 flex items-center gap-2">
                         <span>{new Date(event.created_at).toLocaleTimeString()}</span>
                         <ArrowRight size={10} className="opacity-0 group-hover:opacity-100 -translate-x-2 group-hover:translate-x-0 transition-all" />
                      </div>
                    </motion.div>
                  ))}
                </AnimatePresence>
                
                {events.length === 0 && !isLoading && (
                  <div className="p-20 border border-dashed border-white/5 flex flex-col items-center gap-4">
                    <Github size={32} className="text-zinc-900" />
                    <span className="text-[9px] font-bold text-zinc-700 uppercase tracking-widest">
                       {error ? 'TELEMETRY_LINK_BROKEN' : 'Awaiting system telemetry...'}
                    </span>
                  </div>
                )}
              </div>
            </div>
          </div>
        </div>

        {/* Operational Sidebar (PRs & Branches) */}
        <div className="w-96 border-l border-white/5 bg-zinc-950/20 p-8 overflow-y-auto custom-scrollbar shrink-0">
          <div className="space-y-12">
            {/* PR Section */}
            <section className="space-y-6">
              <div className="flex items-center justify-between">
                <h4 className="text-[10px] font-black uppercase tracking-[0.3em] text-zinc-500">Execution Phase</h4>
                <span className="text-[8px] font-mono text-zinc-700">{prs.length} Open</span>
              </div>
              <div className="space-y-4">
                {prs.map(pr => (
                  <div key={pr.id} className="p-4 bg-zinc-900/50 border border-white/5 hover:border-primary/30 transition-all cursor-pointer group">
                    <div className="text-[11px] font-bold text-white mb-2 line-clamp-2 leading-tight group-hover:text-primary transition-colors">{pr.title}</div>
                    <div className="flex items-center justify-between text-[9px] font-mono text-zinc-600">
                      <span>#{pr.number}</span>
                      <span className="bg-purple-950/30 text-purple-500 px-1.5 py-0.5 border border-purple-500/20 uppercase tracking-tighter">Pull_Authored</span>
                    </div>
                  </div>
                ))}
                {prs.length === 0 && !isLoading && (
                   <div className="text-[10px] text-zinc-800 italic uppercase">No active pull requests.</div>
                )}
              </div>
            </section>

            {/* Branch Section */}
            <section className="space-y-6">
              <h4 className="text-[10px] font-black uppercase tracking-[0.3em] text-zinc-500">Logical Branches</h4>
              <div className="space-y-2 font-mono">
                {branches.map(branch => (
                  <div key={branch.name} className="flex items-center justify-between p-2 hover:bg-white/5 transition-colors group">
                    <span className="text-[10px] text-zinc-500 group-hover:text-zinc-300">{branch.name}</span>
                    {branch.name === 'main' && <div className="w-1.5 h-1.5 rounded-full bg-primary glow-primary" />}
                  </div>
                ))}
              </div>
            </section>
          </div>
        </div>
      </div>

      {/* Industrial Footer */}
      <div className="h-8 bg-zinc-950 border-t border-white/5 flex items-center justify-between px-8 text-[8px] font-mono text-zinc-700 uppercase tracking-[0.5em] font-bold">
        <div className="flex gap-6">
          <span>GITHUB_HANDSHAKE: {error ? 'FAIL' : 'OK'}</span>
          <span>STREAM_BUFFER: {events.length > 0 ? 'ACTIVE' : 'IDLE'}</span>
        </div>
        <div className="flex gap-6">
          <span>LATENCY: 42MS</span>
          <span>SYNC_ID: {Math.random().toString(16).substring(2, 10).toUpperCase()}</span>
        </div>
      </div>
    </div>
  );
};
