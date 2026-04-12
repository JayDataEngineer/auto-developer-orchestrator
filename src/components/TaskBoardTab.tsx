import React, { useState, useEffect, useCallback } from 'react';
import { cn } from '../lib/utils';
import { api, SchedulerJob, CreateJobRequest, SchedulerExecution, RunLogEntry } from '../lib/api';
import { useToastContext } from './ui/Toast';
import { StatusBadge } from './ui/StatusBadge';
import { EmptyState } from './ui/EmptyState';
import {
  LayoutGrid, Plus, Play, Trash2, Clock,
  Loader, ChevronRight, ChevronDown, AlertCircle, X, Layers, RefreshCw
} from 'lucide-react';

// ─── Helpers ─────────────────────────────────────────────────

function formatDuration(ms?: number): string {
  if (!ms) return '—';
  if (ms < 1000) return `${ms}ms`;
  if (ms < 60000) return `${(ms / 1000).toFixed(1)}s`;
  return `${(ms / 60000).toFixed(1)}m`;
}

function formatTokens(n?: number): string {
  if (!n) return '—';
  if (n < 1000) return String(n);
  return `${(n / 1000).toFixed(1)}k`;
}

function formatSchedule(job: SchedulerJob): string {
  switch (job.scheduleType) {
    case 'cron': return job.cronExpr || 'N/A';
    case 'every': {
      const s = job.everySeconds || 0;
      if (s < 60) return `Every ${s}s`;
      if (s < 3600) return `Every ${Math.floor(s / 60)}min`;
      if (s < 86400) return `Every ${Math.floor(s / 3600)}h`;
      return `Every ${Math.floor(s / 86400)}d`;
    }
    case 'at': return job.atTime ? `At ${new Date(job.atTime).toLocaleString()}` : 'N/A';
    case 'manual': return 'Manual';
    default: return 'Unknown';
  }
}

function formatNextRun(nextRunAt?: string): string {
  if (!nextRunAt) return '—';
  const d = new Date(nextRunAt);
  if (isNaN(d.getTime())) return 'Invalid';
  const diff = d.getTime() - Date.now();
  if (diff < 0) return 'Overdue';
  if (diff < 60000) return '<1m';
  if (diff < 3600000) return `${Math.ceil(diff / 60000)}m`;
  if (diff < 86400000) return `${Math.ceil(diff / 3600000)}h`;
  return d.toLocaleDateString();
}

// Map scheduler job status to display status for StatusBadge
function jobBadgeStatus(job: SchedulerJob): string {
  if (job.status === 'running') return 'in_progress';
  if (job.lastRunStatus === 'success') return 'completed';
  if (job.lastRunStatus === 'error') return 'failed';
  if (job.scheduleType === 'manual' && !job.lastRunAt) return 'pending';
  return job.status;
}

// ─── Manual Task Card ────────────────────────────────────────

interface TaskCardProps {
  task: SchedulerJob;
  isSelected: boolean;
  onSelect: () => void;
  onTrigger: () => void;
  onDelete: () => void;
}

function TaskCard({ task, isSelected, onSelect, onTrigger, onDelete }: TaskCardProps) {
  const isRunning = task.status === 'running';
  const hasRun = !!task.lastRunAt;

  return (
    <div
      onClick={onSelect}
      className={cn(
        'p-2.5 border cursor-pointer transition-colors group',
        isSelected ? 'border-primary/30 bg-primary/5' : 'border-white/5 bg-zinc-900 hover:border-white/10 hover:bg-zinc-900/80'
      )}
    >
      <div className="flex items-start gap-2">
        <StatusBadge status={jobBadgeStatus(task) as any} size="sm" />
        <div className="flex-1 min-w-0">
          <p className="text-[10px] font-medium truncate">{task.name}</p>
          {task.description && (
            <p className="text-[9px] font-mono text-zinc-600 truncate mt-0.5">{task.description}</p>
          )}
        </div>
      </div>
      <div className="flex items-center gap-3 mt-1.5 text-[8px] font-mono text-zinc-600">
        {task.durationMs ? (
          <span className="flex items-center gap-0.5"><Clock size={7} />{formatDuration(task.durationMs)}</span>
        ) : null}
        {task.inputTokens || task.outputTokens ? (
          <span>{formatTokens(task.inputTokens)}in / {formatTokens(task.outputTokens)}out</span>
        ) : null}
        <span className="ml-auto text-zinc-700">{new Date(task.updatedAt).toLocaleString()}</span>
      </div>
      {/* Actions on hover */}
      <div className="flex items-center gap-1 mt-1.5 opacity-0 group-hover:opacity-100 transition-opacity">
        {!isRunning && (
          <button onClick={e => { e.stopPropagation(); onTrigger(); }} className="p-1 text-zinc-500 hover:text-emerald-400 transition-colors" title="Run">
            <Play size={10} />
          </button>
        )}
        {isRunning && (
          <span className="p-1 text-yellow-400 animate-pulse"><Loader size={10} /></span>
        )}
        <button onClick={e => { e.stopPropagation(); onDelete(); }} className="p-1 text-zinc-500 hover:text-red-400 transition-colors" title="Delete">
          <Trash2 size={10} />
        </button>
      </div>
    </div>
  );
}

// ─── Task Detail Panel ────────────────────────────────────────

interface TaskDetailPanelProps {
  task: SchedulerJob;
  executions: SchedulerExecution[];
  runLogs: RunLogEntry[];
  onClose: () => void;
  onTrigger: () => void;
  onDelete: () => void;
}

function TaskDetailPanel({ task, executions, runLogs, onClose, onTrigger, onDelete }: TaskDetailPanelProps) {
  const isRunning = task.status === 'running';

  return (
    <div className="border-t border-white/5 bg-zinc-950/80 flex flex-col max-h-[40%]">
      {/* Header */}
      <div className="flex items-center gap-2 px-4 py-2 border-b border-white/5 shrink-0">
        <StatusBadge status={jobBadgeStatus(task) as any} size="md" />
        <span className="text-[11px] font-medium flex-1 truncate">{task.name}</span>
        <div className="flex items-center gap-1">
          {!isRunning && (
            <button onClick={onTrigger} className="flex items-center gap-1 px-2 py-1 text-[9px] font-mono uppercase tracking-widest bg-emerald-500/10 text-emerald-400 hover:bg-emerald-500/20 transition-colors">
              <Play size={10} /> Run
            </button>
          )}
          {isRunning && (
            <span className="flex items-center gap-1 px-2 py-1 text-[9px] font-mono uppercase tracking-widest text-yellow-400">
              <Loader size={10} className="animate-spin" /> Running
            </span>
          )}
          <button onClick={onDelete} className="p-1 text-zinc-500 hover:text-red-400 transition-colors">
            <Trash2 size={12} />
          </button>
          <button onClick={onClose} className="p-1 text-zinc-500 hover:text-zinc-300 transition-colors">
            <X size={12} />
          </button>
        </div>
      </div>

      {/* Body */}
      <div className="flex-1 overflow-y-auto custom-scrollbar p-4 space-y-3">
        {task.description && (
          <div>
            <span className="text-[8px] font-mono uppercase text-zinc-600 tracking-widest">Description</span>
            <p className="text-[10px] font-mono text-zinc-400 mt-1">{task.description}</p>
          </div>
        )}

        {task.message && (
          <div>
            <span className="text-[8px] font-mono uppercase text-zinc-600 tracking-widest">Prompt</span>
            <p className="text-[10px] font-mono text-zinc-400 mt-1">{task.message}</p>
          </div>
        )}

        {/* Metrics */}
        <div className="flex items-center gap-4">
          <div>
            <span className="text-[8px] font-mono uppercase text-zinc-600 tracking-widest">Duration</span>
            <p className="text-[10px] font-mono text-zinc-300">{formatDuration(task.durationMs)}</p>
          </div>
          <div>
            <span className="text-[8px] font-mono uppercase text-zinc-600 tracking-widest">Tokens</span>
            <p className="text-[10px] font-mono text-zinc-300">{formatTokens(task.inputTokens)} in / {formatTokens(task.outputTokens)} out</p>
          </div>
          {task.model && (
            <div>
              <span className="text-[8px] font-mono uppercase text-zinc-600 tracking-widest">Model</span>
              <p className="text-[10px] font-mono text-zinc-300">{task.model}</p>
            </div>
          )}
        </div>

        {/* Dependencies */}
        {(task.blocks && task.blocks.length > 0) || (task.blockedBy && task.blockedBy.length > 0) ? (
          <div>
            <span className="text-[8px] font-mono uppercase text-zinc-600 tracking-widest">Dependencies</span>
            {task.blockedBy && task.blockedBy.length > 0 && (
              <p className="text-[9px] font-mono text-yellow-400/70 mt-1">Blocked by: {task.blockedBy.join(', ')}</p>
            )}
            {task.blocks && task.blocks.length > 0 && (
              <p className="text-[9px] font-mono text-zinc-500 mt-0.5">Blocks: {task.blocks.join(', ')}</p>
            )}
          </div>
        ) : null}

        {/* Error */}
        {task.lastError && (
          <div>
            <span className="text-[8px] font-mono uppercase text-zinc-600 tracking-widest">Error</span>
            <div className="mt-1 p-2 bg-red-500/5 border border-red-500/20 flex items-start gap-2">
              <AlertCircle size={12} className="text-red-400 shrink-0 mt-0.5" />
              <pre className="text-[9px] font-mono text-red-400 whitespace-pre-wrap">{task.lastError}</pre>
            </div>
          </div>
        )}

        {/* Recent runs */}
        {executions.length > 0 && (
          <div>
            <span className="text-[8px] font-mono uppercase text-zinc-600 tracking-widest">Recent runs</span>
            <div className="space-y-1 mt-1">
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
                </div>
              ))}
            </div>
          </div>
        )}

        {/* Run logs (persistent) */}
        {runLogs.length > 0 && (
          <div>
            <span className="text-[8px] font-mono uppercase text-zinc-600 tracking-widest">Run log</span>
            <div className="space-y-1 mt-1">
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
                    {run.summary && <p className="text-zinc-400 mt-0.5 truncate">{run.summary}</p>}
                  </div>
                </div>
              ))}
            </div>
          </div>
        )}
      </div>
    </div>
  );
}

// ─── Scheduled Job Row ────────────────────────────────────────

interface JobRowProps {
  job: SchedulerJob;
  expanded: boolean;
  executions: SchedulerExecution[];
  runLogs: RunLogEntry[];
  onToggleExpand: () => void;
  onTrigger: () => void;
  onToggle: () => void;
  onDelete: () => void;
}

function JobRow({ job, expanded, executions, runLogs, onToggleExpand, onTrigger, onToggle, onDelete }: JobRowProps) {
  return (
    <div>
      <div
        className={cn(
          "px-4 py-2.5 flex items-center gap-3 cursor-pointer hover:bg-white/[0.02] transition-colors",
          expanded && "bg-white/[0.02]"
        )}
        onClick={onToggleExpand}
      >
        <div className={cn(
          "w-2 h-2 rounded-full shrink-0",
          job.status === 'running' ? "bg-yellow-400 animate-pulse" :
          job.status === 'error' ? "bg-red-400" :
          job.enabled ? "bg-emerald-400" : "bg-zinc-600"
        )} />
        <div className="flex-1 min-w-0">
          <div className="flex items-center gap-2">
            <span className="text-[10px] font-medium truncate">{job.name}</span>
            {!job.enabled && <span className="text-[7px] font-mono uppercase bg-zinc-800 px-1 py-0.5 text-zinc-500">off</span>}
          </div>
          <div className="flex items-center gap-3 mt-0.5">
            <span className="text-[9px] font-mono text-zinc-500">{job.project}</span>
            <span className="text-[9px] font-mono text-zinc-600">{formatSchedule(job)}</span>
            <span className="text-[9px] font-mono text-zinc-600">{formatNextRun(job.nextRunAt)}</span>
          </div>
        </div>
        <div className="flex items-center gap-1" onClick={e => e.stopPropagation()}>
          <button onClick={onTrigger} disabled={job.status === 'running'} className="p-1 text-zinc-500 hover:text-emerald-400 transition-colors disabled:opacity-30" title="Run now">
            <Play size={11} />
          </button>
          <button onClick={onToggle} className={cn("p-1 transition-colors text-[9px] font-mono", job.enabled ? "text-emerald-400 hover:text-yellow-400" : "text-zinc-600 hover:text-emerald-400")} title={job.enabled ? "Pause schedule" : "Enable schedule"}>
            {job.enabled ? 'ON' : 'OFF'}
          </button>
          <button onClick={onDelete} className="p-1 text-zinc-500 hover:text-red-400 transition-colors" title="Delete">
            <Trash2 size={11} />
          </button>
          {expanded ? <ChevronDown size={11} className="text-zinc-600" /> : <ChevronRight size={11} className="text-zinc-600" />}
        </div>
      </div>
      {expanded && (
        <div className="px-4 pb-2 space-y-1.5 border-t border-white/5 pt-2 bg-black/20">
          <p className="text-[9px] font-mono text-zinc-400"><span className="text-zinc-600">Prompt: </span>{job.message}</p>
          {job.lastError && <p className="text-[9px] font-mono text-red-400"><span className="text-zinc-600">Error: </span>{job.lastError}</p>}
          {job.lastRunAt && <p className="text-[9px] font-mono text-zinc-600">Last run: {new Date(job.lastRunAt).toLocaleString()} ({job.lastRunStatus})</p>}
          {job.deliveryMode && job.deliveryMode !== 'store' && (
            <p className="text-[9px] font-mono text-zinc-500">Delivery: {job.deliveryMode}{job.deliveryWebhookUrl ? ` → ${job.deliveryWebhookUrl.slice(0, 40)}...` : ''}</p>
          )}
          {executions.length > 0 && (
            <div className="mt-1 space-y-0.5">
              <span className="text-[8px] font-mono uppercase text-zinc-600 tracking-widest">Recent</span>
              {executions.slice(0, 5).map(exec => (
                <div key={exec.id} className="flex items-center gap-2 text-[8px] font-mono">
                  <div className={cn("w-1.5 h-1.5 rounded-full", exec.status === 'success' ? "bg-emerald-400" : exec.status === 'error' ? "bg-red-400" : "bg-yellow-400")} />
                  <span className="text-zinc-500">{new Date(exec.startedAt).toLocaleString()}</span>
                  <span className={cn("uppercase", exec.status === 'success' ? "text-emerald-400" : "text-red-400")}>{exec.status}</span>
                </div>
              ))}
            </div>
          )}
        </div>
      )}
    </div>
  );
}

// ─── Create Task/Job Form ─────────────────────────────────────

interface CreateFormProps {
  projectDir: string;
  onSubmit: (data: { name: string; description?: string; message: string; model?: string; scheduleType: string; cronExpr?: string; everySeconds?: number }) => Promise<void>;
  onCancel: () => void;
}

function CreateForm({ projectDir, onSubmit, onCancel }: CreateFormProps) {
  const [name, setName] = useState('');
  const [description, setDescription] = useState('');
  const [message, setMessage] = useState('');
  const [model, setModel] = useState('');
  const [scheduleType, setScheduleType] = useState<'manual' | 'cron' | 'every'>('manual');
  const [cronExpr, setCronExpr] = useState('');
  const [everySeconds, setEverySeconds] = useState('3600');
  const [submitting, setSubmitting] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!name || !message) { setError('Name and prompt are required'); return; }
    setSubmitting(true);
    setError(null);
    try {
      await onSubmit({
        name,
        description: description || undefined,
        message,
        model: model || undefined,
        scheduleType,
        cronExpr: scheduleType === 'cron' ? cronExpr : undefined,
        everySeconds: scheduleType === 'every' ? parseInt(everySeconds) || 3600 : undefined,
      });
      setName(''); setDescription(''); setMessage(''); setModel('');
    } catch (err: any) {
      setError(err.message);
    } finally {
      setSubmitting(false);
    }
  };

  return (
    <form onSubmit={handleSubmit} className="p-3 border-b border-white/5 bg-zinc-950/50 space-y-2">
      <div className="flex items-center justify-between">
        <span className="text-[9px] font-mono uppercase tracking-widest text-primary">New Task</span>
        <button type="button" onClick={onCancel} className="text-zinc-500 hover:text-zinc-300"><X size={12} /></button>
      </div>
      {error && <div className="p-1.5 bg-red-500/10 border border-red-500/20 text-[9px] text-red-400">{error}</div>}
      <div className="flex gap-2">
        <input value={name} onChange={e => setName(e.target.value)} placeholder="Task name" className="flex-1 bg-zinc-900 border border-white/5 rounded px-2 py-1.5 text-[10px] outline-none focus:border-primary/40" autoFocus />
        <select value={scheduleType} onChange={e => setScheduleType(e.target.value as any)} className="bg-zinc-900 border border-white/5 rounded px-2 py-1.5 text-[10px] outline-none focus:border-primary/40">
          <option value="manual">Manual</option>
          <option value="cron">Cron</option>
          <option value="every">Interval</option>
        </select>
      </div>
      {scheduleType === 'cron' && (
        <input value={cronExpr} onChange={e => setCronExpr(e.target.value)} placeholder="Cron expression (e.g. 0 9 * * *)" className="w-full bg-zinc-900 border border-white/5 rounded px-2 py-1.5 text-[10px] outline-none focus:border-primary/40" />
      )}
      {scheduleType === 'every' && (
        <input type="number" value={everySeconds} onChange={e => setEverySeconds(e.target.value)} placeholder="Interval (seconds)" className="w-full bg-zinc-900 border border-white/5 rounded px-2 py-1.5 text-[10px] outline-none focus:border-primary/40" />
      )}
      <textarea value={message} onChange={e => setMessage(e.target.value)} placeholder="Prompt message" rows={2} className="w-full bg-zinc-900 border border-white/5 rounded px-2 py-1.5 text-[10px] outline-none focus:border-primary/40 resize-none" />
      <div className="flex items-center gap-2">
        <input value={model} onChange={e => setModel(e.target.value)} placeholder="Model (optional)" className="flex-1 bg-zinc-900 border border-white/5 rounded px-2 py-1.5 text-[10px] outline-none focus:border-primary/40" />
        <button type="submit" disabled={submitting || !name || !message} className="px-3 py-1.5 bg-primary text-black text-[9px] font-mono uppercase tracking-widest hover:bg-primary/80 disabled:opacity-30 transition-colors">
          {submitting ? 'Creating...' : 'Create'}
        </button>
      </div>
    </form>
  );
}

// ─── Task Board Tab (Unified) ─────────────────────────────────

interface TaskBoardTabProps {
  selectedProject: string | null;
}

export function TaskBoardTab({ selectedProject }: TaskBoardTabProps) {
  const { addToast } = useToastContext();
  const [allJobs, setAllJobs] = useState<SchedulerJob[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [showCreate, setShowCreate] = useState(false);
  const [selectedId, setSelectedId] = useState<string | null>(null);
  const [expandedJobId, setExpandedJobId] = useState<string | null>(null);
  const [executions, setExecutions] = useState<SchedulerExecution[]>([]);
  const [runLogs, setRunLogs] = useState<RunLogEntry[]>([]);

  // Only show jobs for the selected project
  const projectJobs = allJobs.filter(j => j.project === selectedProject);
  const manualTasks = projectJobs.filter(j => j.scheduleType === 'manual');
  const scheduledJobs = projectJobs.filter(j => j.scheduleType !== 'manual');

  const selectedTask = allJobs.find(j => j.id === selectedId) || null;

  const fetchJobs = useCallback(async () => {
    try {
      setLoading(true);
      const data = await api.scheduler.list();
      setAllJobs(data.jobs || []);
      setError(null);
    } catch (err: any) {
      setError(err.message);
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => { fetchJobs(); }, [fetchJobs]);

  // Auto-refresh every 5s
  useEffect(() => {
    const interval = setInterval(fetchJobs, 5000);
    return () => clearInterval(interval);
  }, [fetchJobs]);

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

  const handleCreate = useCallback(async (data: { name: string; description?: string; message: string; model?: string; scheduleType: string; cronExpr?: string; everySeconds?: number }) => {
    if (!selectedProject) return;
    const job: CreateJobRequest = {
      name: data.name,
      description: data.description,
      project: selectedProject,
      message: data.message,
      model: data.model,
      scheduleType: data.scheduleType as any,
      cronExpr: data.cronExpr,
      everySeconds: data.everySeconds,
      enabled: data.scheduleType !== 'manual', // manual tasks start disabled
    };
    await api.scheduler.create(job);
    addToast('success', `Task "${data.name}" created`);
    setShowCreate(false);
    fetchJobs();
  }, [selectedProject, addToast, fetchJobs]);

  const handleTrigger = useCallback(async (id: string) => {
    try {
      await api.scheduler.trigger(id);
      addToast('info', 'Task triggered');
      fetchJobs();
    } catch (err: any) {
      addToast('error', err.message);
    }
  }, [addToast, fetchJobs]);

  const handleDelete = useCallback(async (id: string) => {
    try {
      await api.scheduler.delete(id);
      if (selectedId === id) setSelectedId(null);
      addToast('success', 'Deleted');
      fetchJobs();
    } catch (err: any) {
      addToast('error', err.message);
    }
  }, [selectedId, addToast, fetchJobs]);

  const handleToggleScheduled = useCallback(async (job: SchedulerJob) => {
    try {
      await api.scheduler.update(job.id, { enabled: !job.enabled });
      fetchJobs();
    } catch (err: any) {
      addToast('error', err.message);
    }
  }, [fetchJobs, addToast]);

  if (!selectedProject) {
    return <EmptyState icon={<LayoutGrid size={32} />} title="Select a project to view tasks" />;
  }

  return (
    <div className="flex flex-col h-full">
      {/* Create form */}
      {showCreate && (
        <CreateForm
          projectDir={selectedProject}
          onSubmit={handleCreate}
          onCancel={() => setShowCreate(false)}
        />
      )}

      {/* Error banner */}
      {error && (
        <div className="mx-3 mt-2 p-2 bg-red-500/10 border border-red-500/20 flex items-center gap-2 text-[9px] text-red-400">
          <AlertCircle size={10} /> {error}
        </div>
      )}

      {/* Content */}
      <div className="flex-1 overflow-y-auto custom-scrollbar">
        {loading && projectJobs.length === 0 ? (
          <div className="flex-1 flex items-center justify-center h-32">
            <Loader size={16} className="animate-spin text-zinc-600" />
          </div>
        ) : projectJobs.length === 0 ? (
          <EmptyState
            icon={<LayoutGrid size={32} />}
            title="No tasks yet"
            description="Create a manual task or schedule a recurring job"
            action={{ label: '+ New Task', onClick: () => setShowCreate(true) }}
          />
        ) : (
          <div className="p-3 space-y-4">
            {/* Manual Tasks — Kanban */}
            {manualTasks.length > 0 && (
              <div>
                <div className="flex items-center gap-2 mb-2 px-1">
                  <Layers size={10} className="text-primary" />
                  <span className="text-[9px] font-mono uppercase tracking-widest text-zinc-500 font-bold">Manual Tasks</span>
                  <span className="text-[8px] font-mono text-zinc-700">{manualTasks.length}</span>
                </div>
                <div className="space-y-1">
                  {manualTasks.map(task => (
                    <TaskCard
                      key={task.id}
                      task={task}
                      isSelected={selectedId === task.id}
                      onSelect={() => {
                        setSelectedId(task.id);
                        fetchExecutions(task.id);
                      }}
                      onTrigger={() => handleTrigger(task.id)}
                      onDelete={() => handleDelete(task.id)}
                    />
                  ))}
                </div>
              </div>
            )}

            {/* Scheduled Jobs — List */}
            {scheduledJobs.length > 0 && (
              <div>
                <div className="flex items-center gap-2 mb-2 px-1">
                  <Clock size={10} className="text-primary" />
                  <span className="text-[9px] font-mono uppercase tracking-widest text-zinc-500 font-bold">Scheduled Jobs</span>
                  <span className="text-[8px] font-mono text-zinc-700">{scheduledJobs.length}</span>
                </div>
                <div className="border border-white/5 divide-y divide-white/5">
                  {scheduledJobs.map(job => (
                    <JobRow
                      key={job.id}
                      job={job}
                      expanded={expandedJobId === job.id}
                      executions={expandedJobId === job.id ? executions : []}
                      runLogs={expandedJobId === job.id ? runLogs : []}
                      onToggleExpand={() => {
                        if (expandedJobId === job.id) {
                          setExpandedJobId(null);
                          setExecutions([]);
                        } else {
                          setExpandedJobId(job.id);
                          fetchExecutions(job.id);
                        }
                      }}
                      onTrigger={() => handleTrigger(job.id)}
                      onToggle={() => handleToggleScheduled(job)}
                      onDelete={() => handleDelete(job.id)}
                    />
                  ))}
                </div>
              </div>
            )}
          </div>
        )}
      </div>

      {/* Bottom bar */}
      {!showCreate && (
        <div className="px-3 py-2 border-t border-white/5 shrink-0 flex items-center gap-2">
          <button
            onClick={() => setShowCreate(true)}
            className="flex items-center gap-1.5 px-3 py-1.5 text-[9px] font-mono uppercase tracking-widest bg-primary/10 text-primary hover:bg-primary/20 transition-colors"
          >
            <Plus size={10} /> New Task
          </button>
          <button onClick={fetchJobs} className="p-1.5 text-zinc-500 hover:text-zinc-300 transition-colors" title="Refresh">
            <RefreshCw size={12} />
          </button>
          <span className="ml-auto text-[8px] font-mono text-zinc-700">{projectJobs.length} total</span>
        </div>
      )}

      {/* Detail panel for selected manual task */}
      {selectedTask && selectedTask.scheduleType === 'manual' && (
        <TaskDetailPanel
          task={selectedTask}
          executions={executions}
          runLogs={runLogs}
          onClose={() => setSelectedId(null)}
          onTrigger={() => handleTrigger(selectedTask.id)}
          onDelete={() => handleDelete(selectedTask.id)}
        />
      )}
    </div>
  );
}
