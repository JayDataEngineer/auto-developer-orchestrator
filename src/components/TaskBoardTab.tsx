import React, { useState, useEffect, useCallback } from 'react';
import { motion, AnimatePresence } from 'motion/react';
import { cn } from '../lib/utils';
import { api, SchedulerJob, CreateJobRequest } from '../lib/api';
import { useToastContext } from './ui/Toast';
import { StatusBadge } from './ui/StatusBadge';
import { EmptyState } from './ui/EmptyState';
import {
  LayoutGrid, Plus, Play, Trash2, Clock,
  Loader, AlertCircle, X, Layers, RefreshCw, Save, Copy, Link
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
          <p className="text-sm font-medium truncate">{task.name}</p>
          {task.description && (
            <p className="text-xs font-mono text-zinc-600 truncate mt-0.5">{task.description}</p>
          )}
        </div>
      </div>
      <div className="flex items-center gap-3 mt-1.5 text-xs font-mono text-zinc-600">
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

// ─── Edit Task Modal ──────────────────────────────────────────

interface EditTaskModalProps {
  task: SchedulerJob;
  onClose: () => void;
  onSave: (id: string, data: Partial<CreateJobRequest>) => Promise<void>;
  onTrigger: (id: string) => void;
  onDelete: (id: string) => void;
}

function EditTaskModal({ task, onClose, onSave, onTrigger, onDelete }: EditTaskModalProps) {
  const { addToast } = useToastContext();
  const [name, setName] = useState(task.name);
  const [description, setDescription] = useState(task.description || '');
  const [message, setMessage] = useState(task.message);
  const [model, setModel] = useState(task.model || '');
  const [scheduleType, setScheduleType] = useState<'manual' | 'cron' | 'every'>(task.scheduleType === 'at' ? 'manual' : task.scheduleType as any);
  const [cronExpr, setCronExpr] = useState(task.cronExpr || '');
  const [everySeconds, setEverySeconds] = useState(String(task.everySeconds || 3600));
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const isRunning = task.status === 'running';

  const handleSave = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!name || !message) { setError('Name and prompt are required'); return; }
    if (scheduleType === 'cron' && !cronExpr.trim()) { setError('Cron expression is required'); return; }
    if (scheduleType === 'cron') {
      const parts = cronExpr.trim().split(/\s+/);
      if (parts.length !== 6) { setError('Cron must have 6 fields (sec min hour day month dow)'); return; }
    }
    if (scheduleType === 'every' && (!everySeconds || parseInt(everySeconds) < 10)) { setError('Interval must be at least 10 seconds'); return; }
    setSaving(true);
    setError(null);
    try {
      await onSave(task.id, {
        name,
        description: description || undefined,
        message,
        model: model || undefined,
        scheduleType,
        cronExpr: scheduleType === 'cron' ? cronExpr : undefined,
        everySeconds: scheduleType === 'every' ? parseInt(everySeconds) || 3600 : undefined,
      });
      onClose();
    } catch (err: any) {
      setError(err.message);
    } finally {
      setSaving(false);
    }
  };

  return (
    <AnimatePresence>
      <div className="fixed inset-0 z-[100] flex items-center justify-center p-4">
        <motion.div
          initial={{ opacity: 0 }}
          animate={{ opacity: 1 }}
          exit={{ opacity: 0 }}
          onClick={onClose}
          className="absolute inset-0 bg-black/80 backdrop-blur-sm"
        />
        <motion.div
          initial={{ opacity: 0, scale: 0.95, y: 20 }}
          animate={{ opacity: 1, scale: 1, y: 0 }}
          exit={{ opacity: 0, scale: 0.95, y: 20 }}
          className="relative w-full max-w-lg bg-zinc-900 border border-white/10 rounded-3xl overflow-hidden shadow-2xl"
        >
          {/* Header */}
          <div className="flex items-center gap-3 px-6 py-4 border-b border-white/5">
            <StatusBadge status={jobBadgeStatus(task) as any} size="md" />
            <span className="text-sm font-bold flex-1 truncate">Edit Task</span>
            <div className="flex items-center gap-1">
              {!isRunning && (
                <button onClick={() => onTrigger(task.id)} className="flex items-center gap-1 px-2 py-1 text-xs font-mono uppercase tracking-widest bg-emerald-500/10 text-emerald-400 hover:bg-emerald-500/20 transition-colors rounded">
                  <Play size={10} /> Run
                </button>
              )}
              {isRunning && (
                <span className="flex items-center gap-1 px-2 py-1 text-xs font-mono uppercase tracking-widest text-yellow-400">
                  <Loader size={10} className="animate-spin" /> Running
                </span>
              )}
              <button onClick={() => { onDelete(task.id); onClose(); }} className="p-1.5 text-zinc-500 hover:text-red-400 transition-colors">
                <Trash2 size={14} />
              </button>
              <button onClick={onClose} className="p-1.5 text-zinc-500 hover:text-zinc-300 transition-colors">
                <X size={14} />
              </button>
            </div>
          </div>

          {/* Form */}
          <form onSubmit={handleSave} className="p-6 space-y-3 max-h-[70vh] overflow-y-auto custom-scrollbar">
            {error && (
              <div className="p-2 bg-red-500/10 border border-red-500/20 text-xs text-red-400 rounded-lg">{error}</div>
            )}

            {task.lastError && (
              <div className="p-2 bg-red-500/5 border border-red-500/20 flex items-start gap-2 rounded-lg">
                <AlertCircle size={12} className="text-red-400 shrink-0 mt-0.5" />
                <pre className="text-xs font-mono text-red-400 whitespace-pre-wrap">{task.lastError}</pre>
              </div>
            )}

            <div className="flex gap-2">
              <input value={name} onChange={e => setName(e.target.value)} placeholder="Task name" className="flex-1 bg-black border border-white/10 rounded-xl px-3 py-2.5 text-sm text-zinc-300 outline-none focus:border-primary/50 transition-colors" autoFocus />
              <select value={scheduleType} onChange={e => setScheduleType(e.target.value as any)} className="bg-black border border-white/10 rounded-xl px-3 py-2.5 text-sm text-zinc-300 outline-none focus:border-primary/50 transition-colors">
                <option value="manual">Manual</option>
                <option value="cron">Cron</option>
                <option value="every">Interval</option>
              </select>
            </div>

            <input value={description} onChange={e => setDescription(e.target.value)} placeholder="Description (optional)" className="w-full bg-black border border-white/10 rounded-xl px-3 py-2.5 text-sm text-zinc-300 outline-none focus:border-primary/50 transition-colors" />

            {scheduleType === 'cron' && (
              <div>
                <input value={cronExpr} onChange={e => setCronExpr(e.target.value)} placeholder="0 0 9 * * *" className="w-full bg-black border border-white/10 rounded-xl px-3 py-2.5 text-sm text-zinc-300 outline-none focus:border-primary/50 transition-colors font-mono" />
                <p className="text-[10px] text-zinc-600 mt-1 ml-1">6 fields: sec min hour day month dow — e.g. "0 0 9 * * *" = daily at 9am</p>
              </div>
            )}

            {scheduleType === 'every' && (
              <select value={everySeconds} onChange={e => setEverySeconds(e.target.value)} className="w-full bg-black border border-white/10 rounded-xl px-3 py-2.5 text-sm text-zinc-300 outline-none focus:border-primary/50 transition-colors">
                <option value="300">Every 5 minutes</option>
                <option value="900">Every 15 minutes</option>
                <option value="1800">Every 30 minutes</option>
                <option value="3600">Every hour</option>
                <option value="21600">Every 6 hours</option>
                <option value="43200">Every 12 hours</option>
                <option value="86400">Every 24 hours</option>
              </select>
            )}

            <textarea value={message} onChange={e => setMessage(e.target.value)} placeholder="Prompt — what should the agent do?" rows={4} className="w-full bg-black border border-white/10 rounded-xl px-3 py-2.5 text-sm text-zinc-300 outline-none focus:border-primary/50 transition-colors resize-none" />

            <div className="flex items-center gap-2">
              <input value={model} onChange={e => setModel(e.target.value)} placeholder="Model (optional)" className="flex-1 bg-black border border-white/10 rounded-xl px-3 py-2.5 text-sm text-zinc-300 outline-none focus:border-primary/50 transition-colors" />
            </div>

            {/* Metrics (read-only) */}
            <div className="flex items-center gap-4 pt-2 border-t border-white/5">
              <div>
                <span className="text-[10px] font-mono uppercase text-zinc-600 tracking-widest">Duration</span>
                <p className="text-xs font-mono text-zinc-400">{formatDuration(task.durationMs)}</p>
              </div>
              <div>
                <span className="text-[10px] font-mono uppercase text-zinc-600 tracking-widest">Tokens</span>
                <p className="text-xs font-mono text-zinc-400">{formatTokens(task.inputTokens)} in / {formatTokens(task.outputTokens)} out</p>
              </div>
              <div>
                <span className="text-[10px] font-mono uppercase text-zinc-600 tracking-widest">Last Run</span>
                <p className="text-xs font-mono text-zinc-400">{task.lastRunAt ? new Date(task.lastRunAt).toLocaleString() : '—'}</p>
              </div>
            </div>

            {/* Webhook URL */}
            {task.webhookToken && (
              <div className="pt-2 border-t border-white/5">
                <span className="text-[10px] font-mono uppercase text-zinc-600 tracking-widest">Webhook URL</span>
                <div className="flex items-center gap-2 mt-1">
                  <Link size={10} className="text-zinc-600 shrink-0" />
                  <code className="flex-1 text-xs font-mono text-zinc-400 bg-black/50 rounded-lg px-2 py-1.5 truncate border border-white/5">
                    {window.location.origin}/api/scheduler/webhook/{task.webhookToken}
                  </code>
                  <button
                    type="button"
                    onClick={() => {
                      navigator.clipboard.writeText(`${window.location.origin}/api/scheduler/webhook/${task.webhookToken}`);
                      addToast('success', 'Webhook URL copied');
                    }}
                    className="p-1.5 text-zinc-500 hover:text-primary transition-colors shrink-0"
                    title="Copy webhook URL"
                  >
                    <Copy size={12} />
                  </button>
                </div>
                <p className="text-[10px] text-zinc-700 mt-1 ml-4">POST to this URL to trigger the job. Optional body: {"{"}"message": "override prompt"{"}"}</p>
              </div>
            )}

            {/* Save button */}
            <button
              type="submit"
              disabled={saving || !name || !message}
              className="w-full bg-primary text-black font-bold py-3 rounded-xl text-xs uppercase tracking-widest hover:bg-primary/90 transition-all flex items-center justify-center gap-2 shadow-lg shadow-primary/20 disabled:opacity-50 disabled:cursor-not-allowed"
            >
              {saving ? (
                <><Loader size={14} className="animate-spin" /> Saving...</>
              ) : (
                <><Save size={14} /> Save Changes</>
              )}
            </button>
          </form>
        </motion.div>
      </div>
    </AnimatePresence>
  );
}

// ─── Scheduled Job Row ────────────────────────────────────────

interface JobRowProps {
  job: SchedulerJob;
  onEdit: () => void;
  onTrigger: () => void;
  onToggle: () => void;
  onDelete: () => void;
}

function JobRow({ job, onEdit, onTrigger, onToggle, onDelete }: JobRowProps) {
  return (
    <div
      className="px-4 py-2.5 flex items-center gap-3 cursor-pointer hover:bg-white/[0.02] transition-colors"
      onClick={onEdit}
    >
      <div className={cn(
        "w-2 h-2 rounded-full shrink-0",
        job.status === 'running' ? "bg-yellow-400 animate-pulse" :
        job.status === 'error' ? "bg-red-400" :
        job.enabled ? "bg-emerald-400" : "bg-zinc-600"
      )} />
      <div className="flex-1 min-w-0">
        <div className="flex items-center gap-2">
          <span className="text-sm font-medium truncate">{job.name}</span>
          {!job.enabled && <span className="text-[10px] font-mono uppercase bg-zinc-800 px-1 py-0.5 text-zinc-500">off</span>}
        </div>
        <div className="flex items-center gap-3 mt-0.5">
          <span className="text-xs font-mono text-zinc-600">{formatSchedule(job)}</span>
          <span className="text-xs font-mono text-zinc-600">{formatNextRun(job.nextRunAt)}</span>
        </div>
      </div>
      <div className="flex items-center gap-1" onClick={e => e.stopPropagation()}>
        <button onClick={onTrigger} disabled={job.status === 'running'} className="p-1 text-zinc-500 hover:text-emerald-400 transition-colors disabled:opacity-30" title="Run now">
          <Play size={11} />
        </button>
        <button onClick={onToggle} className={cn("p-1 transition-colors text-xs font-mono", job.enabled ? "text-emerald-400 hover:text-yellow-400" : "text-zinc-600 hover:text-emerald-400")} title={job.enabled ? "Pause schedule" : "Enable schedule"}>
          {job.enabled ? 'ON' : 'OFF'}
        </button>
        <button onClick={onDelete} className="p-1 text-zinc-500 hover:text-red-400 transition-colors" title="Delete">
          <Trash2 size={11} />
        </button>
      </div>
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
    if (scheduleType === 'cron' && !cronExpr.trim()) { setError('Cron expression is required'); return; }
    if (scheduleType === 'cron') {
      const parts = cronExpr.trim().split(/\s+/);
      if (parts.length !== 6) { setError('Cron must have 6 fields (sec min hour day month dow). Example: 0 0 9 * * *'); return; }
    }
    if (scheduleType === 'every' && (!everySeconds || parseInt(everySeconds) < 10)) { setError('Interval must be at least 10 seconds'); return; }
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
      setName(''); setDescription(''); setMessage(''); setModel(''); setCronExpr(''); setEverySeconds('3600');
    } catch (err: any) {
      setError(err.message);
    } finally {
      setSubmitting(false);
    }
  };

  return (
    <form onSubmit={handleSubmit} className="p-3 border-b border-white/5 bg-zinc-950/50 space-y-2">
      <div className="flex items-center justify-between">
        <span className="text-xs font-mono uppercase tracking-widest text-primary">New Task</span>
        <button type="button" onClick={onCancel} className="text-zinc-500 hover:text-zinc-300"><X size={12} /></button>
      </div>
      {error && <div className="p-1.5 bg-red-500/10 border border-red-500/20 text-xs text-red-400">{error}</div>}
      <div className="flex gap-2">
        <input value={name} onChange={e => setName(e.target.value)} placeholder="Task name" className="flex-1 bg-zinc-900 border border-white/5 rounded px-2 py-1.5 text-sm outline-none focus:border-primary/40" autoFocus />
        <select value={scheduleType} onChange={e => setScheduleType(e.target.value as any)} className="bg-zinc-900 border border-white/5 rounded px-2 py-1.5 text-sm outline-none focus:border-primary/40">
          <option value="manual">Manual</option>
          <option value="cron">Cron</option>
          <option value="every">Interval</option>
        </select>
      </div>
      <input value={description} onChange={e => setDescription(e.target.value)} placeholder="Description (optional)" className="w-full bg-zinc-900 border border-white/5 rounded px-2 py-1.5 text-sm outline-none focus:border-primary/40" />
      {scheduleType === 'cron' && (
        <div>
          <input value={cronExpr} onChange={e => setCronExpr(e.target.value)} placeholder="0 0 9 * * *" className="w-full bg-zinc-900 border border-white/5 rounded px-2 py-1.5 text-sm outline-none focus:border-primary/40 font-mono" />
          <p className="text-[10px] text-zinc-600 mt-0.5">6 fields: sec min hour day month dow — e.g. "0 0 9 * * *" = daily at 9am</p>
        </div>
      )}
      {scheduleType === 'every' && (
        <select value={everySeconds} onChange={e => setEverySeconds(e.target.value)} className="w-full bg-zinc-900 border border-white/5 rounded px-2 py-1.5 text-sm outline-none focus:border-primary/40">
          <option value="300">Every 5 minutes</option>
          <option value="900">Every 15 minutes</option>
          <option value="1800">Every 30 minutes</option>
          <option value="3600">Every hour</option>
          <option value="21600">Every 6 hours</option>
          <option value="43200">Every 12 hours</option>
          <option value="86400">Every 24 hours</option>
        </select>
      )}
      <textarea value={message} onChange={e => setMessage(e.target.value)} placeholder="Prompt — what should the agent do?" rows={3} className="w-full bg-zinc-900 border border-white/5 rounded px-2 py-1.5 text-sm outline-none focus:border-primary/40 resize-none" />
      <div className="flex items-center gap-2">
        <input value={model} onChange={e => setModel(e.target.value)} placeholder="Model (optional)" className="flex-1 bg-zinc-900 border border-white/5 rounded px-2 py-1.5 text-sm outline-none focus:border-primary/40" />
        <button type="submit" disabled={submitting || !name || !message} className="px-3 py-1.5 bg-primary text-black text-xs font-mono uppercase tracking-widest hover:bg-primary/80 disabled:opacity-30 transition-colors">
          {submitting ? 'Creating...' : 'Create'}
        </button>
      </div>
    </form>
  );
}

// ─── Task Board Tab (Unified) ─────────────────────────────────

export function TaskBoardTab() {
  const { addToast } = useToastContext();
  const [allJobs, setAllJobs] = useState<SchedulerJob[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [showCreate, setShowCreate] = useState(false);
  const [editingTask, setEditingTask] = useState<SchedulerJob | null>(null);

  const manualTasks = allJobs.filter(j => j.scheduleType === 'manual');
  const scheduledJobs = allJobs.filter(j => j.scheduleType !== 'manual');

  const fetchJobs = useCallback(async () => {
    try {
      setLoading(true);
      const data = await api.scheduler.list();
      const jobs = data.jobs || [];
      jobs.sort((a, b) => new Date(b.updatedAt).getTime() - new Date(a.updatedAt).getTime());
      setAllJobs(jobs);
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

  const handleCreate = useCallback(async (data: { name: string; description?: string; message: string; model?: string; scheduleType: string; cronExpr?: string; everySeconds?: number }) => {
    const job: CreateJobRequest = {
      name: data.name,
      description: data.description,
      project: '',
      message: data.message,
      model: data.model,
      scheduleType: data.scheduleType as any,
      cronExpr: data.cronExpr,
      everySeconds: data.everySeconds,
      enabled: data.scheduleType !== 'manual',
    };
    await api.scheduler.create(job);
    addToast('success', `Task "${data.name}" created`);
    setShowCreate(false);
    fetchJobs();
  }, [addToast, fetchJobs]);

  const handleSave = useCallback(async (id: string, data: Partial<CreateJobRequest>) => {
    await api.scheduler.update(id, data);
    addToast('success', 'Task updated');
    fetchJobs();
  }, [addToast, fetchJobs]);

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
      setEditingTask(null);
      addToast('success', 'Deleted');
      fetchJobs();
    } catch (err: any) {
      addToast('error', err.message);
    }
  }, [addToast, fetchJobs]);

  const handleToggleScheduled = useCallback(async (job: SchedulerJob) => {
    try {
      await api.scheduler.update(job.id, { enabled: !job.enabled });
      fetchJobs();
    } catch (err: any) {
      addToast('error', err.message);
    }
  }, [fetchJobs, addToast]);

  return (
    <div className="flex flex-col h-full">
      {/* Create form */}
      {showCreate && (
        <CreateForm
          projectDir=""
          onSubmit={handleCreate}
          onCancel={() => setShowCreate(false)}
        />
      )}

      {/* Error banner */}
      {error && (
        <div className="mx-3 mt-2 p-2 bg-red-500/10 border border-red-500/20 flex items-center gap-2 text-xs text-red-400">
          <AlertCircle size={10} /> {error}
        </div>
      )}

      {/* Content */}
      <div className="flex-1 overflow-y-auto custom-scrollbar">
        {loading && allJobs.length === 0 ? (
          <div className="flex-1 flex items-center justify-center h-32">
            <Loader size={16} className="animate-spin text-zinc-600" />
          </div>
        ) : allJobs.length === 0 ? (
          <EmptyState
            icon={<LayoutGrid size={32} />}
            title="No tasks yet"
            description="Create a manual task or schedule a recurring job"
            action={{ label: '+ New Task', onClick: () => setShowCreate(true) }}
          />
        ) : (
          <div className="p-3 space-y-4">
            {/* Manual Tasks */}
            {manualTasks.length > 0 && (
              <div>
                <div className="flex items-center gap-2 mb-2 px-1">
                  <Layers size={10} className="text-primary" />
                  <span className="text-xs font-mono uppercase tracking-widest text-zinc-500 font-bold">Manual Tasks</span>
                  <span className="text-xs font-mono text-zinc-700">{manualTasks.length}</span>
                </div>
                <div className="space-y-1">
                  {manualTasks.map(task => (
                    <TaskCard
                      key={task.id}
                      task={task}
                      isSelected={editingTask?.id === task.id}
                      onSelect={() => setEditingTask(task)}
                      onTrigger={() => handleTrigger(task.id)}
                      onDelete={() => handleDelete(task.id)}
                    />
                  ))}
                </div>
              </div>
            )}

            {/* Scheduled Jobs */}
            {scheduledJobs.length > 0 && (
              <div>
                <div className="flex items-center gap-2 mb-2 px-1">
                  <Clock size={10} className="text-primary" />
                  <span className="text-xs font-mono uppercase tracking-widest text-zinc-500 font-bold">Scheduled Jobs</span>
                  <span className="text-xs font-mono text-zinc-700">{scheduledJobs.length}</span>
                </div>
                <div className="border border-white/5 divide-y divide-white/5">
                  {scheduledJobs.map(job => (
                    <JobRow
                      key={job.id}
                      job={job}
                      onEdit={() => setEditingTask(job)}
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
            className="flex items-center gap-1.5 px-3 py-1.5 text-xs font-mono uppercase tracking-widest bg-primary/10 text-primary hover:bg-primary/20 transition-colors"
          >
            <Plus size={10} /> New Task
          </button>
          <button onClick={fetchJobs} className="p-1.5 text-zinc-500 hover:text-zinc-300 transition-colors" title="Refresh">
            <RefreshCw size={12} />
          </button>
          <span className="ml-auto text-xs font-mono text-zinc-700">{allJobs.length} total</span>
        </div>
      )}

      {/* Edit modal */}
      {editingTask && (
        <EditTaskModal
          task={editingTask}
          onClose={() => setEditingTask(null)}
          onSave={handleSave}
          onTrigger={handleTrigger}
          onDelete={handleDelete}
        />
      )}
    </div>
  );
}
