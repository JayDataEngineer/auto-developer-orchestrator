# ─────────────────────────────────────────────────────────────────────────────
# Pux Sandbox — multi-target Dockerfile
#
# Two VNC backends:
#   standard  — TigerVNC + noVNC + websockify (lightweight, ~200MB)
#   kasm      — KasmVNC with H.264/WebRTC (low latency, ~400MB)
#
# Build:
#   docker build -t pux-sandbox          sandbox/             # default = standard
#   docker build --target kasm -t pux-sandbox-kasm sandbox/
#
# Run:
#   docker run -p 6080:6080 -p 9222:9222 pux-sandbox
#   docker run -p 8444:8444 -p 9222:9222 pux-sandbox-kasm
# ─────────────────────────────────────────────────────────────────────────────

# ── Shared base: all common packages ────────────────────────────────────────
FROM ubuntu:22.04 AS base

ENV DEBIAN_FRONTEND=noninteractive
ENV DISPLAY=:99
# Large virtual screen gives RANDR headroom for resize=remote.
# setup-display.sh sets the actual desktop to 1280x720 after Xvfb starts.
ENV RESOLUTION=4096x2160x24
ENV CHROME_REMOTE_DEBUGGING_PORT=9222

# Core dependencies — everything a dev sandbox needs
RUN apt-get update && apt-get install -y --no-install-recommends \
    curl \
    wget \
    jq \
    gnupg \
    ca-certificates \
    file \
    git \
    openssh-client \
    unzip \
    zip \
    tar \
    gzip \
    bzip2 \
    xz-utils \
    zstd \
    python3 \
    python3-pip \
    python3-venv \
    python3-tk \
    build-essential \
    make \
    cmake \
    pkg-config \
    rsync \
    tree \
    htop \
    vim \
    nano \
    less \
    grep \
    sed \
    gawk \
    findutils \
    procps \
    iputils-ping \
    netcat-openbsd \
    xvfb \
    fluxbox \
    pcmanfm \
    supervisor \
    socat \
    wmctrl \
    xdotool \
    xclip \
    xterm \
    x11vnc \
    x11-xserver-utils \
    imagemagick \
    ffmpeg \
    scrot \
    tesseract-ocr \
    tesseract-ocr-eng \
    fonts-liberation \
    fonts-noto-cjk \
    fonts-noto-color-emoji \
    fonts-freefont-ttf \
    fonts-dejavu-core \
    fonts-noto-core \
    fonts-noto-ui-core \
    fonts-noto-mono \
    libappindicator3-1 \
    libasound2 \
    libatk-bridge2.0-0 \
    libatk1.0-0 \
    libcups2 \
    libdbus-1-3 \
    libdrm2 \
    libgbm1 \
    libgtk-3-0 \
    libnspr4 \
    libnss3 \
    libx11-xcb1 \
    libxcomposite1 \
    libxdamage1 \
    libxrandr2 \
    libpng-dev \
    libjpeg-dev \
    libwebp-dev \
    xdg-utils \
    ripgrep \
    iptables \
    && rm -rf /var/lib/apt/lists/*

# Modern Node.js LTS. Ubuntu 22.04's apt ships Node.js v12, which can't run
# modern web apps (Next.js 14+, Vite, Remix, etc. require v18+). NodeSource
# provides a current LTS (v22.x) with npm bundled — replaces the removed
# system nodejs/npm packages.
RUN curl -fsSL https://deb.nodesource.com/setup_22.x | bash - \
    && apt-get install -y --no-install-recommends nodejs \
    && apt-get clean \
    && rm -rf /var/lib/apt/lists/*

# Python packages for web scraping, image processing, and stealth browsing.
# onnxruntime-genai: required by /usr/local/bin/describe_image.py for vision
# inference against Qwen3.5-2B-ONNX-OPT. The model itself is opt-in — operators
# run scripts/bootstrap-vision.sh from the host to download weights to
# <project>/.pux/models/. Without the weights, describe_image exits 2 with a
# friendly "run bootstrap-vision.sh" message and the rest of the server is
# unaffected. numpy comes in via Pillow's transitive deps; explicit here so
# the describe_image script can rely on it without import-time surprise.
#
# sentence-transformers + torch (CPU): required by the DRE embed helper
# (sandbox/embed.py) which loads microsoft/harrier-oss-v1-0.6b — the SOTA
# multilingual embedding model (1024-dim, MTEB v2 69.0, 94 languages). torch
# is pinned to the CPU wheel to avoid pulling ~2GB of CUDA blobs. The model
# weights download to HF_HOME on first use (~1.2GB).
RUN python3 -m pip install seleniumbase requests beautifulsoup4 Pillow httpx \
        onnxruntime-genai numpy \
    && python3 -m pip install --index-url https://download.pytorch.org/whl/cpu torch \
    && python3 -m pip install sentence-transformers \
    && rm -rf /root/.cache/pip
ENV HF_HOME=/data/hf-cache

# Chrome
RUN wget -q -O /tmp/chrome.deb https://dl.google.com/linux/direct/google-chrome-stable_current_amd64.deb \
    && apt-get update \
    && apt-get install -y --no-install-recommends /tmp/chrome.deb \
    && rm /tmp/chrome.deb \
    && rm -rf /var/lib/apt/lists/* \
    && ln -sf /usr/bin/google-chrome-stable /usr/bin/google-chrome

# Bun runtime
RUN curl -fsSL https://bun.sh/install | bash \
    && ln -sf /root/.bun/bin/bun /usr/local/bin/bun
ENV BUN_INSTALL="/root/.bun"

# Shared scripts
COPY scripts/sb_agent.py /usr/local/bin/sb_agent.py
COPY scripts/sb_server.py /usr/local/bin/sb_server.py
COPY scripts/desktop_observe.py /usr/local/bin/desktop_observe.py
COPY scripts/describe_image.py /usr/local/bin/describe_image.py
COPY scripts/start-kasmvnc.sh /usr/local/bin/start-kasmvnc.sh
COPY scripts/setup-display.sh /usr/local/bin/setup-display.sh
COPY scripts/fluxbox-apps-menu.sh /usr/local/bin/fluxbox-apps-menu.sh
COPY scripts/apply-egress-policy.sh /usr/local/bin/apply-egress-policy.sh
COPY scripts/refresh-egress-dns.sh /usr/local/bin/refresh-egress-dns.sh
COPY scripts/egress-dns-refresh-loop.sh /usr/local/bin/egress-dns-refresh-loop.sh
COPY scripts/seed-cookies.sh /usr/local/bin/seed-cookies.sh
RUN chmod +x /usr/local/bin/sb_agent.py /usr/local/bin/sb_server.py /usr/local/bin/desktop_observe.py /usr/local/bin/describe_image.py /usr/local/bin/start-kasmvnc.sh /usr/local/bin/setup-display.sh /usr/local/bin/fluxbox-apps-menu.sh /usr/local/bin/apply-egress-policy.sh /usr/local/bin/refresh-egress-dns.sh /usr/local/bin/egress-dns-refresh-loop.sh /usr/local/bin/seed-cookies.sh

# Fluxbox config — dark theme, dynamic app menu
COPY fluxbox/ /root/.fluxbox/

# Desktop shortcuts (shown by pcmanfm --desktop)
COPY desktop/ /root/Desktop/
RUN chmod +x /root/Desktop/*.desktop

# pcmanfm desktop config (dark background)
COPY pcmanfm/ /root/.config/pcmanfm/default/

# Supervisor + log dir
RUN mkdir -p /var/log/supervisor

# VNC backend marker — overwritten by each target
ARG VNC_BACKEND=standard
ENV VNC_BACKEND=${VNC_BACKEND}

WORKDIR /sandbox/workspace


# ── Target: standard (TigerVNC + noVNC + websockify) ────────────────────────
FROM base AS standard

RUN apt-get update && apt-get install -y --no-install-recommends \
    novnc \
    websockify \
    && rm -rf /var/lib/apt/lists/*

# noVNC symlink for index.html
RUN ln -sf /usr/share/novnc/vnc.html /usr/share/novnc/index.html

ENV NOVNC_PORT=6080

COPY supervisord.conf /etc/supervisor/conf.d/sandbox.conf

EXPOSE 9222 6080

CMD ["/usr/bin/supervisord", "-c", "/etc/supervisor/supervisord.conf"]


# ── Target: kasm (KasmVNC with H.264 encoding) ─────────────────────────────
FROM base AS kasm

# KasmVNC dependencies — only packages not already in base
RUN apt-get update && apt-get install -y --no-install-recommends \
    libpulse0 \
    libopus0 \
    libvpx7 \
    libx264-163 \
    libssl3 \
    locales \
    xfonts-base \
    && rm -rf /var/lib/apt/lists/*

# Install KasmVNC from official release
ARG KASMVNC_VERSION=1.3.2
RUN wget -q "https://github.com/kasmtech/KasmVNC/releases/download/v${KASMVNC_VERSION}/kasmvncserver_jammy_${KASMVNC_VERSION}_amd64.deb" -O /tmp/kasmvnc.deb \
    && apt-get update \
    && apt-get install -y /tmp/kasmvnc.deb \
    && rm /tmp/kasmvnc.deb \
    && rm -rf /var/lib/apt/lists/*

# Generate locale (KasmVNC needs UTF-8)
RUN locale-gen en_US.UTF-8
ENV LANG=en_US.UTF-8
ENV LANGUAGE=en_US:en
ENV LC_ALL=en_US.UTF-8

# KasmVNC config directory + desktop selection marker
RUN mkdir -p /root/.vnc /tmp/.vnc \
    && touch /root/.vnc/.de-was-selected

# KasmVNC default config — software H.264 encoding, no GPU required
COPY kasmvnc.yaml /root/.vnc/kasmvnc.yaml

# Xstartup script — KasmVNC launches fluxbox via this
COPY scripts/xstartup.sh /root/.vnc/xstartup
RUN chmod +x /root/.vnc/xstartup

ENV KASMVNC_PORT=8444

# KasmVNC supervisord config (replaces x11vnc + websockify + novnc)
COPY supervisord-kasm.conf /etc/supervisor/conf.d/sandbox.conf

EXPOSE 9222 8444

CMD ["/usr/bin/supervisord", "-c", "/etc/supervisor/supervisord.conf"]

# Default target
FROM standard
