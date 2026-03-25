import React, { useState, useEffect, useRef } from 'react';
import { AnimatePresence } from 'motion/react';
import { Sidebar } from './components/Sidebar';
import { GoogleGenAI, Type } from "@google/genai";
import { Header } from './components/Header';
import { Checklist } from './components/Checklist';
import { Terminal } from './components/Terminal';
import { CLITerminal } from './components/CLITerminal';
import { AgentsView } from './components/AgentsView';
import { ReviewModal } from './components/ReviewModal';
import { CurrentTaskCard } from './components/CurrentTaskCard';
import { AIConfigModal } from './components/AIConfigModal';
import { CoverageReportModal } from './components/CoverageReportModal';
import { CloneModal } from './components/CloneModal';
import { UserModal } from './components/UserModal';
import { AddProjectModal } from './components/AddProjectModal';
import { ActivityView } from './components/ActivityView';
import { GithubView } from './components/GithubView';

interface Task {
  id: string;
  text: string;
  completed: boolean;
  status: 'completed' | 'in-progress' | 'pending';
}

interface Status {
  gitState: string;
  workingTree: string;
  isAutoMode: boolean;
  agentStatus: string;
  lastCommit: string;
}

const LOG_MESSAGES = [
  "Initialized task execution environment.",
  "Reading file src/api/routes/index.ts...",
  "Analyzing existing routing architecture... OK.",
  "Generating src/api/routes/stats.ts",
  "Writing controller logic for daily aggregates...",
  "WARN: Missing type definition for TransactionSummary in models.",
  "Creating interface ITransactionSummary in src/types/index.d.ts",
  "Running linter on modified files...",
  "Executing npm run test:unit -- src/api/routes/stats.test.ts",
  "Waiting for test runner to complete..."
];

export default function App() {
  const [activeTab, setActiveTab] = useState<'terminal' | 'activity' | 'github' | 'agents'>('terminal');
  const [tasks, setTasks] = useState<Task[]>([]);
  const [status, setStatus] = useState<Status | null>(null);
  const [logs, setLogs] = useState<string[]>([]);
  const [isReviewOpen, setIsReviewOpen] = useState(false);
  const [isAIConfigOpen, setIsAIConfigOpen] = useState(false);
  const [isCoverageOpen, setIsCoverageOpen] = useState(false);
  const [isCloneOpen, setIsCloneOpen] = useState(false);
  const [isAddProjectOpen, setIsAddProjectOpen] = useState(false);
  const [isUserOpen, setIsUserOpen] = useState(false);
  const [isSidebarOpen, setIsSidebarOpen] = useState(false);
  const [isGeneratingChecklist, setIsGeneratingChecklist] = useState(false);
  const [isDispatching, setIsDispatching] = useState(false);
  const [isCLITerminalOpen, setIsCLITerminalOpen] = useState(false);
  const [aiConfig, setAiConfig] = useState<any>(null);
  const [lastTests, setLastTests] = useState<string[]>([]);
  const [projects, setProjects] = useState<string[]>([]);
  const [selectedProject, setSelectedProject] = useState<string>('');
  const logEndRef = useRef<HTMLDivElement>(null);

  useEffect(() => {
    fetchProjects();
    fetchAiConfig();
    
    // Simulate logs
    let i = 0;
    const interval = setInterval(() => {
      if (i < LOG_MESSAGES.length) {
        setLogs(prev => [...prev, `[${new Date().toLocaleTimeString()}] ${LOG_MESSAGES[i]}`]);
        i++;
      }
    }, 2000);

    return () => clearInterval(interval);
  }, []);

  useEffect(() => {
    if (selectedProject) {
      fetchStatus();
      fetchChecklist();
    } else {
      setTasks([]);
      setStatus(null);
    }
  }, [selectedProject]);

  useEffect(() => {
    logEndRef.current?.scrollIntoView({ behavior: 'smooth' });
  }, [logs]);

  const fetchProjects = async () => {
    try {
      const res = await fetch('/api/projects');
      const data = await res.json();
      setProjects(data.projects);
      if (data.projects.length > 0 && !selectedProject) {
        setSelectedProject(data.projects[0]);
      }
    } catch (e) {
      console.error("Failed to fetch projects", e);
    }
  };

  const fetchStatus = async () => {
    if (!selectedProject) return;
    try {
      const res = await fetch(`/api/status?project=${encodeURIComponent(selectedProject)}`);
      const data = await res.json();
      setStatus(data);
    } catch (e) {
      console.error("Failed to fetch status", e);
    }
  };

  const fetchChecklist = async () => {
    if (!selectedProject) return;
    try {
      const res = await fetch(`/api/checklist?project=${encodeURIComponent(selectedProject)}`);
      const data = await res.json();
      setTasks(data.tasks || []);
    } catch (e) {
      console.error("Failed to fetch checklist", e);
    }
  };

  const fetchAiConfig = async () => {
    try {
      const res = await fetch('/api/config/ai');
      const data = await res.json();
      setAiConfig(data);
    } catch (e) {
      console.error("Failed to fetch AI config", e);
    }
  };

  const handleMerge = async () => {
    if (!selectedProject) return;
    setLogs(prev => [...prev, `[${new Date().toLocaleTimeString()}] PR #102 approved and merged to main.`]);
    
    try {
      const mergeRes = await fetch('/api/merge', { 
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ project: selectedProject })
      });
      const mergeData = await mergeRes.json();
      
      const checklistRes = await fetch(`/api/checklist?project=${encodeURIComponent(selectedProject)}`);
      const checklistData = await checklistRes.json();
      setTasks(checklistData.tasks || []);

      setLogs(prev => [...prev, `[${new Date().toLocaleTimeString()}] Commit summary extracted: "${mergeData.summary}"`]);

      if (aiConfig?.fullAutomationMode) {
        setLogs(prev => [...prev, `[${new Date().toLocaleTimeString()}] Full Automation Mode active. Triggering post-merge AI test generation...`]);
        setLogs(prev => [...prev, `[${new Date().toLocaleTimeString()}] Sending summary to GEMINI for bug testing...`]);
        
        try {
          const genRes = await fetch('/api/generate-tests', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ 
              summary: mergeData.summary,
              engine: 'gemini',
              prompt: aiConfig.testGenPrompt
            })
          });
          const genData = await genRes.json();
          setLastTests(genData.tests);
          
          genData.tests.forEach((test: string) => {
            setLogs(prev => [...prev, `[${new Date().toLocaleTimeString()}] [AI TEST GEN] ${test}`]);
          });

          setLogs(prev => [...prev, `[${new Date().toLocaleTimeString()}] Running generated tests in CI environment...`]);
          
          const runRes = await fetch('/api/run-tests', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ tests: genData.tests })
          });
          const runData = await runRes.json();

          runData.results.forEach((res: any) => {
            setLogs(prev => [...prev, `[${new Date().toLocaleTimeString()}] [TEST RUN] ${res.test} ... ${res.status} (${res.duration}ms)`]);
          });

          const allPassed = runData.results.every((r: any) => r.status === 'PASSED');
          if (!allPassed) {
            setLogs(prev => [...prev, `[${new Date().toLocaleTimeString()}] ERROR: One or more tests failed. Pausing automation for manual review.`]);
            return;
          }

          setLogs(prev => [...prev, `[${new Date().toLocaleTimeString()}] All tests passed. Moving to next to-do item...`]);
          
          // Find next task from the FRESH data
          const nextTask = checklistData.tasks.find((t: any) => t.status === 'pending');
          if (nextTask) {
            setLogs(prev => [...prev, `[${new Date().toLocaleTimeString()}] Next task identified: "${nextTask.text}"`]);
            setLogs(prev => [...prev, `[${new Date().toLocaleTimeString()}] Automatically creating issue and assigning to JULES...`]);
            
            try {
              const dispatchRes = await fetch('/api/dispatch', {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({ taskId: nextTask.id, project: selectedProject, repoOwner: "JayDataEngineer", repoName: selectedProject })
              });
              const dispatchData = await dispatchRes.json();
              setLogs(prev => [...prev, `[${new Date().toLocaleTimeString()}] ${dispatchData.message}: ${dispatchData.issueUrl}`]);
              
              // Refresh checklist again to show new in-progress task
              fetchChecklist();
            } catch (e) {
              console.error("Failed to dispatch next task", e);
            }
          } else {
            setLogs(prev => [...prev, `[${new Date().toLocaleTimeString()}] No more pending tasks found. Automation loop complete.`]);
          }
        } catch (e) {
          console.error("Failed to generate tests", e);
        }
      }
    } catch (e) {
      console.error("Failed to merge", e);
    }
  };

  const handleGenerateChecklist = async (prompt?: string) => {
    if (!selectedProject) return;
    setIsGeneratingChecklist(true);
    setLogs(prev => [...prev, `[${new Date().toLocaleTimeString()}] Generating checklist from codebase analysis...`]);
    if (prompt) {
      setLogs(prev => [...prev, `[${new Date().toLocaleTimeString()}] Using guidance prompt: "${prompt}"`]);
    }

    try {
      const res = await fetch('/api/ai/agent-checklist', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ prompt, project: selectedProject })
      });

      if (!res.ok) {
        const data = await res.json();
        throw new Error(data.error || "Checklist generation failed");
      }

      if (!res.body) throw new Error("No response body");

      const reader = res.body.getReader();
      const decoder = new TextDecoder();
      let done = false;
      let finalTasksFromEvent: any[] = [];

      while (!done) {
        const { value, done: readerDone } = await reader.read();
        done = readerDone;
        if (value) {
          const chunk = decoder.decode(value, { stream: true });
          const lines = chunk.split('\n\n');

          for (const line of lines) {
            if (line.startsWith('data: ')) {
              try {
                const data = JSON.parse(line.slice(6));

                if (data.error) {
                  throw new Error(data.error);
                }

                // Handle the new final_result event with structured todos
                if (data.event === "final_result") {
                  finalTasksFromEvent = data.todos;
                } else if (data.event === "on_tool_start") {
                  setLogs(prev => [...prev, `[${new Date().toLocaleTimeString()}] Running tool ${data.name}...`]);
                }
              } catch (e) {
                console.error("Error parsing SSE chunk", e);
              }
            }
          }
        }
      }

      if (finalTasksFromEvent.length > 0) {
        const newTasks = finalTasksFromEvent.map((todo: any, i: number) => ({
          id: todo.id || `task-${i}-${Date.now()}`,
          text: todo.content,
          completed: todo.status === 'completed',
          status: (todo.status as any) === 'cancelled' ? 'pending' : (todo.status as any) || 'pending'
        }));

        // Update backend
        await fetch('/api/checklist/update', {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ tasks: newTasks, project: selectedProject })
        });

        setLogs(prev => [...prev, `[${new Date().toLocaleTimeString()}] Analysis complete. Generated ${newTasks.length} tasks.`]);
        fetchChecklist();
      } else {
        setLogs(prev => [...prev, `[${new Date().toLocaleTimeString()}] Completed but no tasks were generated.`]);
      }
    } catch (error: any) {
      console.error("Failed to generate checklist", error);
      setLogs(prev => [...prev, `[${new Date().toLocaleTimeString()}] ERROR: ${error.message}`]);
    } finally {
      setIsGeneratingChecklist(false);
    }
  };

  const handleDispatchAll = async () => {
    if (!selectedProject || tasks.length === 0) return;
    
    setIsDispatching(true);
    setLogs(prev => [...prev, `[${new Date().toLocaleTimeString()}] Initiating GitHub Pipeline Dispatch for ${tasks.length} tasks...`]);
    
    try {
      // For now we use the project name as a placeholder for repo details
      // In a real app, these would come from project settings
      const repoOwner = "JayDataEngineer"; 
      const repoName = selectedProject;

      const res = await fetch('/api/dispatch/all', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ 
          project: selectedProject, 
          todos: tasks.map(t => ({ content: t.text })),
          repoOwner,
          repoName
        })
      });

      const data = await res.json();
      if (data.success) {
        setLogs(prev => [...prev, `[${new Date().toLocaleTimeString()}] SUCCESS: Successfully dispatched tasks to ${repoOwner}/${repoName}.`]);
        data.results.forEach((r: any) => {
          setLogs(prev => [...prev, `[${new Date().toLocaleTimeString()}] - Created: ${r.issueUrl}`]);
        });
      } else {
        throw new Error(data.error || "Dispatch failed");
      }
    } catch (error: any) {
      console.error("Failed to dispatch all tasks", error);
      setLogs(prev => [...prev, `[${new Date().toLocaleTimeString()}] ERROR: GitHub Dispatch failed: ${error.message}`]);
    } finally {
      setIsDispatching(false);
    }
  };

  const handleClone = async (url: string) => {
    setLogs(prev => [...prev, `[${new Date().toLocaleTimeString()}] SYSTEM: Cloning repository from ${url}...`]);
    try {
      const res = await fetch('/api/clone', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ url })
      });
      const data = await res.json();
      setLogs(prev => [...prev, `[${new Date().toLocaleTimeString()}] SYSTEM: ${data.message}`]);
      await fetchProjects();
      if (data.projectName) {
        setSelectedProject(data.projectName);
      }
    } catch (e) {
      console.error("Failed to clone repository", e);
      setLogs(prev => [...prev, `[${new Date().toLocaleTimeString()}] ERROR: Failed to clone repository.`]);
    }
  };

  const toggleMode = async () => {
    if (!status) return;
    const newMode = status.isAutoMode ? 'manual' : 'auto';
    try {
      await fetch('/api/settings/mode', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ mode: newMode, project: selectedProject })
      });
      fetchStatus();
    } catch (e) {
      console.error("Failed to toggle mode", e);
    }
  };

  const handleRetry = async () => {
    if (lastTests.length === 0) return;
    
    setLogs(prev => [...prev, `[${new Date().toLocaleTimeString()}] Retrying failed tests...`]);
    
    try {
      const runRes = await fetch('/api/run-tests', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ tests: lastTests })
      });
      const runData = await runRes.json();

      runData.results.forEach((res: any) => {
        setLogs(prev => [...prev, `[${new Date().toLocaleTimeString()}] [TEST RUN] ${res.test} ... ${res.status} (${res.duration}ms)`]);
      });

      const allPassed = runData.results.every((r: any) => r.status === 'PASSED');
      if (!allPassed) {
        setLogs(prev => [...prev, `[${new Date().toLocaleTimeString()}] ERROR: Tests failed again. Manual intervention required.`]);
        return;
      }

      setLogs(prev => [...prev, `[${new Date().toLocaleTimeString()}] All tests passed on retry. Resuming automation loop...`]);
      
      // Resume automation loop logic
      const checklistRes = await fetch('/api/checklist');
      const checklistData = await checklistRes.json();
      const nextTask = checklistData.tasks.find((t: any) => t.status === 'pending');
      if (nextTask) {
        setLogs(prev => [...prev, `[${new Date().toLocaleTimeString()}] Next task identified: "${nextTask.text}"`]);
        setLogs(prev => [...prev, `[${new Date().toLocaleTimeString()}] Automatically creating issue and assigning to JULES...`]);
        
        const dispatchRes = await fetch('/api/dispatch', {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ taskId: nextTask.id, project: selectedProject, repoOwner: "JayDataEngineer", repoName: selectedProject })
        });
        const dispatchData = await dispatchRes.json();
        setLogs(prev => [...prev, `[${new Date().toLocaleTimeString()}] ${dispatchData.message}: ${dispatchData.issueUrl}`]);
        fetchChecklist();
      }
    } catch (e) {
      console.error("Failed to retry tests", e);
    }
  };

  const handleAddProject = async (name: string, path: string) => {
    setLogs(prev => [...prev, `[${new Date().toLocaleTimeString()}] SYSTEM: Registering existing project "${name}" at ${path}...`]);
    try {
      const res = await fetch('/api/projects/add', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ name, path })
      });
      const data = await res.json();
      if (data.success) {
        setLogs(prev => [...prev, `[${new Date().toLocaleTimeString()}] SYSTEM: ${data.message}`]);
        await fetchProjects();
        setSelectedProject(name);
      } else {
        throw new Error(data.error || "Failed to add project");
      }
    } catch (e: any) {
      console.error("Failed to add project", e);
      setLogs(prev => [...prev, `[${new Date().toLocaleTimeString()}] ERROR: ${e.message}`]);
    }
  };

  const handleTerminalCommand = (cmd: string) => {
    setLogs(prev => [...prev, `[${new Date().toLocaleTimeString()}] $ ${cmd}`]);
    
    const normalizedCmd = cmd.toLowerCase().trim();
    if (normalizedCmd === 'generate-checklist' || normalizedCmd === 'gen') {
      handleGenerateChecklist();
    } else if (normalizedCmd === 'retry') {
      handleRetry();
    } else if (normalizedCmd === 'clear') {
      setLogs([`[${new Date().toLocaleTimeString()}] SYSTEM: Terminal logs cleared.`]);
    } else if (normalizedCmd === 'debug' || normalizedCmd === 'issue') {
      setLogs(prev => [...prev, `[${new Date().toLocaleTimeString()}] JULES: Initializing debugging sequence for new issue...`]);
      setTimeout(() => {
        setLogs(prev => [...prev, `[${new Date().toLocaleTimeString()}] JULES: Analyzing codebase for potential regressions...`]);
      }, 1000);
    } else {
      setLogs(prev => [...prev, `[${new Date().toLocaleTimeString()}] ERROR: Command not found: ${cmd}`]);
    }
  };

  const activeTask = tasks.find(t => t.status === 'in-progress') || tasks.find(t => t.status === 'pending');

  const handleDispatch = async (taskId: string) => {
    if (!selectedProject) return;
    setLogs(prev => [...prev, `[${new Date().toLocaleTimeString()}] Manually dispatching task ${taskId} to JULES...`]);
    try {
      const res = await fetch('/api/dispatch', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ taskId, project: selectedProject, repoOwner: "JayDataEngineer", repoName: selectedProject })
      });
      const data = await res.json();
      setLogs(prev => [...prev, `[${new Date().toLocaleTimeString()}] ${data.message}: ${data.issueUrl}`]);
      fetchChecklist();
    } catch (e) {
      console.error("Failed to dispatch task", e);
    }
  };

  return (
    <div className="flex h-screen bg-black text-slate-100 font-sans selection:bg-primary/30 overflow-hidden relative">
      <Sidebar
        activeTab={activeTab}
        isOpen={isSidebarOpen}
        onClose={() => setIsSidebarOpen(false)}
        onSettingsClick={() => setIsAIConfigOpen(true)}
        onTerminalClick={() => { setActiveTab('terminal'); setIsSidebarOpen(false); }}
        onActivityClick={() => { setActiveTab('activity'); setIsSidebarOpen(false); }}
        onGithubClick={() => { setActiveTab('github'); setIsSidebarOpen(false); }}
        onAgentsClick={() => { setActiveTab('agents'); setIsSidebarOpen(false); }}
        onUserClick={() => setIsUserOpen(true)}
      />

      <main className="flex-1 flex flex-col overflow-hidden w-full">
        <Header
          status={status}
          onToggleMode={toggleMode}
          onMenuToggle={() => setIsSidebarOpen(true)}
          onCoverageClick={() => setIsCoverageOpen(true)}
          onCloneClick={() => setIsCloneOpen(true)}
          onAddExistingClick={() => setIsAddProjectOpen(true)}
          onRefreshProjects={fetchProjects}
          onCLIClick={() => setIsCLITerminalOpen(true)}
          fullAutomationMode={aiConfig?.fullAutomationMode ?? false}
          projects={projects}
          selectedProject={selectedProject}
          onProjectSelect={setSelectedProject}
        />

        <div className="flex-1 flex flex-col lg:flex-row overflow-hidden">
          {activeTab === 'terminal' && (
            <>
              <Checklist 
                tasks={tasks} 
                onGenerateAI={handleGenerateChecklist}
                onDispatchAll={handleDispatchAll}
                isGenerating={isGeneratingChecklist}
                isDispatching={isDispatching}
              />

              <section className="flex-1 lg:w-1/2 flex flex-col p-4 lg:p-6 gap-4 lg:gap-6 bg-black overflow-hidden">
                <AnimatePresence mode="wait">
                  {activeTask && (
                    <CurrentTaskCard 
                      task={activeTask} 
                      isAutoMode={status?.isAutoMode ?? false} 
                      onReview={() => setIsReviewOpen(true)} 
                      onDispatch={() => handleDispatch(activeTask.id)}
                    />
                  )}
                </AnimatePresence>

                <Terminal 
                  logs={logs} 
                  logEndRef={logEndRef} 
                  onRetry={handleRetry} 
                  onCommand={handleTerminalCommand}
                />
              </section>
            </>
          )}
          {activeTab === 'activity' && <ActivityView logs={logs} />}
          {activeTab === 'github' && <GithubView />}
          {activeTab === 'agents' && <AgentsView />}
        </div>
      </main>

      <ReviewModal 
        isOpen={isReviewOpen} 
        onClose={() => setIsReviewOpen(false)} 
        onMerge={handleMerge}
      />
      <AIConfigModal 
        isOpen={isAIConfigOpen} 
        onClose={() => {
          setIsAIConfigOpen(false);
          fetchAiConfig(); // Refresh config after closing modal
        }} 
      />
      <CoverageReportModal 
        isOpen={isCoverageOpen} 
        onClose={() => setIsCoverageOpen(false)} 
      />
      <CloneModal 
        isOpen={isCloneOpen} 
        onClose={() => setIsCloneOpen(false)} 
        onClone={handleClone}
      />
      <AddProjectModal 
        isOpen={isAddProjectOpen} 
        onClose={() => setIsAddProjectOpen(false)} 
        onAdd={handleAddProject}
      />
      <UserModal
        isOpen={isUserOpen}
        onClose={() => setIsUserOpen(false)}
        userEmail="protopomp@gmail.com"
      />

      {/* CLI Terminal */}
      <CLITerminal
        isOpen={isCLITerminalOpen}
        onClose={() => setIsCLITerminalOpen(false)}
      />
    </div>
  );
}
