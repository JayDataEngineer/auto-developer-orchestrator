# Auto-Developer Orchestrator

A Dockerized orchestration platform for managing AI coding tasks via GitHub, featuring autonomous and manual modes. Integrates with Google's **JULES** cloud service and other AI providers.

## Features

- **Multi-Repository Support**: Manage multiple projects from a single dashboard.
- **Local File System Integration**: Point the orchestrator to your local folder containing GitHub repositories.
- **Clone Repository**: Directly clone new repositories into your projects folder from the UI.
- **Autonomous Loop**: Automatically generate tasks, write code, run tests, and merge PRs.
- **AI Provider Integration**: Connect to Google JULES, Gemini, Claude, OpenAI, and other AI services.
- **Real-time Terminal**: View live logs of agent actions and system status.
- **Coverage Reports**: Track test coverage across your projects.

## Quick Start

1. **Install Dependencies**:
   ```bash
   npm install
   ```

2. **Configure Environment**:
   ```bash
   cp .env.example .env
   ```
   Edit `.env` and add your API keys (Gemini, Claude, OpenAI, GitHub, etc.).

3. **Start the Server**:
   ```bash
   npm run dev
   ```

4. **Configure Local Path**:
   - Open the app in your browser (`http://localhost:3847`).
   - Click the **Settings (gear icon)** in the sidebar.
   - Enter the absolute path to your repositories folder (e.g., `/Users/username/code/repos`).
   - Click **Save Configuration**.

5. **Connect Projects**:
   - The app automatically scans the configured folder for subdirectories containing `TODO_FOR_JULES.md`.
   - Use the **"+" (Plus icon)** to clone new repositories directly into your projects folder.

## Project Structure

```
├── /projects          # Default location for repositories
├── /src               # React frontend application
├── /python_agent      # LangGraph-based Python agent
├── server.ts          # Express backend coordinating AI agents
└── docker-compose.yml # Container orchestration (optional)
```

## Adding a Project

Create a new directory with a `TODO_FOR_JULES.md` file:

```bash
mkdir -p projects/my-new-project
touch projects/my-new-project/TODO_FOR_JULES.md
```

Format `TODO_FOR_JULES.md` as a markdown checklist:

```markdown
- [ ] Setup initial routing
- [ ] Implement user authentication
- [ ] Write unit tests for auth service
```

## Configuration

Click the **Settings (gear icon)** to configure:
- **Full Automation Mode**: Enable/disable autonomous task execution
- **Auto-Task Generation**: Automatically generate tasks from project analysis
- **Test Generation Prompts**: Configure AI behavior for test creation
- **API Keys**: Manage AI provider credentials

## Environment Variables

| Variable | Description |
|----------|-------------|
| `GEMINI_API_KEY` | Google Gemini AI API key |
| `CLAUDE_API_KEY` | Anthropic Claude API key |
| `OPENAI_API_KEY` | OpenAI API key |
| `GITHUB_TOKEN` | GitHub personal access token |
| `APP_URL` | Application base URL |

See `.env.example` for the full list of configurable variables.

## Docker Deployment (Optional)

Run the orchestrator in a containerized environment:

```bash
docker-compose up -d
```

## License

MIT
