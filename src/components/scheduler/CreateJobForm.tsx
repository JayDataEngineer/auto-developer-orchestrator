import React, { useState } from 'react';
import { X } from 'lucide-react';
import { cn } from '../../lib/utils';
import { CreateJobRequest } from '../../lib/api';

export type ScheduleType = 'cron' | 'every' | 'at';

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

export { SCHEDULE_PRESETS };

interface CreateJobFormProps {
  projects: string[];
  onSubmit: (job: CreateJobRequest) => Promise<void>;
  onCancel: () => void;
}

export function CreateJobForm({ projects, onSubmit, onCancel }: CreateJobFormProps) {
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
  const [deliveryMode, setDeliveryMode] = useState<'store' | 'webhook' | 'session'>('store');
  const [webhookUrl, setWebhookUrl] = useState('');

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
        deliveryMode,
        deliveryWebhookUrl: deliveryMode === 'webhook' ? webhookUrl : undefined,
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

      <div>
        <label className="text-[8px] font-mono uppercase text-zinc-600 tracking-widest">Delivery Mode</label>
        <div className="flex gap-2 mt-1">
          {(['store', 'session', 'webhook'] as const).map(mode => (
            <button
              key={mode}
              type="button"
              onClick={() => setDeliveryMode(mode)}
              className={cn(
                "px-2 py-1 text-[9px] font-mono uppercase border rounded transition-colors",
                deliveryMode === mode
                  ? "border-primary/40 bg-primary/10 text-primary"
                  : "border-white/5 text-zinc-500 hover:text-zinc-300"
              )}
            >
              {mode}
            </button>
          ))}
        </div>
        {deliveryMode === 'webhook' && (
          <input
            value={webhookUrl}
            onChange={e => setWebhookUrl(e.target.value)}
            placeholder="https://example.com/webhook"
            className="w-full mt-1 bg-zinc-900 border border-white/5 rounded px-2 py-1.5 text-[10px] outline-none focus:border-primary/40"
          />
        )}
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

      <div>
        <label className="text-[8px] font-mono uppercase text-zinc-600 tracking-widest">Delivery</label>
        <div className="flex gap-2 mt-1">
          <select
            value={deliveryMode}
            onChange={e => setDeliveryMode(e.target.value as any)}
            className="bg-zinc-900 border border-white/5 rounded px-2 py-1.5 text-[10px] outline-none"
          >
            <option value="store">Store only</option>
            <option value="session">Inject into session</option>
            <option value="webhook">Webhook</option>
          </select>
          {deliveryMode === 'webhook' && (
            <input
              value={webhookUrl}
              onChange={e => setWebhookUrl(e.target.value)}
              placeholder="https://example.com/webhook"
              className="bg-zinc-900 border border-white/5 rounded px-2 py-1.5 text-[10px] outline-none flex-1"
            />
          )}
        </div>
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
