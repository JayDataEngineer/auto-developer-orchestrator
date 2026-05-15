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
ENV RESOLUTION=1280x720x24
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
    nodejs \
    npm \
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
    supervisor \
    socat \
    wmctrl \
    xdotool \
    xclip \
    xterm \
    imagemagick \
    scrot \
    tesseract-ocr \
    tesseract-ocr-eng \
    fonts-liberation \
    fonts-noto-cjk \
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
    && rm -rf /var/lib/apt/lists/*

# Python packages for web scraping, image processing, and stealth browsing
RUN python3 -m pip install seleniumbase requests beautifulsoup4 Pillow httpx \
    && rm -rf /root/.cache/pip

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
COPY scripts/start-kasmvnc.sh /usr/local/bin/start-kasmvnc.sh
RUN chmod +x /usr/local/bin/sb_agent.py /usr/local/bin/sb_server.py /usr/local/bin/desktop_observe.py /usr/local/bin/start-kasmvnc.sh

# Supervisor + log dir
RUN mkdir -p /var/log/supervisor

# VNC backend marker — overwritten by each target
ARG VNC_BACKEND=standard
ENV VNC_BACKEND=${VNC_BACKEND}

WORKDIR /sandbox/workspace


# ── Target: standard (TigerVNC + noVNC + websockify) ────────────────────────
FROM base AS standard

RUN apt-get update && apt-get install -y --no-install-recommends \
    x11vnc \
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
