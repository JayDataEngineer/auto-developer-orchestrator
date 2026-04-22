import React, { useState } from 'react';
import { Shield, HelpCircle, Check, X, Send } from 'lucide-react';
import { PuxApprovalRequest } from '../../lib/pux-events';
import { Button } from '../ui/button';
import { Input } from '../ui/input';

interface ApprovalBannerProps {
  approval: PuxApprovalRequest;
  onRespond: (requestId: string, action: 'approve' | 'deny' | 'answer', message?: string) => void;
}

const riskColors: Record<string, string> = {
  low: 'text-emerald-400 bg-emerald-400/10',
  medium: 'text-amber-400 bg-amber-400/10',
  high: 'text-red-400 bg-red-400/10',
};

export const ApprovalBanner: React.FC<ApprovalBannerProps> = ({ approval, onRespond }) => {
  const [answer, setAnswer] = useState('');
  const isQuestion = approval.type === 'question';

  return (
    <div className={`w-full border-t ${
      isQuestion ? 'border-blue-500/30 bg-blue-500/5' : 'border-amber-500/30 bg-amber-500/5'
    }`}>
      <div className="mx-auto py-3 px-6 flex items-start gap-3">
        {/* Icon */}
        <div className={`shrink-0 w-7 h-7 flex items-center justify-center rounded-full ${
          isQuestion ? 'bg-blue-500/20 text-blue-400' : 'bg-amber-500/20 text-amber-400'
        }`}>
          {isQuestion ? <HelpCircle size={14} /> : <Shield size={14} />}
        </div>

        {/* Content */}
        <div className="flex-1 min-w-0">
          <div className="flex items-center gap-2 mb-1">
            <span className={`text-sm font-black uppercase tracking-widest ${
              isQuestion ? 'text-blue-400' : 'text-amber-400'
            }`}>
              {isQuestion ? 'Agent Asks' : 'Approval Required'}
            </span>
            {!isQuestion && (
              <span className={`text-xs font-mono font-bold uppercase px-1.5 py-0.5 ${riskColors[approval.risk]}`}>
                {approval.risk}
              </span>
            )}
            {approval.toolName && (
              <span className="text-xs font-mono text-muted-foreground bg-white/5 px-1.5 py-0.5">
                {approval.toolName}
              </span>
            )}
          </div>
          <p className="text-xs text-white/90 font-mono leading-relaxed">
            {approval.message}
          </p>
          {approval.toolArgs && approval.toolArgs.command && (
            <code className="block mt-1.5 text-sm font-mono text-muted-foreground bg-black/30 px-2 py-1 border border-white/5 break-all">
              {String(approval.toolArgs.command)}
            </code>
          )}
        </div>

        {/* Actions */}
        <div className="shrink-0 flex items-center gap-2">
          {isQuestion ? (
            <>
              <Input
                type="text"
                value={answer}
                onChange={(e) => setAnswer(e.target.value)}
                placeholder="Your answer..."
                className="w-48 placeholder:text-muted-foreground focus-visible:border-blue-500/40"
                onKeyDown={(e) => {
                  if (e.key === 'Enter' && answer.trim()) {
                    onRespond(approval.requestId, 'answer', answer.trim());
                    setAnswer('');
                  }
                }}
              />
              <Button
                variant="default"
                size="xs"
                onClick={() => { onRespond(approval.requestId, 'answer', answer.trim() || 'No answer provided'); setAnswer(''); }}
                disabled={!answer.trim()}
              >
                <Send size={9} /> Submit
              </Button>
            </>
          ) : (
            <>
              <Button
                variant="default"
                size="xs"
                onClick={() => onRespond(approval.requestId, 'approve')}
              >
                <Check size={9} /> Approve
              </Button>
              <Button
                variant="destructive"
                size="xs"
                onClick={() => onRespond(approval.requestId, 'deny', 'User denied this action')}
              >
                <X size={9} /> Deny
              </Button>
            </>
          )}
        </div>
      </div>
    </div>
  );
};
