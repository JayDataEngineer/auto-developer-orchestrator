# Invest org sandbox container — Python 3.12 + uv.
# Used by docker-compose.yml for users who want a clean env.
#
# Build:
#   docker build -t invest-sandbox orgs/invest/
#
# Run (mounts the project so files persist outside the container):
#   docker run --rm -v "$PWD":/workspace -w /workspace invest-sandbox python3 sandbox/fetch_data.py

FROM python:3.12-slim

# uv for fast dependency install
COPY --from=ghcr.io/astral-sh/uv:latest /uv /usr/local/bin/uv

WORKDIR /workspace

# Install requirements first (cache layer)
COPY requirements.txt /tmp/requirements.txt
RUN uv pip install --system -r /tmp/requirements.txt

# Sandbox scripts + config
COPY sandbox/ /workspace/sandbox/
COPY config/  /workspace/config/

# Default entrypoint — show help
CMD ["python3", "/workspace/sandbox/fetch_data.py", "--help"]
