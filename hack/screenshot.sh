#!/usr/bin/env bash
# Regenerate the README screenshots from fabricated session data.
#
# The frames come from TestGenerateScreenshots, which builds a synthetic home
# directory (invented projects, invented conversations) and renders the TUI
# against it. Nothing here touches your real ~/.claude, ~/.gemini or ~/.copilot.
#
# Requires: freeze (github.com/charmbracelet/freeze), optionally pngquant.
set -euo pipefail

cd "$(dirname "$0")/.."

if ! command -v freeze >/dev/null; then
	echo "freeze not found: go install github.com/charmbracelet/freeze@latest" >&2
	exit 1
fi

ACV_SCREENSHOT=1 go test -run TestGenerateScreenshots

for frame in docs/*.ansi; do
	png="${frame%.ansi}.png"
	freeze \
		--language ansi \
		--theme charm \
		--font.size 14 \
		--line-height 1.2 \
		--padding 20 \
		--margin 0 \
		--border.radius 8 \
		--window \
		--output "$png" \
		"$frame"

	if command -v pngquant >/dev/null; then
		pngquant --force --skip-if-larger --quality 60-90 --output "$png" -- "$png" || true
	fi
	rm "$frame"
done

ls -l docs/*.png
