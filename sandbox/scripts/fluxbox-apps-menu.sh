#!/bin/bash
# Generate fluxbox menu entries and desktop icon symlinks from installed .desktop files.
#
# Called:
#   - At container startup (via supervisord)
#   - On "Reload Menu" from the fluxbox right-click menu
#   - After apt installs new applications
#
# Outputs:
#   ~/.fluxbox/apps-menu   — static fluxbox menu snippet ([include]d by main menu)
#   ~/Desktop/*.desktop    — symlinks for desktop icons (shown by pcmanfm --desktop)

DATA_DIRS="/usr/share/applications ${XDG_DATA_HOME:-$HOME/.local/share}/applications"
MENU_FILE="$HOME/.fluxbox/apps-menu"
DESKTOP_DIR="$HOME/Desktop"

mkdir -p "$DESKTOP_DIR"

# ── Generate fluxbox menu ──
tmpfile=$(mktemp)
echo "[submenu] (Applications)" > "$tmpfile"

for dir in $DATA_DIRS; do
	[ -d "$dir" ] || continue
	for desktop_file in "$dir"/*.desktop; do
		[ -f "$desktop_file" ] || continue

		# Skip hidden / NoDisplay entries
		grep -qiE "^(NoDisplay|Hidden)=true" "$desktop_file" && continue

		# Skip CLI-only / system config tools
		grep -qiE "^Categories=.*(ConsoleOnly)" "$desktop_file" && continue

		name=$(grep -m1 "^Name=" "$desktop_file" | cut -d= -f2-)
		exec_cmd=$(grep -m1 "^Exec=" "$desktop_file" | cut -d= -f2-)
		[ -z "$name" ] || [ -z "$exec_cmd" ] && continue

		# Strip field codes like %f, %u, %U, etc.
		exec_cmd=$(echo "$exec_cmd" | sed -E 's/ %[fFuUdDnNickvm]//g')

		echo "[exec] ($name) {$exec_cmd}"
	done
done | sort -t'(' -k2 -u >> "$tmpfile"

echo "[end]" >> "$tmpfile"
mv "$tmpfile" "$MENU_FILE"

# ── Symlink new apps to ~/Desktop for pcmanfm desktop icons ──
# Only symlinks apps that don't already have a desktop shortcut.
for dir in $DATA_DIRS; do
	[ -d "$dir" ] || continue
	for desktop_file in "$dir"/*.desktop; do
		[ -f "$desktop_file" ] || continue
		base=$(basename "$desktop_file")

		# Skip if we already have a pre-built shortcut or existing symlink
		[ -e "$DESKTOP_DIR/$base" ] && continue

		# Skip hidden / NoDisplay
		grep -qiE "^(NoDisplay|Hidden)=true" "$desktop_file" && continue

		# Skip CLI-only
		grep -qiE "^Categories=.*(ConsoleOnly)" "$desktop_file" && continue

		ln -sf "$desktop_file" "$DESKTOP_DIR/$base"
	done
done
