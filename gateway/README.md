# Pux Messaging Gateway

Standalone adapters that bridge messaging platforms to the Pux agent.
Each adapter receives messages from a platform, sends them to the Go backend
via `/api/pux/prompt`, and streams the SSE response back to the platform.

## Architecture

```
Telegram/Discord/Slack  →  gateway adapter (Bun)  →  POST /api/pux/prompt
                                                       ↓
                                                    Go Backend (3847)
                                                       ↓
                                                    SSE stream
                                                       ↓
                              gateway adapter parses SSE  →  platform message API
```

## Running

```bash
# Telegram
cd gateway/telegram && bun install && bun run src/index.ts

# Discord
cd gateway/discord && bun install && bun run src/index.ts
```

## Environment Variables

| Variable | Required | Description |
|----------|----------|-------------|
| `PUX_BACKEND_URL` | No | Go backend URL (default: `http://localhost:3847`) |
| `TELEGRAM_BOT_TOKEN` | Yes (Telegram) | Bot token from @BotFather |
| `DISCORD_BOT_TOKEN` | Yes (Discord) | Bot token from Discord Developer Portal |
| `PUX_PROJECT` | No | Default project name |
| `PUX_ORG` | No | Default org name |

## Adding New Platforms

1. Create a new directory under `gateway/<platform>/`
2. Implement the `GatewayAdapter` interface:
   - `start()`: Connect to platform, listen for messages
   - `onMessage(userId, text, context)`: Forward to Pux backend
   - `sendResponse(userId, text)`: Send response back to platform
3. Use `PuxClient` from `gateway/shared.ts` to talk to the backend
