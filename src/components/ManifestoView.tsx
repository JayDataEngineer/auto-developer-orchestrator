import React from 'react';
import { motion } from 'motion/react';
import { Terminal, Shield, Zap, Globe, Cpu, Layers, Database, Lock } from 'lucide-react';

export const ManifestoView: React.FC = () => {
  const principles = [
    {
      icon: <Zap className="text-primary" size={20} />,
      title: "EXTREME VELOCITY",
      text: "Developer friction is the enemy. We automate the mundane to liberate the creative. Every millisecond saved is a victory for progress."
    },
    {
      icon: <Shield className="text-primary" size={20} />,
      title: "INDUSTRIAL SECURE",
      text: "Trust is built into the architecture. Encrypted pipelines, authenticated agents, and paranoid audit logs ensure your IP remains yours."
    },
    {
      icon: <Cpu className="text-primary" size={20} />,
      title: "AGENTIC AUTONOMY",
      text: "We don't build tools; we build teammates. Our agents specialize in deep-context synthesis and multi-step execution without supervision."
    },
    {
      icon: <Layers className="text-primary" size={20} />,
      title: "DISTRIBUTED CORES",
      text: "Scale horizontally across regions. Deploy agents to any cloud, any repo, any time. The orchestrator is the brain, the cloud is the muscle."
    }
  ];

  return (
    <div className="h-full bg-black overflow-y-auto custom-scrollbar p-12 lg:p-24 relative selection:bg-primary selection:text-black">
      {/* Background Grid Decoration */}
      <div className="absolute inset-0 bg-[url('https://grainy-gradients.vercel.app/noise.svg')] opacity-20 pointer-events-none" />
      <div className="absolute top-0 left-0 w-full h-px bg-gradient-to-r from-transparent via-primary/30 to-transparent" />
      
      <div className="max-w-5xl mx-auto space-y-32 relative">
        {/* Header Section */}
        <section className="space-y-8">
          <motion.div 
            initial={{ opacity: 0, y: 20 }}
            animate={{ opacity: 1, y: 0 }}
            className="flex items-center gap-4 text-primary font-mono text-xs font-black tracking-[0.5em] uppercase"
          >
            <div className="w-12 h-px bg-primary" />
            Core Manifesto // Ver 2.0.4
          </motion.div>
          
          <motion.h1 
            initial={{ opacity: 0, x: -20 }}
            animate={{ opacity: 1, x: 0 }}
            transition={{ delay: 0.2 }}
            className="text-6xl lg:text-8xl font-black italic uppercase tracking-tighter text-white leading-none overflow-hidden"
          >
            Orchestrating <br />
            <span className="text-transparent bg-clip-text bg-gradient-to-r from-primary via-magenta-500 to-primary glow-primary inline-block">
              Machine Intelligence
            </span>
          </motion.h1>

          <motion.p 
            initial={{ opacity: 0 }}
            animate={{ opacity: 1 }}
            transition={{ delay: 0.4 }}
            className="text-zinc-500 font-mono text-sm max-w-2xl leading-relaxed uppercase tracking-wider"
          >
            The Auto-Developer Orchestrator is more than a control panel. It is the bridge between human intent and autonomous execution. We are rewriting the rules of production.
          </motion.p>
        </section>

        {/* The Pillars */}
        <section className="grid md:grid-cols-2 gap-12 lg:gap-24">
          {principles.map((p, i) => (
            <motion.div 
              key={p.title}
              initial={{ opacity: 0, scale: 0.95 }}
              whileInView={{ opacity: 1, scale: 1 }}
              viewport={{ once: true }}
              transition={{ delay: i * 0.1 }}
              className="p-8 border border-white/5 bg-zinc-900/20 backdrop-blur-sm group hover:border-primary/40 transition-all duration-500"
            >
              <div className="mb-6">{p.icon}</div>
              <h3 className="text-white font-black uppercase tracking-[0.3em] mb-4 group-hover:text-primary transition-colors italic">{p.title}</h3>
              <p className="text-zinc-500 text-xs font-mono leading-relaxed">{p.text}</p>
            </motion.div>
          ))}
        </section>

        {/* System Diagnostics Visual */}
        <section className="border-y border-white/5 py-24 relative overflow-hidden">
          <div className="grid grid-cols-2 md:grid-cols-4 gap-8">
            {[
              { label: "Uptime", value: "99.998%", sub: "High Availability" },
              { label: "Latency", value: "14ms", sub: "Internal Sync" },
              { label: "Active Nodes", value: "1,204", sub: "Global Compute" },
              { label: "Throughput", value: "4.8PB", sub: "Project Context" }
            ].map((stat, i) => (
              <div key={stat.label} className="space-y-2">
                <div className="text-[10px] font-mono text-zinc-700 uppercase tracking-widest">{stat.label}</div>
                <div className="text-2xl font-black text-white glow-white">{stat.value}</div>
                <div className="text-[8px] font-mono text-primary uppercase font-bold tracking-tighter italic">{stat.sub}</div>
              </div>
            ))}
          </div>
          
          {/* Animated Background Text */}
          <div className="absolute inset-0 -z-10 flex items-center justify-center opacity-[0.02] pointer-events-none select-none">
            <span className="text-[300px] font-black uppercase tracking-tighter">ANTIGRAVITY</span>
          </div>
        </section>

        {/* Bottom Call to Action / Motto */}
        <section className="text-center space-y-12">
          <div className="inline-flex flex-col items-center">
             <div className="w-1 h-24 bg-gradient-to-t from-primary to-transparent mb-8" />
             <h2 className="text-3xl font-black italic uppercase tracking-[0.5em] text-white">
                Build Fast. <br />
                Scale Forever.
             </h2>
          </div>
          
          <div className="flex flex-wrap items-center justify-center gap-6 text-[10px] font-mono text-zinc-700 uppercase tracking-widest font-black">
             <span className="flex items-center gap-2"><Globe size={12} /> Edge Network</span>
             <span className="flex items-center gap-2"><Database size={12} /> Persistent Core</span>
             <span className="flex items-center gap-2"><Lock size={12} /> Quantum Guard</span>
          </div>
        </section>
      </div>
      
      {/* Footer Branding */}
      <div className="mt-32 border-t border-white/5 pt-8 text-center">
         <div className="text-[9px] font-mono text-zinc-800 uppercase tracking-[1em] font-black">
            Auto-Developer Orchestrator // Project Jules v1.0
         </div>
      </div>
    </div>
  );
};
