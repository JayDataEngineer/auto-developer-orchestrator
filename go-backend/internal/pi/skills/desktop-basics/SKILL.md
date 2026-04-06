---
name: desktop-basics
description: Essential desktop interaction patterns for the Xfce4 sandbox environment. Covers right-click menus, terminal, file manager, window management, screenshots, and clipboard. Use when working with the graphical desktop.
metadata:
  {
    "os": ["linux"],
    "desktop": "xfce4",
    "requires": { "bins": ["xdotool", "xfce4-terminal", "xfce4-popup-whiskermenu", "scrot"] }
  }
---

# Desktop Basics

This sandbox runs **XFCE4 desktop** on a virtual display (Xvfb). Use these patterns to interact with it.

## Window Manager

The window manager is **xfwm4**. Windows have title bars, can be resized, minimized, and moved.

### Focus a Window by Title

```bash
xdotool search --name "Terminal" windowactivate --sync
```

### Maximize / Minimize / Close

```bash
# Maximize active window
xdotool key ctrl+super+Up

# Close active window
xdotool key alt+F4

# Minimize (iconify)
xdotool windowminimize $(xdotool getactivewindow)
```

### Move Window to Specific Position

```bash
xdotool getactivewindow windowmove 0 0  # top-left
xdotool getactivewindow windowsize 1280 720
```

## Whisker Menu (Start Menu)

The Whisker Menu is the main application launcher (bottom-left panel icon).

### Open Whisker Menu

```bash
xfce4-popup-whiskermenu
# Then use xdotool to type a search and press Enter
xdotool type "terminal"
xdotool key Return
```

### Alternative: Run Dialog

```bash
# Open the Run Application dialog (Alt+F2 equivalent)
xfce4-appfinder
# Then type the app name
```

### List Installed Applications

```bash
# All desktop entries
ls /usr/share/applications/*.desktop | head -30

# Search for apps
ls /usr/share/applications/ | grep -i telegram
```

## Terminal

The terminal emulator is **xfce4-terminal**.

### Open Terminal

```bash
xfce4-terminal &
# Wait a moment for it to appear
sleep 1
# Focus it
xdotool search --name "Terminal" windowactivate --sync
```

### Open Terminal with a Command

```bash
xfce4-terminal -e "bash -c 'ls -la /sandbox/workspace; exec bash'" &
```

### Open Multiple Terminal Tabs

```bash
# Open new tab in existing terminal
xdotool key --clearmodifiers ctrl+shift+t
# Or open a new terminal window
xfce4-terminal &
```

### Type Commands in Terminal

```bash
# Make sure terminal is focused first
xdotool search --name "Terminal" windowactivate --sync
sleep 0.5

# Type the command
xdotool type "sudo apt update"
# Press Enter
xdotool key Return
```

### Copy / Paste in Terminal

```bash
# Copy: Ctrl+Shift+C
xdotool key --clearmodifiers ctrl+shift+c

# Paste: Ctrl+Shift+V
xdotool key --clearmodifiers ctrl+shift+v
```

## Right-Click Context Menu

Right-click behavior depends on where you click:

### Desktop Right-Click (XFCE Desktop Menu)

```bash
# Right-click on desktop (coordinates: center-ish of 1280x720)
xdotool mousemove 640 360
xdotool click --delay 100 3  # button 3 = right click
```

The desktop right-click menu shows:
- Create Launcher
- Create Document → Empty File / From Template
- Open Terminal Here
- Desktop Settings
- Properties

### Application Right-Click

Right-clicking inside applications shows their context menus. Position the mouse over the target element first.

### Panel Right-Click

Right-click the bottom panel → Panel → Panel Preferences to customize.

## File Manager

The file manager is **Thunar**.

### Open File Manager

```bash
thunar /sandbox/workspace &
sleep 1
xdotool search --name "Thunar" windowactivate --sync
```

### Navigate in Thunar

```bash
# Open a folder: double-click
xdotool mousemove 300 200
xdotool click --delay 50 1
xdotool click --delay 50 1

# Open with Enter (when folder is selected)
xdotool key Return

# Go up: Alt+Up
xdotool key alt+Up

# Go home: Alt+Home
xdotool key alt+Home
```

## Screenshots (Beyond CDP)

Use `scrot` for full desktop screenshots (including non-browser apps):

```bash
# Full desktop screenshot
scrot /tmp/desktop-shot.png

# Screenshot with 2-second delay
scrot -d 2 /tmp/desktop-shot.png

# Select area screenshot
scrot -s /tmp/selection.png

# View the screenshot
xdg-open /tmp/desktop-shot.png &
```

## Clipboard

```bash
# Copy text to clipboard
echo "some text" | xclip -selection clipboard

# Paste from clipboard
xclip -selection clipboard -o

# Copy file contents to clipboard
cat /tmp/output.txt | xclip -selection clipboard
```

## Keyboard Shortcuts (XFCE Default)

| Shortcut | Action |
|----------|--------|
| `Alt+F2` | Run Application dialog |
| `Alt+F4` | Close window |
| `Alt+Tab` | Switch windows |
| `Alt+F7` | Move window (arrow keys) |
| `Alt+F8` | Resize window (arrow keys) |
| `Ctrl+Alt+D` | Show/hide desktop |
| `Ctrl+Alt+Left/Right` | Switch workspace |
| `Super` | Open Whisker Menu |
| `Print` | Screenshot (scrot) |

## Common Application Launch Patterns

### By Desktop Entry Name

```bash
# Find the exec line
grep -i "^Exec=" /usr/share/applications/telegram.desktop
# Launch it directly
$(grep -i "^Exec=" /usr/share/applications/telegram.desktop | head -1 | cut -d= -f2 | awk '{print $1}') &
```

### By Binary Name

```bash
which telegram-desktop && telegram-desktop &
which code && code &
which firefox && firefox &
which chromium-browser && chromium-browser &
```

### Installed Applications Quick List

```bash
# Common apps check
for app in telegram-desktop code firefox chromium-browser libreoffice vlc gimp; do
  if command -v $app &>/dev/null; then echo "INSTALLED: $app"; fi
done
```

## Workflow Patterns

### Install Software via Terminal

```bash
# 1. Open terminal
xfce4-terminal &
sleep 1
xdotool search --name "Terminal" windowactivate --sync
sleep 0.5

# 2. Type install command
xdotool type "sudo apt update && sudo apt install -y telegram-desktop"
xdotool key Return

# 3. Wait for completion (check with scrot or CDP)
sleep 30
scrot /tmp/after-install.png
```

### Launch Application After Install

```bash
# Launch via desktop entry or binary
telegram-desktop &
sleep 2
# Screenshot to verify
scrot /tmp/app-running.png
```

### Open Settings / Preferences

```bash
# XFCE Settings Manager
xfce4-settings-manager &

# Or individual settings panels
xfce4-display-settings &
xfce4-appearance-settings &
```

## Important Notes

- **Always wait after launching apps** — use `sleep 1` or `sleep 2` before interacting
- **Focus windows before typing** — use `xdotool search --name "..." windowactivate --sync`
- **Use `scrot` for non-browser screenshots** — the CDP screenshot only captures Chrome
- **Desktop is 1280x720** — the Xvfb virtual display resolution
- **Run as root** — the sandbox user is root, so `sudo` may not be needed for most commands
