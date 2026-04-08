import { useState, useCallback, useRef, useEffect, useMemo } from 'react';
import { api, PiTask } from '../lib/api';

interface UseTasksReturn {
  tasks: PiTask[];
  loading: boolean;
  error: string | null;
  fetchTasks: () => Promise<void>;
  createTask: (task: { title: string; description?: string; model?: string; blocks?: string[]; blockedBy?: string[] }) => Promise<PiTask>;
  updateTask: (taskId: string, updates: Partial<Pick<PiTask, 'title' | 'description' | 'status' | 'model'>>) => Promise<void>;
  stopTask: (taskId: string) => Promise<void>;
  deleteTask: (taskId: string) => Promise<void>;
  setDependencies: (taskId: string, blocks?: string[], blockedBy?: string[]) => Promise<void>;
  groupedByStatus: Record<PiTask['status'], PiTask[]>;
}

export function useTasks(projectDir: string | null, parentAgent?: string): UseTasksReturn {
  const [tasks, setTasks] = useState<PiTask[]>([]);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const intervalRef = useRef<ReturnType<typeof setInterval>>(undefined);

  const fetchTasks = useCallback(async () => {
    if (!projectDir) return;
    try {
      setLoading(true);
      const res = await api.tasks.list(projectDir, parentAgent);
      setTasks(res.tasks || []);
      setError(null);
    } catch (err) {
      setError(String(err));
    } finally {
      setLoading(false);
    }
  }, [projectDir, parentAgent]);

  useEffect(() => {
    if (!projectDir) return;
    fetchTasks();
    intervalRef.current = setInterval(fetchTasks, 5000);
    return () => {
      if (intervalRef.current) clearInterval(intervalRef.current);
    };
  }, [projectDir, fetchTasks]);

  const createTask = useCallback(async (task: { title: string; description?: string; model?: string; blocks?: string[]; blockedBy?: string[] }) => {
    if (!projectDir) throw new Error('No project selected');
    const created = await api.tasks.create({
      ...task,
      projectDir,
      parentAgent: parentAgent || 'default',
    });
    setTasks(prev => [...prev, created]);
    return created;
  }, [projectDir, parentAgent]);

  const updateTask = useCallback(async (taskId: string, updates: Partial<Pick<PiTask, 'title' | 'description' | 'status' | 'model'>>) => {
    const updated = await api.tasks.update(taskId, updates);
    setTasks(prev => prev.map(t => t.id === taskId ? updated : t));
  }, []);

  const stopTask = useCallback(async (taskId: string) => {
    await api.tasks.stop(taskId);
    await fetchTasks();
  }, [fetchTasks]);

  const deleteTask = useCallback(async (taskId: string) => {
    await api.tasks.delete(taskId);
    setTasks(prev => prev.filter(t => t.id !== taskId));
  }, []);

  const setDependencies = useCallback(async (taskId: string, blocks?: string[], blockedBy?: string[]) => {
    await api.tasks.setDeps(taskId, blocks, blockedBy);
    await fetchTasks();
  }, [fetchTasks]);

  const groupedByStatus = useMemo(() => {
    const groups: Record<PiTask['status'], PiTask[]> = {
      pending: [],
      in_progress: [],
      completed: [],
      failed: [],
    };
    for (const task of tasks) {
      groups[task.status].push(task);
    }
    return groups;
  }, [tasks]);

  return {
    tasks,
    loading,
    error,
    fetchTasks,
    createTask,
    updateTask,
    stopTask,
    deleteTask,
    setDependencies,
    groupedByStatus,
  };
}
