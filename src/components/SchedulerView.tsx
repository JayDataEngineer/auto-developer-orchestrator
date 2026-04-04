import React, { useState, useEffect, useCallback } from 'react';
import {
  Clock, Play, Pause, Trash2, Plus, RefreshCw, Calendar,
  Timer, Zap, ChevronDown, ChevronUp, AlertCircle, Check, X
} from 'lucide-react';
import { cn } from '../lib/utils';
import { api, SchedulerJob, CreateJobRequest, SchedulerExecution } from '../lib/api';

interface SchedulerViewProps {
  projects: string[];
  onClose?: () => void;
}

type ScheduleType = 'cron' | 'every' | 'at';

const SCHEDULE_PRESETS: { label: string; type: ScheduleType; cronExpr?: string; everySeconds?: number }[] = [
  { label: 'Every hour', type: 'every', everySeconds: 3600 },
  { label: 'Every 6 hours', type: 'every', everySeconds: 21600 },
  { label: 'Every 30 minutes', type: 'every', everySeconds: 1800 },
  { label: 'Daily at 9am', type: 'cron', cronExpr: '0 9 * * *' },
  { label: 'Daily at 9pm', type: 'cron', cronExpr: '0 21 * * *' },
  { label: 'Weekdays at 9am', type: 'cron', cronExpr: '0 9 * * 1-5' },
  { label: 'Weekly (Mon 9am)', type: 'cron', cronExpr: '0 9 * * 1' },
  { label: 'Custom...', type: 'cron', cronExpr: '' },
];

function formatNextRun(nextRunAt?: string): string {
  if (!nextRunAt) return 'Not scheduled';
  const d = new Date(nextRunAt);
  if (isNaN(d.getTime())) return 'Invalid date';
  const now = new Date();
  const diff = d.getTime() - now.getTime();
  if (diff < 0) return 'Overdue';
  if (diff < 60000) return 'In less than 1 minute';
  if (diff < 3600000) return `In ${Math.ceil(diff / 60000)} minutes`;
  if (diff < 86400000) return `In ${Math.ceil(diff / 3600000)} hours`;
  return d.toLocaleDateString() + ' ' + d.toLocaleTimeString();
}

function formatSchedule(job: SchedulerJob): string {
  switch (job.scheduleType) {
    case 'cron':
      return job.cronExpr || 'N/A';
    case 'every': {
      const s = job.everySeconds || 0;
      if (s < 60) return `Every ${s}s`;
      if (s < 3600) return `Every ${Math.floor(s / 60)}min`;
      if (s < 86400) return `Every ${Math.floor(s / 3600)}h`;
      return `Every ${Math.floor(s / 86400)}d`;
    }
    case 'at':
      return job.atTime ? `At ${new Date(job.atTime).toLocaleString()}` : 'N/A';
    default:
      return 'Unknown';
  }
}

export function SchedulerView({ projects, onClose }: SchedulerViewProps) {
  const [jobs, setJobs] = useState<SchedulerJob[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [showCreate, setShowCreate] = useState(false);
  const [expandedJob, setExpandedJob] = useState<string | null>(null);
  const [executions, setExecutions] = useState<SchedulerExecution[]>([]);

  const fetchJobs = useCallback(async () => {
    try {
      setLoading(true);
      const data = await api.scheduler.list();
      setJobs(data.jobs || []);
      setError(null);
    } catch (err: any) {
      setError(err.message);
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => { fetchJobs(); }, [fetchJobs]);

  const fetchExecutions = useCallback(async (jobId: string) => {
    try {
      const data = await api.scheduler.executions(jobId);
      setExecutions(data.executions || []);
    } catch {}
  }, []);

  const toggleExpand = useCallback((jobId: string) => {
    setExpandedJob(prev => {
      if (prev === jobId) {
        setExecutions([]);
        return null;
      }
      fetchExecutions(jobId);
      return jobId;
    });
  }, [fetchExecutions]);

  const handleToggle = useCallback(async (job: SchedulerJob) => {
    try {
      await api.scheduler.update(job.id, { enabled: !job.enabled });
      fetchJobs();
    } catch (err: any) {
      setError(err.message);
    }
  }, [fetchJobs]);

  const handleDelete = useCallback(async (jobId: string) => {
    try {
      await api.scheduler.delete(jobId);
      fetchJobs();
    } catch (err: any) {
      setError(err.message);
    }
  }, [fetchJobs]);

  const handleTrigger = useCallback(async (jobId: string) => {
    try {
      await api.scheduler.trigger(jobId);
      fetchJobs();
    } catch (err: any) {
      setError(err.message);
    }
  }, [fetchJobs]);

  return (
    <div className="flex flex-col h-full">
      {/* Header */}
      <div className="flex items-center justify-between px-4 py-3 border-b border-white/5">
        <div className="flex items-center gap-2">
          <Clock size={16} className="text-primary" />
          <h2 className="text-[11px] font-mono uppercase tracking-widest font-bold">Scheduled Jobs</h2>
          <span className="text-[9px] font-mono text-zinc-600">{jobs.length} jobs</span>
        </div>
        <div className="flex items-center gap-2">
          <button onClick={fetchJobs} className="p-1.5 text-zinc-500 hover:text-zinc-300 transition-colors">
            <RefreshCw size={12} />
          </button>
          <button
            onClick={() => setShowCreate(!showCreate)}
            className={cn(
              "flex items-center gap-1.5 px-3 py-1.5 text-[9px] font-mono uppercase tracking-widest transition-colors",
              showCreate ? "bg-zinc-800 text-zinc-300" : "bg-primary/10 text-primary hover:bg-primary/20"
            )}
          >
            <Plus size={10} /> New Job
          </button>
          {onClose && (
            <button onClick={onClose} className="p-1.5 text-zinc-500 hover:text-zinc-300 transition-colors">
              <X size={14} />
            </button>
          )}
        </div>
      </div>

      {/* Error */}
      {error && (
        <div className="mx-4 mt-2 p-2 bg-red-500/10 border border-red-500/20 flex items-center gap-2 text-[10px] text-red-400">
          <AlertCircle size={12} />
          {error}
          <button onClick={() => setError(null)} className="ml-auto"><X size={10} /></button>
        </div>
      )}

      {/* Create form */}
      {showCreate && (
        <CreateJobForm
          projects={projects}
          onSubmit={async (job) => {
            await api.scheduler.create(job);
            setShowCreate(false);
            fetchJobs();
          }}
          onCancel={() => setShowCreate(false)}
        />
      )}

      {/* Job list */}
      <div className="flex-1 overflow-y-auto custom-scrollbar">
        {loading && jobs.length === 0 ? (
          <div className="flex items-center justify-center h-32 text-zinc-600 text-[10px] font-mono">
            Loading jobs...
          </div>
        ) : jobs.length === 0 ? (
          <div className="flex flex-col items-center justify-center h-32 text-zinc-600 text-center space-y-2">
            <Clock size={24} className="opacity-30" />
            <p className="text-[10px] font-mono">No scheduled jobs yet</p>
            <p className="text-[9px] font-mono text-zinc-700">Create a job to automate recurring tasks</p>
          </div>
        ) : (
          <div className="divide-y divide-white/5">
            {jobs.map(job => (
              <div key={job.id}>
                <div
                  className={cn(
                    "px-4 py-3 flex items-center gap-3 cursor-pointer hover:bg-white/[0.02] transition-colors",
                    expandedJob === job.id && "bg-white/[0.02]"
                  )}
                  onClick={() => toggleExpand(job.id)}
                >
                  {/* Status indicator */}
                  <div className={cn(
                    "w-2 h-2 rounded-full shrink-0",
                    job.status === 'running' ? "bg-yellow-400 animate-pulse" :
                    job.status === 'error' ? "bg-red-400" :
                    job.enabled ? "bg-emerald-400" : "bg-zinc-600"
                  )} />

                  {/* Job info */}
                  <div className="flex-1 min-w-0">
                    <div className="flex items-center gap-2">
                      <span className="text-[11px] font-medium truncate">{job.name}</span>
                      {!job.enabled && (
                        <span className="text-[8px] font-mono uppercase bg-zinc-800 px-1.5 py-0.5 text-zinc-500">disabled</span>
                      )}
                    </div>
                    <div className="flex items-center gap-3 mt-0.5">
                      <span className="text-[9px] font-mono text-zinc-500">{job.project}</span>
                      <span className="text-[9px] font-mono text-zinc-600">{formatSchedule(job)}</span>
                      <span className="text-[9px] font-mono text-zinc-600">{formatNextRun(job.nextRunAt)}</span>
                    </div>
                  </div>

                  {/* Actions */}
                  <div className="flex items-center gap-1" onClick={e => e.stopPropagation()}>
                    <button
                      onClick={() => handleTrigger(job.id)}
                      disabled={job.status === 'running'}
                      className="p-1.5 text-zinc-500 hover:text-primary transition-colors disabled:opacity-30"
                      title="Run now"
                    >
                      <Play size={12} />
                    </button>
                    <button
                      onClick={() => handleToggle(job)}
                      className={cn(
                        "p-1.5 transition-colors",
                        job.enabled ? "text-zinc-400 hover:text-yellow-400" : "text-zinc-600 hover:text-emerald-400"
                      )}
                      title={job.enabled ? "Disable" : "Enable"}
                    >
                      {job.enabled ? <Pause size={12} /> : <Play size={12} />}
                    </button>
                    <button
                      onClick={() => handleDelete(job.id)}
                      className="p-1.5 text-zinc-500 hover:text-red-400 transition-colors"
                      title="Delete"
                    >
                      <Trash2 size={12} />
                    </button>
                    {expandedJob === job.id ? <ChevronUp size={12} className="text-zinc-600" /> : <ChevronDown size={12} className="text-zinc-600" />}
                  </div>
                </div>

                {/* Expanded details */}
                {expandedJob === job.id && (
                  <div className="px-4 pb-3 space-y-2 border-t border-white/5 pt-2 bg-black/20">
                    <p className="text-[10px] font-mono text-zinc-400">
                      <span className="text-zinc-600">Prompt: </span>{job.message}
                    </p>
                    {job.lastError && (
                      <p className="text-[10px] font-mono text-red-400">
                        <span className="text-zinc-600">Last error: </span>{job.lastError}
                      </p>
                    )}
                    {job.lastRunAt && (
                      <p className="text-[9px] font-mono text-zinc-600">
                        Last run: {new Date(job.lastRunAt).toLocaleString()} ({job.lastRunStatus})
                      </p>
                    )}

                    {/* Executions */}
                    {executions.length > 0 && (
                      <div className="mt-2 space-y-1">
                        <span className="text-[8px] font-mono uppercase text-zinc-600 tracking-widest">Recent runs</span>
                        {executions.slice(0, 10).map(exec => (
                          <div key={exec.id} className="flex items-center gap-2 text-[9px] font-mono">
                            <div className={cn(
                              "w-1.5 h-1.5 rounded-full",
                              exec.status === 'success' ? "bg-emerald-400" :
                              exec.status === 'error' ? "bg-red-400" : "bg-yellow-400"
                            )} />
                            <span className="text-zinc-500">{new Date(exec.startedAt).toLocaleString()}</span>
                            <span className={cn(
                              "uppercase",
                              exec.status === 'success' ? "text-emerald-400" :
                              exec.status === 'error' ? "text-red-400" : "text-yellow-400"
                            )}>{exec.status}</span>
                            {exec.error && <span className="text-red-400/70 truncate max-w-[200px]">{exec.error}</span>}
                          </div>
                        ))}
                      </div>
                    )}
                  </div>
                )}
              </div>
            ))}
          </div>
        )}
      </div>
    </div>
  );
}

// ─── Create Job Form ──────────────────────────────────────────

interface CreateJobFormProps {
  projects: string[];
  onSubmit: (job: CreateJobRequest) => Promise<void>;
  onCancel: () => void;
}

function CreateJobForm({ projects, onSubmit, onCancel }: CreateJobFormProps) {
  const [name, setName] = useState('');
  const [project, setProject] = useState(projects[0] || '');
  const [message, setMessage] = useState('');
  const [scheduleType, setScheduleType] = useState<ScheduleType>('cron');
  const [cronExpr, setCronExpr] = useState('0 9 * * *');
  const [everySeconds, setEverySeconds] = useState(3600);
  const [atTime, setAtTime] = useState('');
  const [autoBranch, setAutoBranch] = useState(false);
  const [autoMerge, setAutoMerge] = useState(false);
  const [enabled, setEnabled] = useState(true);
  const [submitting, setSubmitting] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!name || !project || !message) {
      setError('Name, project, and message are required');
      return;
    }
    setSubmitting(true);
    setError(null);
    try {
      await onSubmit({
        name,
        project,
        message,
        scheduleType,
        cronExpr: scheduleType === 'cron' ? cronExpr : undefined,
        everySeconds: scheduleType === 'every' ? everySeconds : undefined,
        atTime: scheduleType === 'at' ? atTime : undefined,
        autoBranch,
        autoMerge,
        enabled,
      });
    } catch (err: any) {
      setError(err.message);
    } finally {
      setSubmitting(false);
    }
  };

  return (
    <form onSubmit={handleSubmit} className="p-4 border-b border-white/5 bg-zinc-950/50 space-y-3">
      <div className="flex items-center justify-between">
        <span className="text-[9px] font-mono uppercase tracking-widest text-primary">New Scheduled Job</span>
        <button type="button" onClick={onCancel} className="text-zinc-500 hover:text-zinc-300">
          <X size={14} />
        </button>
      </div>

      {error && (
        <div className="p-2 bg-red-500/10 border border-red-500/20 text-[10px] text-red-400">{error}</div>
      )}

      <div className="grid grid-cols-2 gap-3">
        <div>
          <label className="text-[8px] font-mono uppercase text-zinc-600 tracking-widest">Name</label>
          <input
            value={name}
            onChange={e => setName(e.target.value)}
            placeholder="Daily status check"
            className="w-full mt-1 bg-zinc-900 border border-white/5 rounded px-2 py-1.5 text-[11px] outline-none focus:border-primary/40"
          />
        </div>
        <div>
          <label className="text-[8px] font-mono uppercase text-zinc-600 tracking-widest">Project</label>
          <select
            value={project}
            onChange={e => setProject(e.target.value)}
            className="w-full mt-1 bg-zinc-900 border border-white/5 rounded px-2 py-1.5 text-[11px] outline-none focus:border-primary/40"
          >
            {projects.map(p => <option key={p} value={p}>{p}</option>)}
          </select>
        </div>
      </div>

      <div>
        <label className="text-[8px] font-mono uppercase text-zinc-600 tracking-widest">Prompt Message</label>
        <textarea
          value={message}
          onChange={e => setMessage(e.target.value)}
          placeholder="What should the agent do? e.g. Check recent GitHub issues and create a summary"
          rows={3}
          className="w-full mt-1 bg-zinc-900 border border-white/5 rounded px-2 py-1.5 text-[11px] outline-none focus:border-primary/40 resize-none"
        />
      </div>

      <div>
        <label className="text-[8px] font-mono uppercase text-zinc-600 tracking-widest">Schedule</label>
        <div className="flex gap-2 mt-1">
          <select
            value={scheduleType}
            onChange={e => setScheduleType(e.target.value as ScheduleType)}
            className="bg-zinc-900 border border-white/5 rounded px-2 py-1.5 text-[10px] outline-none"
          >
            <option value="cron">Cron</option>
            <option value="every">Every</option>
            <option value="at">One-time</option>
          </select>
          {scheduleType === 'cron' && (
            <>
              <select
                value={cronExpr}
                onChange={e => setCronExpr(e.target.value)}
                className="bg-zinc-900 border border-white/5 rounded px-2 py-1.5 text-[10px] outline-none flex-1"
              >
                {SCHEDULE_PRESETS.filter(p => p.type === 'cron').map(p => (
                  <option key={p.label} value={p.cronExpr || ''}>{p.label}</option>
                ))}
              </select>
              {!SCHEDULE_PRESETS.find(p => p.cronExpr === cronExpr && p.type === 'cron') && (
                <input
                  value={cronExpr}
                  onChange={e => setCronExpr(e.target.value)}
                  placeholder="0 9 * * *"
                  className="bg-zinc-900 border border-white/5 rounded px-2 py-1.5 text-[10px] outline-none flex-1"
                />
              )}
            </>
          )}
          {scheduleType === 'every' && (
            <select
              value={everySeconds}
              onChange={e => setEverySeconds(Number(e.target.value))}
              className="bg-zinc-900 border border-white/5 rounded px-2 py-1.5 text-[10px] outline-none flex-1"
            >
              <option value={1800}>Every 30 minutes</option>
              <option value={3600}>Every hour</option>
              <option value={21600}>Every 6 hours</option>
              <option value={43200}>Every 12 hours</option>
              <option value={86400}>Every 24 hours</option>
            </select>
          )}
          {scheduleType === 'at' && (
            <input
              type="datetime-local"
              value={atTime}
              onChange={e => setAtTime(new Date(e.target.value).toISOString())}
              className="bg-zinc-900 border border-white/5 rounded px-2 py-1.5 text-[10px] outline-none flex-1"
            />
          )}
        </div>
      </div>

      <div className="flex items-center gap-4">
        <label className="flex items-center gap-1.5 text-[9px] font-mono text-zinc-500 cursor-pointer">
          <input type="checkbox" checked={autoBranch} onChange={e => setAutoBranch(e.target.checked)} className="accent-primary" />
          Auto-Branch
        </label>
        <label className="flex items-center gap-1.5 text-[9px] font-mono text-zinc-500 cursor-pointer">
          <input type="checkbox" checked={autoMerge} onChange={e => setAutoMerge(e.target.checked)} className="accent-primary" />
          Auto-Merge
        </label>
        <label className="flex items-center gap-1.5 text-[9px] font-mono text-zinc-500 cursor-pointer">
          <input type="checkbox" checked={enabled} onChange={e => setEnabled(e.target.checked)} className="accent-primary" />
          Enabled
        </label>
      </div>

      <div className="flex items-center gap-2 pt-1">
        <button
          type="submit"
          disabled={submitting || !name || !project || !message}
          className="px-4 py-1.5 bg-primary text-black text-[9px] font-mono uppercase tracking-widest hover:bg-primary/80 disabled:opacity-30 transition-colors"
        >
          {submitting ? 'Creating...' : 'Create Job'}
        </button>
        <button type="button" onClick={onCancel} className="px-4 py-1.5 text-[9px] font-mono text-zinc-500 hover:text-zinc-300 transition-colors">
          Cancel
        </button>
      </div>
    </form>
  );
}
