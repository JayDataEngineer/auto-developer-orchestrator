# JULES AI - Multi-Repository Automation Tool

JULES AI is an autonomous, full-stack coding agent designed to manage, test, and merge code across multiple Git repositories.

## Features

- **Multi-Repository Support**: Manage multiple projects from a single dashboard.
- **Local File System Integration**: Point JULES to your local folder containing your GitHub repositories.
- **Clone Repository**: Directly clone new repositories into your projects folder from the UI.
- **Autonomous Loop**: Automatically generate tasks, write code, run tests, and merge PRs.
- **AI Test Generation**: Uses Gemini to automatically generate comprehensive test suites based on commit summaries.
- **Real-time Terminal**: View live logs of the agent's actions and system status.
- **Coverage Reports**: Track test coverage across your projects.

## Local Setup & GitHub Integration

JULES is designed to be run locally on your machine to interact with your actual project folders.

1. **Download the App**: Use the "Export to ZIP" or "Export to GitHub" option in the AI Studio settings menu.
2. **Install Dependencies**:
   ```bash
   npm install
   ```
3. **Start JULES**:
   ```bash
   npm run dev
   ```
4. **Configure Local Path**:
   - Open JULES in your browser (`http://localhost:3000`).
   - Click the **Settings (gear icon)** in the sidebar.
   - Under **Local System Environment**, enter the absolute path to the folder where you keep your GitHub repositories (e.g., `/Users/username/code/repos` or `C:\Users\username\Documents\GitHub`).
   - Click **Save Configuration**.

5. **Connect Projects**:
   - JULES will automatically scan that folder for subdirectories.
   - Any folder containing a `TODO_FOR_JULES.md` file will be recognized as a project.
   - You can also use the **"+" (Plus icon)** in the header to clone a new repository directly into that folder.

## Project Structure

- `/projects`: Default location for repositories if no custom path is configured.
- `/src`: The React frontend application.
- `server.ts`: The Express backend that coordinates the AI agent and manages the projects.

## Getting Started

1. Install dependencies:
   ```bash
   npm install
   ```

2. Start the development server:
   ```bash
   npm run dev
   ```

3. Open your browser to `http://localhost:3000`.

## Adding a Project

To add a new project, simply create a new directory inside the `/projects` folder and add a `TODO_FOR_JULES.md` file with your tasks:

```bash
mkdir -p projects/my-new-project
touch projects/my-new-project/TODO_FOR_JULES.md
```

The format for `TODO_FOR_JULES.md` should be a markdown checklist:

```markdown
- [ ] Setup initial routing
- [ ] Implement user authentication
- [ ] Write unit tests for auth service
```

## Configuration

You can configure the AI agent's behavior by clicking the Settings (gear) icon in the sidebar. This allows you to toggle Full Automation Mode, Auto-Task Generation, and configure the test generation prompts.
