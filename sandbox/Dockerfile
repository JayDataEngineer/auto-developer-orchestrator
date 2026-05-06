FROM ubuntu:22.04

ENV DEBIAN_FRONTEND=noninteractive
ENV DISPLAY=:99
ENV RESOLUTION=1280x720x24
ENV CHROME_REMOTE_DEBUGGING_PORT=9222
ENV NOVNC_PORT=6080

# Install core dependencies — everything a dev sandbox needs
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
    x11vnc \
    fluxbox \
    novnc \
    websockify \
    supervisor \
    socat \
    wmctrl \
    xdotool \
    xclip \
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

# Install Google Chrome stable
RUN wget -q -O /tmp/chrome.deb https://dl.google.com/linux/direct/google-chrome-stable_current_amd64.deb \
    && apt-get update \
    && apt-get install -y --no-install-recommends /tmp/chrome.deb \
    && rm /tmp/chrome.deb \
    && rm -rf /var/lib/apt/lists/* \
    && ln -sf /usr/bin/google-chrome-stable /usr/bin/google-chrome

# Set up noVNC
RUN ln -sf /usr/share/novnc/vnc.html /usr/share/novnc/index.html

# Supervisor config to manage all services
RUN mkdir -p /var/log/supervisor
COPY supervisord.conf /etc/supervisor/conf.d/sandbox.conf

# Working directory
RUN mkdir -p /workspace
WORKDIR /workspace

EXPOSE 9222 6080

CMD ["/usr/bin/supervisord", "-c", "/etc/supervisor/supervisord.conf"]
