import React, { useEffect } from 'react';
import { cn } from '../lib/utils';
import { useFileBrowser } from '../hooks/useFileBrowser';
import { EmptyState } from './ui/EmptyState';
import {
  Folder, FolderOpen, File, ChevronRight, ChevronDown,
  RefreshCw, Loader, FolderTree
} from 'lucide-react';

// ─── Tree Node ────────────────────────────────────────────────

interface TreeNodeRenderProps {
  node: {
    name: string;
    path: string;
    isDir: boolean;
    expanded?: boolean;
    children?: any[];
    loaded?: boolean;
  };
  depth: number;
  selectedPath: string | null;
  onExpand: (path: string) => void;
  onSelect: (path: string) => void;
}

function TreeNode({ node, depth, selectedPath, onExpand, onSelect }: TreeNodeRenderProps) {
  const indent = depth * 16;
  const isSelected = selectedPath === node.path;

  return (
    <>
      <div
        className={cn(
          'flex items-center gap-1 py-1 px-2 cursor-pointer hover:bg-white/[0.03] transition-colors',
          isSelected && 'bg-primary/5'
        )}
        style={{ paddingLeft: `${indent + 8}px` }}
        onClick={() => node.isDir ? onExpand(node.path) : onSelect(node.path)}
      >
        {/* Expand chevron for directories */}
        {node.isDir ? (
          node.expanded ? <ChevronDown size={10} className="text-zinc-500 shrink-0" /> : <ChevronRight size={10} className="text-zinc-500 shrink-0" />
        ) : (
          <span className="w-[10px] shrink-0" />
        )}
        {/* Icon */}
        {node.isDir ? (
          node.expanded ? <FolderOpen size={12} className="text-primary/60 shrink-0" /> : <Folder size={12} className="text-zinc-500 shrink-0" />
        ) : (
          <File size={12} className="text-zinc-600 shrink-0" />
        )}
        {/* Name */}
        <span className={cn(
          'text-sm font-mono truncate',
          node.isDir ? 'text-zinc-300' : isSelected ? 'text-primary' : 'text-zinc-400'
        )}>
          {node.name}
        </span>
      </div>
      {/* Children */}
      {node.isDir && node.expanded && node.children?.map(child => (
        <TreeNode
          key={child.path}
          node={child}
          depth={depth + 1}
          selectedPath={selectedPath}
          onExpand={onExpand}
          onSelect={onSelect}
        />
      ))}
    </>
  );
}

// ─── File Browser Panel ───────────────────────────────────────

interface FileBrowserPanelProps {
  rootPath?: string;
  className?: string;
}

export function FileBrowserPanel({ rootPath, className }: FileBrowserPanelProps) {
  const { tree, selectedFile, loading, error, expandDir, selectFile, refresh, setRootPath } = useFileBrowser(rootPath);

  // Set root path when prop changes
  useEffect(() => {
    if (rootPath) setRootPath(rootPath);
  }, [rootPath, setRootPath]);

  if (!rootPath) {
    return (
      <EmptyState
        icon={<FolderTree size={32} />}
        title="Select a project to browse files"
        className={className}
      />
    );
  }

  return (
    <div className={cn('flex flex-col h-full', className)}>
      {/* Header */}
      <div className="flex items-center gap-2 px-3 py-2 border-b border-white/5 shrink-0">
        <FolderTree size={12} className="text-primary" />
        <span className="text-xs font-mono uppercase tracking-widest text-zinc-500 flex-1">Files</span>
        <button onClick={refresh} className="p-1 text-zinc-500 hover:text-zinc-300 transition-colors">
          <RefreshCw size={10} />
        </button>
      </div>

      {/* Error */}
      {error && (
        <div className="px-3 py-1.5 text-xs text-red-400 font-mono">{error}</div>
      )}

      {/* Tree */}
      <div className="flex-1 overflow-y-auto custom-scrollbar">
        {loading && tree.length === 0 ? (
          <div className="flex items-center justify-center h-20">
            <Loader size={14} className="animate-spin text-zinc-600" />
          </div>
        ) : (
          tree.map(node => (
            <TreeNode
              key={node.path}
              node={node}
              depth={0}
              selectedPath={selectedFile?.path || null}
              onExpand={expandDir}
              onSelect={selectFile}
            />
          ))
        )}
      </div>

      {/* File content viewer */}
      {selectedFile && (
        <div className="border-t border-white/5 flex flex-col max-h-[50%]">
          <div className="px-3 py-1.5 border-b border-white/5 flex items-center gap-2 shrink-0">
            <File size={10} className="text-zinc-500" />
            <span className="text-xs font-mono text-zinc-400 truncate">{selectedFile.path}</span>
          </div>
          <pre className="flex-1 p-3 text-xs font-mono text-zinc-300 overflow-auto custom-scrollbar whitespace-pre-wrap bg-zinc-950/50">
            {selectedFile.content}
          </pre>
        </div>
      )}
    </div>
  );
}
