# Orchestrator — API Usage Examples

All examples use the running Go backend at `localhost:3847`.
Test with: `python3 -c "$(curl -s http://localhost:3847/api/health)"` or `curl http://localhost:3847/api/health`.

---

## 1. Health Check

```bash
curl -s http://localhost:3847/api/health
```

Returns: `{"status":"ok","llm":"healthy","sandbox":"available","version":"0.2.0"}`

---

## 2. List Available Tools

```bash
curl -s http://localhost:3847/api/tools | python3 -c "
import json,sys
tools = json.load(sys.stdin).get('tools',[])
for t in tools:
    print(f'  {t[\"name\"]}')
"
```

Returns 36 tools: bash, file_read, file_write, 18 MCP research tools, 13 MCP media tools.

---

## 3. Direct Tool Execution (no LLM)

All tools can be called directly via `POST /api/tools/exec` — no model thinking required.

### 3a. Web Search

```bash
curl -s http://localhost:3847/api/tools/exec -X POST \
  -H 'Content-Type: application/json' \
  -d '{"tool":"search","args":{"query":"AAPL stock price","top_k":3}}'
```

### 3b. Deep Research (search + scrape combined)

```bash
curl -s http://localhost:3847/api/tools/exec -X POST \
  -H 'Content-Type: application/json' \
  -d '{"tool":"research","args":{"query":"current S&P 500 PE ratio","max_results":2}}'
```

### 3c. Scrape a Page

```bash
curl -s http://localhost:3847/api/tools/exec -X POST \
  -H 'Content-Type: application/json' \
  -d '{"tool":"scrape","args":{"url":"https://httpbin.org/html"}}'
```

### 3d. Discover URLs (sitemap / Common Crawl)

```bash
curl -s http://localhost:3847/api/tools/exec -X POST \
  -H 'Content-Type: application/json' \
  -d '{"tool":"map","args":{"domain":"docs.python.org","max_urls":10}}'
```

### 3e. Extract Structured Data

```bash
curl -s http://localhost:3847/api/tools/exec -X POST \
  -H 'Content-Type: application/json' \
  -d '{"tool":"extract","args":{"url":"https://techcrunch.com","schema_type":"news"}}'
```

Schema types: `ecommerce`, `news`, `jobs`, `blog`, `social`, `products`.

### 3f. Crawl a Site (follow links)

```bash
curl -s http://localhost:3847/api/tools/exec -X POST \
  -H 'Content-Type: application/json' \
  -d '{"tool":"crawl","args":{"url":"https://httpbin.org","max_pages":3,"max_depth":1}}'
```

### 3g. HTML → Clean Markdown

```bash
curl -s http://localhost:3847/api/tools/exec -X POST \
  -H 'Content-Type: application/json' \
  -d '{"tool":"process_html","args":{"html":"<html><body><h1>Title</h1><script>noise</script></body></html>","url":"https://example.com"}}'
```

### 3h. List Documentation Sources

```bash
curl -s http://localhost:3847/api/tools/exec -X POST \
  -H 'Content-Type: application/json' \
  -d '{"tool":"docs_list_sources","args":{}}'
```

### 3i. Scrape Stats & Domain Tracking

```bash
curl -s http://localhost:3847/api/tools/exec -X POST \
  -H 'Content-Type: application/json' \
  -d '{"tool":"stats","args":{}}'
```

### 3j. Image Analysis (vision)

```bash
curl -s http://localhost:3847/api/tools/exec -X POST \
  -H 'Content-Type: application/json' \
  -d '{"tool":"analyze_image","args":{"imageSource":"https://images.unsplash.com/photo-1511707171634-5f897ff02aa9?w=400","prompt":"Describe this image"}}'
```

### 3k. Extract Color Palette

```bash
curl -s http://localhost:3847/api/tools/exec -X POST \
  -H 'Content-Type: application/json' \
  -d '{"tool":"extract_colors","args":{"imageSource":"https://www.google.com/images/branding/googlelogo/2x/googlelogo_color_272x92dp.png","color_count":4}}'
```

### 3l. Object Detection

```bash
curl -s http://localhost:3847/api/tools/exec -X POST \
  -H 'Content-Type: application/json' \
  -d '{"tool":"detect_objects","args":{"imageSource":"https://images.unsplash.com/photo-1511707171634-5f897ff02aa9?w=400"}}'
```

### 3m. Image Tagging

```bash
curl -s http://localhost:3847/api/tools/exec -X POST \
  -H 'Content-Type: application/json' \
  -d '{"tool":"tag_image","args":{"imageSource":"https://images.unsplash.com/photo-1511707171634-5f897ff02aa9?w=400"}}'
```

### 3n. Proxy Configuration

```bash
curl -s http://localhost:3847/api/tools/exec -X POST \
  -H 'Content-Type: application/json' \
  -d '{"tool":"proxy_status","args":{}}'
```

---

## 4. Agent SSE Pipeline

Send a prompt to the agent, stream events back via SSE.

```bash
curl -sN http://localhost:3847/api/pux/prompt \
  -H 'Content-Type: application/json' \
  -d '{"message":"Run: echo hello-from-sandbox","project":"deep-research-engine","agentId":"test"}'
```

Events received:
- `agent_spawned` — agent created
- `thinking_delta` — model reasoning (streamed)
- `tool_execution_start` / `tool_execution_end` — tool calls
- `text_delta` — final response text
- `agent_end` — run complete
- `data: [DONE]` — stream ended

---

## 5. Python SDK

```python
from orch import OrchestratorClient

client = OrchestratorClient(base_url="http://localhost:3847")

# Health
print(client.health())

# List tools
tools = client.tools()
print(f"{len(tools)} tools available")

# Direct tool execution
results = client.mcp_search("AAPL stock price", top_k=3)
for r in results:
    print(f"  {r['title']}: {r['url']}")

# Scrape a page
page = client.scrape("https://httpbin.org/html")
print(page["content"][:200])

# Run agent prompt (blocking)
result = client.prompt("Read the file config.yaml", project="deep-research-engine")
print(result)
```

Install: `cd sdk/python && uv venv && source .venv/bin/activate && uv pip install -e .`

---

## 6. CLI (Terminal)

```bash
# Build
cd go-backend && go build -o orch ./cmd/cli/

# Agent prompt (streams SSE as text)
./orch agent prompt "list files in the project" -p deep-research-engine

# Agent prompt (JSON output)
./orch agent prompt "what tools are available" -p deep-research-engine -o json

# List sandboxes
./orch sandbox list

# List projects
./orch project list
```

---

## 7. TUI (Interactive Terminal)

```bash
task chat                                    # Interactive TUI with default project
cd ts-tui-pi && bun run src/main.ts --project invest-bot
```

---

## 8. Invest-Bot Observability

### Langfuse Scores Posted Automatically

When invest-bot tools run through the agent, the Langfuse hook posts these scores:

| Score | Type | Description |
|-------|------|-------------|
| equity | NUMERIC | Portfolio equity (USD) |
| total_pnl | NUMERIC | Cumulative P&L |
| daily_pnl | NUMERIC | Daily P&L |
| sharpe_ratio | NUMERIC | Annualized Sharpe ratio |
| max_drawdown | NUMERIC | Maximum drawdown |
| win_rate | NUMERIC | Win rate (0-1) |
| profit_factor | NUMERIC | Gross wins / gross losses |
| return_pct | NUMERIC | Total return % |
| regime_composite | NUMERIC | Market regime (0-100) |
| portfolio_heat | NUMERIC | Risk exposure % |
| prediction_accuracy | NUMERIC | Signal accuracy (0-1) |
| is_simulation | BOOLEAN | Backtest vs live |
| workflow_type | NUMERIC | 1=trade, 2=scan, 3=backtest, 4=metrics, 5=review |
| signal:{TICKER} | NUMERIC | 1=buy, -1=sell, 0=hold |
| signal_confidence:{TICKER} | NUMERIC | Model confidence (0-1) |
| regime | CATEGORICAL | Market regime label |

### LLM Judge (Finance-Specific Scoring)

```bash
cd evaluator
python3 invest_judge.py              # Score unscored invest traces
python3 invest_judge.py --dry-run    # Preview scores
python3 invest_judge.py --watch      # Continuous polling
```

### Weekly Correlation Analytics

```bash
cd evaluator
python3 correlations.py              # 7-day lookback
python3 correlations.py --days 30    # 30-day lookback
python3 correlations.py --dry-run    # Preview
```

---

## 9. Scrape Stats & Monitoring

```bash
# View scrape statistics
curl -s http://localhost:3847/api/tools/exec -X POST \
  -H 'Content-Type: application/json' \
  -d '{"tool":"stats","args":{"hours":24}}'

# View tracked domains
curl -s http://localhost:3847/api/tools/exec -X POST \
  -H 'Content-Type: application/json' \
  -d '{"tool":"domains","args":{}}'

# Reset if something's wrong
curl -s http://localhost:3847/api/tools/exec -X POST \
  -H 'Content-Type: application/json' \
  -d '{"tool":"clear_blacklist","args":{}}'
```
