import React, { useState, useEffect, useCallback } from 'react';
import {
  Clock, Play, Pause, Trash2, Plus, RefreshCw, Calendar,
  Timer, Zap, ChevronDown, ChevronUp, AlertCircle, Check, X
} from 'lucide-react';
import { cn } from '../lib/utils';
import { api, SchedulerJob, CreateJobRequest, SchedulerExecution, RunLogEntry } from '../lib/api';
import { CreateJobForm, ScheduleType, SCHEDULE_PRESETS } from './scheduler/CreateJobForm';

interface SchedulerViewProps {
  projects: string[];
  onClose?: () => void;
}

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
  const [runLogs, setRunLogs] = useState<RunLogEntry[]>([]);

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
      const [execData, runData] = await Promise.all([
        api.scheduler.executions(jobId),
        api.scheduler.runs(jobId, 50),
      ]);
      setExecutions(execData.executions || []);
      setRunLogs(runData.runs || []);
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

                    {/* Delivery info */}
                    {job.deliveryMode && job.deliveryMode !== 'store' && (
                      <p className="text-[9px] font-mono text-zinc-500">
                        Delivery: {job.deliveryMode}{job.deliveryWebhookUrl ? ` → ${job.deliveryWebhookUrl.slice(0, 40)}...` : ''}
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

                    {/* Run logs (persistent) */}
                    {runLogs.length > 0 && (
                      <div className="mt-2 space-y-1">
                        <span className="text-[8px] font-mono uppercase text-zinc-600 tracking-widest">Run log</span>
                        {runLogs.slice(0, 20).map((run, i) => (
                          <div key={i} className="flex items-start gap-2 text-[9px] font-mono p-1 bg-white/5 rounded">
                            <div className={cn(
                              "w-1.5 h-1.5 rounded-full mt-0.5 shrink-0",
                              run.status === 'ok' ? "bg-emerald-400" :
                              run.status === 'error' ? "bg-red-400" : "bg-yellow-400"
                            )} />
                            <div className="flex-1 min-w-0">
                              <div className="flex items-center gap-2">
                                <span className="text-zinc-500">{run.runAtMs ? new Date(run.runAtMs).toLocaleString() : '—'}</span>
                                <span className={cn(
                                  "uppercase",
                                  run.status === 'ok' ? "text-emerald-400" : "text-red-400"
                                )}>{run.status}</span>
                                <span className="text-zinc-600">{run.durationMs}ms</span>
                              </div>
                              {run.summary && (
                                <p className="text-zinc-400 mt-0.5 truncate">{run.summary}</p>
                              )}
                              {run.error && (
                                <p className="text-red-400/70 mt-0.5">{run.error.slice(0, 200)}</p>
                              )}
                            </div>
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
