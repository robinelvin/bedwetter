#!/usr/bin/env bash
set -euo pipefail

DIR="$(cd "$(dirname "$0")/../web/static/icons" && pwd)"
TMPDIR=$(mktemp -d)
trap 'rm -rf "$TMPDIR"' EXIT

echo "Downloading @meteocons/svg-static..."

npm pack @meteocons/svg-static --pack-destination "$TMPDIR" --silent 2>/dev/null

TGZ=$(ls "$TMPDIR"/meteocons-svg-static-*.tgz 2>/dev/null || true)
if [ -z "$TGZ" ]; then
    echo "Error: failed to download package" >&2
    exit 1
fi

tar -xzf "$TGZ" -C "$TMPDIR"

# Copy only the icons we need
ICONS=(
    clear-day
    mostly-clear-day
    partly-cloudy-day
    overcast-day
    fog-day
    overcast-drizzle
    overcast-rain
    overcast-snow
    thunderstorms
    thunderstorms-hail
    not-available
)

for name in "${ICONS[@]}"; do
    src="$TMPDIR/package/fill/${name}.svg"
    if [ -f "$src" ]; then
        cp "$src" "$DIR/${name}.svg"
        echo "  ${name}.svg"
    else
        echo "  WARNING: ${name}.svg not found" >&2
    fi
done

echo "Done. Icons placed in $DIR"
