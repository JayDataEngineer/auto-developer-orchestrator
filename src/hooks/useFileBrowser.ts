import { useState, useCallback, useRef } from 'react';
import { api, FileEntry } from '../lib/api';

interface TreeNode {
  name: string;
  path: string;
  isDir: boolean;
  size?: number;
  expanded?: boolean;
  children?: TreeNode[];
  loaded?: boolean;
}

interface SelectedFile {
  path: string;
  content: string;
}

interface UseFileBrowserReturn {
  tree: TreeNode[];
  selectedFile: SelectedFile | null;
  loading: boolean;
  error: string | null;
  expandDir: (path: string) => Promise<void>;
  selectFile: (path: string) => Promise<void>;
  refresh: () => Promise<void>;
  setRootPath: (path: string) => void;
}

function insertChildren(tree: TreeNode[], parentPath: string, children: TreeNode[]): TreeNode[] {
  return tree.map(node => {
    if (node.path === parentPath) {
      return { ...node, children, expanded: true, loaded: true };
    }
    if (node.children) {
      return { ...node, children: insertChildren(node.children, parentPath, children) };
    }
    return node;
  });
}

function toggleExpanded(tree: TreeNode[], path: string): TreeNode[] {
  return tree.map(node => {
    if (node.path === path) {
      return { ...node, expanded: !node.expanded };
    }
    if (node.children) {
      return { ...node, children: toggleExpanded(node.children, path) };
    }
    return node;
  });
}

function entriesToNodes(entries: FileEntry[], parentPath: string): TreeNode[] {
  return entries
    .sort((a, b) => {
      // Directories first, then alphabetical
      if (a.is_dir !== b.is_dir) return a.is_dir ? -1 : 1;
      return a.name.localeCompare(b.name);
    })
    .map(entry => {
      const path = parentPath === '/' ? `/${entry.name}` : `${parentPath}/${entry.name}`;
      return {
        name: entry.name,
        path,
        isDir: entry.is_dir,
        size: entry.size,
        expanded: false,
        children: entry.is_dir ? [] : undefined,
        loaded: false,
      };
    });
}

export function useFileBrowser(initialRootPath?: string): UseFileBrowserReturn {
  const [tree, setTree] = useState<TreeNode[]>([]);
  const [selectedFile, setSelectedFile] = useState<SelectedFile | null>(null);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const rootPathRef = useRef(initialRootPath || '/');

  const setRootPath = useCallback((path: string) => {
    rootPathRef.current = path;
    setTree([]);
    setSelectedFile(null);
    // Auto-load root
    loadRoot(path);
  }, []);

  const loadRoot = useCallback(async (rootPath?: string) => {
    const path = rootPath || rootPathRef.current;
    if (!path) return;
    try {
      setLoading(true);
      setError(null);
      const res = await api.cli.ls(path);
      const children = entriesToNodes(res.entries || [], path);
      setTree(children);
    } catch (err) {
      setError(String(err));
    } finally {
      setLoading(false);
    }
  }, []);

  const expandDir = useCallback(async (path: string) => {
    // Check if already loaded — just toggle
    setTree(prev => {
      const findAndCheck = (nodes: TreeNode[]): boolean => {
        for (const node of nodes) {
          if (node.path === path) return node.loaded || false;
          if (node.children && findAndCheck(node.children)) return true;
        }
        return false;
      };
      const isLoaded = findAndCheck(prev);
      if (isLoaded) {
        return toggleExpanded(prev, path);
      }
      return prev;
    });

    // If not loaded, fetch from API
    try {
      setError(null);
      const res = await api.cli.ls(path);
      const children = entriesToNodes(res.entries || [], path);
      setTree(prev => insertChildren(prev, path, children));
    } catch (err) {
      setError(String(err));
    }
  }, []);

  const selectFile = useCallback(async (path: string) => {
    try {
      setError(null);
      const res = await api.cli.read(path);
      setSelectedFile({ path, content: res.content });
    } catch (err) {
      setSelectedFile({ path, content: `Error reading file: ${err}` });
    }
  }, []);

  const refresh = useCallback(async () => {
    setSelectedFile(null);
    await loadRoot();
  }, [loadRoot]);

  return {
    tree,
    selectedFile,
    loading,
    error,
    expandDir,
    selectFile,
    refresh,
    setRootPath,
  };
}
