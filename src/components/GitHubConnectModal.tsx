import React, { useState } from 'react';
import { Github, ShieldCheck, ExternalLink } from 'lucide-react';
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
  DialogDescription,
  DialogFooter,
} from './ui/dialog';
import { Input } from './ui/input';
import { Button } from './ui/button';

interface GitHubConnectModalProps {
  isOpen: boolean;
  onClose: () => void;
  onConnect: (token: string) => void;
}

export const GitHubConnectModal: React.FC<GitHubConnectModalProps> = ({ isOpen, onClose, onConnect }) => {
  const [token, setToken] = useState('');
  const [isConnecting, setIsConnecting] = useState(false);

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    if (token) {
      setIsConnecting(true);
      try {
        await onConnect(token);
        setToken('');
        onClose();
      } finally {
        setIsConnecting(false);
      }
    }
  };

  return (
    <Dialog open={isOpen} onOpenChange={(open) => !open && onClose()}>
      <DialogContent className="max-w-lg p-10">
        <div className="absolute top-0 left-0 w-full h-1 bg-primary" />

        <DialogHeader>
          <div className="flex items-center gap-4">
            <div className="p-3 bg-primary/10 border border-primary/20 text-primary">
              <Github size={24} />
            </div>
            <div>
              <DialogTitle className="text-xl font-black tracking-[0.2em] text-white uppercase italic">
                Connect GitHub
              </DialogTitle>
              <DialogDescription className="text-sm text-zinc-700 font-bold uppercase tracking-widest mt-1">
                Authorize Orchestrator for Real-World Ops
              </DialogDescription>
            </div>
          </div>
        </DialogHeader>

        <form onSubmit={handleSubmit} className="space-y-6 mt-6">
          <div className="space-y-2">
            <div className="flex justify-between items-end mb-1">
              <label className="text-sm font-bold uppercase tracking-[0.2em] text-zinc-500 ml-1">Personal Access Token (classic)</label>
              <a
                href="https://github.com/settings/tokens"
                target="_blank"
                rel="noopener noreferrer"
                className="text-sm text-primary hover:underline flex items-center gap-1 font-bold uppercase tracking-widest"
              >
                Generate <ExternalLink size={10} />
              </a>
            </div>
            <Input
              type="password"
              value={token}
              onChange={(e) => setToken(e.target.value)}
              placeholder="ghp_xxxxxxxxxxxxxxxxxxxx"
              className="w-full bg-black border border-border px-5 py-4 text-sm text-white placeholder:text-zinc-900 font-mono"
              autoFocus
            />
          </div>

          <div className="p-5 bg-zinc-950 border border-border flex gap-4 text-zinc-500 text-xs leading-relaxed">
            <ShieldCheck size={18} className="text-primary shrink-0" />
            <div>
              <p className="font-bold text-zinc-400 uppercase tracking-wider mb-1">Security Protocol</p>
              <p>Tokens are stored locally in your <code className="text-primary">.env</code> file. The orchestrator requires <code className="text-white">'repo'</code> scopes to clone, merge, and dispatch autonomously.</p>
            </div>
          </div>

          <DialogFooter>
            <Button
              type="submit"
              disabled={!token || isConnecting}
              className="w-full py-5 text-sm font-black uppercase tracking-[0.4em] shadow-lg glow-primary"
            >
              {isConnecting ? 'VERIFYING_AUTH...' : 'INITIALIZE_CONNECTION'}
            </Button>
          </DialogFooter>
        </form>

        <div className="mt-8 pt-8 border-t border-border/50 text-center">
          <p className="text-sm text-zinc-800 font-bold uppercase tracking-[0.3em]">
            SYS_AUTH_READY // WAITING_FOR_INPUT
          </p>
        </div>
      </DialogContent>
    </Dialog>
  );
};
