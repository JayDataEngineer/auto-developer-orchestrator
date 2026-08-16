# browser-specialist workload container — runs INSIDE OpenSandbox.
#
# The OpenSandbox platform (server + execd + egress) is upstream; this image
# is only the workload: the mc_browser MCP server (HTTP) + Chromium, started
# as the sandbox entrypoint. It carries NO credentials — the model token
# lives in the trusted Aegra tier, which talks to this container over MCP
# HTTP. Host filesystem, host processes and host secrets are unreachable
# from here by construction.
#
# Build from the REPO ROOT (mc_browser.py is the single source of truth):
#   docker build -f deployments/browser-specialist/sandbox/Dockerfile \
#                -t browser-specialist-sandbox:latest .
#   (or: make aegra-sandbox-image)

FROM python:3.12-slim-bookworm

RUN apt-get update && apt-get install -y --no-install-recommends \
        chromium \
        ca-certificates \
        fonts-liberation \
        fonts-noto-color-emoji \
        procps \
    && rm -rf /var/lib/apt/lists/*

RUN pip install --no-cache-dir "fastmcp>=3.4" "seleniumbase>=4.40"

COPY plugins/browser/mc_browser.py /opt/mc_browser.py

ENV MC_BROWSER_TRANSPORT=http \
    MC_BROWSER_HOST=0.0.0.0 \
    MC_BROWSER_PORT=8765 \
    MC_BROWSER_HEADLESS=1 \
    MC_BROWSER_CHROME=/usr/bin/chromium \
    PYTHONUNBUFFERED=1

EXPOSE 8765

# The MCP server IS the workload: OpenSandbox starts the entrypoint when the
# sandbox comes up; Chrome spawns lazily on the first tool call.
ENTRYPOINT ["python", "/opt/mc_browser.py"]
