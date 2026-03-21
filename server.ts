import express from "express";
import { createServer as createViteServer } from "vite";
import path from "path";
import fs from "fs";
import cors from "cors";
import morgan from "morgan";
import { Client } from "@langchain/langgraph-sdk";

async function startServer() {
  const app = express();
  const PORT = process.env.SERVER_PORT || 3847;

  app.use(cors());
  app.use(express.json());
  app.use(morgan("dev"));

  // Mock state
  let isAutoModes: Record<string, boolean> = {}; // Map of project name to auto mode status
  let currentTaskIndices: Record<string, number> = {}; // Map of project name to current task index
  let systemConfig = {
    projectsDir: path.join(process.cwd(), "projects")
  };
  let aiConfig = {
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
  };

  // API Routes
  app.get("/api/status", (req, res) => {
    const projectName = req.query.project as string;
    
    // In a real app, this would check the git status of the specific project directory
    // const projectDir = path.join(process.cwd(), "projects", projectName);
    
    const isAutoMode = isAutoModes[projectName] ?? false;
    
    res.json({
      gitState: "clean",
      workingTree: "main",
      isAutoMode,
      agentStatus: isAutoMode ? "running" : "paused",
      lastCommit: "1a2b3c4",
      project: projectName
    });
  });

  app.post("/api/config/ai", (req, res) => {
    aiConfig = { ...aiConfig, ...req.body };
    console.log("AI Config updated:", aiConfig);
    res.json({ success: true, aiConfig });
  });

  app.get("/api/config/ai", (req, res) => {
    res.json(aiConfig);
  });

  app.get("/api/projects", (req, res) => {
    try {
      const projectsDir = systemConfig.projectsDir;
      if (!fs.existsSync(projectsDir)) {
        fs.mkdirSync(projectsDir, { recursive: true });
      }
      const projects = fs.readdirSync(projectsDir, { withFileTypes: true })
        .filter(dirent => dirent.isDirectory())
        .map(dirent => dirent.name);
      res.json({ projects });
    } catch (error) {
      res.status(500).json({ error: "Failed to list projects" });
    }
  });

  app.get("/api/checklist", (req, res) => {
    try {
      const projectName = req.query.project as string;
      if (!projectName) {
        return res.status(400).json({ error: "Project name is required" });
      }
      
      const filePath = path.join(systemConfig.projectsDir, projectName, "TODO_FOR_JULES.md");
      if (!fs.existsSync(filePath)) {
        // Return empty tasks if file doesn't exist yet
        return res.json({ tasks: [] });
      }

      const content = fs.readFileSync(filePath, "utf-8");
      const lines = content.split("\n").filter(line => line.trim().startsWith("- ["));
      
      const tasks = lines.map((line, index) => {
        const completed = line.includes("[x]");
        const text = line.replace(/- \[[x ]\] /, "").trim();
        let status: 'completed' | 'in-progress' | 'pending' = 'pending';
        
        const currentTaskIndex = currentTaskIndices[projectName] ?? -1;
        if (completed) {
          status = 'completed';
        } else if (index === currentTaskIndex) {
          status = 'in-progress';
        }
        
        return {
          id: `task-${index}`,
          text,
          completed,
          status
        };
      });

      res.json({ tasks });
    } catch (error) {
      res.status(500).json({ error: "Failed to read checklist" });
    }
  });

  app.post("/api/checklist/update", (req, res) => {
    try {
      const { tasks, project } = req.body;
      if (!project) {
        return res.status(400).json({ error: "Project name is required" });
      }
      
      const projectDir = path.join(systemConfig.projectsDir, project);
      if (!fs.existsSync(projectDir)) {
        fs.mkdirSync(projectDir, { recursive: true });
      }
      
      const filePath = path.join(projectDir, "TODO_FOR_JULES.md");
      
      const content = tasks.map((t: any) => `- [${t.completed ? 'x' : ' '}] ${t.text}`).join('\n');
      fs.writeFileSync(filePath, content);
      
      res.json({ success: true, message: "Checklist updated successfully" });
    } catch (error) {
      res.status(500).json({ error: "Failed to update checklist" });
    }
  });

  app.post("/api/merge", (req, res) => {
    try {
      const { project } = req.body;
      if (!project) {
        return res.status(400).json({ error: "Project name is required" });
      }
      
      const filePath = path.join(systemConfig.projectsDir, project, "TODO_FOR_JULES.md");
      if (!fs.existsSync(filePath)) {
        return res.status(404).json({ error: "Checklist not found for project" });
      }
      
      let content = fs.readFileSync(filePath, "utf-8");
      const lines = content.split("\n");
      
      let taskCounter = 0;
      let mergedTaskText = "";
      const currentTaskIndex = currentTaskIndices[project] ?? -1;
      const updatedLines = lines.map(line => {
        if (line.trim().startsWith("- [")) {
          if (taskCounter === currentTaskIndex) {
            mergedTaskText = line.replace(/- \[[x ]\] /, "").trim();
            line = line.replace("- [ ]", "- [x]");
          }
          taskCounter++;
        }
        return line;
      });

      fs.writeFileSync(filePath, updatedLines.join("\n"));
      currentTaskIndices[project] = -1; // No task in progress after merge
      
      res.json({ 
        success: true, 
        message: "PR merged and task marked as completed.",
        summary: mergedTaskText || "General updates and bug fixes."
      });
    } catch (error) {
      res.status(500).json({ error: "Failed to merge and update checklist" });
    }
  });

  app.post("/api/generate-tests", async (req, res) => {
    const { summary, engine, prompt } = req.body;
    
    if (engine === 'gemini') {
      // Gemini is handled client-side for generateContent, 
      // but for consistency we could handle it here too if we had the key server-side.
      // However, instructions say Gemini MUST be client-side.
      // So if engine is gemini, the client should have handled it or we fallback.
      return res.json({ 
        success: true, 
        engine,
        summary,
        tests: [
          `Verify that ${summary} handles null inputs correctly.`,
          `Check for race conditions in ${summary} during high concurrency.`,
          `Ensure ${summary} doesn't leak memory on repeated calls.`
        ],
        timestamp: new Date().toISOString()
      });
    }
    
    // Fallback/Mock
    setTimeout(() => {
      const tests = [
        `Verify that ${summary} handles null inputs correctly.`,
        `Check for race conditions in ${summary} during high concurrency.`,
        `Ensure ${summary} doesn't leak memory on repeated calls.`,
        `Validate that ${summary} respects existing security permissions.`
      ];
      
      res.json({ 
        success: true, 
        engine,
        summary,
        tests,
        timestamp: new Date().toISOString()
      });
    }, 2000);
  });

  app.post("/api/run-tests", (req, res) => {
    const { tests } = req.body;
    
    // Simulate test execution delay
    setTimeout(() => {
      const results = tests.map((test: string) => ({
        test,
        status: Math.random() > 0.1 ? "PASSED" : "FAILED", // 10% failure rate for realism
        duration: Math.floor(Math.random() * 500) + 100
      }));
      
      res.json({ 
        success: true, 
        results,
        timestamp: new Date().toISOString()
      });
    }, 1500);
  });

  app.post("/api/dispatch", (req, res) => {
    const { taskId, project } = req.body;
    if (!project) {
      return res.status(400).json({ error: "Project name is required" });
    }
    // Extract index from taskId (e.g., "task-4")
    const index = parseInt(taskId.split('-')[1]);
    currentTaskIndices[project] = index;
    
    res.json({ 
      success: true, 
      message: `Task ${taskId} dispatched to JULES`,
      taskId,
      issueUrl: "https://github.com/user/repo/issues/102"
    });
  });

  app.post("/api/settings/mode", (req, res) => {
    const { mode, project } = req.body;
    if (!project) {
      return res.status(400).json({ error: "Project name is required" });
    }
    const isAutoMode = mode === "auto";
    isAutoModes[project] = isAutoMode;
    res.json({ success: true, isAutoMode });
  });

  app.get("/api/config/system", (req, res) => {
    res.json(systemConfig);
  });

  app.post("/api/config/system", (req, res) => {
    const { projectsDir } = req.body;
    if (projectsDir) {
      systemConfig.projectsDir = projectsDir;
    }
    res.json({ success: true, systemConfig });
  });

  app.post("/api/clone", (req, res) => {
    const { url } = req.body;
    if (!url) {
      return res.status(400).json({ error: "URL is required" });
    }

    try {
      // Extract project name from URL (e.g., "https://github.com/user/repo.git" -> "repo")
      const projectName = url.split("/").pop()?.replace(".git", "") || "new-project";
      const projectDir = path.join(systemConfig.projectsDir, projectName);

      if (fs.existsSync(projectDir)) {
        return res.status(400).json({ error: "Project already exists locally." });
      }

      // In a real local environment, we would run:
      // exec(`git clone ${url} ${projectDir}`)
      
      // For now, we simulate the creation of the folder and the checklist
      fs.mkdirSync(projectDir, { recursive: true });
      const checklistPath = path.join(projectDir, "TODO_FOR_JULES.md");
      fs.writeFileSync(checklistPath, `- [ ] Initial codebase analysis\n- [ ] Configure CI/CD pipeline\n- [ ] Audit existing test suite\n- [ ] Identify architectural bottlenecks`);

      res.json({ 
        success: true, 
        message: `Repository '${projectName}' cloned successfully to ${projectDir}`,
        projectName 
      });
    } catch (error) {
      res.status(500).json({ error: "Failed to clone repository" });
    }
  });

  app.post("/api/ai/agent-checklist", async (req, res) => {
    try {
      const { prompt, project } = req.body;
      
      // Connect to the Python LangGraph Server
      const client = new Client({ apiUrl: "http://localhost:8194" });
      
      // 1. Create a persistent thread for memory & file system isolation
      const thread = await client.threads.create();

      // 2. Set up Server-Sent Events (SSE) to stream to the frontend
      res.writeHead(200, {
        'Content-Type': 'text/event-stream',
        'Cache-Control': 'no-cache',
        'Connection': 'keep-alive'
      });

      const defaultPrompt = `Analyze the current codebase for project '${project || 'default'}' and generate a list of 5-7 technical tasks/todos to improve or expand the application. Focus on real architectural needs or missing features based on what you see in the files. Return the tasks as a JSON array of strings. ONLY return the JSON array in your final answer.`;
      const finalPrompt = prompt ? `${defaultPrompt}\n\nAdditional Guidance: ${prompt}` : defaultPrompt;

      // 3. Stream the execution of the deep agent
      const stream = client.runs.stream(thread.thread_id, "my_deep_agent", {
        input: { messages: [{ role: "user", content: finalPrompt }] },
        streamMode: "events", // Crucial for seeing sub-agent and tool-calling progress
      });

      for await (const chunk of stream) {
        // Stream updates to the frontend
        res.write(`data: ${JSON.stringify(chunk)}\n\n`);
      }
      
      res.end();
    } catch (error: any) {
      console.error("Deep Agent Error:", error);
      // If the SSE connection hasn't started, we can send a normal JSON error
      if (!res.headersSent) {
        res.status(500).json({ error: "Failed to connect to Python LangGraph Server. Ensure it is running on port 8123." });
      } else {
        res.write(`data: ${JSON.stringify({ error: error.message })}\n\n`);
        res.end();
      }
    }
  });

  // Vite middleware for development
  if (process.env.NODE_ENV !== "production") {
    const vite = await createViteServer({
      server: { middlewareMode: true },
      appType: "spa",
    });
    app.use(vite.middlewares);
  } else {
    const distPath = path.join(process.cwd(), "dist");
    app.use(express.static(distPath));
    app.get("*", (req, res) => {
      res.sendFile(path.join(distPath, "index.html"));
    });
  }

  app.listen(PORT, "0.0.0.0", () => {
    console.log(`Server running on http://localhost:${PORT}`);
  });
}

startServer();
