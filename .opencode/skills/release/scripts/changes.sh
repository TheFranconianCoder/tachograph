#!/usr/bin/env bash
# Print the commits since the last tag, for writing the release notes. Read-only.
set -euo pipefail

root="$(git rev-parse --show-toplevel 2>/dev/null)" || {
    printf 'release changes: not inside a git repository\n' >&2
    exit 1
}
cd "$root"

last_tag="$(git describe --tags --abbrev=0 2>/dev/null || true)"
if [ -n "$last_tag" ]; then
    range="$last_tag..HEAD"
    printf 'last_tag: %s\n' "$last_tag"
else
    range="HEAD"
    printf 'last_tag: none (first release)\n'
fi

printf 'range: %s\n' "$range"
printf 'commits: %s\n' "$(git rev-list --no-merges --count "$range")"

if [ -n "$last_tag" ]; then
    printf 'files_changed: %s\n' "$(git diff --name-only "$range" | wc -l | tr -d ' ')"
fi

printf '\n'
git log --no-merges --format='%h %s' "$range"
