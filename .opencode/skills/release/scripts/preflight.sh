#!/usr/bin/env bash
# Release preflight for tachograph. Read-only: it inspects and reports, it never changes anything.
# Usage: preflight.sh [X.Y.Z]
set -euo pipefail

version="${1:-}"

fail() {
    printf 'release preflight: %s\n' "$*" >&2
    exit 1
}

root="$(git rev-parse --show-toplevel 2>/dev/null)" || fail "not inside a git repository"
cd "$root"

# Go, Task and the GitHub CLI come from mise.toml; fall back to PATH when installed elsewhere.
MISE=()
if command -v mise >/dev/null 2>&1; then
    MISE=(mise x --)
else
    for tool in go task gh; do
        command -v "$tool" >/dev/null 2>&1 || fail "$tool not found on PATH (install mise or the tool)"
    done
fi

branch="$(git rev-parse --abbrev-ref HEAD)"
[ "$branch" = "main" ] || fail "on branch '$branch'; releases are cut from 'main'"

git rev-parse --abbrev-ref --symbolic-full-name '@{u}' >/dev/null 2>&1 ||
    fail "branch '$branch' has no upstream; push it first"
upstream="$(git rev-parse --abbrev-ref --symbolic-full-name '@{u}')"
ahead="$(git rev-list --count "$upstream..HEAD")"
[ "$ahead" -eq 0 ] || fail "$ahead commit(s) not pushed to $upstream; push first (the release tags a pushed commit)"

[ -z "$(git status --porcelain)" ] || fail "working tree is dirty; commit or stash first"

"${MISE[@]}" gh auth status >/dev/null 2>&1 || fail "gh is not authenticated; run 'gh auth login'"

last_tag="$(git describe --tags --abbrev=0 2>/dev/null || true)"

printf 'release preflight\n'
printf '  repo:      %s\n' "$root"
printf '  branch:    %s (upstream %s, up to date)\n' "$branch" "$upstream"
printf '  tree:      clean\n'
printf '  gh:        authenticated\n'
printf '  last tag:  %s\n' "${last_tag:-none}"

if [ -n "$version" ]; then
    [[ "$version" =~ ^[0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z.-]+)?$ ]] ||
        fail "version '$version' is not MAJOR.MINOR.PATCH"

    tag="v$version"

    if git rev-parse -q --verify "refs/tags/$tag" >/dev/null; then
        fail "tag $tag already exists locally"
    fi

    if ! repo="$("${MISE[@]}" gh repo view --json nameWithOwner --jq .nameWithOwner 2>&1)"; then
        printf '%s\n' "$repo" >&2
        fail "cannot resolve the repository from gh"
    fi
    # The remote tag check goes through the gh API instead of `git ls-remote`: the release already
    # publishes with gh over HTTPS, and a sandbox or a network that blocks the SSH transport must
    # not fail the preflight.
    if ! remote_refs="$("${MISE[@]}" gh api "repos/$repo/git/matching-refs/tags/$tag" --jq '.[].ref' 2>&1)"; then
        printf '%s\n' "$remote_refs" >&2
        fail "cannot read tags from origin"
    fi
    if printf '%s\n' "$remote_refs" | grep -qxF "refs/tags/$tag"; then
        fail "tag $tag already exists on origin"
    fi

    printf '  version:   %s (free)\n' "$tag"
fi
