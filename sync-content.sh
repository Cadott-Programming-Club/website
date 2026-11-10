#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")"
SLIDES="$HOME/Library/CloudStorage/Dropbox/Cadott Programming Club/Club 2026-27/curriculum/slides"
GAMES="$HOME/projects/Cadott-Programming-Club"
OUT=content
for f in "$SLIDES"/[0-9][0-9]-*.html; do
  slug=$(basename "$f" .html)
  mkdir -p "$OUT/2026-27/$slug"
  cp "$f" "$OUT/2026-27/$slug/slides.html"
done
for slug in $(rg -o 'Slug: "([a-z0-9-]+)", Title' -r '$1' content.go); do
  mkdir -p "$OUT/games/$slug/play"
  [ -f "$GAMES/$slug/export_presets.cfg" ] || cp export_presets.cfg "$GAMES/$slug/export_presets.cfg"
  godot --headless --path "$GAMES/$slug" --export-release "Web" "$(pwd)/$OUT/games/$slug/play/index.html" >/dev/null 2>&1
  for f in "$OUT/games/$slug/play/"index.{wasm,js,pck}; do gzip -k -9 -f "$f"; done
done
echo "content synced"
