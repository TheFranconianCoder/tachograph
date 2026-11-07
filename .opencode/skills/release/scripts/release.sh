#!/usr/bin/env bash
# Build and publish a tachograph release. Never commits: the tag points at the commit that is
# already on origin/main. Expects .release/title.txt and .release/notes.md (written by the release
# skill) and deletes them on success.
# Usage: release.sh X.Y.Z
set -euo pipefail

fail() {
    printf 'release: %s\n' "$*" >&2
    exit 1
}

version="${1:-}"
if [ -z "$version" ]; then
    printf 'usage: release.sh X.Y.Z\n' >&2
    exit 2
fi

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
root="$(git rev-parse --show-toplevel 2>/dev/null)" || fail "not inside a git repository"
cd "$root"

tag="v$version"
title_file="$root/.release/title.txt"
notes_file="$root/.release/notes.md"

[ -s "$title_file" ] || fail "missing or empty .release/title.txt (write it first, see SKILL.md step 3)"
[ -s "$notes_file" ] || fail "missing or empty .release/notes.md (write it first, see SKILL.md step 3)"

title="$(cat "$title_file")"
case "$title" in
*$'\n'*) fail ".release/title.txt must be a single line" ;;
esac

"$script_dir/preflight.sh" "$version"

MISE=()
if command -v mise >/dev/null 2>&1; then
    MISE=(mise x --)
fi

export CGO_ENABLED=0
commit="$(git rev-parse HEAD)"
short_commit="$(git rev-parse --short HEAD)"
built_at="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
ldflags="-s -w"
ldflags="$ldflags -X github.com/TheFranconianCoder/tachograph/internal/version.Version=$tag"
ldflags="$ldflags -X github.com/TheFranconianCoder/tachograph/internal/version.Commit=$short_commit"
ldflags="$ldflags -X github.com/TheFranconianCoder/tachograph/internal/version.Date=$built_at"

printf '==> web:build\n'
"${MISE[@]}" task web:build

printf '==> build\n'
mkdir -p dist
"${MISE[@]}" go build -trimpath -ldflags "$ldflags" -o dist/tachograph-linux-amd64 ./cmd/tachograph
GOOS=linux GOARCH=arm64 "${MISE[@]}" go build -trimpath -ldflags "$ldflags" \
    -o dist/tachograph-linux-arm64 ./cmd/tachograph
GOOS=darwin GOARCH=amd64 "${MISE[@]}" go build -trimpath -ldflags "$ldflags" \
    -o dist/tachograph-darwin-amd64 ./cmd/tachograph
GOOS=darwin GOARCH=arm64 "${MISE[@]}" go build -trimpath -ldflags "$ldflags" \
    -o dist/tachograph-darwin-arm64 ./cmd/tachograph
GOOS=windows GOARCH=amd64 "${MISE[@]}" go build -trimpath -ldflags "$ldflags" \
    -o dist/tachograph-windows-amd64.exe ./cmd/tachograph

if [ "$(uname -m)" = "x86_64" ]; then
    printf '==> verify version stamp\n'
    stamped="$(./dist/tachograph-linux-amd64 version)"
    [ "${stamped%% *}" = "$tag" ] || fail "binary reports '$stamped' instead of '$tag'"
fi

printf '==> checksums\n'
if command -v sha256sum >/dev/null 2>&1; then
    sha_cmd=(sha256sum)
elif command -v shasum >/dev/null 2>&1; then
    sha_cmd=(shasum -a 256)
else
    fail "neither sha256sum nor shasum found"
fi
(
    cd dist
    "${sha_cmd[@]}" \
        tachograph-linux-amd64 \
        tachograph-linux-arm64 \
        tachograph-darwin-amd64 \
        tachograph-darwin-arm64 \
        tachograph-windows-amd64.exe >SHA256SUMS
)

printf '==> gh release create %s\n' "$tag"
"${MISE[@]}" gh release create "$tag" \
    --target "$commit" \
    --title "$tag: $title" \
    --notes-file "$notes_file" \
    dist/tachograph-linux-amd64 \
    dist/tachograph-linux-arm64 \
    dist/tachograph-darwin-amd64 \
    dist/tachograph-darwin-arm64 \
    dist/tachograph-windows-amd64.exe \
    dist/SHA256SUMS

# Keep local tags in step with the GitHub release, so changes.sh and git describe
# work for the next release.
git tag "$tag" "$commit" 2>/dev/null || true

rm -rf "$root/.release"

url="$("${MISE[@]}" gh release view "$tag" --json url --jq .url)"
printf 'release %s published: %s\n' "$tag" "$url"
