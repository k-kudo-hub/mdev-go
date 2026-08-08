#!/bin/bash
# bump-version.sh - Calculate the next semantic version.
#
# Usage: bump-version.sh <major|minor|patch> <current-version>
#
#   <current-version> may include a leading "v" (e.g. v1.2.3 or 1.2.3).
#   An empty current version is treated as the 0.0.0 seed base, so the first
#   release bumps from there (e.g. `bump-version.sh minor ""` -> v0.1.0).
#
# Prints the next version WITH a leading "v" (e.g. v1.3.0) to stdout.
# Exits non-zero on an invalid bump type or a malformed current version.
#
# bash 3.2 compatible (no associative arrays / bash 4 syntax).

set -e

bump="$1"
current="$2"

case "$bump" in
    major|minor|patch) ;;
    *)
        echo "bump-version.sh: invalid bump type: '$bump' (expected major|minor|patch)" >&2
        exit 1
        ;;
esac

# Empty current version seeds from 0.0.0.
if [[ -z "$current" ]]; then
    current="0.0.0"
fi

# Strip an optional leading "v".
current="${current#v}"

if ! echo "$current" | grep -Eq '^[0-9]+\.[0-9]+\.[0-9]+$'; then
    echo "bump-version.sh: malformed version: '$2' (expected X.Y.Z)" >&2
    exit 1
fi

major="${current%%.*}"
rest="${current#*.}"
minor="${rest%%.*}"
patch="${rest#*.}"

case "$bump" in
    major)
        major=$((major + 1)); minor=0; patch=0
        ;;
    minor)
        minor=$((minor + 1)); patch=0
        ;;
    patch)
        patch=$((patch + 1))
        ;;
esac

echo "v${major}.${minor}.${patch}"
