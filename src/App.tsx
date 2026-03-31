import React from 'react';
import { AnimatePresence } from 'motion/react';
import { cn } from './lib/utils';
import { Sidebar } from './components/Sidebar';
import { Header } from './components/Header';
import { Checklist } from './components/Checklist';
import { Terminal } from './components/Terminal';
import { CLITerminal } from './components/CLITerminal';
import { PiDashboardView } from './components/PiDashboardView';
import { ReviewModal } from './components/ReviewModal';
import { CurrentTaskCard } from './components/CurrentTaskCard';
import { AIConfigModal } from './components/AIConfigModal';
import { CoverageReportModal } from './components/CoverageReportModal';
import { CloneModal } from './components/CloneModal';
import { UserModal } from './components/UserModal';
import { AddProjectModal } from './components/AddProjectModal';
import { GitHubConnectModal } from './components/GitHubConnectModal';
import { ManifestoView } from './components/ManifestoView';
import { ActivityView } from './components/ActivityView';
import { GithubView } from './components/GithubView';
import { ErrorBoundary } from './components/ErrorBoundary';

import { useTerminal } from './hooks/useTerminal';
import { useOrchestrator } from './hooks/useOrchestrator';
import { api, Task } from './lib/api';

export default function App() {
  const { logs, logEndRef, addLog, processCommand } = useTerminal();
  const { state, actions } = useOrchestrator(addLog);

  const {
    activeTab, projects, selectedProject, tasks, status, aiConfig,
    githubUser, isGeneratingChecklist, isDispatching, activeModal,
    isSidebarOpen, isCLITerminalOpen
  } = state;

  const {
    setActiveTab, setSelectedProject, setActiveModal,
    setIsSidebarOpen, setIsCLITerminalOpen,
    handleToggleMode, handleDispatch, handleDispatchAll, handleGenerateChecklist,
    refreshProjectData
  } = actions;

  const handleTerminalCommand = (cmd: string) => {
    processCommand(cmd, {
      'gen': () => handleGenerateChecklist(),
      'generate-checklist': () => handleGenerateChecklist(),
      'retry': () => addLog('Retry sequence initiated... (Stub)', 'SYSTEM'),
      'debug': () => addLog('PI_AGENT: Initializing debugging sequence...', 'SYSTEM'),
    });
  };

  const handleMerge = async () => {
    if (!selectedProject) return;
    addLog(`PR #102 approved and merged for ${selectedProject}.`, 'SUCCESS');
    try {
      const res = await api.git.merge(selectedProject);
      addLog(`Commit summary: "${res.summary}"`, 'INFO');
      refreshProjectData();
    } catch (e) {
      addLog(`Merge failure: ${e instanceof Error ? e.message : String(e)}`, 'ERROR');
    }
  };

  const activeTask = tasks.find(t => t.status === 'in-progress') || tasks.find(t => t.status === 'pending');

  return (
    <ErrorBoundary>
      <div className="flex h-screen bg-black text-slate-100 font-sans selection:bg-primary/20 overflow-hidden relative">
        <Sidebar
          activeTab={activeTab}
          isOpen={isSidebarOpen}
          onClose={() => setIsSidebarOpen(false)}
          onSettingsClick={() => setActiveModal('aiConfig')}
          onTerminalClick={() => { setActiveTab('terminal'); setIsSidebarOpen(false); }}
          onActivityClick={() => { setActiveTab('activity'); setIsSidebarOpen(false); }}
          onGithubClick={() => { setActiveTab('github'); setIsSidebarOpen(false); }}
          onAgentsClick={() => { setActiveTab('agents'); setIsSidebarOpen(false); }}
          onManifestoClick={() => { setActiveTab('manifesto'); setIsSidebarOpen(false); }}
          onConnectGitHubClick={() => setActiveModal('githubConnect')}
          isGitHubConnected={githubUser?.connected ?? false}
          githubUser={githubUser?.user}
          onUserClick={() => setActiveModal('user')}
        />

        <main className={cn(
          "flex-1 flex flex-col overflow-hidden w-full transition-all duration-300",
          isCLITerminalOpen ? "pb-96" : "pb-0"
        )}>
          <Header
            status={status}
            onToggleMode={handleToggleMode}
            onMenuToggle={() => setIsSidebarOpen(true)}
            onCoverageClick={() => setActiveModal('coverage')}
            onCloneClick={() => setActiveModal('clone')}
            onAddExistingClick={() => setActiveModal('addProject')}
            onRefreshProjects={refreshProjectData}
            onCLIClick={() => setIsCLITerminalOpen(true)}
            onAgentsClick={() => { setActiveTab('agents'); setIsSidebarOpen(false); }}
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
                  onGenerateAI={() => handleGenerateChecklist()}
                  onDispatchAll={handleDispatchAll}
                  onDispatchTask={(taskId) => handleDispatch(taskId)}
                  isGenerating={isGeneratingChecklist}
                  isDispatching={isDispatching}
                />

                <section className="flex-1 lg:w-1/2 flex flex-col p-4 lg:p-6 gap-4 lg:gap-6 bg-black overflow-hidden">
                  <AnimatePresence mode="wait">
                    {activeTask && (
                      <CurrentTaskCard 
                        task={activeTask} 
                        isAutoMode={status?.isAutoMode ?? false} 
                        onReview={() => setActiveModal('review')} 
                        onDispatch={() => handleDispatch(activeTask.id)}
                      />
                    )}
                  </AnimatePresence>

                  <Terminal 
                    logs={logs} 
                    logEndRef={logEndRef} 
                    onRetry={() => addLog('Retry sequence initiated...', 'SYSTEM')} 
                    onCommand={handleTerminalCommand}
                  />
                </section>
              </>
            )}
            {activeTab === 'activity' && <ActivityView logs={logs} />}
            {activeTab === 'github' && <GithubView repoOwner={githubUser?.user?.login} repoName={selectedProject} />}
            {activeTab === 'agents' && <PiDashboardView selectedProject={selectedProject} projects={projects} onProjectSelect={setSelectedProject} />}
            {activeTab === 'manifesto' && <ManifestoView />}
          </div>
        </main>

        <ReviewModal isOpen={activeModal === 'review'} onClose={() => setActiveModal(null)} onMerge={handleMerge} />
        <AIConfigModal isOpen={activeModal === 'aiConfig'} onClose={() => { setActiveModal(null); refreshProjectData(); }} />
        <CoverageReportModal isOpen={activeModal === 'coverage'} onClose={() => setActiveModal(null)} />
        <CloneModal isOpen={activeModal === 'clone'} onClose={() => setActiveModal(null)} onClone={(url) => api.git.clone(url).then(refreshProjectData)} />
        <AddProjectModal isOpen={activeModal === 'addProject'} onClose={() => setActiveModal(null)} onAdd={(name, path, url) => api.projects.register(name, path, url).then(refreshProjectData)} />
        <GitHubConnectModal isOpen={activeModal === 'githubConnect'} onClose={() => setActiveModal(null)} onConnect={(token) => api.config.connectGitHub(token).then(refreshProjectData)} />
        <UserModal isOpen={activeModal === 'user'} onClose={() => setActiveModal(null)} userEmail={githubUser?.user?.email ?? "anonymous@orchestrator"} userName={githubUser?.user?.name ?? githubUser?.user?.login} avatarUrl={githubUser?.user?.avatar_url} />

        <CLITerminal isOpen={isCLITerminalOpen} onClose={() => setIsCLITerminalOpen(false)} />
      </div>
    </ErrorBoundary>
  );
}
