import React, { useState, useCallback, memo } from 'react';
import {
  Send, Square, ChevronDown, Trash2,
  RotateCcw, GitBranch, GitPullRequest
} from 'lucide-react';
import { cn } from '../lib/utils';
import { PuxModel } from '../lib/pux-events';
import { Button } from './ui/button';
import { Textarea } from './ui/textarea';
import { Tooltip, TooltipContent, TooltipTrigger } from './ui/tooltip';
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from './ui/dropdown-menu';

export interface InputBarProps {
  isStreaming: boolean;
  disabled: boolean;
  model: string | null;
  models: PuxModel[];
  selectedProject?: string;
  selectedAgentId: string;
  isZenMode: boolean;
  autoBranch: boolean;
  autoMerge: boolean;
  toolModel: string | null;
  onSend: (text: string) => void;
  onAbort: () => void;
  onReset: () => void;
  onSwitchModel: (provider: string, modelId: string) => void;
  onCompact: () => void;
  onAutoBranchChange: (v: boolean) => void;
  onAutoMergeChange: (v: boolean) => void;
  onSetToolModel: (provider: string, modelId: string) => void;
}

export const InputBar = memo(function InputBar({
  isStreaming, disabled, model, models, selectedProject, selectedAgentId,
  isZenMode, autoBranch, autoMerge, toolModel,
  onSend, onAbort, onReset, onSwitchModel, onCompact,
  onAutoBranchChange, onAutoMergeChange, onSetToolModel,
}: InputBarProps) {
  const [input, setInput] = useState('');

  const handleSend = useCallback(() => {
    if (!input.trim() || isStreaming) return;
    onSend(input.trim());
    setInput('');
  }, [input, isStreaming, onSend]);

  const handleKeyDown = useCallback((e: React.KeyboardEvent) => {
    if (e.key === 'Enter' && !e.shiftKey) {
      e.preventDefault();
      handleSend();
    }
  }, [handleSend]);

  return (
    <div className="w-full border-t border-border bg-background/80 backdrop-blur-md">
      <div className={cn(
        "mx-auto p-4 transition-all duration-500",
        isZenMode ? "max-w-7xl" : "max-w-3xl"
      )}>
        <div className="flex gap-2">
          <div className="flex-1 relative">
            <Textarea
              value={input}
              onChange={(e) => setInput(e.target.value)}
              onKeyDown={handleKeyDown}
              placeholder={selectedProject ? "Tell Pux what you want to build or automate..." : "Select a project to get started..."}
              disabled={disabled}
              data-prompt-input
              className="w-full border-border rounded-lg p-4 pr-14 text-base text-foreground placeholder-muted-foreground/60 resize-none bg-card"
              rows={4}
            />
            <div className="absolute right-3 bottom-3 flex items-center gap-2">
              {isStreaming ? (
                <Button variant="destructive" size="xs" onClick={onAbort}>
                  <Square size={16} />
                </Button>
              ) : (
                <Button variant="default" size="xs" onClick={handleSend} disabled={!input.trim() || disabled}>
                  <Send size={16} />
                </Button>
              )}
            </div>
          </div>
        </div>
        <div className="flex items-center gap-4 mt-2">
          <div className="flex items-center gap-3">
            <DropdownMenu>
              <DropdownMenuTrigger asChild>
                <Button variant="ghost" size="xs" className="text-muted hover:text-muted-foreground">
                  Main: {model || 'default'} <ChevronDown size={10} />
                </Button>
              </DropdownMenuTrigger>
              <DropdownMenuContent align="start" className="w-64 max-h-[300px] overflow-y-auto">
                <DropdownMenuLabel>Main Model (conversation)</DropdownMenuLabel>
                <DropdownMenuSeparator />
                {models.length === 0 && (
                  <div className="px-2 py-2 text-xs text-muted-foreground">Loading models...</div>
                )}
                {models.map(m => (
                  <DropdownMenuItem
                    key={m.id}
                    onClick={() => onSwitchModel(m.provider || 'llamacpp', m.id)}
                    className={model === m.id ? 'text-primary' : ''}
                  >
                    {m.name || m.id}
                  </DropdownMenuItem>
                ))}
              </DropdownMenuContent>
            </DropdownMenu>
            <DropdownMenu>
              <DropdownMenuTrigger asChild>
                <Button variant="ghost" size="xs" className="text-muted hover:text-muted-foreground">
                  Tool: {toolModel || 'default'} <ChevronDown size={10} />
                </Button>
              </DropdownMenuTrigger>
              <DropdownMenuContent align="start" className="w-64 max-h-[300px] overflow-y-auto">
                <DropdownMenuLabel>Tool Model (sub-agents/vision)</DropdownMenuLabel>
                <DropdownMenuSeparator />
                {models.length === 0 && (
                  <div className="px-2 py-2 text-xs text-muted-foreground">Loading models...</div>
                )}
                {models.map(m => (
                  <DropdownMenuItem
                    key={m.id}
                    onClick={() => onSetToolModel(m.provider || 'llamacpp', m.id)}
                    className={toolModel === m.id ? 'text-primary' : ''}
                  >
                    {m.name || m.id}
                  </DropdownMenuItem>
                ))}
              </DropdownMenuContent>
            </DropdownMenu>
          </div>

          <Tooltip>
            <TooltipTrigger asChild>
              <Button variant="ghost" size="xs" onClick={onReset}>
                <RotateCcw size={10} /> New Task
              </Button>
            </TooltipTrigger>
            <TooltipContent>New Task <span className="kbd ml-1">Ctrl+Shift+N</span></TooltipContent>
          </Tooltip>

          <Tooltip>
            <TooltipTrigger asChild>
              <Button
                variant="ghost"
                size="xs"
                onClick={() => { onAutoBranchChange(!autoBranch); if (!autoBranch) onAutoMergeChange(false); }}
                className={autoBranch ? 'text-primary' : ''}
              >
                <GitBranch size={10} /> Auto-Branch
              </Button>
            </TooltipTrigger>
            <TooltipContent>Auto-Branch <span className="kbd ml-1">Ctrl+Shift+B</span></TooltipContent>
          </Tooltip>

          {autoBranch && (
            <Tooltip>
              <TooltipTrigger asChild>
                <Button
                  variant="ghost"
                  size="xs"
                  onClick={() => onAutoMergeChange(!autoMerge)}
                  className={autoMerge ? 'text-primary' : ''}
                >
                  <GitPullRequest size={10} /> Auto-Merge
                </Button>
              </TooltipTrigger>
              <TooltipContent>Auto-Merge <span className="kbd ml-1">Ctrl+Shift+M</span></TooltipContent>
            </Tooltip>
          )}

          <Tooltip>
            <TooltipTrigger asChild>
              <Button variant="ghost" size="xs" onClick={onCompact} disabled={isStreaming}>
                <Trash2 size={10} /> Compact
              </Button>
            </TooltipTrigger>
            <TooltipContent>Compact <span className="kbd ml-1">Ctrl+Shift+C</span></TooltipContent>
          </Tooltip>
        </div>
      </div>
    </div>
  );
});
