# Video Production Container — Manim + Kokoro TTS + ffmpeg
#
# Full-stack educational video producer:
#   - Manim 0.19   — animated diagrams, equations, charts
#   - Kokoro TTS   — narration voiceover (GPU-friendly ONNX)
#   - ffmpeg       — render, mux, QC
#   - poppler      — PDF/text asset extraction
#   - espeak-ng    — TTS backend for Kokoro
#   - LaTeX        — Manim equation rendering
#
# Build:
#   docker compose build
#
# Run:
#   docker compose up -d
#   docker compose exec video-producer bash

FROM python:3.11-slim-bookworm

ENV PYTHONDONTWRITEBYTECODE=1 \
    PYTHONUNBUFFERED=1 \
    VIDEO_PRODUCTION_ROOT=/workspace/video-productions \
    PIP_NO_CACHE_DIR=1

WORKDIR /app

# System deps: ffmpeg, LaTeX (for Manim math), poppler (PDF extraction),
# espeak-ng + libsndfile (Kokoro TTS), build tools for manim deps
# manim needs: pkg-config, cairo, pango, gobject-introspection, glib
# PyGObject + pycairo installed via apt (python3-gi + python3-cairo) to avoid
# meson build failures with pip's PyGObject wheel.
RUN apt-get update && apt-get install -y --no-install-recommends \
    ffmpeg \
    texlive texlive-latex-extra texlive-fonts-recommended \
    texlive-science cm-super dvipng \
    poppler-utils \
    espeak-ng espeak-ng-data libsndfile1-dev \
    build-essential pkg-config \
    libcairo2-dev libpango1.0-dev \
    libgirepository1.0-dev libglib2.0-dev \
    python3-gi python3-cairo python-is-python3 \
    curl ca-certificates \
    && rm -rf /var/lib/apt/lists/*

# Python deps
COPY requirements.txt .
RUN pip install --no-cache-dir -r requirements.txt

# Skill scripts — the org ships its skill under
# skills/video-production/{SKILL.md,scripts/,references/} (Agent Skills spec).
# Only the scripts/ are baked into the image (SKILL.md + references/ are read
# from the bind-mounted project by SkillsMiddleware). Each script is symlinked
# onto PATH so the agent invokes them as bare commands: synth_kokoro,
# archive_video, host_video, init_video_job.
COPY skills/video-production/scripts/ /app/skills/scripts/
RUN chmod +x /app/skills/scripts/*.py \
    && for f in /app/skills/scripts/*.py; do \
        ln -sf "$f" /usr/local/bin/"$(basename "$f" .py)"; \
    done

# Workspace volume
RUN mkdir -p /workspace/video-productions/{jobs,backups,serve} \
    && ln -sf /workspace/video-productions /app/workspace

# Stable skill path agents can rely on regardless of who mounts what.
# /sandbox/workspace is what the Pux sandbox manager binds the project dir to.
# DO NOT symlink /sandbox/workspace to anything — Docker resolves the symlink
# chain at bind-mount time and would redirect the project bind to whatever the
# symlink points at (here, /workspace/video-productions). That collides with
# the org-declared named volume on the same path and shadows it. The previous
# `ln -sf /app/workspace /sandbox/workspace` caused exactly that collision;
# removing it lets the Pux bind-mount and the named volume coexist.
RUN mkdir -p /sandbox \
    && ln -sf /app/skills /sandbox/skills

VOLUME ["/workspace/video-productions"]

CMD ["python", "-c", "import time; print('Video Production container ready'); time.sleep(86400)"]
