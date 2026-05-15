#!/bin/bash
# Direct KasmVNC startup — bypasses vncserver Perl wrapper
# The wrapper has issues with Unix socket creation on first try
set -e

DISPLAY_NUM=${1:-99}
WEBSOCKET_PORT=${2:-8444}
GEOMETRY=${3:-1280x720}

# Clean up stale files
rm -f "/tmp/.X${DISPLAY_NUM}-lock" "/tmp/.X11-unix/X${DISPLAY_NUM}" 2>/dev/null || true

# Set password
printf 'password\npassword\n' | vncpasswd -w -u root

# Create xauth entry
xauth -f /root/.Xauthority add ":${DISPLAY_NUM}" MIT-MAGIC-COOKIE-1 "$(xxd -l 16 -p /dev/urandom | head -1)" 2>/dev/null || true

# Start Xvnc directly with all options the wrapper would set
exec /usr/bin/Xvnc ":${DISPLAY_NUM}" \
    -geometry "${GEOMETRY}" \
    -websocketPort "${WEBSOCKET_PORT}" \
    -depth 24 \
    -rfbauth /root/.vnc/passwd \
    -rfbwait 30000 \
    -rfbport 5901 \
    -httpd /usr/share/kasmvnc/www \
    -auth /root/.Xauthority \
    -desktop "sandbox:${DISPLAY_NUM}" \
    -Log *:stdout:30 \
    -interface 0.0.0.0 \
    -UseIPv4 1 \
    -UseIPv6 0 \
    -FrameRate 30 \
    -cert /etc/ssl/certs/ssl-cert-snakeoil.pem \
    -key /etc/ssl/private/ssl-cert-snakeoil.key \
    -sslOnly 0 \
    -KasmPasswordFile /root/.kasmpasswd \
    -BlacklistThreshold 5 \
    -BlacklistTimeout 10 \
    -fp /usr/share/fonts/X11//misc
