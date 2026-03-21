import React, { useState, useEffect } from 'react';
import { motion, AnimatePresence } from 'motion/react';
import { 
  Bot, 
  X, 
  ShieldCheck, 
  Zap, 
  FlaskConical, 
  Activity,
  Save,
  RefreshCw,
  Infinity,
  FolderOpen,
  HardDrive
} from 'lucide-react';

interface AIConfigModalProps {
  isOpen: boolean;
  onClose: () => void;
}

interface ToggleRowProps {
  icon: React.ReactNode;
  iconBg: string;
  iconColor: string;
  title: string;
  description: string;
  checked: boolean;
  onChange: () => void;
  activeColor?: string;
}

const ToggleRow: React.FC<ToggleRowProps> = ({ icon, iconBg, iconColor, title, description, checked, onChange, activeColor = 'bg-primary' }) => (
  <div className="flex items-center justify-between p-3 bg-white/5 rounded-xl border border-white/5">
    <div className="flex items-center gap-3">
      <div className={`w-8 h-8 rounded-lg ${iconBg} flex items-center justify-center ${iconColor}`}>
        {icon}
      </div>
      <div>
        <p className="text-xs font-bold">{title}</p>
        <p className="text-[10px] text-zinc-500">{description}</p>
      </div>
    </div>
    <button 
      onClick={onChange}
      className={`w-10 h-5 rounded-full transition-colors relative ${checked ? activeColor : 'bg-zinc-700'}`}
    >
      <div className={`absolute top-1 w-3 h-3 bg-white rounded-full transition-all ${checked ? 'left-6' : 'left-1'}`} />
    </button>
  </div>
);

export const AIConfigModal: React.FC<AIConfigModalProps> = ({ isOpen, onClose }) => {
  const [config, setConfig] = useState({
    autoTask: true,
    autoTest: true,
    fullAutomationMode: false,
    postMergeTestGen: false,
    testGenPrompt: 'Generate comprehensive tests for the recent changes, ensuring edge cases are covered.',
    testTypes: {
      unit: true,
      e2e: true,
      integration: false,
      chaos: false,
      security: false,
      performance: false
    }
  });

  const [systemConfig, setSystemConfig] = useState({
    projectsDir: ''
  });

  const [isTesting, setIsTesting] = useState(false);

  useEffect(() => {
    if (isOpen) {
      fetch('/api/config/ai')
        .then(res => res.json())
        .then(data => setConfig(data))
        .catch(err => console.error("Failed to fetch AI config", err));

      fetch('/api/config/system')
        .then(res => res.json())
        .then(data => setSystemConfig(data))
        .catch(err => console.error("Failed to fetch system config", err));
    }
  }, [isOpen]);

  const handleSave = async () => {
    // Save AI config
    await fetch('/api/config/ai', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(config)
    });

    // Save System config
    await fetch('/api/config/system', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(systemConfig)
    });

    onClose();
  };

  return (
    <AnimatePresence>
      {isOpen && (
        <div className="fixed inset-0 z-[100] flex items-center justify-center p-4 lg:p-6">
          <motion.div 
            initial={{ opacity: 0 }}
            animate={{ opacity: 1 }}
            exit={{ opacity: 0 }}
            onClick={onClose}
            className="absolute inset-0 bg-black/80 backdrop-blur-sm"
          />
          
          <motion.div 
            initial={{ opacity: 0, scale: 0.95, y: 20 }}
            animate={{ opacity: 1, scale: 1, y: 0 }}
            exit={{ opacity: 0, scale: 0.95, y: 20 }}
            className="relative w-full max-w-2xl bg-zinc-900 border border-white/10 rounded-3xl overflow-hidden shadow-2xl max-h-[90vh] flex flex-col"
          >
            {/* Header */}
            <div className="p-4 lg:p-6 border-b border-white/10 flex items-center justify-between bg-zinc-900/50 shrink-0">
              <div className="flex items-center gap-3">
                <div className="w-8 h-8 lg:w-10 lg:h-10 rounded-xl bg-primary/20 flex items-center justify-center text-primary">
                  <Bot size={18} />
                </div>
                <div>
                  <h2 className="text-sm lg:text-lg font-bold tracking-tight">Agent Intelligence</h2>
                  <p className="text-[10px] lg:text-xs text-zinc-500">Configure OpenAI-compatible LLMs & Auto-Testing</p>
                </div>
              </div>
              <button 
                onClick={onClose}
                className="text-zinc-500 hover:text-white transition-colors p-2 hover:bg-white/5 rounded-full"
              >
                <X size={18} />
              </button>
            </div>

            {/* Content */}
            <div className="p-4 lg:p-6 space-y-6 overflow-y-auto flex-1 terminal-scrollbar">
              {/* System Configuration */}
              <section className="space-y-4 pt-4 border-t border-white/5">
                <h3 className="text-[10px] font-bold uppercase tracking-widest text-zinc-500 flex items-center gap-2">
                  <HardDrive size={12} />
                  Local System Environment
                </h3>
                <div className="space-y-3">
                  <div className="p-3 bg-white/5 rounded-xl border border-white/5 space-y-2">
                    <div className="flex items-center gap-2 text-zinc-400 mb-1">
                      <FolderOpen size={14} />
                      <span className="text-[10px] font-bold uppercase tracking-widest">Projects Root Directory</span>
                    </div>
                    <div className="flex gap-2">
                      <input 
                        type="text"
                        value={systemConfig.projectsDir}
                        onChange={(e) => setSystemConfig({...systemConfig, projectsDir: e.target.value})}
                        className="flex-1 bg-black border border-white/10 rounded-lg px-3 py-2 text-xs text-zinc-300 focus:border-primary/50 outline-none"
                        placeholder="/path/to/your/github/repos"
                      />
                    </div>
                    <p className="text-[9px] text-zinc-500 italic">
                      Point JULES to the folder containing your local Git repositories.
                    </p>
                  </div>
                </div>
              </section>

              {/* Automation Toggles */}
              <section className="space-y-4 pt-4 border-t border-white/5">
                <h3 className="text-[10px] font-bold uppercase tracking-widest text-zinc-500 flex items-center gap-2">
                  <Zap size={12} />
                  Automation Behaviors
                </h3>
                <div className="space-y-3">
                  <ToggleRow 
                    icon={<Infinity size={16} />}
                    iconBg="bg-red-500/10"
                    iconColor="text-red-500"
                    title="Full Automation Mode"
                    description="Autonomous loop: Task → Issue → JULES → Test → Next"
                    checked={config.fullAutomationMode}
                    onChange={() => setConfig({...config, fullAutomationMode: !config.fullAutomationMode})}
                    activeColor="bg-red-500"
                  />
                  
                  <ToggleRow 
                    icon={<Activity size={16} />}
                    iconBg="bg-emerald-500/10"
                    iconColor="text-emerald-500"
                    title="Auto-Task Generation"
                    description="AI updates TODO_FOR_JULES.md based on context"
                    checked={config.autoTask}
                    onChange={() => setConfig({...config, autoTask: !config.autoTask})}
                  />

                  <ToggleRow 
                    icon={<ShieldCheck size={16} />}
                    iconBg="bg-blue-500/10"
                    iconColor="text-blue-500"
                    title="Post-Merge Auto-Testing"
                    description="Trigger test suites automatically after merge"
                    checked={config.autoTest}
                    onChange={() => setConfig({...config, autoTest: !config.autoTest})}
                  />

                  <ToggleRow 
                    icon={<Bot size={16} />}
                    iconBg="bg-purple-500/10"
                    iconColor="text-purple-500"
                    title="AI Test Generation"
                    description={`Use Gemini to generate new tests via prompt`}
                    checked={config.postMergeTestGen}
                    onChange={() => setConfig({...config, postMergeTestGen: !config.postMergeTestGen})}
                  />

                  <AnimatePresence>
                    {config.postMergeTestGen && (
                      <motion.div 
                        initial={{ height: 0, opacity: 0 }}
                        animate={{ height: 'auto', opacity: 1 }}
                        exit={{ height: 0, opacity: 0 }}
                        className="overflow-hidden"
                      >
                        <div className="p-3 bg-black/40 rounded-xl border border-white/5 space-y-2">
                          <label className="text-[10px] text-zinc-500 uppercase tracking-widest font-bold">Generation Prompt</label>
                          <textarea 
                            value={config.testGenPrompt}
                            onChange={(e) => setConfig({...config, testGenPrompt: e.target.value})}
                            className="w-full bg-black border border-white/10 rounded-lg p-3 text-xs text-zinc-300 focus:border-primary/50 outline-none min-h-[80px] resize-none"
                            placeholder="Tell the AI what kind of tests to generate..."
                          />
                        </div>
                      </motion.div>
                    )}
                  </AnimatePresence>
                </div>
              </section>

              {/* Test Suite Configuration */}
              <section className={`space-y-4 pt-4 border-t border-white/5 transition-opacity ${config.autoTest ? 'opacity-100' : 'opacity-40 pointer-events-none'}`}>
                <h3 className="text-[10px] font-bold uppercase tracking-widest text-zinc-500 flex items-center gap-2">
                  <FlaskConical size={12} />
                  Test Suite Selection
                </h3>
                <div className="grid grid-cols-2 gap-3">
                  {Object.entries(config.testTypes).map(([key, value]) => (
                    <button
                      key={key}
                      onClick={() => setConfig({
                        ...config, 
                        testTypes: { ...config.testTypes, [key]: !value }
                      })}
                      className={`flex items-center justify-between p-3 rounded-xl border transition-all ${
                        value ? 'bg-primary/10 border-primary/30 text-primary' : 'bg-black border-white/5 text-zinc-500'
                      }`}
                    >
                      <span className="text-[10px] font-bold uppercase tracking-wider">{key} Tests</span>
                      <div className={`w-2 h-2 rounded-full ${value ? 'bg-primary shadow-[0_0_8px_rgba(0,255,157,0.5)]' : 'bg-zinc-800'}`} />
                    </button>
                  ))}
                </div>
              </section>
            </div>

            {/* Footer */}
            <div className="p-4 lg:p-6 bg-zinc-950 flex flex-col sm:flex-row gap-3 shrink-0">
              <button 
                onClick={() => {
                  setIsTesting(true);
                  setTimeout(() => setIsTesting(false), 2000);
                }}
                disabled={isTesting}
                className="flex-1 bg-zinc-800 hover:bg-zinc-700 text-white font-bold py-3 rounded-xl text-[10px] lg:text-xs uppercase tracking-widest transition-all flex items-center justify-center gap-2"
              >
                {isTesting ? <RefreshCw size={14} className="animate-spin" /> : <Activity size={14} />}
                Test Connection
              </button>
              <button 
                onClick={handleSave}
                className="flex-1 bg-primary text-black font-bold py-3 rounded-xl text-[10px] lg:text-xs uppercase tracking-widest hover:bg-primary/90 transition-all flex items-center justify-center gap-2 shadow-lg shadow-primary/20"
              >
                <Save size={14} />
                Save Configuration
              </button>
            </div>
          </motion.div>
        </div>
      )}
    </AnimatePresence>
  );
};
