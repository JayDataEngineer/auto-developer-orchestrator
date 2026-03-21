import React, { useState } from 'react';
import { motion, AnimatePresence } from 'motion/react';
import { X, BarChart3, CheckCircle2, AlertCircle, Zap, History, TrendingUp, TrendingDown } from 'lucide-react';
import { cn } from '../lib/utils';

interface CoverageReportModalProps {
  isOpen: boolean;
  onClose: () => void;
}

const COVERAGE_DATA = [
  { module: 'src/api/routes', coverage: 94, tests: 12, status: 'passed' },
  { module: 'src/services/ai', coverage: 88, tests: 8, status: 'passed' },
  { module: 'src/components/ui', coverage: 100, tests: 24, status: 'passed' },
  { module: 'src/lib/utils', coverage: 76, tests: 5, status: 'warning' },
  { module: 'src/hooks', coverage: 91, tests: 10, status: 'passed' },
];

const HISTORY_DATA = [
  { date: '2026-03-21 10:42', coverage: 91.2, tests: 59, change: '+0.4%', trend: 'up' },
  { date: '2026-03-21 09:15', coverage: 90.8, tests: 55, change: '+1.2%', trend: 'up' },
  { date: '2026-03-20 18:30', coverage: 89.6, tests: 52, change: '-0.2%', trend: 'down' },
  { date: '2026-03-20 14:20', coverage: 89.8, tests: 52, change: '+2.5%', trend: 'up' },
  { date: '2026-03-19 11:05', coverage: 87.3, tests: 48, change: '0.0%', trend: 'neutral' },
];

export const CoverageReportModal: React.FC<CoverageReportModalProps> = ({ isOpen, onClose }) => {
  const [activeTab, setActiveTab] = useState<'breakdown' | 'history'>('breakdown');

  return (
    <AnimatePresence>
      {isOpen && (
        <div className="fixed inset-0 z-50 flex items-center justify-center p-4 bg-black/80 backdrop-blur-sm">
          <motion.div
            initial={{ opacity: 0, scale: 0.95, y: 20 }}
            animate={{ opacity: 1, scale: 1, y: 0 }}
            exit={{ opacity: 0, scale: 0.95, y: 20 }}
            className="w-full max-w-2xl bg-zinc-900 border border-white/10 rounded-3xl overflow-hidden shadow-2xl"
          >
            <div className="p-6 border-b border-white/10 flex items-center justify-between bg-zinc-900/50">
              <div className="flex items-center gap-3">
                <div className="p-2 bg-primary/10 rounded-xl">
                  <BarChart3 className="text-primary" size={20} />
                </div>
                <div>
                  <h2 className="text-lg font-semibold text-white">Coverage Report</h2>
                  <p className="text-xs text-zinc-500">System-wide test coverage analysis</p>
                </div>
              </div>
              <button
                onClick={onClose}
                className="p-2 hover:bg-white/5 rounded-full transition-colors text-zinc-400 hover:text-white"
              >
                <X size={20} />
              </button>
            </div>

            <div className="p-6 space-y-6">
              <div className="flex p-1 bg-black/40 rounded-xl border border-white/5 w-fit">
                <button
                  onClick={() => setActiveTab('breakdown')}
                  className={cn(
                    "px-4 py-1.5 rounded-lg text-xs font-medium transition-all flex items-center gap-2",
                    activeTab === 'breakdown' ? "bg-zinc-800 text-white shadow-lg" : "text-zinc-500 hover:text-zinc-300"
                  )}
                >
                  <BarChart3 size={14} />
                  Breakdown
                </button>
                <button
                  onClick={() => setActiveTab('history')}
                  className={cn(
                    "px-4 py-1.5 rounded-lg text-xs font-medium transition-all flex items-center gap-2",
                    activeTab === 'history' ? "bg-zinc-800 text-white shadow-lg" : "text-zinc-500 hover:text-zinc-300"
                  )}
                >
                  <History size={14} />
                  History
                </button>
              </div>

              <div className="grid grid-cols-3 gap-4">
                <div className="p-4 bg-white/5 rounded-2xl border border-white/5">
                  <p className="text-[10px] uppercase tracking-wider text-zinc-500 mb-1">Total Coverage</p>
                  <p className="text-2xl font-bold text-white">91.2%</p>
                  <div className="mt-2 h-1 w-full bg-zinc-800 rounded-full overflow-hidden">
                    <div className="h-full bg-primary w-[91.2%]" />
                  </div>
                </div>
                <div className="p-4 bg-white/5 rounded-2xl border border-white/5">
                  <p className="text-[10px] uppercase tracking-wider text-zinc-500 mb-1">Total Tests</p>
                  <p className="text-2xl font-bold text-white">59</p>
                  <div className="mt-2 flex items-center gap-1 text-[10px] text-emerald-400">
                    <CheckCircle2 size={10} />
                    <span>All passing</span>
                  </div>
                </div>
                <div className="p-4 bg-white/5 rounded-2xl border border-white/5">
                  <p className="text-[10px] uppercase tracking-wider text-zinc-500 mb-1">Avg. Duration</p>
                  <p className="text-2xl font-bold text-white">248ms</p>
                  <div className="mt-2 flex items-center gap-1 text-[10px] text-primary">
                    <Zap size={10} />
                    <span>Optimized</span>
                  </div>
                </div>
              </div>

              <AnimatePresence mode="wait">
                {activeTab === 'breakdown' ? (
                  <motion.div
                    key="breakdown"
                    initial={{ opacity: 0, x: -10 }}
                    animate={{ opacity: 1, x: 0 }}
                    exit={{ opacity: 0, x: 10 }}
                    className="space-y-3"
                  >
                    <h3 className="text-xs font-medium text-zinc-400 uppercase tracking-wider">Module Breakdown</h3>
                    <div className="space-y-2">
                      {COVERAGE_DATA.map((item, i) => (
                        <div key={i} className="p-3 bg-white/5 rounded-xl border border-white/5 flex items-center justify-between group hover:bg-white/10 transition-colors">
                          <div className="flex items-center gap-3">
                            {item.status === 'passed' ? (
                              <CheckCircle2 size={16} className="text-emerald-400" />
                            ) : (
                              <AlertCircle size={16} className="text-amber-400" />
                            )}
                            <span className="text-sm font-medium text-zinc-200">{item.module}</span>
                          </div>
                          <div className="flex items-center gap-6">
                            <div className="text-right">
                              <p className="text-xs text-zinc-500">Tests</p>
                              <p className="text-sm font-mono text-zinc-300">{item.tests}</p>
                            </div>
                            <div className="w-24 text-right">
                              <p className="text-xs text-zinc-500">Coverage</p>
                              <p className={`text-sm font-mono ${item.coverage > 90 ? 'text-emerald-400' : item.coverage > 80 ? 'text-primary' : 'text-amber-400'}`}>
                                {item.coverage}%
                              </p>
                            </div>
                          </div>
                        </div>
                      ))}
                    </div>
                  </motion.div>
                ) : (
                  <motion.div
                    key="history"
                    initial={{ opacity: 0, x: -10 }}
                    animate={{ opacity: 1, x: 0 }}
                    exit={{ opacity: 0, x: 10 }}
                    className="space-y-3"
                  >
                    <h3 className="text-xs font-medium text-zinc-400 uppercase tracking-wider">Coverage History</h3>
                    <div className="space-y-2">
                      {HISTORY_DATA.map((item, i) => (
                        <div key={i} className="p-3 bg-white/5 rounded-xl border border-white/5 flex items-center justify-between group hover:bg-white/10 transition-colors">
                          <div className="flex flex-col">
                            <span className="text-xs text-zinc-500">{item.date}</span>
                            <span className="text-sm font-medium text-zinc-200">CI Build #{1024 - i}</span>
                          </div>
                          <div className="flex items-center gap-8">
                            <div className="text-right">
                              <p className="text-xs text-zinc-500">Tests</p>
                              <p className="text-sm font-mono text-zinc-300">{item.tests}</p>
                            </div>
                            <div className="flex items-center gap-3">
                              <div className="text-right">
                                <p className="text-xs text-zinc-500">Coverage</p>
                                <p className="text-sm font-mono text-white">{item.coverage}%</p>
                              </div>
                              <div className={cn(
                                "flex items-center gap-1 px-2 py-0.5 rounded text-[10px] font-bold",
                                item.trend === 'up' ? "bg-emerald-500/10 text-emerald-400" : 
                                item.trend === 'down' ? "bg-rose-500/10 text-rose-400" : 
                                "bg-zinc-500/10 text-zinc-400"
                              )}>
                                {item.trend === 'up' && <TrendingUp size={10} />}
                                {item.trend === 'down' && <TrendingDown size={10} />}
                                {item.change}
                              </div>
                            </div>
                          </div>
                        </div>
                      ))}
                    </div>
                  </motion.div>
                )}
              </AnimatePresence>
            </div>

            <div className="p-6 bg-zinc-900/50 border-t border-white/10 flex justify-end">
              <button
                onClick={onClose}
                className="px-6 py-2 bg-white text-black text-sm font-semibold rounded-xl hover:bg-zinc-200 transition-colors"
              >
                Done
              </button>
            </div>
          </motion.div>
        </div>
      )}
    </AnimatePresence>
  );
};
