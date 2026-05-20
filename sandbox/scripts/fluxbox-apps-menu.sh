#!/bin/bash
# Generate fluxbox menu entries from installed .desktop files.
# Used as a dynamic menu by fluxbox's [include] directive.
#
# Output format: fluxbox menu XML snippet, e.g.:
#   [exec] (Google Chrome) {/usr/bin/google-chrome-stable}
#   [exec] (Terminal) {xterm}

DATA_DIRS="/usr/share/applications ${XDG_DATA_HOME:-$HOME/.local/share}/applications"

has_display() {
	# Quick check if the app needs a display (GUI vs CLI)
	grep -qiE "^(NoDisplay|Hidden)=true" "$1" && return 1
	grep -qiE "^Categories=.*(Settings|System|ConsoleOnly)" "$1" && return 1
	return 0
}

echo "[submenu] (Applications)"

# Scan standard locations
for dir in $DATA_DIRS; do
	[ -d "$dir" ] || continue
	for desktop_file in "$dir"/*.desktop; do
		[ -f "$desktop_file" ] || continue
		has_display "$desktop_file" || continue

		name=$(grep -m1 "^Name=" "$desktop_file" | cut -d= -f2-)
		exec_cmd=$(grep -m1 "^Exec=" "$desktop_file" | cut -d= -f2-)
		[ -z "$name" ] || [ -z "$exec_cmd" ] && continue

		# Strip field codes like %f, %u, %U, etc.
		exec_cmd=$(echo "$exec_cmd" | sed -E 's/ %[fFuUdDnNickvm]//g')

		echo "[exec] ($name) {$exec_cmd}"
	done
done | sort -t'(' -k2 -u

echo "[end]"
