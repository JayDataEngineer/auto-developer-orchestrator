#!/bin/bash
# Configure Xvfb for dynamic RANDR resize support.
#
# Xvfb starts at 4096x2160 (giving RANDR a large maximum).
# This script disables the fixed "screen" output so that
# x11vnc's XRRSetScreenSize can resize to any size within
# the RANDR limits without CRTC conflicts.
#
# Usage: setup-display.sh [display] [width] [height]

DISPLAY="${1:-:99}"
INIT_W="${2:-1280}"
INIT_H="${3:-720}"

# Wait for Xvfb to accept connections
for i in $(seq 1 50); do
    xdpyinfo -display "$DISPLAY" >/dev/null 2>&1 && break
    sleep 0.1
done

# Disable the fixed output — removes CRTC constraint so
# XRRSetScreenSize works for any size within RANDR limits
xrandr --display "$DISPLAY" --output screen --off 2>/dev/null || true

# Set initial framebuffer to desired desktop resolution
xrandr --display "$DISPLAY" --fb "${INIT_W}x${INIT_H}" 2>/dev/null || true

echo "Display $DISPLAY: fb=${INIT_W}x${INIT_H}, randr_max=4096x2160, output=off"
