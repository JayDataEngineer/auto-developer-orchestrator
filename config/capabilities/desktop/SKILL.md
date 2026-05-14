# Desktop Capability

You control a Linux desktop environment via xdotool.

## Available Functions

desktop_observe(): Screenshot + OCR element detection + window list
  USE FIRST for any task. Returns elements with IDs, text, coordinates.
  This is the primary way to understand the desktop state.

desktop_screenshot(): Capture a screenshot
  Quick visual check without element data.
  Use when you just need to see what's on screen.

desktop_click(x, y, button): Click at coordinates
  Coordinates are 0-1000 normalized (auto-scaled to resolution).
  button: 1=left, 2=middle, 3=right.

desktop_type(text): Type text via keyboard
  Types character by character. Use for entering text in fields.

desktop_key(key): Press a key or key combo
  Examples: "Return", "Escape", "ctrl+c", "alt+F4", "super"
  For combos, use + separator: "ctrl+shift+t"

app_interact(action, params, profile): Execute a semantic action from an app profile
  Translates high-level actions to key/mouse commands automatically.
  Example: app_interact(action="jump") → presses spacebar

app_profile(operation, profile, content): Manage app interaction profiles
  list: show available profiles
  show: display a profile's actions
  select: set active profile for app_interact
  create: create a new profile from YAML
  update: update a profile (merge or replace)

## Workflow
1. desktop_observe() to see the current state and find elements
2. desktop_click() or desktop_key() to interact
3. desktop_observe() again to verify changes
4. For apps/games: app_profile("select", profile="name"), then app_interact(action="...")

## Tips
- ALWAYS observe before clicking — coordinates change between windows
- For games/apps, load an interaction profile with app_profile first
- Use desktop_key for keyboard shortcuts, desktop_type for text input
- desktop_observe returns window list — check you're in the right window
