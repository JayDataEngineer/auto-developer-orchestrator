import React, { useState, useCallback } from 'react';
import { cn } from '../lib/utils';
import { PiTask } from '../lib/api';
import { useTasks } from '../hooks/useTasks';
import { useToastContext } from './ui/Toast';
import { StatusBadge } from './ui/StatusBadge';
import { EmptyState } from './ui/EmptyState';
import {
  LayoutGrid, Plus, Play, Square, Trash2, Clock,
  Loader, ChevronRight, ChevronDown, AlertCircle, X, Layers
} from 'lucide-react';

type StatusColumn = PiTask['status'];

const COLUMNS: { status: StatusColumn; label: string }[] = [
  { status: 'pending', label: 'Pending' },
  { status: 'in_progress', label: 'In Progress' },
  { status: 'completed', label: 'Completed' },
  { status: 'failed', label: 'Failed' },
];

function formatDuration(ms?: number): string {
  if (!ms) return '—';
  if (ms < 1000) return `${ms}ms`;
  if (ms < 60000) return `${(ms / 1000).toFixed(1)}s`;
  return `${(ms / 60000).toFixed(1)}m`;
}

function formatTime(ts: number): string {
  return new Date(ts).toLocaleString();
}

function formatTokens(n?: number): string {
  if (!n) return '—';
  if (n < 1000) return String(n);
  return `${(n / 1000).toFixed(1)}k`;
}

// ─── Task Card ────────────────────────────────────────────────

interface TaskCardProps {
  task: PiTask;
  isSelected: boolean;
  onSelect: () => void;
  onStart: () => void;
  onStop: () => void;
  onDelete: () => void;
}

function TaskCard({ task, isSelected, onSelect, onStart, onStop, onDelete }: TaskCardProps) {
  return (
    <div
      onClick={onSelect}
      className={cn(
        'p-2.5 border cursor-pointer transition-colors group',
        isSelected ? 'border-primary/30 bg-primary/5' : 'border-white/5 bg-zinc-900 hover:border-white/10 hover:bg-zinc-900/80'
      )}
    >
      <div className="flex items-start gap-2">
        <StatusBadge status={task.status} size="sm" />
        <div className="flex-1 min-w-0">
          <p className="text-[10px] font-medium truncate">{task.title}</p>
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
        <span className="ml-auto text-zinc-700">{formatTime(task.updatedAt)}</span>
      </div>
      {/* Actions on hover */}
      <div className="flex items-center gap-1 mt-1.5 opacity-0 group-hover:opacity-100 transition-opacity">
        {task.status === 'pending' && (
          <button onClick={e => { e.stopPropagation(); onStart(); }} className="p-1 text-zinc-500 hover:text-emerald-400 transition-colors" title="Start">
            <Play size={10} />
          </button>
        )}
        {task.status === 'in_progress' && (
          <button onClick={e => { e.stopPropagation(); onStop(); }} className="p-1 text-zinc-500 hover:text-yellow-400 transition-colors" title="Stop">
            <Square size={10} />
          </button>
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
  task: PiTask;
  onClose: () => void;
  onStart: () => void;
  onStop: () => void;
  onDelete: () => void;
}

function TaskDetailPanel({ task, onClose, onStart, onStop, onDelete }: TaskDetailPanelProps) {
  return (
    <div className="border-t border-white/5 bg-zinc-950/80 flex flex-col max-h-[40%]">
      {/* Header */}
      <div className="flex items-center gap-2 px-4 py-2 border-b border-white/5 shrink-0">
        <StatusBadge status={task.status} size="md" />
        <span className="text-[11px] font-medium flex-1 truncate">{task.title}</span>
        <div className="flex items-center gap-1">
          {task.status === 'pending' && (
            <button onClick={onStart} className="flex items-center gap-1 px-2 py-1 text-[9px] font-mono uppercase tracking-widest bg-emerald-500/10 text-emerald-400 hover:bg-emerald-500/20 transition-colors">
              <Play size={10} /> Start
            </button>
          )}
          {task.status === 'in_progress' && (
            <button onClick={onStop} className="flex items-center gap-1 px-2 py-1 text-[9px] font-mono uppercase tracking-widest bg-yellow-500/10 text-yellow-400 hover:bg-yellow-500/20 transition-colors">
              <Square size={10} /> Stop
            </button>
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
          <div>
            <span className="text-[8px] font-mono uppercase text-zinc-600 tracking-widest">Created</span>
            <p className="text-[10px] font-mono text-zinc-300">{formatTime(task.createdAt)}</p>
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

        {/* Output */}
        {task.output && (
          <div>
            <span className="text-[8px] font-mono uppercase text-zinc-600 tracking-widest">Output</span>
            <pre className="mt-1 p-2 bg-zinc-900 border border-white/5 text-[9px] font-mono text-zinc-300 overflow-x-auto max-h-48 overflow-y-auto custom-scrollbar whitespace-pre-wrap">
              {task.output}
            </pre>
          </div>
        )}

        {/* Error */}
        {task.error && (
          <div>
            <span className="text-[8px] font-mono uppercase text-zinc-600 tracking-widest">Error</span>
            <div className="mt-1 p-2 bg-red-500/5 border border-red-500/20 flex items-start gap-2">
              <AlertCircle size={12} className="text-red-400 shrink-0 mt-0.5" />
              <pre className="text-[9px] font-mono text-red-400 whitespace-pre-wrap">{task.error}</pre>
            </div>
          </div>
        )}
      </div>
    </div>
  );
}

// ─── Create Task Form ─────────────────────────────────────────

interface CreateTaskFormProps {
  projectDir: string;
  onSubmit: (task: { title: string; description?: string; model?: string }) => Promise<void>;
  onCancel: () => void;
}

function CreateTaskForm({ projectDir, onSubmit, onCancel }: CreateTaskFormProps) {
  const [title, setTitle] = useState('');
  const [description, setDescription] = useState('');
  const [model, setModel] = useState('');
  const [submitting, setSubmitting] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!title) { setError('Title is required'); return; }
    setSubmitting(true);
    setError(null);
    try {
      await onSubmit({ title, description: description || undefined, model: model || undefined });
      setTitle('');
      setDescription('');
      setModel('');
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
      <input
        value={title}
        onChange={e => setTitle(e.target.value)}
        placeholder="Task title"
        className="w-full bg-zinc-900 border border-white/5 rounded px-2 py-1.5 text-[10px] outline-none focus:border-primary/40"
        autoFocus
      />
      <textarea
        value={description}
        onChange={e => setDescription(e.target.value)}
        placeholder="Description (optional)"
        rows={2}
        className="w-full bg-zinc-900 border border-white/5 rounded px-2 py-1.5 text-[10px] outline-none focus:border-primary/40 resize-none"
      />
      <div className="flex items-center gap-2">
        <input
          value={model}
          onChange={e => setModel(e.target.value)}
          placeholder="Model (optional)"
          className="flex-1 bg-zinc-900 border border-white/5 rounded px-2 py-1.5 text-[10px] outline-none focus:border-primary/40"
        />
        <button
          type="submit"
          disabled={submitting || !title}
          className="px-3 py-1.5 bg-primary text-black text-[9px] font-mono uppercase tracking-widest hover:bg-primary/80 disabled:opacity-30 transition-colors"
        >
          {submitting ? 'Creating...' : 'Create'}
        </button>
      </div>
    </form>
  );
}

// ─── Task Board Tab ───────────────────────────────────────────

interface TaskBoardTabProps {
  selectedProject: string | null;
}

export function TaskBoardTab({ selectedProject }: TaskBoardTabProps) {
  const { tasks, loading, error, groupedByStatus, createTask, updateTask, stopTask, deleteTask } = useTasks(selectedProject);
  const { addToast } = useToastContext();
  const [selectedTaskId, setSelectedTaskId] = useState<string | null>(null);
  const [showCreate, setShowCreate] = useState(false);

  const selectedTask = tasks.find(t => t.id === selectedTaskId) || null;

  const handleCreate = useCallback(async (data: { title: string; description?: string; model?: string }) => {
    const task = await createTask(data);
    addToast('success', `Task "${task.title}" created`);
    setShowCreate(false);
  }, [createTask, addToast]);

  const handleStart = useCallback(async (taskId: string) => {
    await updateTask(taskId, { status: 'in_progress' });
    addToast('info', 'Task started');
  }, [updateTask, addToast]);

  const handleStop = useCallback(async (taskId: string) => {
    await stopTask(taskId);
    addToast('info', 'Task stopped');
  }, [stopTask, addToast]);

  const handleDelete = useCallback(async (taskId: string) => {
    await deleteTask(taskId);
    if (selectedTaskId === taskId) setSelectedTaskId(null);
    addToast('success', 'Task deleted');
  }, [deleteTask, selectedTaskId, addToast]);

  if (!selectedProject) {
    return <EmptyState icon={<LayoutGrid size={32} />} title="Select a project to view tasks" />;
  }

  return (
    <div className="flex flex-col h-full">
      {/* Create form */}
      {showCreate && (
        <CreateTaskForm
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

      {/* Kanban board */}
      <div className="flex-1 overflow-hidden flex">
        {loading && tasks.length === 0 ? (
          <div className="flex-1 flex items-center justify-center">
            <Loader size={16} className="animate-spin text-zinc-600" />
          </div>
        ) : tasks.length === 0 ? (
          <EmptyState
            icon={<LayoutGrid size={32} />}
            title="No tasks yet"
            description="Create a task to track work for this project"
            action={{ label: '+ New Task', onClick: () => setShowCreate(true) }}
          />
        ) : (
          <div className="flex-1 flex gap-2 p-3 overflow-x-auto">
            {COLUMNS.map(col => (
              <div key={col.status} className="flex-1 min-w-[200px] flex flex-col">
                {/* Column header */}
                <div className="flex items-center gap-2 px-2 py-1.5 mb-2">
                  <StatusBadge status={col.status} label={col.label} size="md" />
                  <span className="text-[8px] font-mono text-zinc-700">{groupedByStatus[col.status].length}</span>
                </div>
                {/* Cards */}
                <div className="flex-1 overflow-y-auto custom-scrollbar space-y-1.5">
                  {groupedByStatus[col.status].map(task => (
                    <TaskCard
                      key={task.id}
                      task={task}
                      isSelected={selectedTaskId === task.id}
                      onSelect={() => setSelectedTaskId(task.id)}
                      onStart={() => handleStart(task.id)}
                      onStop={() => handleStop(task.id)}
                      onDelete={() => handleDelete(task.id)}
                    />
                  ))}
                </div>
              </div>
            ))}
          </div>
        )}
      </div>

      {/* New task button */}
      {!showCreate && (
        <div className="px-3 py-2 border-t border-white/5 shrink-0">
          <button
            onClick={() => setShowCreate(true)}
            className="flex items-center gap-1.5 px-3 py-1.5 text-[9px] font-mono uppercase tracking-widest bg-primary/10 text-primary hover:bg-primary/20 transition-colors"
          >
            <Plus size={10} /> New Task
          </button>
        </div>
      )}

      {/* Detail panel */}
      {selectedTask && (
        <TaskDetailPanel
          task={selectedTask}
          onClose={() => setSelectedTaskId(null)}
          onStart={() => handleStart(selectedTask.id)}
          onStop={() => handleStop(selectedTask.id)}
          onDelete={() => handleDelete(selectedTask.id)}
        />
      )}
    </div>
  );
}
