import React, { useState } from 'react';
import { Shield, HelpCircle, Check, X, Send } from 'lucide-react';
import { PiApprovalRequest } from '../../lib/pi-events';

interface ApprovalBannerProps {
  approval: PiApprovalRequest;
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
            <span className={`text-[10px] font-black uppercase tracking-widest ${
              isQuestion ? 'text-blue-400' : 'text-amber-400'
            }`}>
              {isQuestion ? 'Agent Asks' : 'Approval Required'}
            </span>
            {!isQuestion && (
              <span className={`text-[8px] font-mono font-bold uppercase px-1.5 py-0.5 ${riskColors[approval.risk]}`}>
                {approval.risk}
              </span>
            )}
            {approval.toolName && (
              <span className="text-[8px] font-mono text-muted-foreground bg-white/5 px-1.5 py-0.5">
                {approval.toolName}
              </span>
            )}
          </div>
          <p className="text-[11px] text-white/90 font-mono leading-relaxed">
            {approval.message}
          </p>
          {approval.toolArgs && approval.toolArgs.command && (
            <code className="block mt-1.5 text-[10px] font-mono text-muted-foreground bg-black/30 px-2 py-1 border border-white/5 break-all">
              {String(approval.toolArgs.command)}
            </code>
          )}
        </div>

        {/* Actions */}
        <div className="shrink-0 flex items-center gap-2">
          {isQuestion ? (
            <>
              <input
                type="text"
                value={answer}
                onChange={(e) => setAnswer(e.target.value)}
                placeholder="Your answer..."
                className="w-48 bg-zinc-900 border border-white/10 px-3 py-1.5 text-[10px] text-white font-mono placeholder-zinc-600 outline-none focus:border-blue-500/40"
                onKeyDown={(e) => {
                  if (e.key === 'Enter' && answer.trim()) {
                    onRespond(approval.requestId, 'answer', answer.trim());
                    setAnswer('');
                  }
                }}
              />
              <button
                onClick={() => { onRespond(approval.requestId, 'answer', answer.trim() || 'No answer provided'); setAnswer(''); }}
                disabled={!answer.trim()}
                className="flex items-center gap-1 px-3 py-1.5 bg-blue-500 text-black text-[9px] font-black uppercase tracking-widest hover:bg-blue-400 disabled:opacity-30 transition-colors"
              >
                <Send size={9} /> Submit
              </button>
            </>
          ) : (
            <>
              <button
                onClick={() => onRespond(approval.requestId, 'approve')}
                className="flex items-center gap-1 px-3 py-1.5 bg-primary text-black text-[9px] font-black uppercase tracking-widest hover:bg-primary/80 transition-colors"
              >
                <Check size={9} /> Approve
              </button>
              <button
                onClick={() => onRespond(approval.requestId, 'deny', 'User denied this action')}
                className="flex items-center gap-1 px-3 py-1.5 bg-red-500/20 text-red-400 text-[9px] font-black uppercase tracking-widest hover:bg-red-500/30 transition-colors border border-red-500/30"
              >
                <X size={9} /> Deny
              </button>
            </>
          )}
        </div>
      </div>
    </div>
  );
};
