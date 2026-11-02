#!/usr/bin/env bash
# Puts taxidermist release $1 (v0.9.0 or 0.9.0) on PATH for the job's later
# steps, from its GitHub release for the runner's OS and architecture, checked
# against the release's checksums. action.yml runs it.
set -euo pipefail
VERSION=${1:-}
version=${VERSION#v}
if [[ ! $version =~ ^[0-9]+\.[0-9]+\.[0-9]+ ]]; then
  echo "::error::taxidermist: '$VERSION' is not a release; call the action at a release tag or pass version"
  exit 1
fi
case "$RUNNER_OS" in
  Linux) os=linux ;;
  macOS) os=darwin ;;
  *) echo "::error::taxidermist: no release build for $RUNNER_OS"; exit 1 ;;
esac
case "$RUNNER_ARCH" in
  X64) arch=amd64 ;;
  ARM64) arch=arm64 ;;
  *) echo "::error::taxidermist: no release build for $RUNNER_ARCH"; exit 1 ;;
esac
base="https://github.com/jvanmelckebeke/taxidermist/releases/download/v${version}"
tarball="taxidermist_${version}_${os}_${arch}.tar.gz"
tmp=$(mktemp -d)
curl -fsSL -o "$tmp/checksums.txt" "$base/checksums.txt"
if ! grep -q " $tarball\$" "$tmp/checksums.txt"; then
  echo "::error::taxidermist: v$version ships no $tarball; it has $(awk '{print $2}' "$tmp/checksums.txt" | tr '\n' ' ')"
  exit 1
fi
curl -fsSL -o "$tmp/$tarball" "$base/$tarball"
if command -v sha256sum >/dev/null; then sum=(sha256sum); else sum=(shasum -a 256); fi
(cd "$tmp" && grep " $tarball\$" checksums.txt | "${sum[@]}" -c -)
bin="$RUNNER_TEMP/taxidermist-bin"
mkdir -p "$bin"
tar -xzf "$tmp/$tarball" -C "$bin" taxidermist
echo "$bin" >> "$GITHUB_PATH"

