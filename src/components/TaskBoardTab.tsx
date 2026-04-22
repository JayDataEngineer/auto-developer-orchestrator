import React, { useState, useEffect, useCallback } from 'react';
import { motion, AnimatePresence } from 'motion/react';
import { cn } from '../lib/utils';
import { api, SchedulerJob, CreateJobRequest } from '../lib/api';
import { useToastContext } from './ui/Toast';
import { StatusBadge } from './ui/StatusBadge';
import { EmptyState } from './ui/EmptyState';
import { usePolling } from '../hooks/usePolling';
import { Button } from './ui/button';
import { Input } from './ui/input';
import { Textarea } from './ui/textarea';
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from './ui/select';
import { Switch } from './ui/switch';
import { ScrollArea } from './ui/scroll-area';
import { Badge } from './ui/badge';
import { Separator } from './ui/separator';
import {
  LayoutGrid, Plus, Play, Trash2, Clock,
  Loader, AlertCircle, X, Layers, RefreshCw, Save, Copy, Link
} from 'lucide-react';

// --- Helpers ---

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

// --- Manual Task Card ---

interface TaskCardProps {
  task: SchedulerJob;
  isSelected: boolean;
  onSelect: () => void;
  onTrigger: () => void;
  onDelete: () => void;
}

function TaskCard({ task, isSelected, onSelect, onTrigger, onDelete }: TaskCardProps) {
  const isRunning = task.status === 'running';

  return (
    <div
      onClick={onSelect}
      className={cn(
        'p-2.5 border cursor-pointer transition-colors group',
        isSelected ? 'border-primary/30 bg-primary/5' : 'border-border bg-card hover:border-border hover:bg-card/80'
      )}
    >
      <div className="flex items-start gap-2">
        <StatusBadge status={jobBadgeStatus(task) as any} size="sm" />
        <div className="flex-1 min-w-0">
          <p className="text-sm font-medium truncate">{task.name}</p>
          {task.description && (
            <p className="text-xs text-muted-foreground/60 truncate mt-0.5">{task.description}</p>
          )}
        </div>
      </div>
      <div className="flex items-center gap-3 mt-1.5 text-xs text-muted-foreground/60">
        {task.durationMs ? (
          <span className="flex items-center gap-0.5"><Clock size={7} />{formatDuration(task.durationMs)}</span>
        ) : null}
        {task.inputTokens || task.outputTokens ? (
          <span>{formatTokens(task.inputTokens)}in / {formatTokens(task.outputTokens)}out</span>
        ) : null}
        <span className="ml-auto text-muted-foreground/50">{new Date(task.updatedAt).toLocaleString()}</span>
      </div>
      {/* Actions on hover */}
      <div className="flex items-center gap-1 mt-1.5 opacity-0 group-hover:opacity-100 transition-opacity">
        {!isRunning && (
          <Button variant="ghost" size="icon-xs" onClick={e => { e.stopPropagation(); onTrigger(); }} title="Run">
            <Play size={10} />
          </Button>
        )}
        {isRunning && (
          <span className="p-1 text-yellow-400 animate-pulse"><Loader size={10} /></span>
        )}
        <Button variant="ghost" size="icon-xs" onClick={e => { e.stopPropagation(); onDelete(); }} title="Delete">
          <Trash2 size={10} />
        </Button>
      </div>
    </div>
  );
}

// --- Edit Task Modal ---

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
          className="absolute inset-0 bg-background/80 backdrop-blur-sm"
        />
        <motion.div
          initial={{ opacity: 0, scale: 0.95, y: 20 }}
          animate={{ opacity: 1, scale: 1, y: 0 }}
          exit={{ opacity: 0, scale: 0.95, y: 20 }}
          className="relative w-full max-w-lg bg-card border border-border rounded-3xl overflow-hidden shadow-2xl"
        >
          {/* Header */}
          <div className="flex items-center gap-3 px-6 py-4 border-b border-border">
            <StatusBadge status={jobBadgeStatus(task) as any} size="md" />
            <span className="text-sm font-bold flex-1 truncate">Edit Task</span>
            <div className="flex items-center gap-1">
              {!isRunning && (
                <Button variant="ghost" size="xs" onClick={() => onTrigger(task.id)}>
                  <Play size={10} /> Run
                </Button>
              )}
              {isRunning && (
                <span className="flex items-center gap-1 px-2 py-1 text-xs font-semibold text-yellow-400">
                  <Loader size={10} className="animate-spin" /> Running
                </span>
              )}
              <Button variant="ghost" size="icon-xs" onClick={() => { onDelete(task.id); onClose(); }}>
                <Trash2 size={14} />
              </Button>
              <Button variant="ghost" size="icon-xs" onClick={onClose}>
                <X size={14} />
              </Button>
            </div>
          </div>

          {/* Form */}
          <form onSubmit={handleSave} className="p-6 space-y-3">
            <ScrollArea className="max-h-[70vh]">
              <div className="space-y-3 pr-2">
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
                  <Input value={name} onChange={e => setName(e.target.value)} placeholder="Task name" className="flex-1" autoFocus />
                  <Select value={scheduleType} onValueChange={v => setScheduleType(v as any)}>
                    <SelectTrigger className="w-[120px]">
                      <SelectValue />
                    </SelectTrigger>
                    <SelectContent>
                      <SelectItem value="manual">Manual</SelectItem>
                      <SelectItem value="cron">Cron</SelectItem>
                      <SelectItem value="every">Interval</SelectItem>
                    </SelectContent>
                  </Select>
                </div>

                <Input value={description} onChange={e => setDescription(e.target.value)} placeholder="Description (optional)" />

                {scheduleType === 'cron' && (
                  <div>
                    <Input value={cronExpr} onChange={e => setCronExpr(e.target.value)} placeholder="0 0 9 * * *" className="font-mono" />
                    <p className="text-[10px] text-muted-foreground/60 mt-1 ml-1">6 fields: sec min hour day month dow — e.g. "0 0 9 * * *" = daily at 9am</p>
                  </div>
                )}

                {scheduleType === 'every' && (
                  <Select value={everySeconds} onValueChange={setEverySeconds}>
                    <SelectTrigger>
                      <SelectValue />
                    </SelectTrigger>
                    <SelectContent>
                      <SelectItem value="300">Every 5 minutes</SelectItem>
                      <SelectItem value="900">Every 15 minutes</SelectItem>
                      <SelectItem value="1800">Every 30 minutes</SelectItem>
                      <SelectItem value="3600">Every hour</SelectItem>
                      <SelectItem value="21600">Every 6 hours</SelectItem>
                      <SelectItem value="43200">Every 12 hours</SelectItem>
                      <SelectItem value="86400">Every 24 hours</SelectItem>
                    </SelectContent>
                  </Select>
                )}

                <Textarea value={message} onChange={e => setMessage(e.target.value)} placeholder="Prompt — what should the agent do?" rows={4} />

                <Input value={model} onChange={e => setModel(e.target.value)} placeholder="Model (optional)" />

                {/* Metrics (read-only) */}
                <Separator className="my-2" />
                <div className="flex items-center gap-4 pt-2">
                  <div>
                    <span className="text-[10px] font-medium text-muted-foreground/60">Duration</span>
                    <p className="text-xs text-muted-foreground">{formatDuration(task.durationMs)}</p>
                  </div>
                  <div>
                    <span className="text-[10px] font-medium text-muted-foreground/60">Tokens</span>
                    <p className="text-xs text-muted-foreground">{formatTokens(task.inputTokens)} in / {formatTokens(task.outputTokens)} out</p>
                  </div>
                  <div>
                    <span className="text-[10px] font-medium text-muted-foreground/60">Last Run</span>
                    <p className="text-xs text-muted-foreground">{task.lastRunAt ? new Date(task.lastRunAt).toLocaleString() : '—'}</p>
                  </div>
                </div>

                {/* Webhook URL */}
                {task.webhookToken && (
                  <>
                    <Separator className="my-2" />
                    <div className="pt-2">
                      <span className="text-[10px] font-medium text-muted-foreground/60">Webhook URL</span>
                      <div className="flex items-center gap-2 mt-1">
                        <Link size={10} className="text-muted-foreground/60 shrink-0" />
                        <code className="flex-1 text-xs font-mono text-muted-foreground bg-muted rounded-lg px-2 py-1.5 truncate border border-border">
                          {window.location.origin}/api/scheduler/webhook/{task.webhookToken}
                        </code>
                        <Button
                          type="button"
                          variant="ghost"
                          size="icon-xs"
                          onClick={() => {
                            navigator.clipboard.writeText(`${window.location.origin}/api/scheduler/webhook/${task.webhookToken}`);
                            addToast('success', 'Webhook URL copied');
                          }}
                          title="Copy webhook URL"
                        >
                          <Copy size={12} />
                        </Button>
                      </div>
                      <p className="text-[10px] text-muted-foreground/50 mt-1 ml-4">POST to this URL to trigger the job. Optional body: {"{"}"message": "override prompt"{"}"}</p>
                    </div>
                  </>
                )}

                {/* Save button */}
                <Button
                  type="submit"
                  variant="default"
                  size="default"
                  disabled={saving || !name || !message}
                  className="w-full"
                >
                  {saving ? (
                    <><Loader size={14} className="animate-spin" /> Saving...</>
                  ) : (
                    <><Save size={14} /> Save Changes</>
                  )}
                </Button>
              </div>
            </ScrollArea>
          </form>
        </motion.div>
      </div>
    </AnimatePresence>
  );
}

// --- Scheduled Job Row ---

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
      className="px-4 py-2.5 flex items-center gap-3 cursor-pointer hover:bg-muted/50 transition-colors"
      onClick={onEdit}
    >
      <div className={cn(
        "w-2 h-2 rounded-full shrink-0",
        job.status === 'running' ? "bg-yellow-400 animate-pulse" :
        job.status === 'error' ? "bg-red-400" :
        job.enabled ? "bg-emerald-400" : "bg-muted-foreground/60"
      )} />
      <div className="flex-1 min-w-0">
        <div className="flex items-center gap-2">
          <span className="text-sm font-medium truncate">{job.name}</span>
          {!job.enabled && <Badge variant="outline">off</Badge>}
        </div>
        <div className="flex items-center gap-3 mt-0.5">
          <span className="text-xs text-muted-foreground/60">{formatSchedule(job)}</span>
          <span className="text-xs text-muted-foreground/60">{formatNextRun(job.nextRunAt)}</span>
        </div>
      </div>
      <div className="flex items-center gap-1" onClick={e => e.stopPropagation()}>
        <Button variant="ghost" size="icon-xs" onClick={onTrigger} disabled={job.status === 'running'} title="Run now">
          <Play size={11} />
        </Button>
        <Switch checked={job.enabled} onCheckedChange={onToggle} title={job.enabled ? "Pause schedule" : "Enable schedule"} />
        <Button variant="ghost" size="icon-xs" onClick={onDelete} title="Delete">
          <Trash2 size={11} />
        </Button>
      </div>
    </div>
  );
}

// --- Create Task/Job Form ---

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
    <form onSubmit={handleSubmit} className="p-3 border-b border-border bg-muted/50 space-y-2">
      <div className="flex items-center justify-between">
        <span className="text-xs font-semibold text-primary">New Task</span>
        <Button type="button" variant="ghost" size="icon-xs" onClick={onCancel}>
          <X size={12} />
        </Button>
      </div>
      {error && <div className="p-1.5 bg-red-500/10 border border-red-500/20 text-xs text-red-400">{error}</div>}
      <div className="flex gap-2">
        <Input value={name} onChange={e => setName(e.target.value)} placeholder="Task name" className="flex-1" autoFocus />
        <Select value={scheduleType} onValueChange={v => setScheduleType(v as any)}>
          <SelectTrigger className="w-[120px]">
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value="manual">Manual</SelectItem>
            <SelectItem value="cron">Cron</SelectItem>
            <SelectItem value="every">Interval</SelectItem>
          </SelectContent>
        </Select>
      </div>
      <Input value={description} onChange={e => setDescription(e.target.value)} placeholder="Description (optional)" />
      {scheduleType === 'cron' && (
        <div>
          <Input value={cronExpr} onChange={e => setCronExpr(e.target.value)} placeholder="0 0 9 * * *" className="font-mono" />
              <p className="text-[10px] text-muted-foreground/60 mt-0.5">6 fields: sec min hour day month dow — e.g. "0 0 9 * * *" = daily at 9am</p>
        </div>
      )}
      {scheduleType === 'every' && (
        <Select value={everySeconds} onValueChange={setEverySeconds}>
          <SelectTrigger>
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value="300">Every 5 minutes</SelectItem>
            <SelectItem value="900">Every 15 minutes</SelectItem>
            <SelectItem value="1800">Every 30 minutes</SelectItem>
            <SelectItem value="3600">Every hour</SelectItem>
            <SelectItem value="21600">Every 6 hours</SelectItem>
            <SelectItem value="43200">Every 12 hours</SelectItem>
            <SelectItem value="86400">Every 24 hours</SelectItem>
          </SelectContent>
        </Select>
      )}
      <Textarea value={message} onChange={e => setMessage(e.target.value)} placeholder="Prompt — what should the agent do?" rows={3} />
      <div className="flex items-center gap-2">
        <Input value={model} onChange={e => setModel(e.target.value)} placeholder="Model (optional)" className="flex-1" />
        <Button type="submit" variant="default" size="xs" disabled={submitting || !name || !message}>
          {submitting ? 'Creating...' : 'Create'}
        </Button>
      </div>
    </form>
  );
}

// --- Task Board Tab (Unified) ---

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
  usePolling(fetchJobs, 5000, true);

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
      <ScrollArea className="flex-1">
        {loading && allJobs.length === 0 ? (
          <div className="flex-1 flex items-center justify-center h-32">
            <Loader size={16} className="animate-spin text-muted-foreground/60" />
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
                  <span className="text-xs font-semibold text-muted-foreground">Manual Tasks</span>
                  <Badge variant="outline">{manualTasks.length}</Badge>
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
                  <span className="text-xs font-semibold text-muted-foreground">Scheduled Jobs</span>
                  <Badge variant="outline">{scheduledJobs.length}</Badge>
                </div>
                <div className="border border-border divide-y divide-border">
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
      </ScrollArea>

      {/* Bottom bar */}
      {!showCreate && (
        <div className="px-3 py-2 border-t border-border shrink-0 flex items-center gap-2">
          <Button
            variant="default"
            size="xs"
            onClick={() => setShowCreate(true)}
          >
            <Plus size={10} /> New Task
          </Button>
          <Button variant="ghost" size="icon-xs" onClick={fetchJobs} title="Refresh">
            <RefreshCw size={12} />
          </Button>
          <span className="ml-auto text-xs text-muted-foreground/50">{allJobs.length} total</span>
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
