import React from 'react';
import { motion } from 'motion/react';
import { Shield, Zap, Box, Terminal as TerminalIcon, Globe } from 'lucide-react';

export const ManifestoView: React.FC = () => {
  const points = [
    { 
      icon: Zap, 
      title: "Autonomy over Assistance", 
      text: "We don't just 'suggest' code. We orchestrate the entire software engineering lifecycle—from task identification to Pull Request merging." 
    },
    { 
      icon: Box, 
      title: "Industrial Density", 
      text: "Modern engineering is high-density. Our Grok-inspired UI provides maximum information at a glance, eliminating 'white space' in favor of 'signal.'" 
    },
    { 
      icon: TerminalIcon, 
      title: "Real-Life Performance", 
      text: "We favor Go for back-end concurrency and Python for deep AI reasoning. We are 8x faster than traditional Node.js orchestrators." 
    },
    { 
      icon: Globe, 
      title: "Transparent Intelligence", 
      text: "Every AI log, every terminal command, and every Jules session is visible. We build trust through full observability via Langfuse and LiteLLM." 
    }
  ];

  return (
    <div className="flex-1 p-10 lg:p-20 overflow-y-auto bg-black text-white relative">
      {/* Background decoration */}
      <div className="absolute top-0 right-0 w-[600px] h-[600px] bg-primary/5 rounded-full blur-[120px] pointer-events-none" />
      <div className="absolute bottom-0 left-0 w-[400px] h-[400px] bg-primary/5 rounded-full blur-[100px] pointer-events-none" />

      <motion.div 
        initial={{ opacity: 0, y: 30 }}
        animate={{ opacity: 1, y: 0 }}
        transition={{ duration: 0.8 }}
        className="max-w-4xl mx-auto relative z-10"
      >
        <div className="flex items-center gap-4 mb-12">
          <div className="h-[2px] w-20 bg-primary" />
          <span className="text-primary font-black uppercase tracking-[0.5em] text-xs">The Proclamation</span>
        </div>

        <h1 className="text-6xl lg:text-8xl font-black mb-16 tracking-tighter leading-none italic uppercase">
          THE <span className="text-primary glow-primary">ORCHESTRATOR'S</span> MANIFESTO
        </h1>

        <p className="text-xl lg:text-2xl text-zinc-400 font-medium mb-24 leading-relaxed max-w-2xl">
          This is not a dashboard. This is a <span className="text-white">command center</span> for professional code automation.
        </p>

        <div className="grid grid-cols-1 md:grid-cols-2 gap-12 mb-32">
          {points.map((point, index) => (
            <motion.div 
              key={index}
              initial={{ opacity: 0, x: -20 }}
              whileInView={{ opacity: 1, x: 0 }}
              transition={{ delay: index * 0.1 }}
              viewport={{ once: true }}
              className="group p-8 border border-white/5 bg-zinc-950/50 hover:border-primary/30 transition-all duration-500 relative"
            >
              <div className="absolute top-0 left-0 w-1 h-0 group-hover:h-full bg-primary transition-all duration-500" />
              <div className="p-3 bg-zinc-900 border border-white/5 text-zinc-500 group-hover:text-primary group-hover:border-primary/20 transition-all mb-6 inline-block">
                <point.icon size={24} />
              </div>
              <h3 className="text-xl font-black uppercase tracking-widest text-white mb-4 italic">{point.title}</h3>
              <p className="text-sm text-zinc-500 leading-relaxed group-hover:text-zinc-400 transition-colors">
                {point.text}
              </p>
            </motion.div>
          ))}
        </div>

        <div className="p-12 border border-primary/20 bg-primary/5 flex flex-col items-center text-center relative overflow-hidden group">
          <div className="absolute -top-24 -right-24 w-48 h-48 bg-primary/10 rounded-full blur-[60px] group-hover:scale-125 transition-transform duration-1000" />
          <h2 className="text-2xl font-black italic uppercase tracking-[0.3em] text-white mb-6">READY // VISION_LOCKED</h2>
          <p className="text-xs text-zinc-500 uppercase tracking-[0.4em] font-bold">
            Project initialized for real-world autonomy.
          </p>
        </div>

        <div className="mt-32 text-center pb-20">
          <p className="text-[10px] text-zinc-800 font-bold uppercase tracking-[0.6em]">
            SYSTEM_STABLE // JULES_READY // GEMINI_ACTIVE
          </p>
        </div>
      </motion.div>
    </div>
  );
};
