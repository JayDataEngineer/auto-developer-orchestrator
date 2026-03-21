import React from 'react';
import { Activity, GitCommit, CheckCircle, XCircle, Terminal as TerminalIcon } from 'lucide-react';

interface ActivityViewProps {
  logs: string[];
}

export const ActivityView: React.FC<ActivityViewProps> = ({ logs }) => {
  // Parse logs into activity items
  const activities = logs.slice().reverse().map((log, index) => {
    const timeMatch = log.match(/\[(.*?)\]/);
    const time = timeMatch ? timeMatch[1] : 'Just now';
    const message = log.replace(/\[.*?\]\s*/, '');
    
    let icon = TerminalIcon;
    let color = 'text-zinc-400';
    
    if (message.includes('ERROR')) {
      icon = XCircle;
      color = 'text-red-400';
    } else if (message.includes('PASSED') || message.includes('complete') || message.includes('merged')) {
      icon = CheckCircle;
      color = 'text-emerald-400';
    } else if (message.includes('AGENT') || message.includes('JULES')) {
      icon = GitCommit;
      color = 'text-blue-400';
    }

    return { id: index, message, time, icon, color };
  });

  // Fallback if no logs
  const displayActivities = activities.length > 0 ? activities : [
    { id: 1, type: 'commit', message: 'fix: resolve type error in server.ts', time: '10 mins ago', icon: GitCommit, color: 'text-blue-400' },
    { id: 2, type: 'success', message: 'Test suite passed (42/42)', time: '15 mins ago', icon: CheckCircle, color: 'text-emerald-400' },
    { id: 3, type: 'error', message: 'Build failed: TS2322 in server.ts', time: '20 mins ago', icon: XCircle, color: 'text-red-400' },
  ];

  return (
    <div className="flex-1 p-6 lg:p-10 overflow-y-auto bg-black text-slate-200">
      <div className="max-w-4xl mx-auto">
        <h2 className="text-2xl font-semibold mb-8 flex items-center gap-3">
          <Activity className="text-primary" /> Activity Log
        </h2>
        
        <div className="space-y-8 border-l border-white/10 ml-4 pl-8 relative">
          {displayActivities.map((act) => (
            <div key={act.id} className="relative">
              <div className="absolute -left-[49px] top-1 flex items-center justify-center w-8 h-8 rounded-full border border-white/10 bg-zinc-950 shadow-sm">
                <act.icon size={14} className={act.color} />
              </div>
              <div className="p-5 rounded-xl border border-white/10 bg-zinc-900/30 hover:bg-zinc-900/50 transition-colors backdrop-blur-sm shadow-sm">
                <div className="flex items-center justify-between mb-2">
                  <span className="text-base font-medium text-slate-200">{act.message}</span>
                </div>
                <div className="text-sm text-slate-500">{act.time}</div>
              </div>
            </div>
          ))}
        </div>
      </div>
    </div>
  );
};
