#!/bin/bash
# Generate fluxbox menu entries from installed .desktop files.
# Writes to ~/.fluxbox/apps-menu which is [include]d by the main menu.
#
# Called:
#   - At container startup (via supervisord)
#   - On "Reload Menu" from the fluxbox right-click menu
#   - After apt installs new applications
#
# Output: static fluxbox menu XML snippet at ~/.fluxbox/apps-menu

DATA_DIRS="/usr/share/applications ${XDG_DATA_HOME:-$HOME/.local/share}/applications"
OUTFILE="$HOME/.fluxbox/apps-menu"

tmpfile=$(mktemp)
echo "[submenu] (Applications)" > "$tmpfile"

for dir in $DATA_DIRS; do
	[ -d "$dir" ] || continue
	for desktop_file in "$dir"/*.desktop; do
		[ -f "$desktop_file" ] || continue

		# Skip hidden / NoDisplay entries
		grep -qiE "^(NoDisplay|Hidden)=true" "$desktop_file" && continue

		# Skip CLI-only / system config tools
		grep -qiE "^Categories=.*(ConsoleOnly|Settings;System)" "$desktop_file" && continue

		name=$(grep -m1 "^Name=" "$desktop_file" | cut -d= -f2-)
		exec_cmd=$(grep -m1 "^Exec=" "$desktop_file" | cut -d= -f2-)
		[ -z "$name" ] || [ -z "$exec_cmd" ] && continue

		# Strip field codes like %f, %u, %U, etc.
		exec_cmd=$(echo "$exec_cmd" | sed -E 's/ %[fFuUdDnNickvm]//g')

		echo "[exec] ($name) {$exec_cmd}"
	done
done | sort -t'(' -k2 -u >> "$tmpfile"

echo "[end]" >> "$tmpfile"
mv "$tmpfile" "$OUTFILE"
